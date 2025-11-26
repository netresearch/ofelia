package ports

import (
	"context"

	"github.com/netresearch/ofelia/core/domain"
)

// NetworkService provides operations for managing Docker networks.
type NetworkService interface {
	// Connect connects a container to a network.
	Connect(ctx context.Context, networkID, containerID string, config *domain.EndpointSettings) error

	// Disconnect disconnects a container from a network.
	Disconnect(ctx context.Context, networkID, containerID string, force bool) error

	// List returns a list of networks matching the options.
	List(ctx context.Context, opts domain.NetworkListOptions) ([]domain.Network, error)

	// Inspect returns detailed information about a network.
	Inspect(ctx context.Context, networkID string) (*domain.Network, error)

	// Create creates a new network.
	Create(ctx context.Context, name string, opts NetworkCreateOptions) (string, error)

	// Remove removes a network.
	Remove(ctx context.Context, networkID string) error
}

// NetworkCreateOptions represents options for creating a network.
type NetworkCreateOptions struct {
	Driver     string
	Scope      string
	EnableIPv6 bool
	IPAM       *domain.IPAM
	Internal   bool
	Attachable bool
	Ingress    bool
	Options    map[string]string
	Labels     map[string]string
}
