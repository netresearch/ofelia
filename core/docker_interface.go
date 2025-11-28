package core

import (
	"context"
	"io"
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// DockerProvider defines the interface for Docker operations.
// The SDK adapter implements this interface.
type DockerProvider interface {
	// Container operations
	CreateContainer(ctx context.Context, config *domain.ContainerConfig, name string) (string, error)
	StartContainer(ctx context.Context, containerID string) error
	StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error
	RemoveContainer(ctx context.Context, containerID string, force bool) error
	InspectContainer(ctx context.Context, containerID string) (*domain.Container, error)
	ListContainers(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error)
	WaitContainer(ctx context.Context, containerID string) (int64, error)
	GetContainerLogs(ctx context.Context, containerID string, opts ContainerLogsOptions) (io.ReadCloser, error)

	// Exec operations
	CreateExec(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error)
	StartExec(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error)
	InspectExec(ctx context.Context, execID string) (*domain.ExecInspect, error)
	RunExec(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error)

	// Image operations
	PullImage(ctx context.Context, image string) error
	HasImageLocally(ctx context.Context, image string) (bool, error)
	EnsureImage(ctx context.Context, image string, forcePull bool) error

	// Network operations
	ConnectNetwork(ctx context.Context, networkID, containerID string) error
	FindNetworkByName(ctx context.Context, networkName string) ([]domain.Network, error)

	// Event operations
	SubscribeEvents(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error)

	// Service operations (Swarm)
	CreateService(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error)
	InspectService(ctx context.Context, serviceID string) (*domain.Service, error)
	ListTasks(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error)
	RemoveService(ctx context.Context, serviceID string) error
	WaitForServiceTasks(ctx context.Context, serviceID string, timeout time.Duration) ([]domain.Task, error)

	// System operations
	Info(ctx context.Context) (*domain.SystemInfo, error)
	Ping(ctx context.Context) error

	// Lifecycle
	Close() error
}

// ContainerLogsOptions defines options for container log retrieval.
type ContainerLogsOptions struct {
	ShowStdout bool
	ShowStderr bool
	Since      time.Time
	Tail       string
	Follow     bool
}
