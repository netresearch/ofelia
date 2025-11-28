// Package mock provides mock implementations of the ports interfaces for testing.
package mock

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// DockerClient is a mock implementation of ports.DockerClient.
type DockerClient struct {
	mu sync.RWMutex

	containers *ContainerService
	exec       *ExecService
	images     *ImageService
	events     *EventService
	services   *SwarmService
	networks   *NetworkService
	system     *SystemService

	closed   bool
	closeErr error
}

// NewDockerClient creates a new mock DockerClient.
func NewDockerClient() *DockerClient {
	return &DockerClient{
		containers: NewContainerService(),
		exec:       NewExecService(),
		images:     NewImageService(),
		events:     NewEventService(),
		services:   NewSwarmService(),
		networks:   NewNetworkService(),
		system:     NewSystemService(),
	}
}

// Containers returns the container service.
func (c *DockerClient) Containers() ports.ContainerService {
	return c.containers
}

// Exec returns the exec service.
func (c *DockerClient) Exec() ports.ExecService {
	return c.exec
}

// Images returns the image service.
func (c *DockerClient) Images() ports.ImageService {
	return c.images
}

// Events returns the event service.
func (c *DockerClient) Events() ports.EventService {
	return c.events
}

// Services returns the Swarm service.
func (c *DockerClient) Services() ports.SwarmService {
	return c.services
}

// Networks returns the network service.
func (c *DockerClient) Networks() ports.NetworkService {
	return c.networks
}

// System returns the system service.
func (c *DockerClient) System() ports.SystemService {
	return c.system
}

// Close closes the client.
func (c *DockerClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return c.closeErr
}

// SetCloseError sets the error returned by Close().
func (c *DockerClient) SetCloseError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeErr = err
}

// IsClosed returns true if the client has been closed.
func (c *DockerClient) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// ContainerService is a mock implementation of ports.ContainerService.
type ContainerService struct {
	mu sync.RWMutex

	// Callbacks for customizing behavior
	OnCreate  func(ctx context.Context, config *domain.ContainerConfig) (string, error)
	OnStart   func(ctx context.Context, containerID string) error
	OnStop    func(ctx context.Context, containerID string, timeout *time.Duration) error
	OnRemove  func(ctx context.Context, containerID string, opts domain.RemoveOptions) error
	OnInspect func(ctx context.Context, containerID string) (*domain.Container, error)
	OnList    func(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error)
	OnWait    func(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error)
	OnLogs    func(ctx context.Context, containerID string, opts domain.LogOptions) (io.ReadCloser, error)
	OnKill    func(ctx context.Context, containerID string, signal string) error

	// Call tracking
	CreateCalls  []CreateContainerCall
	StartCalls   []string
	StopCalls    []StopContainerCall
	RemoveCalls  []RemoveContainerCall
	InspectCalls []string
	ListCalls    []domain.ListOptions
	WaitCalls    []string
	LogsCalls    []LogsCall
	KillCalls    []KillCall
}

// CreateContainerCall represents a call to Create().
type CreateContainerCall struct {
	Config *domain.ContainerConfig
}

// StopContainerCall represents a call to Stop().
type StopContainerCall struct {
	ContainerID string
	Timeout     *time.Duration
}

// RemoveContainerCall represents a call to Remove().
type RemoveContainerCall struct {
	ContainerID string
	Options     domain.RemoveOptions
}

// LogsCall represents a call to Logs().
type LogsCall struct {
	ContainerID string
	Options     domain.LogOptions
}

// KillCall represents a call to Kill().
type KillCall struct {
	ContainerID string
	Signal      string
}

// NewContainerService creates a new mock ContainerService.
func NewContainerService() *ContainerService {
	return &ContainerService{}
}

// Create creates a container.
func (s *ContainerService) Create(ctx context.Context, config *domain.ContainerConfig) (string, error) {
	s.mu.Lock()
	s.CreateCalls = append(s.CreateCalls, CreateContainerCall{Config: config})
	s.mu.Unlock()

	if s.OnCreate != nil {
		return s.OnCreate(ctx, config)
	}
	return "mock-container-id", nil
}

// Start starts a container.
func (s *ContainerService) Start(ctx context.Context, containerID string) error {
	s.mu.Lock()
	s.StartCalls = append(s.StartCalls, containerID)
	s.mu.Unlock()

	if s.OnStart != nil {
		return s.OnStart(ctx, containerID)
	}
	return nil
}

