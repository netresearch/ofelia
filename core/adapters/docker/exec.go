package docker

import (
	"context"
	"io"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/netresearch/ofelia/core/domain"
)

// ExecServiceAdapter implements ports.ExecService using Docker SDK.
type ExecServiceAdapter struct {
	client *client.Client
}

// Create creates an exec instance.
func (s *ExecServiceAdapter) Create(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
	execConfig := containertypes.ExecOptions{
		User:         config.User,
		Privileged:   config.Privileged,
		Tty:          config.Tty,
		AttachStdin:  config.AttachStdin,
		AttachStdout: config.AttachStdout,
		AttachStderr: config.AttachStderr,
		Detach:       config.Detach,
		Cmd:          config.Cmd,
		Env:          config.Env,
		WorkingDir:   config.WorkingDir,
	}

	resp, err := s.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", convertError(err)
	}

	return resp.ID, nil
}

// Start starts an exec instance.
func (s *ExecServiceAdapter) Start(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	startConfig := containertypes.ExecStartOptions{
		Detach: opts.Detach,
		Tty:    opts.Tty,
	}

	resp, err := s.client.ContainerExecAttach(ctx, execID, startConfig)
	if err != nil {
		return nil, convertError(err)
	}

	return &domain.HijackedResponse{
		Conn:   resp.Conn,
		Reader: resp.Reader,
	}, nil
}

// Inspect returns exec information.
func (s *ExecServiceAdapter) Inspect(ctx context.Context, execID string) (*domain.ExecInspect, error) {
	resp, err := s.client.ContainerExecInspect(ctx, execID)
	if err != nil {
		return nil, convertError(err)
	}

	return &domain.ExecInspect{
		ID:          resp.ExecID,
		ContainerID: resp.ContainerID,
		Running:     resp.Running,
		ExitCode:    resp.ExitCode,
		Pid:         resp.Pid,
		// ProcessConfig is not available in official Docker SDK
		ProcessConfig: nil,
	}, nil
}

// Run executes a command in a container and waits for it to complete.
func (s *ExecServiceAdapter) Run(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
	// Create exec instance
	execID, err := s.Create(ctx, containerID, config)
	if err != nil {
		return -1, err
	}

	// Start exec and attach
	hijacked, err := s.Start(ctx, execID, domain.ExecStartOptions{
		Detach: false,
		Tty:    config.Tty,
	})
	if err != nil {
		return -1, err
	}
	defer hijacked.Close()

	// Copy output
	if config.Tty {
		// TTY mode: stdout and stderr are combined
		if stdout != nil {
			_, err = io.Copy(stdout, hijacked.Reader)
		}
	} else {
		// Non-TTY mode: demultiplex stdout and stderr
		_, err = stdcopy.StdCopy(stdout, stderr, hijacked.Reader)
	}
	if err != nil && err != io.EOF {
		return -1, err
	}

	// Get exit code
	inspect, err := s.Inspect(ctx, execID)
	if err != nil {
		return -1, err
	}

	return inspect.ExitCode, nil
}
