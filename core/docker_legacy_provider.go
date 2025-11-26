package core

import (
	"context"
	"io"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/netresearch/ofelia/core/domain"
)

// LegacyDockerProvider implements DockerProvider using go-dockerclient.
// This provides backward compatibility during the migration period.
type LegacyDockerProvider struct {
	client          *docker.Client
	logger          Logger
	metricsRecorder MetricsRecorder
}

// NewLegacyDockerProvider creates a new legacy Docker provider.
func NewLegacyDockerProvider(client *docker.Client, logger Logger, metricsRecorder MetricsRecorder) *LegacyDockerProvider {
	return &LegacyDockerProvider{
		client:          client,
		logger:          logger,
		metricsRecorder: metricsRecorder,
	}
}

// GetLegacyClient returns the underlying go-dockerclient client.
// This is needed for compatibility with code that still uses go-dockerclient directly.
func (p *LegacyDockerProvider) GetLegacyClient() *docker.Client {
	return p.client
}

// CreateContainer creates a new container.
func (p *LegacyDockerProvider) CreateContainer(ctx context.Context, config *domain.ContainerConfig, name string) (string, error) {
	p.recordOperation("create_container")

	opts := convertToDockerclientCreateOpts(config, name)
	container, err := p.client.CreateContainer(opts)
	if err != nil {
		p.recordError("create_container")
		return "", WrapContainerError("create", name, err)
	}

	p.logNotice("Created container %s (%s)", container.ID, name)
	return container.ID, nil
}

// StartContainer starts a container.
func (p *LegacyDockerProvider) StartContainer(ctx context.Context, containerID string) error {
	p.recordOperation("start_container")

	if err := p.client.StartContainer(containerID, nil); err != nil {
		p.recordError("start_container")
		return WrapContainerError("start", containerID, err)
	}

	p.logNotice("Started container %s", containerID)
	return nil
}

// StopContainer stops a container.
func (p *LegacyDockerProvider) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	p.recordOperation("stop_container")

	var timeoutSecs uint = 10
	if timeout != nil {
		timeoutSecs = uint(timeout.Seconds())
	}

	if err := p.client.StopContainer(containerID, timeoutSecs); err != nil {
		p.recordError("stop_container")
		return WrapContainerError("stop", containerID, err)
	}

	p.logNotice("Stopped container %s", containerID)
	return nil
}

// RemoveContainer removes a container.
func (p *LegacyDockerProvider) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	p.recordOperation("remove_container")

	opts := docker.RemoveContainerOptions{
		ID:    containerID,
		Force: force,
	}

	if err := p.client.RemoveContainer(opts); err != nil {
		p.recordError("remove_container")
		return WrapContainerError("remove", containerID, err)
	}

	p.logNotice("Removed container %s", containerID)
	return nil
}

// InspectContainer inspects a container.
func (p *LegacyDockerProvider) InspectContainer(ctx context.Context, containerID string) (*domain.Container, error) {
	p.recordOperation("inspect_container")

	container, err := p.client.InspectContainerWithOptions(docker.InspectContainerOptions{
		ID: containerID,
	})
	if err != nil {
		p.recordError("inspect_container")
		return nil, WrapContainerError("inspect", containerID, err)
	}

	return convertFromDockerclientContainer(container), nil
}

// WaitContainer waits for a container to exit.
func (p *LegacyDockerProvider) WaitContainer(ctx context.Context, containerID string) (int64, error) {
	p.recordOperation("wait_container")

	exitCode, err := p.client.WaitContainer(containerID)
	if err != nil {
		p.recordError("wait_container")
		return -1, WrapContainerError("wait", containerID, err)
	}

	return int64(exitCode), nil
}

// GetContainerLogs retrieves container logs.
func (p *LegacyDockerProvider) GetContainerLogs(ctx context.Context, containerID string, opts ContainerLogsOptions) (io.ReadCloser, error) {
	p.recordOperation("get_logs")

	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()

		logOpts := docker.LogsOptions{
			Container:    containerID,
			Stdout:       opts.ShowStdout,
			Stderr:       opts.ShowStderr,
			Tail:         opts.Tail,
			Follow:       opts.Follow,
			OutputStream: pw,
			ErrorStream:  pw,
		}

		if !opts.Since.IsZero() {
			logOpts.Since = opts.Since.Unix()
		}

		if err := p.client.Logs(logOpts); err != nil {
			p.recordError("get_logs")
			pw.CloseWithError(err)
		}
	}()

	return pr, nil
}

// CreateExec creates an exec instance.
func (p *LegacyDockerProvider) CreateExec(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
	p.recordOperation("create_exec")

	opts := docker.CreateExecOptions{
		Container:    containerID,
		Cmd:          config.Cmd,
		AttachStdin:  config.AttachStdin,
		AttachStdout: config.AttachStdout,
		AttachStderr: config.AttachStderr,
		Tty:          config.Tty,
		Env:          config.Env,
		User:         config.User,
		WorkingDir:   config.WorkingDir,
		Privileged:   config.Privileged,
	}

	exec, err := p.client.CreateExec(opts)
	if err != nil {
		p.recordError("create_exec")
		return "", WrapContainerError("create_exec", containerID, err)
	}

	p.logDebug("Created exec instance %s for container %s", exec.ID, containerID)
	return exec.ID, nil
}

