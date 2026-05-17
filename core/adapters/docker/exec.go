// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"errors"
	"fmt"
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

// Sentinel errors for input validation. Defined as static values so callers
// can use errors.Is for branching and to satisfy err113 lint guidance.
var (
	// ErrNilExecConfig is returned when an ExecConfig pointer is nil.
	ErrNilExecConfig = errors.New("exec: nil ExecConfig")
	// ErrNoExecOutputWriter is returned when Run is invoked in non-TTY mode
	// with both stdout and stderr nil. stdcopy.StdCopy panics in that case
	// the moment it has output to dispatch.
	ErrNoExecOutputWriter = errors.New("exec: non-TTY mode requires at least one of stdout or stderr")
)

// checkClient returns ErrNilDockerClient if the embedded SDK client is nil.
// See docker.ErrNilDockerClient for rationale.
func (s *ExecServiceAdapter) checkClient() error {
	if s.client == nil {
		return ErrNilDockerClient
	}
	return nil
}

// Create creates an exec instance.
func (s *ExecServiceAdapter) Create(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
	if err := s.checkClient(); err != nil {
		return "", err
	}
	if config == nil {
		return "", ErrNilExecConfig
	}

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
		ConsoleSize:  config.ConsoleSize,
	}

	resp, err := s.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", convertError(err)
	}

	return resp.ID, nil
}

// Start starts an exec instance.
func (s *ExecServiceAdapter) Start(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
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
	if err := s.checkClient(); err != nil {
		return nil, err
	}
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
func (s *ExecServiceAdapter) Run(
	ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer,
) (int, error) {
	if err := s.checkClient(); err != nil {
		return -1, err
	}
	if config == nil {
		return -1, ErrNilExecConfig
	}
	// Non-TTY mode demultiplexes via stdcopy.StdCopy, which panics if both
	// writers are nil and there is any output to dispatch. Guard the input
	// to keep the contract symmetric with TTY mode (which already tolerates
	// a nil stdout by skipping the copy).
	if !config.Tty && stdout == nil && stderr == nil {
		return -1, ErrNoExecOutputWriter
	}

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
	if err != nil && !errors.Is(err, io.EOF) {
		return -1, fmt.Errorf("copying exec output: %w", err)
	}

	// Get exit code
	inspect, err := s.Inspect(ctx, execID)
	if err != nil {
		return -1, err
	}

	return inspect.ExitCode, nil
}
