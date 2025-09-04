package core

import (
	"context"
	"fmt"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// SimpleLogger is a basic logger implementation for when context logger is not available
type SimpleLogger struct{}

func (s *SimpleLogger) Criticalf(format string, args ...interface{}) {}
func (s *SimpleLogger) Debugf(format string, args ...interface{})    {}
func (s *SimpleLogger) Errorf(format string, args ...interface{})    {}
func (s *SimpleLogger) Noticef(format string, args ...interface{})   {}
func (s *SimpleLogger) Warningf(format string, args ...interface{})  {}

// ContainerMonitor provides efficient container monitoring using Docker events
type ContainerMonitor struct {
	client       *docker.Client
	logger       Logger
	useEventsAPI bool
	metrics      MetricsRecorder // Optional metrics recorder
}

// NewContainerMonitor creates a new container monitor
func NewContainerMonitor(client *docker.Client, logger Logger) *ContainerMonitor {
	return &ContainerMonitor{
		client:       client,
		logger:       logger,
		useEventsAPI: true, // Default to using events API
	}
}

// SetUseEventsAPI allows toggling between events API and polling (for compatibility)
func (cm *ContainerMonitor) SetUseEventsAPI(use bool) {
	cm.useEventsAPI = use
}

// SetMetricsRecorder sets the metrics recorder for monitoring metrics
func (cm *ContainerMonitor) SetMetricsRecorder(recorder MetricsRecorder) {
	cm.metrics = recorder
}

// WaitForContainer waits for a container to complete using the most efficient method available
func (cm *ContainerMonitor) WaitForContainer(containerID string, maxRuntime time.Duration) (*docker.State, error) {
	startTime := time.Now()
	var state *docker.State
	var err error

	if cm.useEventsAPI {
		// Record that we're using events API
		if cm.metrics != nil {
			cm.metrics.RecordContainerMonitorMethod(true)
		}

		// Try events API first (most efficient)
		state, err = cm.waitWithEvents(containerID, maxRuntime)
		if err == nil {
			// Record successful event monitoring
			if cm.metrics != nil {
				duration := time.Since(startTime).Seconds()
				cm.metrics.RecordContainerWaitDuration(duration)
			}
			return state, nil
		}

		// Log the error and fall back to polling
		cm.logger.Debugf("Events API failed for container %s: %v, falling back to polling", containerID, err)
		if cm.metrics != nil {
			cm.metrics.RecordContainerMonitorFallback()
		}
	}

	// Record that we're using polling
	if cm.metrics != nil {
		cm.metrics.RecordContainerMonitorMethod(false)
	}

	// Fall back to polling method
	state, err = cm.waitWithPolling(containerID, maxRuntime)

	// Record duration
	if cm.metrics != nil && err == nil {
		duration := time.Since(startTime).Seconds()
		cm.metrics.RecordContainerWaitDuration(duration)
	}

	return state, err
}

// waitWithEvents uses Docker events API for efficient container monitoring
func (cm *ContainerMonitor) waitWithEvents(containerID string, maxRuntime time.Duration) (*docker.State, error) {
	// Create a context with timeout if maxRuntime is specified
	ctx := context.Background()
	var cancel context.CancelFunc
	if maxRuntime > 0 {
		ctx, cancel = context.WithTimeout(ctx, maxRuntime)
		defer cancel()
	}

	// Set up event listener
	eventChan := make(chan *docker.APIEvents, 10)

	// Create event listener options
	opts := docker.EventsOptions{
		Filters: map[string][]string{
			"container": {containerID},
			"event":     {"die", "kill", "stop", "oom"},
		},
	}

	// Start listening for events
	if err := cm.client.AddEventListenerWithOptions(opts, eventChan); err != nil {
		return nil, fmt.Errorf("failed to add event listener: %w", err)
	}
	defer func() {
		if err := cm.client.RemoveEventListener(eventChan); err != nil {
			cm.logger.Warningf("Failed to remove event listener: %v", err)
		}
		close(eventChan)
	}()

	// Check if container is already stopped
	container, err := cm.client.InspectContainerWithOptions(docker.InspectContainerOptions{
		ID:      containerID,
		Context: ctx,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	if !container.State.Running {
		return &container.State, nil
	}

	// Wait for container to stop
	for {
		select {
		case <-ctx.Done():
			// Timeout reached
			if maxRuntime > 0 {
				return nil, ErrMaxTimeRunning
			}
			return nil, fmt.Errorf("container monitoring context canceled: %w", ctx.Err())

		case event, ok := <-eventChan:
			if !ok {
				return nil, fmt.Errorf("event channel closed unexpectedly")
			}

			// Container stopped, get final state
			if event.ID == containerID || event.Actor.ID == containerID {
				// Record event received
				if cm.metrics != nil {
					cm.metrics.RecordContainerEvent()
				}

				container, err := cm.client.InspectContainerWithOptions(docker.InspectContainerOptions{
					ID:      containerID,
					Context: ctx,
				})
				if err != nil {
					return nil, fmt.Errorf("failed to inspect container after event: %w", err)
				}

				return &container.State, nil
			}
		}
	}
}

// waitWithPolling falls back to the original polling method (for compatibility)
func (cm *ContainerMonitor) waitWithPolling(containerID string, maxRuntime time.Duration) (*docker.State, error) {
	const pollInterval = 100 * time.Millisecond
	var elapsed time.Duration

	for {
		time.Sleep(pollInterval)
		elapsed += pollInterval

		if maxRuntime > 0 && elapsed > maxRuntime {
			return nil, ErrMaxTimeRunning
		}

		container, err := cm.client.InspectContainerWithOptions(docker.InspectContainerOptions{
			ID: containerID,
		})
		if err != nil {
			return nil, fmt.Errorf("inspect container %q: %w", containerID, err)
		}

		if !container.State.Running {
			return &container.State, nil
		}
	}
}

// MonitorContainerLogs streams container logs efficiently
func (cm *ContainerMonitor) MonitorContainerLogs(containerID string, stdout, stderr bool) error {
	opts := docker.LogsOptions{
		Container:    containerID,
		OutputStream: nil, // Will be set by caller
		ErrorStream:  nil, // Will be set by caller
		Follow:       true,
		Stdout:       stdout,
		Stderr:       stderr,
		Timestamps:   false,
	}

	if err := cm.client.Logs(opts); err != nil {
		return fmt.Errorf("failed to get logs for container %s: %w", containerID, err)
	}
	return nil
}
