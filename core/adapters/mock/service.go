package mock

import (
	"context"
	"sync"
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// SwarmService is a mock implementation of ports.SwarmService.
type SwarmService struct {
	mu sync.RWMutex

	// Callbacks for customizing behavior
	OnCreate    func(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error)
	OnInspect   func(ctx context.Context, serviceID string) (*domain.Service, error)
	OnList      func(ctx context.Context, opts domain.ServiceListOptions) ([]domain.Service, error)
	OnRemove    func(ctx context.Context, serviceID string) error
	OnListTasks func(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error)

	// Call tracking
	CreateCalls    []ServiceCreateCall
	InspectCalls   []string
	ListCalls      []domain.ServiceListOptions
	RemoveCalls    []string
	ListTasksCalls []domain.TaskListOptions

	// Simulated data
	Services []domain.Service
	Tasks    []domain.Task
}

// ServiceCreateCall represents a call to Create().
type ServiceCreateCall struct {
	Spec    domain.ServiceSpec
	Options domain.ServiceCreateOptions
}

// NewSwarmService creates a new mock SwarmService.
func NewSwarmService() *SwarmService {
	return &SwarmService{}
}

// Create creates a service.
func (s *SwarmService) Create(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
	s.mu.Lock()
	s.CreateCalls = append(s.CreateCalls, ServiceCreateCall{Spec: spec, Options: opts})
	s.mu.Unlock()

	if s.OnCreate != nil {
		return s.OnCreate(ctx, spec, opts)
	}
	return "mock-service-id", nil
}

// Inspect returns service information.
func (s *SwarmService) Inspect(ctx context.Context, serviceID string) (*domain.Service, error) {
	s.mu.Lock()
	s.InspectCalls = append(s.InspectCalls, serviceID)
	services := s.Services
	s.mu.Unlock()

	if s.OnInspect != nil {
		return s.OnInspect(ctx, serviceID)
	}

	// Find service by ID
	for i := range services {
		if services[i].ID == serviceID {
			return &services[i], nil
		}
	}

	return &domain.Service{
		ID: serviceID,
		Spec: domain.ServiceSpec{
			Name: "mock-service",
		},
	}, nil
}

// List lists services.
func (s *SwarmService) List(ctx context.Context, opts domain.ServiceListOptions) ([]domain.Service, error) {
	s.mu.Lock()
	s.ListCalls = append(s.ListCalls, opts)
	services := s.Services
	s.mu.Unlock()

	if s.OnList != nil {
		return s.OnList(ctx, opts)
	}
	return services, nil
}

// Remove removes a service.
func (s *SwarmService) Remove(ctx context.Context, serviceID string) error {
	s.mu.Lock()
	s.RemoveCalls = append(s.RemoveCalls, serviceID)
	s.mu.Unlock()

	if s.OnRemove != nil {
		return s.OnRemove(ctx, serviceID)
	}
	return nil
}

// ListTasks lists tasks.
func (s *SwarmService) ListTasks(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
	s.mu.Lock()
	s.ListTasksCalls = append(s.ListTasksCalls, opts)
	tasks := s.Tasks
	s.mu.Unlock()

	if s.OnListTasks != nil {
		return s.OnListTasks(ctx, opts)
	}
	return tasks, nil
}

// WaitForTask waits for a task to reach a terminal state.
func (s *SwarmService) WaitForTask(ctx context.Context, taskID string, timeout time.Duration) (*domain.Task, error) {
	// For mock, immediately return a completed task
	return &domain.Task{
		ID: taskID,
		Status: domain.TaskStatus{
			State: domain.TaskStateComplete,
		},
	}, nil
}

// WaitForServiceTasks waits for all service tasks to complete.
func (s *SwarmService) WaitForServiceTasks(ctx context.Context, serviceID string, timeout time.Duration) ([]domain.Task, error) {
	tasks, err := s.ListTasks(ctx, domain.TaskListOptions{
		Filters: map[string][]string{
			"service": {serviceID},
		},
	})
	if err != nil {
		return nil, err
	}

	// Mark all tasks as complete for mock
	for i := range tasks {
		tasks[i].Status.State = domain.TaskStateComplete
	}
	return tasks, nil
}

// SetServices sets the services returned by List() and Inspect().
func (s *SwarmService) SetServices(services []domain.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Services = services
}

// SetTasks sets the tasks returned by ListTasks().
func (s *SwarmService) SetTasks(tasks []domain.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tasks = tasks
}

// AddCompletedTask adds a completed task for a service.
func (s *SwarmService) AddCompletedTask(serviceID, containerID string, exitCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tasks = append(s.Tasks, domain.Task{
		ID:        "mock-task-id",
		ServiceID: serviceID,
		Status: domain.TaskStatus{
			State: domain.TaskStateComplete,
			ContainerStatus: &domain.ContainerStatus{
				ContainerID: containerID,
				ExitCode:    exitCode,
			},
		},
	})
}
