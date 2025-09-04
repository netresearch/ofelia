package core

import (
	"fmt"
	"io"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// DockerOperations provides a high-level interface for common Docker operations
// with consistent error handling and logging
type DockerOperations struct {
	client          *docker.Client
	logger          Logger
	metricsRecorder MetricsRecorder
}

// NewDockerOperations creates a new Docker operations wrapper
func NewDockerOperations(client *docker.Client, logger Logger, metricsRecorder MetricsRecorder) *DockerOperations {
	return &DockerOperations{
		client:          client,
		logger:          logger,
		metricsRecorder: metricsRecorder,
	}
}

// ContainerLifecycle provides container lifecycle management operations
type ContainerLifecycle struct {
	*DockerOperations
}

// NewContainerLifecycle creates a new container lifecycle manager
func (d *DockerOperations) NewContainerLifecycle() *ContainerLifecycle {
	return &ContainerLifecycle{DockerOperations: d}
}

// InspectContainer inspects a container with consistent error handling
func (cl *ContainerLifecycle) InspectContainer(containerID string) (*docker.Container, error) {
	if cl.metricsRecorder != nil {
		cl.metricsRecorder.RecordDockerOperation("inspect_container")
	}

	container, err := cl.client.InspectContainerWithOptions(docker.InspectContainerOptions{
		ID: containerID,
	})
	if err != nil {
		if cl.metricsRecorder != nil {
			cl.metricsRecorder.RecordDockerError("inspect_container")
		}
		return nil, WrapContainerError("inspect", containerID, err)
	}

	return container, nil
}

// StartContainer starts a container with consistent error handling and metrics
func (cl *ContainerLifecycle) StartContainer(containerID string, hostConfig *docker.HostConfig) error {
	if cl.metricsRecorder != nil {
		cl.metricsRecorder.RecordDockerOperation("start_container")
	}

	if err := cl.client.StartContainer(containerID, hostConfig); err != nil {
		if cl.metricsRecorder != nil {
			cl.metricsRecorder.RecordDockerError("start_container")
		}
		return WrapContainerError("start", containerID, err)
	}

	if cl.logger != nil {
		cl.logger.Noticef("Started container %s", containerID)
	}
	return nil
}

// StopContainer stops a container with timeout and consistent error handling
func (cl *ContainerLifecycle) StopContainer(containerID string, timeout uint) error {
	if cl.metricsRecorder != nil {
		cl.metricsRecorder.RecordDockerOperation("stop_container")
	}

	if err := cl.client.StopContainer(containerID, timeout); err != nil {
		if cl.metricsRecorder != nil {
			cl.metricsRecorder.RecordDockerError("stop_container")
		}
		return WrapContainerError("stop", containerID, err)
	}

	if cl.logger != nil {
		cl.logger.Noticef("Stopped container %s", containerID)
	}
	return nil
}

// RemoveContainer removes a container with consistent error handling
func (cl *ContainerLifecycle) RemoveContainer(containerID string, force bool) error {
	if cl.metricsRecorder != nil {
		cl.metricsRecorder.RecordDockerOperation("remove_container")
	}

	opts := docker.RemoveContainerOptions{
		ID:    containerID,
		Force: force,
	}

	if err := cl.client.RemoveContainer(opts); err != nil {
		if cl.metricsRecorder != nil {
			cl.metricsRecorder.RecordDockerError("remove_container")
		}
		return WrapContainerError("remove", containerID, err)
	}

	if cl.logger != nil {
		cl.logger.Noticef("Removed container %s", containerID)
	}
	return nil
}

// CreateContainer creates a container with consistent error handling
func (cl *ContainerLifecycle) CreateContainer(opts docker.CreateContainerOptions) (*docker.Container, error) {
	if cl.metricsRecorder != nil {
		cl.metricsRecorder.RecordDockerOperation("create_container")
	}

	container, err := cl.client.CreateContainer(opts)
	if err != nil {
		if cl.metricsRecorder != nil {
			cl.metricsRecorder.RecordDockerError("create_container")
		}
		return nil, WrapContainerError("create", opts.Name, err)
	}

	if cl.logger != nil {
		cl.logger.Noticef("Created container %s (%s)", container.ID, opts.Name)
	}
	return container, nil
}

// ImageOperations provides image management operations
type ImageOperations struct {
	*DockerOperations
}

// NewImageOperations creates a new image operations manager
func (d *DockerOperations) NewImageOperations() *ImageOperations {
	return &ImageOperations{DockerOperations: d}
}

// PullImage pulls an image with authentication and consistent error handling
func (imgOps *ImageOperations) PullImage(image string) error {
	if imgOps.metricsRecorder != nil {
		imgOps.metricsRecorder.RecordDockerOperation("pull_image")
	}

	opts, auth := buildPullOptions(image)
	if err := imgOps.client.PullImage(opts, auth); err != nil {
		if imgOps.metricsRecorder != nil {
			imgOps.metricsRecorder.RecordDockerError("pull_image")
		}
		return WrapImageError("pull", image, err)
	}

	if imgOps.logger != nil {
		imgOps.logger.Noticef("Pulled image %s", image)
	}
	return nil
}

// ListImages lists images matching the given image name
func (imgOps *ImageOperations) ListImages(image string) ([]docker.APIImages, error) {
	if imgOps.metricsRecorder != nil {
		imgOps.metricsRecorder.RecordDockerOperation("list_images")
	}

	opts := buildFindLocalImageOptions(image)
	images, err := imgOps.client.ListImages(opts)
	if err != nil {
		if imgOps.metricsRecorder != nil {
			imgOps.metricsRecorder.RecordDockerError("list_images")
		}
		return nil, WrapImageError("list", image, err)
	}

	return images, nil
}

// HasImageLocally checks if an image exists locally
func (imgOps *ImageOperations) HasImageLocally(image string) (bool, error) {
	images, err := imgOps.ListImages(image)
	if err != nil {
		return false, err
	}
	return len(images) > 0, nil
}

// EnsureImage ensures an image is available locally, pulling if necessary
func (imgOps *ImageOperations) EnsureImage(image string, forcePull bool) error {
	var pullError error

	// Pull if forced or if not found locally
	if forcePull {
		if pullError = imgOps.PullImage(image); pullError == nil {
			return nil
		}
	}

	// Check if available locally
	hasImage, checkErr := imgOps.HasImageLocally(image)
	if checkErr == nil && hasImage {
		if imgOps.logger != nil {
			imgOps.logger.Noticef("Found image %s locally", image)
		}
		return nil
	}

	// Try to pull if not found locally and not already attempted
	if !forcePull {
		if pullError = imgOps.PullImage(image); pullError == nil {
			return nil
		}
	}

	// Return the most relevant error
	if pullError != nil {
		return pullError
	}
	return checkErr
}

// LogsOperations provides container log operations
type LogsOperations struct {
	*DockerOperations
}

// NewLogsOperations creates a new logs operations manager
func (d *DockerOperations) NewLogsOperations() *LogsOperations {
	return &LogsOperations{DockerOperations: d}
}

// GetLogs retrieves container logs with consistent error handling
func (lo *LogsOperations) GetLogs(containerID string, opts docker.LogsOptions) error {
	if lo.metricsRecorder != nil {
		lo.metricsRecorder.RecordDockerOperation("get_logs")
	}

	opts.Container = containerID
	if err := lo.client.Logs(opts); err != nil {
		if lo.metricsRecorder != nil {
			lo.metricsRecorder.RecordDockerError("get_logs")
		}
		return WrapContainerError("get_logs", containerID, err)
	}

	return nil
}

// GetLogsSince retrieves container logs since a specific time
func (lo *LogsOperations) GetLogsSince(
	containerID string, since time.Time, stdout, stderr bool, outputStream, errorStream io.Writer,
) error {
	opts := docker.LogsOptions{
		Container:   containerID,
		Stdout:      stdout,
		Stderr:      stderr,
		Since:       since.Unix(),
		RawTerminal: false,
	}

	// Set stream writers
	if outputStream != nil {
		opts.OutputStream = outputStream
	}
	if errorStream != nil {
		opts.ErrorStream = errorStream
	}

	if lo.metricsRecorder != nil {
		lo.metricsRecorder.RecordDockerOperation("get_logs")
	}

	if err := lo.client.Logs(opts); err != nil {
		if lo.metricsRecorder != nil {
			lo.metricsRecorder.RecordDockerError("get_logs")
		}
		return WrapContainerError("get_logs", containerID, err)
	}

	return nil
}

// NetworkOperations provides network management operations
type NetworkOperations struct {
	*DockerOperations
}

// NewNetworkOperations creates a new network operations manager
func (d *DockerOperations) NewNetworkOperations() *NetworkOperations {
	return &NetworkOperations{DockerOperations: d}
}

// ConnectContainerToNetwork connects a container to a network
func (no *NetworkOperations) ConnectContainerToNetwork(containerID, networkID string) error {
	if no.metricsRecorder != nil {
		no.metricsRecorder.RecordDockerOperation("connect_network")
	}

	opts := docker.NetworkConnectionOptions{
		Container: containerID,
	}

	if err := no.client.ConnectNetwork(networkID, opts); err != nil {
		if no.metricsRecorder != nil {
			no.metricsRecorder.RecordDockerError("connect_network")
		}
		return fmt.Errorf("connect container %q to network %q: %w", containerID, networkID, err)
	}

	if no.logger != nil {
		no.logger.Noticef("Connected container %s to network %s", containerID, networkID)
	}
	return nil
}

// FindNetworkByName finds a network by name
func (no *NetworkOperations) FindNetworkByName(networkName string) ([]docker.Network, error) {
	if no.metricsRecorder != nil {
		no.metricsRecorder.RecordDockerOperation("list_networks")
	}

	networkOpts := docker.NetworkFilterOpts{}
	networkOpts["name"] = map[string]bool{networkName: true}

	networks, err := no.client.FilteredListNetworks(networkOpts)
	if err != nil {
		if no.metricsRecorder != nil {
			no.metricsRecorder.RecordDockerError("list_networks")
		}
		return nil, fmt.Errorf("list networks: %w", err)
	}

	return networks, nil
}

// ExecOperations provides container exec operations with consistent error handling
type ExecOperations struct {
	*DockerOperations
}

// NewExecOperations creates a new exec operations manager
func (d *DockerOperations) NewExecOperations() *ExecOperations {
	return &ExecOperations{DockerOperations: d}
}

// CreateExec creates an exec instance with consistent error handling and metrics
func (eo *ExecOperations) CreateExec(opts docker.CreateExecOptions) (*docker.Exec, error) {
	if eo.metricsRecorder != nil {
		eo.metricsRecorder.RecordDockerOperation("create_exec")
	}

	exec, err := eo.client.CreateExec(opts)
	if err != nil {
		if eo.metricsRecorder != nil {
			eo.metricsRecorder.RecordDockerError("create_exec")
		}
		return nil, WrapContainerError("create_exec", opts.Container, err)
	}

	if eo.logger != nil {
		eo.logger.Debugf("Created exec instance %s for container %s", exec.ID, opts.Container)
	}
	return exec, nil
}

// StartExec starts an exec instance with consistent error handling and metrics
func (eo *ExecOperations) StartExec(execID string, opts docker.StartExecOptions) error {
	if eo.metricsRecorder != nil {
		eo.metricsRecorder.RecordDockerOperation("start_exec")
	}

	if err := eo.client.StartExec(execID, opts); err != nil {
		if eo.metricsRecorder != nil {
			eo.metricsRecorder.RecordDockerError("start_exec")
		}
		return fmt.Errorf("start exec %q: %w", execID, err)
	}

	if eo.logger != nil {
		eo.logger.Debugf("Started exec instance %s", execID)
	}
	return nil
}

// InspectExec inspects an exec instance with consistent error handling
func (eo *ExecOperations) InspectExec(execID string) (*docker.ExecInspect, error) {
	if eo.metricsRecorder != nil {
		eo.metricsRecorder.RecordDockerOperation("inspect_exec")
	}

	inspect, err := eo.client.InspectExec(execID)
	if err != nil {
		if eo.metricsRecorder != nil {
			eo.metricsRecorder.RecordDockerError("inspect_exec")
		}
		return nil, fmt.Errorf("inspect exec %q: %w", execID, err)
	}

	return inspect, nil
}
