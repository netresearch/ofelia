// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// NetworkServiceAdapter implements ports.NetworkService using Docker SDK.
type NetworkServiceAdapter struct {
	client *client.Client
}

// checkClient returns ErrNilDockerClient if the embedded SDK client is nil.
// See docker.ErrNilDockerClient for rationale.
func (s *NetworkServiceAdapter) checkClient() error {
	if s.client == nil {
		return ErrNilDockerClient
	}
	return nil
}

// Connect connects a container to a network.
func (s *NetworkServiceAdapter) Connect(ctx context.Context, networkID, containerID string, config *domain.EndpointSettings) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	var endpointConfig *network.EndpointSettings
	if config != nil {
		endpointConfig = convertToEndpointSettings(config)
	}

	_, err := s.client.NetworkConnect(ctx, networkID, client.NetworkConnectOptions{
		Container:      containerID,
		EndpointConfig: endpointConfig,
	})
	return convertError(err)
}

// Disconnect disconnects a container from a network.
func (s *NetworkServiceAdapter) Disconnect(ctx context.Context, networkID, containerID string, force bool) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.NetworkDisconnect(ctx, networkID, client.NetworkDisconnectOptions{
		Container: containerID,
		Force:     force,
	})
	return convertError(err)
}

// List lists networks.
func (s *NetworkServiceAdapter) List(ctx context.Context, opts domain.NetworkListOptions) ([]domain.Network, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	listOpts := client.NetworkListOptions{}

	if len(opts.Filters) > 0 {
		listOpts.Filters = make(client.Filters)
		for key, values := range opts.Filters {
			listOpts.Filters.Add(key, values...)
		}
	}

	networks, err := s.client.NetworkList(ctx, listOpts)
	if err != nil {
		return nil, convertError(err)
	}

	result := make([]domain.Network, len(networks.Items))
	for i := range networks.Items {
		result[i] = convertFromNetworkResource(&networks.Items[i])
	}
	return result, nil
}

// Inspect returns network information.
func (s *NetworkServiceAdapter) Inspect(ctx context.Context, networkID string) (*domain.Network, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	n, err := s.client.NetworkInspect(ctx, networkID, client.NetworkInspectOptions{})
	if err != nil {
		return nil, convertError(err)
	}

	return convertFromNetworkInspect(&n.Network), nil
}

// Create creates a network.
func (s *NetworkServiceAdapter) Create(ctx context.Context, name string, opts ports.NetworkCreateOptions) (string, error) {
	if err := s.checkClient(); err != nil {
		return "", err
	}
	createOpts := client.NetworkCreateOptions{
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
				Subnet:     parsePrefixOrZero(cfg.Subnet),
				IPRange:    parsePrefixOrZero(cfg.IPRange),
				Gateway:    parseAddrOrZero(cfg.Gateway),
				AuxAddress: parseAddrMapOrEmpty(cfg.AuxAddress),
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
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.NetworkRemove(ctx, networkID, client.NetworkRemoveOptions{})
	return convertError(err)
}
