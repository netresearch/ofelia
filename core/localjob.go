package core

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/gobs/args"
)

type LocalJob struct {
	BareJob     `mapstructure:",squash"`
	Dir         string   `hash:"true"`
	Environment []string `mapstructure:"environment" hash:"true"`
}

func NewLocalJob() *LocalJob {
	return &LocalJob{}
}

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

	return &exec.Cmd{
		Path:   bin,
		Args:   cmdArgs,
		Stdout: ctx.Execution.OutputStream,
		Stderr: ctx.Execution.ErrorStream,
		// add custom env variables to the existing ones
		// instead of overwriting them
		Env: append(os.Environ(), j.Environment...),
		Dir: j.Dir,
	}, nil
}
