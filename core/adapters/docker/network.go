package docker

import (
	"context"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// NetworkServiceAdapter implements ports.NetworkService using Docker SDK.
type NetworkServiceAdapter struct {
	client *client.Client
}

// Connect connects a container to a network.
func (s *NetworkServiceAdapter) Connect(ctx context.Context, networkID, containerID string, config *domain.EndpointSettings) error {
	var endpointConfig *network.EndpointSettings
	if config != nil {
		endpointConfig = convertToEndpointSettings(config)
	}

	err := s.client.NetworkConnect(ctx, networkID, containerID, endpointConfig)
	return convertError(err)
}

// Disconnect disconnects a container from a network.
func (s *NetworkServiceAdapter) Disconnect(ctx context.Context, networkID, containerID string, force bool) error {
	err := s.client.NetworkDisconnect(ctx, networkID, containerID, force)
	return convertError(err)
}

// List lists networks.
func (s *NetworkServiceAdapter) List(ctx context.Context, opts domain.NetworkListOptions) ([]domain.Network, error) {
	listOpts := network.ListOptions{}

	if len(opts.Filters) > 0 {
		listOpts.Filters = filters.NewArgs()
		for key, values := range opts.Filters {
			for _, v := range values {
				listOpts.Filters.Add(key, v)
			}
		}
	}

	networks, err := s.client.NetworkList(ctx, listOpts)
	if err != nil {
		return nil, convertError(err)
	}

	result := make([]domain.Network, len(networks))
	for i, n := range networks {
		result[i] = convertFromNetworkResource(&n)
	}
	return result, nil
}

// Inspect returns network information.
func (s *NetworkServiceAdapter) Inspect(ctx context.Context, networkID string) (*domain.Network, error) {
	n, err := s.client.NetworkInspect(ctx, networkID, network.InspectOptions{})
	if err != nil {
		return nil, convertError(err)
	}

	return convertFromNetworkInspect(&n), nil
}

// Create creates a network.
func (s *NetworkServiceAdapter) Create(ctx context.Context, name string, opts ports.NetworkCreateOptions) (string, error) {
	createOpts := network.CreateOptions{
		Driver:     opts.Driver,
		Scope:      opts.Scope,
		EnableIPv6: &opts.EnableIPv6,
		Internal:   opts.Internal,
		Attachable: opts.Attachable,
		Ingress:    opts.Ingress,
		Options:    opts.Options,
		Labels:     opts.Labels,
	}

	if opts.IPAM != nil {
		createOpts.IPAM = &network.IPAM{
			Driver:  opts.IPAM.Driver,
			Options: opts.IPAM.Options,
		}
		for _, cfg := range opts.IPAM.Config {
			createOpts.IPAM.Config = append(createOpts.IPAM.Config, network.IPAMConfig{
				Subnet:   cfg.Subnet,
				IPRange:  cfg.IPRange,
				Gateway:  cfg.Gateway,
				AuxAddress: cfg.AuxAddress,
			})
		}
	}

	resp, err := s.client.NetworkCreate(ctx, name, createOpts)
	if err != nil {
		return "", convertError(err)
	}

	return resp.ID, nil
}

// Remove removes a network.
func (s *NetworkServiceAdapter) Remove(ctx context.Context, networkID string) error {
	err := s.client.NetworkRemove(ctx, networkID)
	return convertError(err)
}
