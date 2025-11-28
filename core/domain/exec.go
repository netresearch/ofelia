package domain

import (
	"fmt"
	"io"
)

// ExecConfig represents the configuration for creating an exec instance.
type ExecConfig struct {
	// Command to run
	Cmd []string

	// Environment variables
	Env []string

	// Working directory
	WorkingDir string

	// User to run the command as
	User string

	// Attach streams
	AttachStdin  bool
	AttachStdout bool
	AttachStderr bool

	// Allocate pseudo-TTY
	Tty bool

	// Detach from the exec after starting
	Detach bool

	// Privileged mode
	Privileged bool
}

// ExecInspect represents the result of inspecting an exec instance.
type ExecInspect struct {
	ID            string
	ContainerID   string
	Running       bool
	ExitCode      int
	Pid           int
	ProcessConfig *ExecProcessConfig
}

// ExecProcessConfig represents the process configuration for an exec instance.
type ExecProcessConfig struct {
	User       string
	Privileged bool
	Tty        bool
	Entrypoint string
	Arguments  []string
}

// ExecStartOptions represents options for starting an exec instance.
type ExecStartOptions struct {
	Detach bool
	Tty    bool

	// Stdin to attach (optional)
	Stdin io.Reader

	// Output streams
	OutputStream io.Writer
	ErrorStream  io.Writer
}

// HijackedResponse represents a hijacked connection for exec.
type HijackedResponse struct {
	Conn   io.Closer
	Reader io.Reader
}

// Close closes the hijacked connection.
func (h *HijackedResponse) Close() error {
	if h.Conn != nil {
		if err := h.Conn.Close(); err != nil {
			return fmt.Errorf("closing hijacked connection: %w", err)
		}
	}
	return nil
}