// Stop stops a container.
func (s *ContainerService) Stop(ctx context.Context, containerID string, timeout *time.Duration) error {
	s.mu.Lock()
	s.StopCalls = append(s.StopCalls, StopContainerCall{ContainerID: containerID, Timeout: timeout})
	s.mu.Unlock()

	if s.OnStop != nil {
		return s.OnStop(ctx, containerID, timeout)
	}
	return nil
}

// Remove removes a container.
func (s *ContainerService) Remove(ctx context.Context, containerID string, opts domain.RemoveOptions) error {
	s.mu.Lock()
	s.RemoveCalls = append(s.RemoveCalls, RemoveContainerCall{ContainerID: containerID, Options: opts})
	s.mu.Unlock()

	if s.OnRemove != nil {
		return s.OnRemove(ctx, containerID, opts)
	}
	return nil
}

// Inspect returns container information.
func (s *ContainerService) Inspect(ctx context.Context, containerID string) (*domain.Container, error) {
	s.mu.Lock()
	s.InspectCalls = append(s.InspectCalls, containerID)
	s.mu.Unlock()

	if s.OnInspect != nil {
		return s.OnInspect(ctx, containerID)
	}
	return &domain.Container{
		ID:   containerID,
		Name: "mock-container",
		State: domain.ContainerState{
			Running:  false,
			ExitCode: 0,
		},
	}, nil
}

// List lists containers.
func (s *ContainerService) List(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error) {
	s.mu.Lock()
	s.ListCalls = append(s.ListCalls, opts)
	s.mu.Unlock()

	if s.OnList != nil {
		return s.OnList(ctx, opts)
	}
	return []domain.Container{}, nil
}

// Wait waits for a container to stop.
func (s *ContainerService) Wait(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
	s.mu.Lock()
	s.WaitCalls = append(s.WaitCalls, containerID)
	s.mu.Unlock()

	if s.OnWait != nil {
		return s.OnWait(ctx, containerID)
	}

	respCh := make(chan domain.WaitResponse, 1)
	errCh := make(chan error, 1)
	respCh <- domain.WaitResponse{StatusCode: 0}
	close(respCh)
	close(errCh)
	return respCh, errCh
}

// Logs returns container logs.
func (s *ContainerService) Logs(ctx context.Context, containerID string, opts domain.LogOptions) (io.ReadCloser, error) {
	s.mu.Lock()
	s.LogsCalls = append(s.LogsCalls, LogsCall{ContainerID: containerID, Options: opts})
	s.mu.Unlock()

	if s.OnLogs != nil {
		return s.OnLogs(ctx, containerID, opts)
	}
	return io.NopCloser(&emptyReader{}), nil
}

// CopyLogs copies container logs to writers.
func (s *ContainerService) CopyLogs(ctx context.Context, containerID string, stdout, stderr io.Writer, opts domain.LogOptions) error {
	logs, err := s.Logs(ctx, containerID, opts)
	if err != nil {
		return err
	}
	defer logs.Close()

	if stdout != nil {
		if _, err = io.Copy(stdout, logs); err != nil {
			return fmt.Errorf("copying container logs: %w", err)
		}
	}
	return nil
}

// Kill sends a signal to a container.
func (s *ContainerService) Kill(ctx context.Context, containerID string, signal string) error {
	s.mu.Lock()
	s.KillCalls = append(s.KillCalls, KillCall{ContainerID: containerID, Signal: signal})
	s.mu.Unlock()

	if s.OnKill != nil {
		return s.OnKill(ctx, containerID, signal)
	}
	return nil
}

// Pause pauses a container.
func (s *ContainerService) Pause(ctx context.Context, containerID string) error {
	return nil
}

// Unpause unpauses a container.
func (s *ContainerService) Unpause(ctx context.Context, containerID string) error {
	return nil
}

// Rename renames a container.
func (s *ContainerService) Rename(ctx context.Context, containerID string, newName string) error {
	return nil
}

// Attach attaches to a container.
func (s *ContainerService) Attach(ctx context.Context, containerID string, opts ports.AttachOptions) (*domain.HijackedResponse, error) {
	return &domain.HijackedResponse{}, nil
}

// emptyReader is an io.Reader that always returns EOF.
type emptyReader struct{}

func (r *emptyReader) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}
