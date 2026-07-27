// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"net/netip"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	containertypes "github.com/moby/moby/api/types/container"
	networktypes "github.com/moby/moby/api/types/network"

	"github.com/netresearch/ofelia/core/domain"
)

// prefixToString renders a netip.Prefix for the domain model. The zero Prefix
// stringifies to "invalid Prefix", so unset values are mapped to "" to keep the
// empty-means-unset contract the domain model had when the SDK used strings.
func prefixToString(p netip.Prefix) string {
	if !p.IsValid() {
		return ""
	}
	return p.String()
}

// addrToString renders a netip.Addr for the domain model. See prefixToString:
// the zero Addr stringifies to "invalid IP".
func addrToString(a netip.Addr) string {
	if !a.IsValid() {
		return ""
	}
	return a.String()
}

// addrMapToString converts an auxiliary-address map to the domain's string map.
func addrMapToString(in map[string]netip.Addr) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = addrToString(v)
	}
	return out
}

// convertError converts Docker SDK errors to domain errors.
func convertError(err error) error {
	if err == nil {
		return nil
	}

	if cerrdefs.IsNotFound(err) {
		return &domain.ContainerNotFoundError{ID: err.Error()}
	}
	if cerrdefs.IsConflict(err) {
		return domain.ErrConflict
	}
	if cerrdefs.IsUnauthorized(err) {
		return domain.ErrUnauthorized
	}
	if cerrdefs.IsPermissionDenied(err) {
		return domain.ErrForbidden
	}
	if cerrdefs.IsDeadlineExceeded(err) {
		return domain.ErrTimeout
	}
	if cerrdefs.IsCanceled(err) {
		return domain.ErrCanceled
	}
	if cerrdefs.IsUnavailable(err) {
		return domain.ErrConnectionFailed
	}

	return err
}

// convertFromContainerJSON converts SDK InspectResponse to domain Container.
func convertFromContainerJSON(c *containertypes.InspectResponse) *domain.Container {
	if c == nil {
		return nil
	}

	container := &domain.Container{
		ID:      c.ID,
		Name:    c.Name,
		Image:   c.Image,
		Created: parseTime(c.Created),
		Labels:  c.Config.Labels,
	}

	// Convert state
	if c.State != nil {
		container.State = domain.ContainerState{
			Running:    c.State.Running,
			Paused:     c.State.Paused,
			Restarting: c.State.Restarting,
			OOMKilled:  c.State.OOMKilled,
			Dead:       c.State.Dead,
			Pid:        c.State.Pid,
			ExitCode:   c.State.ExitCode,
			Error:      c.State.Error,
			StartedAt:  parseTime(c.State.StartedAt),
			FinishedAt: parseTime(c.State.FinishedAt),
		}

		if c.State.Health != nil {
			container.State.Health = &domain.Health{
				Status:        string(c.State.Health.Status),
				FailingStreak: c.State.Health.FailingStreak,
			}
			for _, log := range c.State.Health.Log {
				container.State.Health.Log = append(container.State.Health.Log, domain.HealthCheckResult{
					Start:    log.Start,
					End:      log.End,
					ExitCode: log.ExitCode,
					Output:   log.Output,
				})
			}
		}
	}

	// Convert config
	if c.Config != nil {
		container.Config = &domain.ContainerConfig{
			Image:        c.Config.Image,
			Cmd:          c.Config.Cmd,
			Entrypoint:   c.Config.Entrypoint,
			Env:          c.Config.Env,
			WorkingDir:   c.Config.WorkingDir,
			User:         c.Config.User,
			Labels:       c.Config.Labels,
			Hostname:     c.Config.Hostname,
			AttachStdin:  c.Config.AttachStdin,
			AttachStdout: c.Config.AttachStdout,
			AttachStderr: c.Config.AttachStderr,
			Tty:          c.Config.Tty,
			OpenStdin:    c.Config.OpenStdin,
			StdinOnce:    c.Config.StdinOnce,
		}
	}

	// Convert mounts
	for _, m := range c.Mounts {
		container.Mounts = append(container.Mounts, domain.Mount{
			Type:     domain.MountType(m.Type),
			Source:   m.Source,
			Target:   m.Destination,
			ReadOnly: !m.RW,
		})
	}

	return container
}