// StartExec starts an exec instance.
func (p *LegacyDockerProvider) StartExec(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	p.recordOperation("start_exec")

	// For legacy client, we need to use StartExecNonBlocking to get a connection
	startOpts := docker.StartExecOptions{
		Detach: opts.Detach,
		Tty:    opts.Tty,
	}

	// Note: go-dockerclient StartExec doesn't return a hijacked connection directly
	// We need to use a different approach for legacy
	if err := p.client.StartExec(execID, startOpts); err != nil {
		p.recordError("start_exec")
		return nil, WrapContainerError("start_exec", execID, err)
	}

	p.logDebug("Started exec instance %s", execID)
	// Legacy client doesn't support hijacked responses in the same way
	return nil, nil
}

// InspectExec inspects an exec instance.
func (p *LegacyDockerProvider) InspectExec(ctx context.Context, execID string) (*domain.ExecInspect, error) {
	p.recordOperation("inspect_exec")

	inspect, err := p.client.InspectExec(execID)
	if err != nil {
		p.recordError("inspect_exec")
		return nil, WrapContainerError("inspect_exec", execID, err)
	}

	return &domain.ExecInspect{
		ID:          inspect.ID,
		ContainerID: inspect.ContainerID,
		Running:     inspect.Running,
		ExitCode:    inspect.ExitCode,
		Pid:         0, // go-dockerclient doesn't expose Pid
		ProcessConfig: &domain.ExecProcessConfig{
			User:       inspect.ProcessConfig.User,
			Privileged: inspect.ProcessConfig.Privileged,
			Tty:        inspect.ProcessConfig.Tty,
			Entrypoint: inspect.ProcessConfig.EntryPoint,
			Arguments:  inspect.ProcessConfig.Arguments,
		},
	}, nil
}

// RunExec executes a command and waits for completion.
func (p *LegacyDockerProvider) RunExec(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
	p.recordOperation("run_exec")

	// Create exec
	execID, err := p.CreateExec(ctx, containerID, config)
	if err != nil {
		return -1, err
	}

	// Start exec with output capture
	startOpts := docker.StartExecOptions{
		OutputStream: stdout,
		ErrorStream:  stderr,
		Tty:          config.Tty,
	}

	if err := p.client.StartExec(execID, startOpts); err != nil {
		p.recordError("run_exec")
		return -1, WrapContainerError("run_exec", containerID, err)
	}

	// Inspect for exit code
	inspect, err := p.InspectExec(ctx, execID)
	if err != nil {
		return -1, err
	}

	return inspect.ExitCode, nil
}

// PullImage pulls an image.
func (p *LegacyDockerProvider) PullImage(ctx context.Context, image string) error {
	p.recordOperation("pull_image")

	opts, auth := buildPullOptions(image)
	if err := p.client.PullImage(opts, auth); err != nil {
		p.recordError("pull_image")
		return WrapImageError("pull", image, err)
	}

	p.logNotice("Pulled image %s", image)
	return nil
}

// HasImageLocally checks if an image exists locally.
func (p *LegacyDockerProvider) HasImageLocally(ctx context.Context, image string) (bool, error) {
	p.recordOperation("check_image")

	opts := buildFindLocalImageOptions(image)
	images, err := p.client.ListImages(opts)
	if err != nil {
		p.recordError("check_image")
		return false, WrapImageError("check", image, err)
	}

	return len(images) > 0, nil
}

