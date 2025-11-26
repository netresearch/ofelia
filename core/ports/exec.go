package ports

import (
	"context"
	"io"

	"github.com/netresearch/ofelia/core/domain"
)

// ExecService provides operations for executing commands in containers.
type ExecService interface {
	// Create creates an exec instance in a container.
	// Returns the exec ID on success.
	Create(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error)

	// Start starts an exec instance.
	// For attached exec, this returns a hijacked connection.
	Start(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error)

	// Inspect returns information about an exec instance.
	Inspect(ctx context.Context, execID string) (*domain.ExecInspect, error)

	// Run is a convenience method that creates, starts, and waits for an exec.
	// It copies stdout and stderr to the provided writers.
	// Returns the exit code of the command.
	Run(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error)
}
