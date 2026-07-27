// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// ContainerServiceAdapter implements ports.ContainerService using Docker SDK.
type ContainerServiceAdapter struct {
	client *client.Client
}

// checkClient returns ErrNilDockerClient if the embedded SDK client is nil.
// Defense-in-depth guard: the public constructor always wires a non-nil
// client, so this is only reachable through hand-rolled adapter values.
func (s *ContainerServiceAdapter) checkClient() error {
	if s.client == nil {
		return ErrNilDockerClient
	}
	return nil
}

// Create creates a new container.
//
// Returns ErrNilContainerConfig (no panic) if config is nil — the
// previous code dereferenced config.HostConfig / config.NetworkConfig
// unconditionally. See #632 / #626.
func (s *ContainerServiceAdapter) Create(ctx context.Context, config *domain.ContainerConfig) (string, error) {
	if err := s.checkClient(); err != nil {
		return "", err
	}
	if config == nil {
		return "", ErrNilContainerConfig
	}
	containerConfig := convertToContainerConfig(config)
	hostConfig := convertToHostConfig(config.HostConfig)
	networkConfig := convertToNetworkingConfig(config.NetworkConfig)

	var platform *ocispec.Platform // Let Docker choose the platform

	resp, err := s.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           containerConfig,
		HostConfig:       hostConfig,
		NetworkingConfig: networkConfig,
		Platform:         platform,
		Name:             config.Name,
	})
	if err != nil {
		return "", convertError(err)
	}

	return resp.ID, nil
}

// Start starts a container.
func (s *ContainerServiceAdapter) Start(ctx context.Context, containerID string) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
	return convertError(err)
}

// Stop stops a container.
func (s *ContainerServiceAdapter) Stop(ctx context.Context, containerID string, opts domain.StopOptions) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	sdkOpts := client.ContainerStopOptions{
		Signal: opts.Signal,
	}
	if opts.Timeout != nil {
		seconds := int(opts.Timeout.Seconds())
		sdkOpts.Timeout = &seconds
	}
	_, err := s.client.ContainerStop(ctx, containerID, sdkOpts)
	return convertError(err)
}

// Remove removes a container.
func (s *ContainerServiceAdapter) Remove(ctx context.Context, containerID string, opts domain.RemoveOptions) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
		RemoveVolumes: opts.RemoveVolumes,
		RemoveLinks:   opts.RemoveLinks,
		Force:         opts.Force,
	})
	return convertError(err)
}

// Inspect returns container information.
func (s *ContainerServiceAdapter) Inspect(ctx context.Context, containerID string) (*domain.Container, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	resp, err := s.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, convertError(err)
	}

	return convertFromContainerJSON(&resp.Container), nil
}

// List lists containers.
func (s *ContainerServiceAdapter) List(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	listOpts := client.ContainerListOptions{
		All:   opts.All,
		Size:  opts.Size,
		Limit: opts.Limit,
	}

	if len(opts.Filters) > 0 {
		listOpts.Filters = make(client.Filters)
		for key, values := range opts.Filters {
			listOpts.Filters.Add(key, values...)
		}
	}

	containers, err := s.client.ContainerList(ctx, listOpts)
	if err != nil {
		return nil, convertError(err)
	}

	result := make([]domain.Container, len(containers.Items))
	for i := range containers.Items {
		result[i] = convertFromAPIContainer(&containers.Items[i])
	}
	return result, nil
}

// Wait waits for a container to stop.
func (s *ContainerServiceAdapter) Wait(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
	respCh := make(chan domain.WaitResponse, 1)
	errCh := make(chan error, 1)

	if err := s.checkClient(); err != nil {
		errCh <- err
		close(respCh)
		close(errCh)
		return respCh, errCh
	}

	go func() {
		defer close(respCh)
		defer close(errCh)

		waitResult := s.client.ContainerWait(ctx, containerID, client.ContainerWaitOptions{
			Condition: container.WaitConditionNotRunning,
		})

		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
		case err := <-waitResult.Error:
			errCh <- convertError(err)
		case status := <-waitResult.Result:
			resp := domain.WaitResponse{
				StatusCode: status.StatusCode,
			}
			if status.Error != nil {
				resp.Error = &domain.WaitError{
					Message: status.Error.Message,
				}
			}
			respCh <- resp
		}
	}()

	return respCh, errCh
}

