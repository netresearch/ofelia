package core

import (
	"context"
	"errors"
	"io"
	"time"

	dockeradapter "github.com/netresearch/ofelia/core/adapters/docker"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// SDKDockerProvider implements DockerProvider using the official Docker SDK.
type SDKDockerProvider struct {
	client          ports.DockerClient
	logger          Logger
	metricsRecorder MetricsRecorder
}

// SDKDockerProviderConfig configures the SDK provider.
type SDKDockerProviderConfig struct {
	// Host is the Docker host address (e.g., "unix:///var/run/docker.sock")
	Host string
	// Logger for operation logging
	Logger Logger
	// MetricsRecorder for metrics tracking
	MetricsRecorder MetricsRecorder
}

// NewSDKDockerProvider creates a new SDK-based Docker provider.
func NewSDKDockerProvider(cfg *SDKDockerProviderConfig) (*SDKDockerProvider, error) {
	clientConfig := dockeradapter.DefaultConfig()
	if cfg != nil && cfg.Host != "" {
		clientConfig.Host = cfg.Host
	}

	client, err := dockeradapter.NewClientWithConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	var logger Logger
	var metricsRecorder MetricsRecorder
	if cfg != nil {
		logger = cfg.Logger
		metricsRecorder = cfg.MetricsRecorder
	}

	return &SDKDockerProvider{
		client:          client,
		logger:          logger,
		metricsRecorder: metricsRecorder,
	}, nil
}

// NewSDKDockerProviderDefault creates a provider with default settings.
func NewSDKDockerProviderDefault() (*SDKDockerProvider, error) {
	return NewSDKDockerProvider(nil)
}

// NewSDKDockerProviderFromClient creates a provider from an existing client.
func NewSDKDockerProviderFromClient(client ports.DockerClient, logger Logger, metricsRecorder MetricsRecorder) *SDKDockerProvider {
	return &SDKDockerProvider{
		client:          client,
		logger:          logger,
		metricsRecorder: metricsRecorder,
	}
}

// CreateContainer creates a new container.
func (p *SDKDockerProvider) CreateContainer(ctx context.Context, config *domain.ContainerConfig, name string) (string, error) {
	p.recordOperation("create_container")

	// Set name in config if provided
	if name != "" {
		config.Name = name
	}

	containerID, err := p.client.Containers().Create(ctx, config)
	if err != nil {
		p.recordError("create_container")
		return "", WrapContainerError("create", name, err)
	}

	p.logNotice("Created container %s (%s)", containerID, name)
	return containerID, nil
}

// StartContainer starts a container.
func (p *SDKDockerProvider) StartContainer(ctx context.Context, containerID string) error {
	p.recordOperation("start_container")

	if err := p.client.Containers().Start(ctx, containerID); err != nil {
		p.recordError("start_container")
		return WrapContainerError("start", containerID, err)
	}

	p.logNotice("Started container %s", containerID)
	return nil
}

// StopContainer stops a container.
func (p *SDKDockerProvider) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	p.recordOperation("stop_container")

	if err := p.client.Containers().Stop(ctx, containerID, timeout); err != nil {
		p.recordError("stop_container")
		return WrapContainerError("stop", containerID, err)
	}

	p.logNotice("Stopped container %s", containerID)
	return nil
}

// RemoveContainer removes a container.
func (p *SDKDockerProvider) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	p.recordOperation("remove_container")

	opts := domain.RemoveOptions{
		Force: force,
	}

	if err := p.client.Containers().Remove(ctx, containerID, opts); err != nil {
		p.recordError("remove_container")
		return WrapContainerError("remove", containerID, err)
	}

	p.logNotice("Removed container %s", containerID)
	return nil
}

// InspectContainer inspects a container.
func (p *SDKDockerProvider) InspectContainer(ctx context.Context, containerID string) (*domain.Container, error) {
	p.recordOperation("inspect_container")

	container, err := p.client.Containers().Inspect(ctx, containerID)
	if err != nil {
		p.recordError("inspect_container")
		return nil, WrapContainerError("inspect", containerID, err)
	}

	return container, nil
}

// WaitContainer waits for a container to exit.
func (p *SDKDockerProvider) WaitContainer(ctx context.Context, containerID string) (int64, error) {
	p.recordOperation("wait_container")

	respCh, errCh := p.client.Containers().Wait(ctx, containerID)

	select {
	case <-ctx.Done():
		p.recordError("wait_container")
		return -1, ctx.Err()
	case err := <-errCh:
		if err != nil {
			p.recordError("wait_container")
			return -1, WrapContainerError("wait", containerID, err)
		}
		return -1, nil
	case resp := <-respCh:
		if resp.Error != nil && resp.Error.Message != "" {
			p.recordError("wait_container")
			return resp.StatusCode, WrapContainerError("wait", containerID, errors.New(resp.Error.Message))
		}
		return resp.StatusCode, nil
	}
}

// GetContainerLogs retrieves container logs.
func (p *SDKDockerProvider) GetContainerLogs(ctx context.Context, containerID string, opts ContainerLogsOptions) (io.ReadCloser, error) {
	p.recordOperation("get_logs")

	logsOpts := domain.LogOptions{
		ShowStdout: opts.ShowStdout,
		ShowStderr: opts.ShowStderr,
		Tail:       opts.Tail,
		Follow:     opts.Follow,
	}

	if !opts.Since.IsZero() {
		logsOpts.Since = opts.Since.Format(time.RFC3339Nano)
	}

	reader, err := p.client.Containers().Logs(ctx, containerID, logsOpts)
	if err != nil {
		p.recordError("get_logs")
		return nil, WrapContainerError("get_logs", containerID, err)
	}

	return reader, nil
}

