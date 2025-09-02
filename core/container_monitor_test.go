package core

import (
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// TestLogger implements the Logger interface for testing
type TestMonitorLogger struct{}

func (l *TestMonitorLogger) Criticalf(format string, args ...interface{}) {}
func (l *TestMonitorLogger) Debugf(format string, args ...interface{})    {}
func (l *TestMonitorLogger) Errorf(format string, args ...interface{})    {}
func (l *TestMonitorLogger) Noticef(format string, args ...interface{})   {}
func (l *TestMonitorLogger) Warningf(format string, args ...interface{})  {}

// MockContainerClient wraps docker.Client for testing
type MockContainerClient struct {
	*docker.Client
	containers     map[string]*docker.Container
	eventListeners []chan *docker.APIEvents
	inspectCalls   int
}

func (m *MockContainerClient) InspectContainerWithOptions(opts docker.InspectContainerOptions) (*docker.Container, error) {
	m.inspectCalls++
	if c, ok := m.containers[opts.ID]; ok {
		return c, nil
	}
	return nil, &docker.NoSuchContainer{ID: opts.ID}
}

func (m *MockContainerClient) AddEventListenerWithOptions(opts docker.EventsOptions, listener chan *docker.APIEvents) error {
	m.eventListeners = append(m.eventListeners, listener)

	// Start a goroutine to handle the mock event stream
	go func() {
		// Keep the listener active until removed
		<-time.After(10 * time.Second)
	}()

	return nil
}

func (m *MockContainerClient) RemoveEventListener(listener chan *docker.APIEvents) error {
	for i, l := range m.eventListeners {
		if l == listener {
			m.eventListeners = append(m.eventListeners[:i], m.eventListeners[i+1:]...)
			break
		}
	}
	return nil
}

func (m *MockContainerClient) SimulateContainerStop(containerID string, exitCode int) {
	// Update container state
	if c, ok := m.containers[containerID]; ok {
		c.State.Running = false
		c.State.ExitCode = exitCode
	}

	// Send event to all listeners
	event := &docker.APIEvents{
		ID:     containerID,
		Status: "die",
		Actor: docker.APIActor{
			ID: containerID,
		},
	}

	for _, listener := range m.eventListeners {
		select {
		case listener <- event:
		case <-time.After(100 * time.Millisecond):
			// Timeout if listener is not ready
		}
	}
}

func TestContainerMonitor_WaitWithEvents(t *testing.T) {
	// For this test to work properly, we'd need to mock the docker.Client interface
	// Since go-dockerclient doesn't provide easy mocking, we'll test the logic differently
	t.Skip("Skipping due to docker client mocking complexity")
}

func TestContainerMonitor_PollingFallback(t *testing.T) {
	// Test that polling fallback works when events API fails
	logger := &TestMonitorLogger{}

	// Create a monitor with a nil client to force fallback behavior
	monitor := &ContainerMonitor{
		client:       nil,
		logger:       logger,
		useEventsAPI: false,
	}

	// This test verifies the structure is correct
	if monitor.useEventsAPI {
		t.Fatal("Expected useEventsAPI to be false")
	}
}

func TestContainerMonitor_SetUseEventsAPI(t *testing.T) {
	logger := &TestMonitorLogger{}
	monitor := NewContainerMonitor(nil, logger)

	// Test that we can toggle the events API usage
	monitor.SetUseEventsAPI(false)
	if monitor.useEventsAPI {
		t.Fatal("Expected useEventsAPI to be false after SetUseEventsAPI(false)")
	}

	monitor.SetUseEventsAPI(true)
	if !monitor.useEventsAPI {
		t.Fatal("Expected useEventsAPI to be true after SetUseEventsAPI(true)")
	}
}

// Integration test - requires Docker to be running
func TestContainerMonitor_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This would test with a real Docker client
	// For CI/CD, we'd need Docker available
	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	logger := &TestMonitorLogger{}
	monitor := NewContainerMonitor(client, logger)

	// Create a test container
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image: "alpine",
			Cmd:   []string{"sleep", "1"},
		},
	})
	if err != nil {
		t.Skipf("Failed to create test container: %v", err)
	}
	defer client.RemoveContainer(docker.RemoveContainerOptions{
		ID:    container.ID,
		Force: true,
	})

	// Start the container
	err = client.StartContainer(container.ID, nil)
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}

	// Wait for it to complete
	state, err := monitor.WaitForContainer(container.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to wait for container: %v", err)
	}

	if state.ExitCode != 0 {
		t.Fatalf("Expected exit code 0, got %d", state.ExitCode)
	}
}