// convertFromAPIContainer converts SDK Container (list result) to domain Container.
// Returns the zero domain.Container (no panic) when c is nil — defense-in-depth
// for an unsafe signature contract. Production callers pass `&loopVar` from a
// `range` over a slice so c is never nil today, but the helper signature
// invites unsafe direct calls. Mirrors PR #648 / #626. See #654.
func convertFromAPIContainer(c *containertypes.Summary) domain.Container {
	if c == nil {
		return domain.Container{}
	}

	var name string
	if len(c.Names) > 0 {
		// Docker API returns container names with leading slash (e.g., "/my-container").
		// Strip it to prevent malformed URLs in the web UI (issue #422).
		name = strings.TrimPrefix(c.Names[0], "/")
	}

	return domain.Container{
		ID:      c.ID,
		Name:    name,
		Image:   c.Image,
		Created: time.Unix(c.Created, 0),
		Labels:  c.Labels,
		State: domain.ContainerState{
			Running: c.State == "running",
		},
	}
}

// convertFromNetworkResource converts SDK NetworkResource to domain Network.
// Returns the zero domain.Network (no panic) when n is nil — defense-in-depth
// for an unsafe signature contract. Production callers pass `&loopVar` from a
// `range` over a slice so n is never nil today, but the helper signature
// invites unsafe direct calls. Mirrors PR #648 / #626. See #654.
func convertFromNetworkResource(n *networktypes.Summary) domain.Network {
	if n == nil {
		return domain.Network{}
	}

	network := domain.Network{
		Name:       n.Name,
		ID:         n.ID,
		Created:    n.Created,
		Scope:      n.Scope,
		Driver:     n.Driver,
		EnableIPv6: n.EnableIPv6,
		Internal:   n.Internal,
		Attachable: n.Attachable,
		Ingress:    n.Ingress,
		Options:    n.Options,
		Labels:     n.Labels,
	}

	// Convert IPAM
	if n.IPAM.Driver != "" || len(n.IPAM.Config) > 0 {
		network.IPAM = domain.IPAM{
			Driver:  n.IPAM.Driver,
			Options: n.IPAM.Options,
		}
		for _, cfg := range n.IPAM.Config {
			network.IPAM.Config = append(network.IPAM.Config, domain.IPAMConfig{
				Subnet:     prefixToString(cfg.Subnet),
				IPRange:    prefixToString(cfg.IPRange),
				Gateway:    addrToString(cfg.Gateway),
				AuxAddress: addrMapToString(cfg.AuxAddress),
			})
		}
	}

	// network.Summary carries no Containers map: the list endpoint does not
	// return per-network endpoints. Callers that need them must Inspect the
	// network, which convertFromNetworkInspect still populates.

	return network
}

// convertFromNetworkInspect converts SDK NetworkResource from inspect to domain Network.
// Returns nil (no panic) when n is nil — defense-in-depth for an unsafe
// signature contract. Production callers (NetworkServiceAdapter.Inspect)
// pass `&n` after a successful SDK call so n is never nil today, but the
// helper signature invites unsafe direct calls. Mirrors PR #648 / #626.
// See #654.
func convertFromNetworkInspect(n *networktypes.Inspect) *domain.Network {
	if n == nil {
		return nil
	}

	network := &domain.Network{
		Name:       n.Name,
		ID:         n.ID,
		Created:    n.Created,
		Scope:      n.Scope,
		Driver:     n.Driver,
		EnableIPv6: n.EnableIPv6,
		Internal:   n.Internal,
		Attachable: n.Attachable,
		Ingress:    n.Ingress,
		Options:    n.Options,
		Labels:     n.Labels,
	}

	// Convert IPAM
	if n.IPAM.Driver != "" || len(n.IPAM.Config) > 0 {
		network.IPAM = domain.IPAM{
			Driver:  n.IPAM.Driver,
			Options: n.IPAM.Options,
		}
		for _, cfg := range n.IPAM.Config {
			network.IPAM.Config = append(network.IPAM.Config, domain.IPAMConfig{
				Subnet:     prefixToString(cfg.Subnet),
				IPRange:    prefixToString(cfg.IPRange),
				Gateway:    addrToString(cfg.Gateway),
				AuxAddress: addrMapToString(cfg.AuxAddress),
			})
		}
	}

	// Convert containers
	if len(n.Containers) > 0 {
		network.Containers = make(map[string]domain.EndpointResource)
		for id, ep := range n.Containers {
			network.Containers[id] = domain.EndpointResource{
				Name:        ep.Name,
				EndpointID:  ep.EndpointID,
				MacAddress:  ep.MacAddress.String(),
				IPv4Address: prefixToString(ep.IPv4Address),
				IPv6Address: prefixToString(ep.IPv6Address),
			}
		}
	}

	return network
}

// parseTime parses a Docker timestamp string.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