// EnsureImage ensures an image is available, pulling if necessary.
func (p *LegacyDockerProvider) EnsureImage(ctx context.Context, image string, forcePull bool) error {
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
func (p *LegacyDockerProvider) ConnectNetwork(ctx context.Context, networkID, containerID string) error {
	p.recordOperation("connect_network")

	opts := docker.NetworkConnectionOptions{
		Container: containerID,
	}

	if err := p.client.ConnectNetwork(networkID, opts); err != nil {
		p.recordError("connect_network")
		return WrapContainerError("connect_network", containerID, err)
	}

	p.logNotice("Connected container %s to network %s", containerID, networkID)
	return nil
}

// FindNetworkByName finds networks by name.
func (p *LegacyDockerProvider) FindNetworkByName(ctx context.Context, networkName string) ([]domain.Network, error) {
	p.recordOperation("list_networks")

	networkOpts := docker.NetworkFilterOpts{}
	networkOpts["name"] = map[string]bool{networkName: true}

	networks, err := p.client.FilteredListNetworks(networkOpts)
	if err != nil {
		p.recordError("list_networks")
		return nil, err
	}

	result := make([]domain.Network, len(networks))
	for i, n := range networks {
		result[i] = domain.Network{
			ID:     n.ID,
			Name:   n.Name,
			Driver: n.Driver,
			Scope:  n.Scope,
		}
	}

	return result, nil
}

// SubscribeEvents subscribes to Docker events.
func (p *LegacyDockerProvider) SubscribeEvents(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
	eventCh := make(chan domain.Event, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		listener := make(chan *docker.APIEvents)
		if err := p.client.AddEventListener(listener); err != nil {
			errCh <- err
			return
		}
		defer p.client.RemoveEventListener(listener)

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-listener:
				if !ok {
					return
				}
				domainEvent := domain.Event{
					Type:   event.Type,
					Action: event.Action,
					Actor: domain.EventActor{
						ID:         event.Actor.ID,
						Attributes: event.Actor.Attributes,
					},
					Time:     time.Unix(event.Time, 0),
					TimeNano: event.TimeNano,
				}
				select {
				case eventCh <- domainEvent:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return eventCh, errCh
}

// Info returns Docker system info.
func (p *LegacyDockerProvider) Info(ctx context.Context) (*domain.SystemInfo, error) {
	p.recordOperation("info")

	info, err := p.client.Info()
	if err != nil {
		p.recordError("info")
		return nil, err
	}

	return &domain.SystemInfo{
		ID:              info.ID,
		Containers:     info.Containers,
		Images:         info.Images,
		Driver:         info.Driver,
		KernelVersion:  info.KernelVersion,
		OperatingSystem: info.OperatingSystem,
		OSType:         info.OSType,
		Architecture:   info.Architecture,
		NCPU:           info.NCPU,
		MemTotal:       info.MemTotal,
		ServerVersion:  info.ServerVersion,
		Name:           info.Name,
	}, nil
}

// Ping pings the Docker daemon.
func (p *LegacyDockerProvider) Ping(ctx context.Context) error {
	p.recordOperation("ping")

	if err := p.client.Ping(); err != nil {
		p.recordError("ping")
		return err
	}

	return nil
}

// Close closes the Docker client.
func (p *LegacyDockerProvider) Close() error {
	// go-dockerclient doesn't have a Close method
	return nil
}

// Helper methods

func (p *LegacyDockerProvider) recordOperation(name string) {
	if p.metricsRecorder != nil {
		p.metricsRecorder.RecordDockerOperation(name)
	}
}

func (p *LegacyDockerProvider) recordError(name string) {
	if p.metricsRecorder != nil {
		p.metricsRecorder.RecordDockerError(name)
	}
}

func (p *LegacyDockerProvider) logNotice(format string, args ...interface{}) {
	if p.logger != nil {
		p.logger.Noticef(format, args...)
	}
}

func (p *LegacyDockerProvider) logDebug(format string, args ...interface{}) {
	if p.logger != nil {
		p.logger.Debugf(format, args...)
	}
}

// Conversion functions

func convertToDockerclientCreateOpts(config *domain.ContainerConfig, name string) docker.CreateContainerOptions {
	opts := docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image:        config.Image,
			Cmd:          config.Cmd,
			Entrypoint:   config.Entrypoint,
			Env:          config.Env,
			WorkingDir:   config.WorkingDir,
			User:         config.User,
			Tty:          config.Tty,
			OpenStdin:    config.OpenStdin,
			AttachStdin:  config.AttachStdin,
			AttachStdout: config.AttachStdout,
			AttachStderr: config.AttachStderr,
			Labels:       config.Labels,
		},
	}

	if config.HostConfig != nil {
		opts.HostConfig = &docker.HostConfig{
			AutoRemove:  config.HostConfig.AutoRemove,
			Privileged:  config.HostConfig.Privileged,
			NetworkMode: config.HostConfig.NetworkMode,
			PidMode:     config.HostConfig.PidMode,
			Binds:       config.HostConfig.Binds,
		}
	}

	return opts
}

func convertFromDockerclientContainer(c *docker.Container) *domain.Container {
	if c == nil {
		return nil
	}

	container := &domain.Container{
		ID:      c.ID,
		Created: c.Created,
		Name:    c.Name,
		Image:   c.Image,
		State: domain.ContainerState{
			Running:    c.State.Running,
			Paused:     c.State.Paused,
			Restarting: c.State.Restarting,
			OOMKilled:  c.State.OOMKilled,
			Dead:       c.State.Dead,
			Pid:        c.State.Pid,
			ExitCode:   c.State.ExitCode,
			Error:      c.State.Error,
			StartedAt:  c.State.StartedAt,
			FinishedAt: c.State.FinishedAt,
		},
	}

	if c.Config != nil {
		container.Config = &domain.ContainerConfig{
			Hostname:   c.Config.Hostname,
			User:       c.Config.User,
			Tty:        c.Config.Tty,
			OpenStdin:  c.Config.OpenStdin,
			Env:        c.Config.Env,
			Cmd:        c.Config.Cmd,
			Image:      c.Config.Image,
			WorkingDir: c.Config.WorkingDir,
			Entrypoint: c.Config.Entrypoint,
			Labels:     c.Config.Labels,
		}
	}

	return container
}

// Ensure LegacyDockerProvider implements DockerProvider
var _ DockerProvider = (*LegacyDockerProvider)(nil)
