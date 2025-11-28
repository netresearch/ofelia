package mock

import (
	"context"
	"sync"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// NetworkService is a mock implementation of ports.NetworkService.
type NetworkService struct {
	mu sync.RWMutex

	// Callbacks for customizing behavior
	OnConnect    func(ctx context.Context, networkID, containerID string, config *domain.EndpointSettings) error
	OnDisconnect func(ctx context.Context, networkID, containerID string, force bool) error
	OnList       func(ctx context.Context, opts domain.NetworkListOptions) ([]domain.Network, error)
	OnInspect    func(ctx context.Context, networkID string) (*domain.Network, error)
	OnCreate     func(ctx context.Context, name string, opts ports.NetworkCreateOptions) (string, error)
	OnRemove     func(ctx context.Context, networkID string) error

	// Call tracking
	ConnectCalls    []NetworkConnectCall
	DisconnectCalls []NetworkDisconnectCall
	ListCalls       []domain.NetworkListOptions
	InspectCalls    []string
	CreateCalls     []NetworkCreateCall
	RemoveCalls     []string

	// Simulated data
	Networks []domain.Network
}

// NetworkConnectCall represents a call to Connect().
type NetworkConnectCall struct {
	NetworkID   string
	ContainerID string
	Config      *domain.EndpointSettings
}

// NetworkDisconnectCall represents a call to Disconnect().
type NetworkDisconnectCall struct {
	NetworkID   string
	ContainerID string
	Force       bool
}

// NetworkCreateCall represents a call to Create().
type NetworkCreateCall struct {
	Name    string
	Options ports.NetworkCreateOptions
}

// NewNetworkService creates a new mock NetworkService.
func NewNetworkService() *NetworkService {
	return &NetworkService{}
}

// Connect connects a container to a network.
func (s *NetworkService) Connect(ctx context.Context, networkID, containerID string, config *domain.EndpointSettings) error {
	s.mu.Lock()
	s.ConnectCalls = append(s.ConnectCalls, NetworkConnectCall{
		NetworkID:   networkID,
		ContainerID: containerID,
		Config:      config,
	})
	s.mu.Unlock()

	if s.OnConnect != nil {
		return s.OnConnect(ctx, networkID, containerID, config)
	}
	return nil
}

// Disconnect disconnects a container from a network.
func (s *NetworkService) Disconnect(ctx context.Context, networkID, containerID string, force bool) error {
	s.mu.Lock()
	s.DisconnectCalls = append(s.DisconnectCalls, NetworkDisconnectCall{
		NetworkID:   networkID,
		ContainerID: containerID,
		Force:       force,
	})
	s.mu.Unlock()

	if s.OnDisconnect != nil {
		return s.OnDisconnect(ctx, networkID, containerID, force)
	}
	return nil
}

// List lists networks.
func (s *NetworkService) List(ctx context.Context, opts domain.NetworkListOptions) ([]domain.Network, error) {
	s.mu.Lock()
	s.ListCalls = append(s.ListCalls, opts)
	networks := s.Networks
	s.mu.Unlock()

	if s.OnList != nil {
		return s.OnList(ctx, opts)
	}
	return networks, nil
}

// Inspect returns network information.
func (s *NetworkService) Inspect(ctx context.Context, networkID string) (*domain.Network, error) {
	s.mu.Lock()
	s.InspectCalls = append(s.InspectCalls, networkID)
	networks := s.Networks
	s.mu.Unlock()

	if s.OnInspect != nil {
		return s.OnInspect(ctx, networkID)
	}

	// Find network by ID
	for i := range networks {
		if networks[i].ID == networkID || networks[i].Name == networkID {
			return &networks[i], nil
		}
	}

	return &domain.Network{
		ID:   networkID,
		Name: networkID,
	}, nil
}

// Create creates a network.
func (s *NetworkService) Create(ctx context.Context, name string, opts ports.NetworkCreateOptions) (string, error) {
	s.mu.Lock()
	s.CreateCalls = append(s.CreateCalls, NetworkCreateCall{Name: name, Options: opts})
	s.mu.Unlock()

	if s.OnCreate != nil {
		return s.OnCreate(ctx, name, opts)
	}
	return "mock-network-id", nil
}

// Remove removes a network.
func (s *NetworkService) Remove(ctx context.Context, networkID string) error {
	s.mu.Lock()
	s.RemoveCalls = append(s.RemoveCalls, networkID)
	s.mu.Unlock()

	if s.OnRemove != nil {
		return s.OnRemove(ctx, networkID)
	}
	return nil
}

// SetNetworks sets the networks returned by List() and Inspect().
func (s *NetworkService) SetNetworks(networks []domain.Network) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Networks = networks
}
