package core

import (
	"fmt"
	"os/exec"

	"github.com/gobs/args"

	"github.com/netresearch/ofelia/config"
)

type ComposeJob struct {
	BareJob `mapstructure:",squash"`
	File    string `default:"compose.yml" gcfg:"file" mapstructure:"file" hash:"true"`
	Service string `gcfg:"service" mapstructure:"service" hash:"true"`
	Exec    bool   `default:"false" gcfg:"exec" mapstructure:"exec" hash:"true"`
}

func NewComposeJob() *ComposeJob { return &ComposeJob{} }

func (j *ComposeJob) Run(ctx *Context) error {
	cmd, err := j.buildCommand(ctx)
	if err != nil {
		return fmt.Errorf("compose build command: %w", err)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compose run: %w", err)
	}
	return nil
}

func (j *ComposeJob) buildCommand(ctx *Context) (*exec.Cmd, error) {
	// Validate inputs to prevent command injection
	validator := config.NewCommandValidator()

	// Validate file path
	if err := validator.ValidateFilePath(j.File); err != nil {
		return nil, fmt.Errorf("invalid compose file path: %w", err)
	}

	// Validate service name
	if err := validator.ValidateServiceName(j.Service); err != nil {
		return nil, fmt.Errorf("invalid service name: %w", err)
	}

	// Build docker compose command
	var cmdArgs []string
	cmdArgs = append(cmdArgs, "docker", "compose", "-f", j.File)

	if j.Exec {
		cmdArgs = append(cmdArgs, "exec", j.Service)
	} else {
		cmdArgs = append(cmdArgs, "run", "--rm", j.Service)
	}

	// Add command arguments if present
	if j.Command != "" {
		commandArgs := args.GetArgs(j.Command)
		if err := validator.ValidateCommandArgs(commandArgs); err != nil {
			return nil, fmt.Errorf("invalid command arguments: %w", err)
		}
		cmdArgs = append(cmdArgs, commandArgs...)
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
	}, nil
}
