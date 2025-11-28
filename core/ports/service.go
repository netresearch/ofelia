package ports

import (
	"context"
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// SwarmService provides operations for managing Docker Swarm services.
type SwarmService interface {
	// Create creates a new Swarm service.
	// Returns the service ID on success.
	Create(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error)

	// Inspect returns detailed information about a service.
	Inspect(ctx context.Context, serviceID string) (*domain.Service, error)

	// List returns a list of services matching the options.
	List(ctx context.Context, opts domain.ServiceListOptions) ([]domain.Service, error)

	// Remove removes a service.
	Remove(ctx context.Context, serviceID string) error

	// ListTasks returns a list of tasks for services matching the options.
	ListTasks(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error)

	// WaitForTask waits for a task to reach a terminal state.
	// Returns the final task state or an error if the timeout is reached.
	WaitForTask(ctx context.Context, taskID string, timeout time.Duration) (*domain.Task, error)

	// WaitForServiceTasks waits for all tasks of a service to reach a terminal state.
	// This is useful for one-shot service jobs.
	WaitForServiceTasks(ctx context.Context, serviceID string, timeout time.Duration) ([]domain.Task, error)
}