// Logs returns container logs.
func (s *ContainerServiceAdapter) Logs(ctx context.Context, containerID string, opts domain.LogOptions) (io.ReadCloser, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	reader, err := s.client.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: opts.ShowStdout,
		ShowStderr: opts.ShowStderr,
		Since:      opts.Since,
		Until:      opts.Until,
		Timestamps: opts.Timestamps,
		Follow:     opts.Follow,
		Tail:       opts.Tail,
		Details:    opts.Details,
	})
	if err != nil {
		return nil, convertError(err)
	}
	return reader, nil
}

// CopyLogs copies container logs to writers.
func (s *ContainerServiceAdapter) CopyLogs(
	ctx context.Context, containerID string, stdout, stderr io.Writer, opts domain.LogOptions,
) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	// First check if container uses TTY
	info, err := s.Inspect(ctx, containerID)
	if err != nil {
		return err
	}

	reader, err := s.Logs(ctx, containerID, opts)
	if err != nil {
		return err
	}
	defer reader.Close()

	if info.Config != nil && info.Config.HostConfig != nil {
		// For TTY containers, copy directly
		if stdout != nil {
			if _, err = io.Copy(stdout, reader); err != nil {
				return fmt.Errorf("copying container output: %w", err)
			}
		}
		return nil
	}

	// For non-TTY containers, use stdcopy to demux
	if _, err = stdcopy.StdCopy(stdout, stderr, reader); err != nil {
		return fmt.Errorf("copying container output: %w", err)
	}
	return nil
}

// Kill sends a signal to a container.
func (s *ContainerServiceAdapter) Kill(ctx context.Context, containerID string, signal string) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.ContainerKill(ctx, containerID, client.ContainerKillOptions{Signal: signal})
	return convertError(err)
}

// Pause pauses a container.
func (s *ContainerServiceAdapter) Pause(ctx context.Context, containerID string) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.ContainerPause(ctx, containerID, client.ContainerPauseOptions{})
	return convertError(err)
}

// Unpause unpauses a container.
func (s *ContainerServiceAdapter) Unpause(ctx context.Context, containerID string) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.ContainerUnpause(ctx, containerID, client.ContainerUnpauseOptions{})
	return convertError(err)
}

// Rename renames a container.
func (s *ContainerServiceAdapter) Rename(ctx context.Context, containerID string, newName string) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	_, err := s.client.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: newName})
	return convertError(err)
}

// Attach attaches to a container.
func (s *ContainerServiceAdapter) Attach(
	ctx context.Context, containerID string, opts ports.AttachOptions,
) (*domain.HijackedResponse, error) {
	if err := s.checkClient(); err != nil {
		return nil, err
	}
	resp, err := s.client.ContainerAttach(ctx, containerID, client.ContainerAttachOptions{
		Stream:     opts.Stream,
		Stdin:      opts.Stdin,
		Stdout:     opts.Stdout,
		Stderr:     opts.Stderr,
		DetachKeys: opts.DetachKeys,
		Logs:       opts.Logs,
	})
	if err != nil {
		return nil, convertError(err)
	}

	return &domain.HijackedResponse{
		Conn:   resp.Conn,
		Reader: resp.Reader,
	}, nil
}

// Helper conversion functions

// The split SDK types network addresses (netip.Addr, network.HardwareAddr,
// network.Port) where the frozen SDK used strings. ofelia's domain model keeps
// strings, so the request path parses them here. Unparseable input yields the
// zero value, i.e. "unset" — the same request the daemon would have received
// for an empty string. No ofelia config path populates these fields today:
// domain.NetworkConfig / EndpointSettings / HostConfig.DNS exist for the ports
// abstraction but are never set by job definitions.
func parseAddrOrZero(s string) netip.Addr {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

func parseAddrsOrEmpty(in []string) []netip.Addr {
	if len(in) == 0 {
		return nil
	}
	out := make([]netip.Addr, 0, len(in))
	for _, s := range in {
		if addr, err := netip.ParseAddr(s); err == nil {
			out = append(out, addr)
		}
	}
	return out
}

func parsePrefixOrZero(s string) netip.Prefix {
	prefix, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}
	}
	return prefix
}

