package docker

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// ContainerServiceAdapter implements ports.ContainerService using Docker SDK.
type ContainerServiceAdapter struct {
	client *client.Client
}

// Create creates a new container.
func (s *ContainerServiceAdapter) Create(ctx context.Context, config *domain.ContainerConfig) (string, error) {
	containerConfig := convertToContainerConfig(config)
	hostConfig := convertToHostConfig(config.HostConfig)
	networkConfig := convertToNetworkingConfig(config.NetworkConfig)

	var platform *ocispec.Platform // Let Docker choose the platform

	resp, err := s.client.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, platform, config.Name)
	if err != nil {
		return "", convertError(err)
	}

	return resp.ID, nil
}

// Start starts a container.
func (s *ContainerServiceAdapter) Start(ctx context.Context, containerID string) error {
	err := s.client.ContainerStart(ctx, containerID, container.StartOptions{})
	return convertError(err)
}

// Stop stops a container.
func (s *ContainerServiceAdapter) Stop(ctx context.Context, containerID string, timeout *time.Duration) error {
	opts := container.StopOptions{}
	if timeout != nil {
		seconds := int(timeout.Seconds())
		opts.Timeout = &seconds
	}
	err := s.client.ContainerStop(ctx, containerID, opts)
	return convertError(err)
}

// Remove removes a container.
func (s *ContainerServiceAdapter) Remove(ctx context.Context, containerID string, opts domain.RemoveOptions) error {
	err := s.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		RemoveVolumes: opts.RemoveVolumes,
		RemoveLinks:   opts.RemoveLinks,
		Force:         opts.Force,
	})
	return convertError(err)
}

// Inspect returns container information.
func (s *ContainerServiceAdapter) Inspect(ctx context.Context, containerID string) (*domain.Container, error) {
	resp, err := s.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, convertError(err)
	}

	return convertFromContainerJSON(&resp), nil
}

// List lists containers.
func (s *ContainerServiceAdapter) List(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error) {
	listOpts := container.ListOptions{
		All:   opts.All,
		Size:  opts.Size,
		Limit: opts.Limit,
	}

	if len(opts.Filters) > 0 {
		listOpts.Filters = filters.NewArgs()
		for key, values := range opts.Filters {
			for _, v := range values {
				listOpts.Filters.Add(key, v)
			}
		}
	}

	containers, err := s.client.ContainerList(ctx, listOpts)
	if err != nil {
		return nil, convertError(err)
	}

	result := make([]domain.Container, len(containers))
	for i, c := range containers {
		result[i] = convertFromAPIContainer(&c)
	}
	return result, nil
}

// Wait waits for a container to stop.
func (s *ContainerServiceAdapter) Wait(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
	respCh := make(chan domain.WaitResponse, 1)
	errCh := make(chan error, 1)

	go func() {
		defer close(respCh)
		defer close(errCh)

		statusCh, sdkErrCh := s.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)

		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
		case err := <-sdkErrCh:
			errCh <- convertError(err)
		case status := <-statusCh:
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
	reader, err := s.client.ContainerLogs(ctx, containerID, container.LogsOptions{
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
func (s *ContainerServiceAdapter) CopyLogs(ctx context.Context, containerID string, stdout, stderr io.Writer, opts domain.LogOptions) error {
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
			_, err = io.Copy(stdout, reader)
		}
		return err
	}

	// For non-TTY containers, use stdcopy to demux
	_, err = stdcopy.StdCopy(stdout, stderr, reader)
	return err
}

// Kill sends a signal to a container.
func (s *ContainerServiceAdapter) Kill(ctx context.Context, containerID string, signal string) error {
	err := s.client.ContainerKill(ctx, containerID, signal)
	return convertError(err)
}

// Pause pauses a container.
func (s *ContainerServiceAdapter) Pause(ctx context.Context, containerID string) error {
	err := s.client.ContainerPause(ctx, containerID)
	return convertError(err)
}

// Unpause unpauses a container.
func (s *ContainerServiceAdapter) Unpause(ctx context.Context, containerID string) error {
	err := s.client.ContainerUnpause(ctx, containerID)
	return convertError(err)
}

// Rename renames a container.
func (s *ContainerServiceAdapter) Rename(ctx context.Context, containerID string, newName string) error {
	err := s.client.ContainerRename(ctx, containerID, newName)
	return convertError(err)
}

// Attach attaches to a container.
func (s *ContainerServiceAdapter) Attach(ctx context.Context, containerID string, opts ports.AttachOptions) (*domain.HijackedResponse, error) {
	resp, err := s.client.ContainerAttach(ctx, containerID, container.AttachOptions{
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
		Binds:           config.Binds,
		NetworkMode:     container.NetworkMode(config.NetworkMode),
		PortBindings:    convertToPortMap(config.PortBindings),
		AutoRemove:      config.AutoRemove,
		Privileged:      config.Privileged,
		ReadonlyRootfs:  config.ReadonlyRootfs,
		DNS:             config.DNS,
		DNSSearch:       config.DNSSearch,
		ExtraHosts:      config.ExtraHosts,
		CapAdd:          config.CapAdd,
		CapDrop:         config.CapDrop,
		SecurityOpt:     config.SecurityOpt,
		PidMode:         container.PidMode(config.PidMode),
		UsernsMode:      container.UsernsMode(config.UsernsMode),
		ShmSize:         config.ShmSize,
		Tmpfs:           config.Tmpfs,
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
		Gateway:             settings.Gateway,
		IPAddress:           settings.IPAddress,
		IPPrefixLen:         settings.IPPrefixLen,
		IPv6Gateway:         settings.IPv6Gateway,
		GlobalIPv6Address:   settings.GlobalIPv6Address,
		GlobalIPv6PrefixLen: settings.GlobalIPv6PrefixLen,
		MacAddress:          settings.MacAddress,
		DriverOpts:          settings.DriverOpts,
	}

	if settings.IPAMConfig != nil {
		endpoint.IPAMConfig = &network.EndpointIPAMConfig{
			IPv4Address:  settings.IPAMConfig.IPv4Address,
			IPv6Address:  settings.IPAMConfig.IPv6Address,
			LinkLocalIPs: settings.IPAMConfig.LinkLocalIPs,
		}
	}

	return endpoint
}

func convertToPortMap(pm domain.PortMap) nat.PortMap {
	if len(pm) == 0 {
		return nil
	}

	result := make(nat.PortMap)
	for port, bindings := range pm {
		natPort := nat.Port(port)
		for _, b := range bindings {
			result[natPort] = append(result[natPort], nat.PortBinding{
				HostIP:   b.HostIP,
				HostPort: b.HostPort,
			})
		}
	}
	return result
}

func convertToMount(m *domain.Mount) mount.Mount {
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
