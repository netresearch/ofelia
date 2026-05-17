// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"fmt"
	"io"

	"github.com/gobs/args"

	"github.com/netresearch/ofelia/core/domain"
)

type ExecJob struct {
	BareJob   `mapstructure:",squash"`
	Provider  DockerProvider `json:"-"` // SDK-based Docker provider
	Container string         `hash:"true"`
	// User specifies the user to run the command as.
	// If not set, uses the global default-user setting (default: "nobody").
	// Set to "default" to explicitly use the container's default user, overriding global setting.
	User        string   `hash:"true"`
	TTY         bool     `default:"false" hash:"true"`
	Environment []string `mapstructure:"environment" hash:"true"`
	EnvFile     []string `gcfg:"env-file" mapstructure:"env-file," hash:"true"`
	EnvFrom     []string `gcfg:"env-from" mapstructure:"env-from," hash:"true"`
	WorkingDir  string   `mapstructure:"working-dir" hash:"true"`
	Privileged  bool     `default:"false" hash:"true"`

	// ConsoleHeight / ConsoleWidth set the initial pseudo-TTY console
	// size (rows × columns) sent to the Docker daemon at exec creation
	// — useful for jobs that render TUIs / tables and need a consistent
	// terminal geometry. Both default to 0, meaning "use Docker's
	// default size"; setting either populates the daemon's ConsoleSize
	// parameter. Only honored when TTY is true; otherwise silently
	// ignored by the daemon. Requires Docker API v1.42+ (Docker Engine
	// 20.10+, released 2020-12).
	// See https://github.com/netresearch/ofelia/issues/235.
	ConsoleHeight uint `gcfg:"console-height" mapstructure:"console-height" hash:"true"`
	ConsoleWidth  uint `gcfg:"console-width" mapstructure:"console-width" hash:"true"`
}

func NewExecJob(provider DockerProvider) *ExecJob {
	return &ExecJob{
		Provider: provider,
	}
}

// InitializeRuntimeFields initializes fields that depend on the Docker provider.
// This should be called after the Provider field is set.
func (j *ExecJob) InitializeRuntimeFields() {
	// No additional initialization needed with DockerProvider
}

// consoleSize returns the [height, width] pair the Docker daemon
// expects for ContainerExecCreate.ConsoleSize, or nil when neither
// dimension was configured. nil means "use Docker's default size",
// matching the daemon contract documented at #235.
//
// A partial value like {40, 0} is intentionally forwarded — the daemon
// honors the 40 and uses its default for the zero dimension. Only the
// "both zero" case collapses to nil (the unconfigured-by-operator case).
func (j *ExecJob) consoleSize() *[2]uint {
	if j.ConsoleHeight == 0 && j.ConsoleWidth == 0 {
		return nil
	}
	return &[2]uint{j.ConsoleHeight, j.ConsoleWidth}
}

// Run executes the configured command inside the target container via
// `docker exec`.
//
// Limitation (issue #655): when the wrapper-level deadline from
// #651's boundJobContext fires (or any other ctx cancellation occurs),
// the SDK read returns promptly but the in-container process is left
// running. The Docker Engine API exposes no `ExecStop` primitive — once
// an exec session is started Ofelia cannot kill the inner process from
// the outside. Operators relying on a hard ceiling for `job-exec` MUST
// enforce it inside the entrypoint (e.g. `timeout 30s ...`) rather than
// via `max-runtime` alone.
func (j *ExecJob) Run(ctx *Context) error {
	// Use the (deadline-bounded) middleware-chain context for cancellation
	// propagation. The fallback to context.Background() is centralized in
	// (*Context).RunContext so a nil ctx.Ctx (legacy literal in older
	// tests) cannot panic. See issue #638.
	runCtx := ctx.RunContext()

	// Resolve environment from env-file, env-from, and explicit environment
	mergedEnv, err := ResolveJobEnvironment(runCtx, j.EnvFile, j.EnvFrom, j.Environment, j.Provider, nil)
	if err != nil {
		return err
	}

	// Use RunExec for a simpler, unified approach
	config := &domain.ExecConfig{
		Cmd:          args.GetArgs(j.Command),
		Env:          mergedEnv,
		WorkingDir:   j.WorkingDir,
		User:         j.User,
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          j.TTY,
		Privileged:   j.Privileged,
		ConsoleSize:  j.consoleSize(),
	}

	exitCode, err := j.Provider.RunExec(
		runCtx,
		j.Container,
		config,
		ctx.Execution.OutputStream,
		ctx.Execution.ErrorStream,
	)
	if err != nil {
		return fmt.Errorf("exec run: %w", err)
	}

	switch exitCode {
	case 0:
		return nil
	case -1:
		return ErrUnexpected
	default:
		return NonZeroExitError{ExitCode: exitCode}
	}
}

// RunWithStreams runs the exec job with custom output streams.
// This is useful for testing or when custom stream handling is needed.
func (j *ExecJob) RunWithStreams(ctx context.Context, stdout, stderr io.Writer) (int, error) {
	mergedEnv, err := ResolveJobEnvironment(ctx, j.EnvFile, j.EnvFrom, j.Environment, j.Provider, nil)
	if err != nil {
		return 0, err
	}

	config := &domain.ExecConfig{
		Cmd:          args.GetArgs(j.Command),
		Env:          mergedEnv,
		WorkingDir:   j.WorkingDir,
		User:         j.User,
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          j.TTY,
		Privileged:   j.Privileged,
		ConsoleSize:  j.consoleSize(),
	}

	exitCode, err := j.Provider.RunExec(ctx, j.Container, config, stdout, stderr)
	if err != nil {
		return exitCode, fmt.Errorf("run exec: %w", err)
	}
	return exitCode, nil
}