func parseAddrMapOrEmpty(in map[string]string) map[string]netip.Addr {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]netip.Addr, len(in))
	for k, v := range in {
		if addr, err := netip.ParseAddr(v); err == nil {
			out[k] = addr
		}
	}
	return out
}

func parseHardwareAddrOrZero(s string) network.HardwareAddr {
	if s == "" {
		return nil
	}
	hw, err := net.ParseMAC(s)
	if err != nil {
		return nil
	}
	return network.HardwareAddr(hw)
}

func convertToContainerConfig(config *domain.ContainerConfig) *container.Config {
	if config == nil {
		return nil
	}

	return &container.Config{
		Hostname:     config.Hostname,
		User:         config.User,
		AttachStdin:  config.AttachStdin,
		AttachStdout: config.AttachStdout,
		AttachStderr: config.AttachStderr,
		Tty:          config.Tty,
		OpenStdin:    config.OpenStdin,
		StdinOnce:    config.StdinOnce,
		Env:          config.Env,
		Cmd:          config.Cmd,
		Image:        config.Image,
		WorkingDir:   config.WorkingDir,
		Entrypoint:   config.Entrypoint,
		Labels:       config.Labels,
	}
}

func convertToHostConfig(config *domain.HostConfig) *container.HostConfig {
	if config == nil {
		return nil
	}

	hostConfig := &container.HostConfig{
		Binds:          config.Binds,
		VolumesFrom:    config.VolumesFrom,
		NetworkMode:    container.NetworkMode(config.NetworkMode),
		PortBindings:   convertToPortMap(config.PortBindings),
		AutoRemove:     config.AutoRemove,
		Privileged:     config.Privileged,
		ReadonlyRootfs: config.ReadonlyRootfs,
		DNS:            parseAddrsOrEmpty(config.DNS),
		DNSSearch:      config.DNSSearch,
		ExtraHosts:     config.ExtraHosts,
		CapAdd:         config.CapAdd,
		CapDrop:        config.CapDrop,
		SecurityOpt:    config.SecurityOpt,
		PidMode:        container.PidMode(config.PidMode),
		UsernsMode:     container.UsernsMode(config.UsernsMode),
		ShmSize:        config.ShmSize,
		Tmpfs:          config.Tmpfs,
		RestartPolicy: container.RestartPolicy{
			Name:              container.RestartPolicyMode(config.RestartPolicy.Name),
			MaximumRetryCount: config.RestartPolicy.MaximumRetryCount,
		},
		Resources: container.Resources{
			Memory:     config.Memory,
			MemorySwap: config.MemorySwap,
			CPUShares:  config.CPUShares,
			CPUPeriod:  config.CPUPeriod,
			CPUQuota:   config.CPUQuota,
			NanoCPUs:   config.NanoCPUs,
		},
		LogConfig: container.LogConfig{
			Type:   config.LogConfig.Type,
			Config: config.LogConfig.Config,
		},
	}

	// Convert mounts
	for _, m := range config.Mounts {
		hostConfig.Mounts = append(hostConfig.Mounts, convertToMount(&m))
	}

	// Convert ulimits
	for _, u := range config.Ulimits {
		hostConfig.Ulimits = append(hostConfig.Ulimits, &container.Ulimit{
			Name: u.Name,
			Soft: u.Soft,
			Hard: u.Hard,
		})
	}

	return hostConfig
}

func convertToNetworkingConfig(config *domain.NetworkConfig) *network.NetworkingConfig {
	if config == nil {
		return nil
	}

	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: make(map[string]*network.EndpointSettings),
	}

	for name, endpoint := range config.EndpointsConfig {
		networkConfig.EndpointsConfig[name] = convertToEndpointSettings(endpoint)
	}

	return networkConfig
}