// CreateExec creates an exec instance.
func (p *SDKDockerProvider) CreateExec(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
	p.recordOperation("create_exec")

	execID, err := p.client.Exec().Create(ctx, containerID, config)
	if err != nil {
		p.recordError("create_exec")
		return "", WrapContainerError("create_exec", containerID, err)
	}

	p.logDebug("Created exec instance %s for container %s", execID, containerID)
	return execID, nil
}

// StartExec starts an exec instance.
func (p *SDKDockerProvider) StartExec(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	p.recordOperation("start_exec")

	resp, err := p.client.Exec().Start(ctx, execID, opts)
	if err != nil {
		p.recordError("start_exec")
		return nil, WrapContainerError("start_exec", execID, err)
	}

	p.logDebug("Started exec instance %s", execID)
	return resp, nil
}

// InspectExec inspects an exec instance.
func (p *SDKDockerProvider) InspectExec(ctx context.Context, execID string) (*domain.ExecInspect, error) {
	p.recordOperation("inspect_exec")

	inspect, err := p.client.Exec().Inspect(ctx, execID)
	if err != nil {
		p.recordError("inspect_exec")
		return nil, WrapContainerError("inspect_exec", execID, err)
	}

	return inspect, nil
}

// RunExec executes a command and waits for completion.
func (p *SDKDockerProvider) RunExec(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
	p.recordOperation("run_exec")

	exitCode, err := p.client.Exec().Run(ctx, containerID, config, stdout, stderr)
	if err != nil {
		p.recordError("run_exec")
		return -1, WrapContainerError("run_exec", containerID, err)
	}

	return exitCode, nil
}

// PullImage pulls an image.
func (p *SDKDockerProvider) PullImage(ctx context.Context, image string) error {
	p.recordOperation("pull_image")

	ref := domain.ParseRepositoryTag(image)
	opts := domain.PullOptions{
		Repository: ref.Repository,
		Tag:        ref.Tag,
	}

	if err := p.client.Images().PullAndWait(ctx, opts); err != nil {
		p.recordError("pull_image")
		return WrapImageError("pull", image, err)
	}

	p.logNotice("Pulled image %s", image)
	return nil
}

// HasImageLocally checks if an image exists locally.
func (p *SDKDockerProvider) HasImageLocally(ctx context.Context, image string) (bool, error) {
	p.recordOperation("check_image")

	exists, err := p.client.Images().Exists(ctx, image)
	if err != nil {
		p.recordError("check_image")
		return false, WrapImageError("check", image, err)
	}

	return exists, nil
}

// EnsureImage ensures an image is available, pulling if necessary.
func (p *SDKDockerProvider) EnsureImage(ctx context.Context, image string, forcePull bool) error {
	var pullError error

	if forcePull {
		if pullError = p.PullImage(ctx, image); pullError == nil {
			return nil
		}
	}

	hasImage, checkErr := p.HasImageLocally(ctx, image)
	if checkErr == nil && hasImage {
		p.logNotice("Found image %s locally", image)
		return nil
	}

	if !forcePull {
		if pullError = p.PullImage(ctx, image); pullError == nil {
			return nil
		}
	}

	if pullError != nil {
		return pullError
	}
	return checkErr
}

// ConnectNetwork connects a container to a network.
func (p *SDKDockerProvider) ConnectNetwork(ctx context.Context, networkID, containerID string) error {
	p.recordOperation("connect_network")

	if err := p.client.Networks().Connect(ctx, networkID, containerID, nil); err != nil {
		p.recordError("connect_network")
		return WrapContainerError("connect_network", containerID, err)
	}

	p.logNotice("Connected container %s to network %s", containerID, networkID)
	return nil
}

// FindNetworkByName finds networks by name.
func (p *SDKDockerProvider) FindNetworkByName(ctx context.Context, networkName string) ([]domain.Network, error) {
	p.recordOperation("list_networks")

	opts := domain.NetworkListOptions{
		Filters: map[string][]string{
			"name": {networkName},
		},
	}

	networks, err := p.client.Networks().List(ctx, opts)
	if err != nil {
		p.recordError("list_networks")
		return nil, err
	}

	return networks, nil
}

// SubscribeEvents subscribes to Docker events.
func (p *SDKDockerProvider) SubscribeEvents(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
	return p.client.Events().Subscribe(ctx, filter)
}

// Info returns Docker system info.
func (p *SDKDockerProvider) Info(ctx context.Context) (*domain.SystemInfo, error) {
	p.recordOperation("info")

	info, err := p.client.System().Info(ctx)
	if err != nil {
		p.recordError("info")
		return nil, err
	}

	return info, nil
}

// Ping pings the Docker daemon.
func (p *SDKDockerProvider) Ping(ctx context.Context) error {
	p.recordOperation("ping")

	_, err := p.client.System().Ping(ctx)
	if err != nil {
		p.recordError("ping")
		return err
	}

	return nil
}

// Close closes the Docker client.
func (p *SDKDockerProvider) Close() error {
	return p.client.Close()
}

// Helper methods for logging and metrics

func (p *SDKDockerProvider) recordOperation(name string) {
	if p.metricsRecorder != nil {
		p.metricsRecorder.RecordDockerOperation(name)
	}
}

func (p *SDKDockerProvider) recordError(name string) {
	if p.metricsRecorder != nil {
		p.metricsRecorder.RecordDockerError(name)
	}
}

func (p *SDKDockerProvider) logNotice(format string, args ...interface{}) {
	if p.logger != nil {
		p.logger.Noticef(format, args...)
	}
}

func (p *SDKDockerProvider) logDebug(format string, args ...interface{}) {
	if p.logger != nil {
		p.logger.Debugf(format, args...)
	}
}

// Ensure SDKDockerProvider implements DockerProvider
var _ DockerProvider = (*SDKDockerProvider)(nil)
