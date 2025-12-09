package core

import (
	"context"
	"fmt"
	"io"

	"github.com/gobs/args"

	"github.com/netresearch/ofelia/core/domain"
)

type ExecJob struct {
	BareJob     `mapstructure:",squash"`
	Provider    DockerProvider `json:"-"` // SDK-based Docker provider
	Container   string         `hash:"true"`
	// User specifies the user to run the command as.
	// If not set, uses the global default-user setting (default: "nobody").
	// To use the container's default user, set "default-user =" (empty) in the [global] section,
	// or set "user =" (empty) for this specific job to inherit the global setting.
	User string `hash:"true"`
	TTY         bool   `default:"false" hash:"true"`
	Environment []string       `mapstructure:"environment" hash:"true"`
	WorkingDir  string         `mapstructure:"working-dir" hash:"true"`
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

func (j *ExecJob) Run(ctx *Context) error {
	// Use RunExec for a simpler, unified approach
	config := &domain.ExecConfig{
		Cmd:          args.GetArgs(j.Command),
		Env:          j.Environment,
		WorkingDir:   j.WorkingDir,
		User:         j.User,
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          j.TTY,
	}

	exitCode, err := j.Provider.RunExec(
		context.Background(),
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
	config := &domain.ExecConfig{
		Cmd:          args.GetArgs(j.Command),
		Env:          j.Environment,
		WorkingDir:   j.WorkingDir,
		User:         j.User,
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          j.TTY,
	}

	exitCode, err := j.Provider.RunExec(ctx, j.Container, config, stdout, stderr)
	if err != nil {
		return exitCode, fmt.Errorf("run exec: %w", err)
	}
	return exitCode, nil
}
