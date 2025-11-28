package ports

import (
	"context"
	"io"
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// ContainerService provides operations for managing Docker containers.
type ContainerService interface {
	// Create creates a new container.
	// Returns the container ID on success.
	Create(ctx context.Context, config *domain.ContainerConfig) (string, error)

	// Start starts a stopped container.
	Start(ctx context.Context, containerID string) error

	// Stop stops a running container.
	// The timeout parameter specifies how long to wait before forcefully killing.
	// If timeout is nil, the default timeout is used.
	Stop(ctx context.Context, containerID string, timeout *time.Duration) error

	// Remove removes a container.
	Remove(ctx context.Context, containerID string, opts domain.RemoveOptions) error

	// Inspect returns detailed information about a container.
	Inspect(ctx context.Context, containerID string) (*domain.Container, error)

	// List returns a list of containers matching the options.
	List(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error)

	// Wait blocks until a container stops and returns its exit status.
	// Returns two channels: one for the wait response, one for errors.
	// The context can be used to cancel the wait operation.
	Wait(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error)

	// Logs returns the logs from a container.
	// The returned ReadCloser must be closed by the caller.
	Logs(ctx context.Context, containerID string, opts domain.LogOptions) (io.ReadCloser, error)

	// CopyLogs copies container logs to the provided writers.
	// This is a convenience method that handles stdout/stderr demultiplexing.
	CopyLogs(ctx context.Context, containerID string, stdout, stderr io.Writer, opts domain.LogOptions) error

	// Kill sends a signal to a container.
	Kill(ctx context.Context, containerID string, signal string) error

	// Pause pauses a container.
	Pause(ctx context.Context, containerID string) error

	// Unpause unpauses a paused container.
	Unpause(ctx context.Context, containerID string) error

	// Rename renames a container.
	Rename(ctx context.Context, containerID string, newName string) error

	// Attach attaches to a container.
	Attach(ctx context.Context, containerID string, opts AttachOptions) (*domain.HijackedResponse, error)
}

// AttachOptions represents options for attaching to a container.
type AttachOptions struct {
	Stream     bool
	Stdin      bool
	Stdout     bool
	Stderr     bool
	DetachKeys string
	Logs       bool
}
