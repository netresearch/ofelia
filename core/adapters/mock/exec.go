package mock

import (
	"context"
	"io"
	"sync"

	"github.com/netresearch/ofelia/core/domain"
)

// ExecService is a mock implementation of ports.ExecService.
type ExecService struct {
	mu sync.RWMutex

	// Callbacks for customizing behavior
	OnCreate  func(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error)
	OnStart   func(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error)
	OnInspect func(ctx context.Context, execID string) (*domain.ExecInspect, error)
	OnRun     func(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error)

	// Call tracking
	CreateCalls  []ExecCreateCall
	StartCalls   []ExecStartCall
	InspectCalls []string
	RunCalls     []ExecRunCall

	// Simulated output
	Output string
}

// ExecCreateCall represents a call to Create().
type ExecCreateCall struct {
	ContainerID string
	Config      *domain.ExecConfig
}

// ExecStartCall represents a call to Start().
type ExecStartCall struct {
	ExecID  string
	Options domain.ExecStartOptions
}

// ExecRunCall represents a call to Run().
type ExecRunCall struct {
	ContainerID string
	Config      *domain.ExecConfig
}

// NewExecService creates a new mock ExecService.
func NewExecService() *ExecService {
	return &ExecService{}
}

// Create creates an exec instance.
func (s *ExecService) Create(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
	s.mu.Lock()
	s.CreateCalls = append(s.CreateCalls, ExecCreateCall{ContainerID: containerID, Config: config})
	s.mu.Unlock()

	if s.OnCreate != nil {
		return s.OnCreate(ctx, containerID, config)
	}
	return "mock-exec-id", nil
}

// Start starts an exec instance.
func (s *ExecService) Start(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	s.mu.Lock()
	s.StartCalls = append(s.StartCalls, ExecStartCall{ExecID: execID, Options: opts})
	s.mu.Unlock()

	if s.OnStart != nil {
		return s.OnStart(ctx, execID, opts)
	}

	// Write simulated output
	if opts.OutputStream != nil && s.Output != "" {
		_, _ = opts.OutputStream.Write([]byte(s.Output))
	}

	return &domain.HijackedResponse{}, nil
}

// Inspect returns exec information.
func (s *ExecService) Inspect(ctx context.Context, execID string) (*domain.ExecInspect, error) {
	s.mu.Lock()
	s.InspectCalls = append(s.InspectCalls, execID)
	s.mu.Unlock()

	if s.OnInspect != nil {
		return s.OnInspect(ctx, execID)
	}
	return &domain.ExecInspect{
		ID:       execID,
		Running:  false,
		ExitCode: 0,
	}, nil
}

// Run runs a command in a container.
func (s *ExecService) Run(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
	s.mu.Lock()
	s.RunCalls = append(s.RunCalls, ExecRunCall{ContainerID: containerID, Config: config})
	s.mu.Unlock()

	if s.OnRun != nil {
		return s.OnRun(ctx, containerID, config, stdout, stderr)
	}

	// Write simulated output
	if stdout != nil && s.Output != "" {
		_, _ = stdout.Write([]byte(s.Output))
	}

	return 0, nil
}

// SetOutput sets the simulated output for exec operations.
func (s *ExecService) SetOutput(output string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Output = output
}