func convertToEndpointSettings(settings *domain.EndpointSettings) *network.EndpointSettings {
	if settings == nil {
		return nil
	}

	endpoint := &network.EndpointSettings{
		Links:               settings.Links,
		Aliases:             settings.Aliases,
		NetworkID:           settings.NetworkID,
		EndpointID:          settings.EndpointID,
		Gateway:             parseAddrOrZero(settings.Gateway),
		IPAddress:           parseAddrOrZero(settings.IPAddress),
		IPPrefixLen:         settings.IPPrefixLen,
		IPv6Gateway:         parseAddrOrZero(settings.IPv6Gateway),
		GlobalIPv6Address:   parseAddrOrZero(settings.GlobalIPv6Address),
		GlobalIPv6PrefixLen: settings.GlobalIPv6PrefixLen,
		MacAddress:          parseHardwareAddrOrZero(settings.MacAddress),
		DriverOpts:          settings.DriverOpts,
	}

	if settings.IPAMConfig != nil {
		endpoint.IPAMConfig = &network.EndpointIPAMConfig{
			IPv4Address:  parseAddrOrZero(settings.IPAMConfig.IPv4Address),
			IPv6Address:  parseAddrOrZero(settings.IPAMConfig.IPv6Address),
			LinkLocalIPs: parseAddrsOrEmpty(settings.IPAMConfig.LinkLocalIPs),
		}
	}

	return endpoint
}

func convertToPortMap(pm domain.PortMap) network.PortMap {
	if len(pm) == 0 {
		return nil
	}

	result := make(network.PortMap)
	for port, bindings := range pm {
		// An unparseable port spec is dropped rather than aborting the map:
		// the frozen SDK's nat.Port(port) accepted it here and left rejection
		// to the daemon.
		sdkPort, err := network.ParsePort(string(port))
		if err != nil {
			continue
		}
		for _, b := range bindings {
			result[sdkPort] = append(result[sdkPort], network.PortBinding{
				HostIP:   parseAddrOrZero(b.HostIP),
				HostPort: b.HostPort,
			})
		}
	}
	return result
}

// convertToMount converts a domain.Mount to a Docker SDK mount.Mount.
// Returns the zero mount.Mount (no panic) when m is nil — defense-in-depth
// for an unsafe signature contract. Production callers pass `&loopVar` from
// a `range` over a slice so m is never nil today, but the helper signature
// invites unsafe direct calls and every other convertTo* helper in this file
// (convertToHostConfig, convertToNetworkingConfig, convertToEndpointSettings,
// convertToContainerConfig) already nil-guards its argument — only this one
// was asymmetric. Mirrors PR #648 / #626. See #654.
func convertToMount(m *domain.Mount) mount.Mount {
	if m == nil {
		return mount.Mount{}
	}

	mnt := mount.Mount{
		Type:        mount.Type(m.Type),
		Source:      m.Source,
		Target:      m.Target,
		ReadOnly:    m.ReadOnly,
		Consistency: mount.Consistency(m.Consistency),
	}

	if m.BindOptions != nil {
		mnt.BindOptions = &mount.BindOptions{
			Propagation: mount.Propagation(m.BindOptions.Propagation),
		}
	}

	if m.VolumeOptions != nil {
		mnt.VolumeOptions = &mount.VolumeOptions{
			NoCopy: m.VolumeOptions.NoCopy,
			Labels: m.VolumeOptions.Labels,
		}
		if m.VolumeOptions.DriverConfig != nil {
			mnt.VolumeOptions.DriverConfig = &mount.Driver{
				Name:    m.VolumeOptions.DriverConfig.Name,
				Options: m.VolumeOptions.DriverConfig.Options,
			}
		}
	}

	if m.TmpfsOptions != nil {
		mnt.TmpfsOptions = &mount.TmpfsOptions{
			SizeBytes: m.TmpfsOptions.SizeBytes,
			Mode:      os.FileMode(m.TmpfsOptions.Mode),
		}
	}

	return mnt
}
