package core

import (
	"fmt"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/gobs/args"
)

type ExecJob struct {
	BareJob     `mapstructure:",squash"`
	Client      *docker.Client `json:"-"`
	Container   string         `hash:"true"`
	User        string         `default:"root" hash:"true"`
	TTY         bool           `default:"false" hash:"true"`
	Environment []string       `mapstructure:"environment" hash:"true"`

	dockerOps *DockerOperations `json:"-"` // High-level Docker operations wrapper
	execID    string
}

func NewExecJob(c *docker.Client) *ExecJob {
	// Initialize Docker operations wrapper with basic logger
	// Metrics will be set later when the job runs in a context
	dockerOps := NewDockerOperations(c, &SimpleLogger{}, nil)

	return &ExecJob{
		Client:    c,
		dockerOps: dockerOps,
	}
}

func (j *ExecJob) Run(ctx *Context) error {
	exec, err := j.buildExec(ctx)
	if err != nil {
		return err
	}

	if exec != nil {
		j.execID = exec.ID
	}

	if err := j.startExec(ctx.Execution); err != nil {
		return err
	}

	inspect, err := j.inspectExec()
	if err != nil {
		return err
	}

	switch inspect.ExitCode {
	case 0:
		return nil
	case -1:
		return ErrUnexpected
	default:
		return NonZeroExitError{ExitCode: inspect.ExitCode}
	}
}

func (j *ExecJob) buildExec(ctx *Context) (*docker.Exec, error) {
	// Update DockerOperations context
	j.dockerOps.logger = ctx.Logger
	if ctx.Scheduler != nil && ctx.Scheduler.metricsRecorder != nil {
		j.dockerOps.metricsRecorder = ctx.Scheduler.metricsRecorder
	}

	execOps := j.dockerOps.NewExecOperations()
	
	exec, err := execOps.CreateExec(docker.CreateExecOptions{
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          j.TTY,
		Cmd:          args.GetArgs(j.Command),
		Container:    j.Container,
		User:         j.User,
		Env:          j.Environment,
	})
	if err != nil {
		return nil, fmt.Errorf("create exec: %w", err)
	}

	return exec, nil
}

func (j *ExecJob) startExec(e *Execution) error {
	execOps := j.dockerOps.NewExecOperations()
	
	err := execOps.StartExec(j.execID, docker.StartExecOptions{
		Tty:          j.TTY,
		OutputStream: e.OutputStream,
		ErrorStream:  e.ErrorStream,
		RawTerminal:  j.TTY,
	})
	if err != nil {
		return fmt.Errorf("start exec: %w", err)
	}

	return nil
}

func (j *ExecJob) inspectExec() (*docker.ExecInspect, error) {
	execOps := j.dockerOps.NewExecOperations()
	
	inspect, err := execOps.InspectExec(j.execID)
	if err != nil {
		return nil, fmt.Errorf("inspect exec: %w", err)
	}

	return inspect, nil
}

