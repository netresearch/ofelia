// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
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

// Run looks up the binary on PATH, resolves the environment and executes the
// command in Dir, blocking until the process exits. Returns ErrEmptyCommand
// for an empty command, a lookup error when the binary is not on PATH, the
// env-resolution error, or the process's exit error. Output is streamed to
// the execution's stdout/stderr streams. The child process is not tied to the
// run context: canceling the run, or a max-runtime deadline, does not kill
// it.
func (j *LocalJob) Run(ctx *Context) error {
	cmd, err := j.buildCommand(ctx)
	if err != nil {
		return err
	}

	if err := cmd.Run(); err != nil {
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
