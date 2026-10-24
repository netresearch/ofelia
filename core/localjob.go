// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/gobs/args"
)

// localJobEnvResolveTimeout bounds the env-resolution step that runs
// before a `job-local` command is invoked. LocalJob passes a nil
// DockerProvider into ResolveJobEnvironment, so this timeout does NOT
// guard against a wedged Docker daemon — `env-from` on local jobs is a
// no-op. The bound exists strictly to cap env-FILE parsing on local
// disk (`env-file: /path/to/foo`) so a hung NFS mount or a pathologically
// large file cannot stall local-job startup. 10s is a generous upper
// bound for a disk read. See issues #638 / #655.
const localJobEnvResolveTimeout = 10 * time.Second

// LocalJob runs a command directly on the host Ofelia runs on, outside any
// container. The child inherits Ofelia's own environment, extended by
// EnvFile and Environment; EnvFrom is accepted but inert on this path
// because no Docker provider is passed to the resolver (see
// localJobEnvResolveTimeout).
type LocalJob struct {
	BareJob     `mapstructure:",squash"`
	Dir         string   `hash:"true"`
	Environment []string `mapstructure:"environment" hash:"true"`
	EnvFile     []string `gcfg:"env-file" mapstructure:"env-file," hash:"true"`
	EnvFrom     []string `gcfg:"env-from" mapstructure:"env-from," hash:"true"`
}

// NewLocalJob returns an empty LocalJob; the caller fills in the embedded
// BareJob, Dir and the environment fields.
func NewLocalJob() *LocalJob {
	return &LocalJob{}
}

// localJobStopGrace is the time between SIGTERM and SIGKILL when a canceled
// local job is terminated. localJobWaitDelay bounds how long Run keeps
// collecting output after the process exited, when a process it started still
// holds the output pipes. Both are vars so tests can shorten them.
var (
	localJobStopGrace = 10 * time.Second
	localJobWaitDelay = 10 * time.Second
)

// ErrLocalTerminationUnconfirmed reports that a canceled local job may still
// be running: its process (group) was still present after SIGKILL, typically
// because a process changed credentials and Ofelia may not signal it.
var ErrLocalTerminationUnconfirmed = errors.New("local job termination unconfirmed")

// Run looks up the binary on PATH, resolves the environment and executes the
// command in Dir, blocking until the process exits. Returns ErrEmptyCommand
// for an empty command, a lookup error when the binary is not on PATH, the
// env-resolution error, or the process's exit error. Output is streamed to
// the execution's stdout/stderr streams.
//
// The process is bound to the run context. When the run is canceled — on
// scheduler shutdown, job removal or the execution time limit — Run sends
// SIGTERM to the process group the command runs in, sends SIGKILL to whatever
// is left after localJobStopGrace, and returns ErrMaxTimeRunning. On Windows,
// which has no process groups or SIGTERM, only the command's own process is
// killed, at once.
func (j *LocalJob) Run(ctx *Context) error {
	cmd, err := j.buildCommand(ctx)
	if err != nil {
		return err
	}
	setLocalProcessGroup(cmd)
	cmd.WaitDelay = localJobWaitDelay

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("local run: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	runCtx := ctx.RunContext()
	select {
	case err := <-done:
		return localExitError(ctx, err)
	case <-runCtx.Done():
	}

	// The process may have exited in the same instant the context fired; a
	// finished run keeps its own result.
	select {
	case err := <-done:
		return localExitError(ctx, err)
	default:
	}

	if exitErr := terminateLocalProcess(cmd, done); exitErr != nil {
		if errors.Is(exitErr, ErrLocalTerminationUnconfirmed) {
			return fmt.Errorf("%w: %w", ErrMaxTimeRunning, exitErr)
		}
		ctx.Log("local process exited after cancellation: " + exitErr.Error())
	}
	return ErrMaxTimeRunning
}

// localExitError classifies the process exit. exec.ErrWaitDelay means the
// process itself succeeded and only output still arriving after
// localJobWaitDelay was dropped, so the run counts as successful with a
// warning.
func localExitError(ctx *Context, err error) error {
	if errors.Is(err, exec.ErrWaitDelay) {
		ctx.Warn("local run: output after the process exited was not collected: " + err.Error())
		return nil
	}
	if err != nil {
		return fmt.Errorf("local run: %w", err)
	}
	return nil
}

func (j *LocalJob) buildCommand(ctx *Context) (*exec.Cmd, error) {
	// Parse command arguments and ensure non-empty
	// Note: Config file commands are validated during load by config.Validator2
	// API-created jobs are validated in web/server.go before reaching here
	cmdArgs := args.GetArgs(j.Command)
	if len(cmdArgs) == 0 {
		return nil, ErrEmptyCommand
	}

	bin, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return nil, fmt.Errorf("look path %q: %w", cmdArgs[0], err)
	}

	// Resolve environment from env-file, env-from, and explicit environment.
	// Bound the resolver context with localJobEnvResolveTimeout to cap
	// env-FILE disk reads (LocalJob passes a nil provider so env-from is
	// a no-op — Docker is never contacted on this path). Inherits
	// cancellation from the scheduler's per-run bounded context when
	// present; otherwise (*Context).RunContext returns context.Background()
	// so legacy *Context{} literals keep working. See issues #638 / #655.
	resolveCtx, cancel := context.WithTimeout(ctx.RunContext(), localJobEnvResolveTimeout)
	defer cancel()
	mergedEnv, err := ResolveJobEnvironment(resolveCtx, j.EnvFile, j.EnvFrom, j.Environment, nil, func(msg string) {
		ctx.Warn(msg)
	})
	if err != nil {
		return nil, err
	}

	return &exec.Cmd{
		Path:   bin,
		Args:   cmdArgs,
		Stdout: ctx.Execution.OutputStream,
		Stderr: ctx.Execution.ErrorStream,
		// add custom env variables to the existing ones
		// instead of overwriting them
		Env: append(os.Environ(), mergedEnv...),
		Dir: j.Dir,
	}, nil
}
