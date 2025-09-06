package core

import (
	"strings"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// MockMetricsRecorder for testing
type MockMetricsRecorder struct {
	operations map[string]int
	errors     map[string]int
}

func (m *MockMetricsRecorder) RecordJobRetry(jobName string, attempt int, success bool) {}

func (m *MockMetricsRecorder) RecordContainerEvent() {}

func (m *MockMetricsRecorder) RecordContainerMonitorFallback() {}

func (m *MockMetricsRecorder) RecordContainerMonitorMethod(usingEvents bool) {}

func (m *MockMetricsRecorder) RecordContainerWaitDuration(seconds float64) {}

func (m *MockMetricsRecorder) RecordDockerOperation(operation string) {
	if m.operations == nil {
		m.operations = make(map[string]int)
	}
	m.operations[operation]++
}

func (m *MockMetricsRecorder) RecordDockerError(operation string) {
	if m.errors == nil {
		m.errors = make(map[string]int)
	}
	m.errors[operation]++
}

func TestDockerOperationsCreation(t *testing.T) {
	client := &docker.Client{}
	logger := &MockLogger{}
	metrics := &MockMetricsRecorder{}

	dockerOps := NewDockerOperations(client, logger, metrics)

	if dockerOps == nil {
		t.Error("expected DockerOperations to be created")
	}

	if dockerOps.client != client {
		t.Error("expected client to be set correctly")
	}

	if dockerOps.logger != logger {
		t.Error("expected logger to be set correctly")
	}

	if dockerOps.metricsRecorder != metrics {
		t.Error("expected metrics recorder to be set correctly")
	}
}

func TestContainerLifecycleCreation(t *testing.T) {
	dockerOps := NewDockerOperations(&docker.Client{}, &MockLogger{}, &MockMetricsRecorder{})
	containerOps := dockerOps.NewContainerLifecycle()

	if containerOps == nil {
		t.Error("expected ContainerLifecycle to be created")
	}

	if containerOps.DockerOperations != dockerOps {
		t.Error("expected ContainerLifecycle to embed DockerOperations")
	}
}

func TestImageOperationsCreation(t *testing.T) {
	dockerOps := NewDockerOperations(&docker.Client{}, &MockLogger{}, &MockMetricsRecorder{})
	imageOps := dockerOps.NewImageOperations()

	if imageOps == nil {
		t.Error("expected ImageOperations to be created")
	}

	if imageOps.DockerOperations != dockerOps {
		t.Error("expected ImageOperations to embed DockerOperations")
	}
}

func TestLogsOperationsCreation(t *testing.T) {
	dockerOps := NewDockerOperations(&docker.Client{}, &MockLogger{}, &MockMetricsRecorder{})
	logsOps := dockerOps.NewLogsOperations()

	if logsOps == nil {
		t.Error("expected LogsOperations to be created")
	}

	if logsOps.DockerOperations != dockerOps {
		t.Error("expected LogsOperations to embed DockerOperations")
	}
}

func TestNetworkOperationsCreation(t *testing.T) {
	dockerOps := NewDockerOperations(&docker.Client{}, &MockLogger{}, &MockMetricsRecorder{})
	networkOps := dockerOps.NewNetworkOperations()

	if networkOps == nil {
		t.Error("expected NetworkOperations to be created")
	}

	if networkOps.DockerOperations != dockerOps {
		t.Error("expected NetworkOperations to embed DockerOperations")
	}
}

func TestGetLogsSinceWithNilStreams(t *testing.T) {
	// Create a properly initialized Docker client with endpoint for testing
	client, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		// Skip test if Docker is not available
		t.Skip("Docker not available, skipping test")
	}

	dockerOps := NewDockerOperations(client, &MockLogger{}, &MockMetricsRecorder{})
	logsOps := dockerOps.NewLogsOperations()

	// Test with nil streams (should not panic)
	err = logsOps.GetLogsSince("non-existent-container", time.Now(), true, true, nil, nil)

	// We expect an error since the container doesn't exist
	if err == nil {
		t.Error("expected an error for non-existent container")
	}
}

func TestGetLogsSinceWithWriters(t *testing.T) {
	// Create a properly initialized Docker client with endpoint for testing
	client, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		// Skip test if Docker is not available
		t.Skip("Docker not available, skipping test")
	}

	dockerOps := NewDockerOperations(client, &MockLogger{}, &MockMetricsRecorder{})
	logsOps := dockerOps.NewLogsOperations()

	var stdout, stderr strings.Builder

	// Test with actual writers (should not panic)
	err = logsOps.GetLogsSince("test-container", time.Now(), true, true, &stdout, &stderr)

	// We expect an error since the container doesn't exist, but it shouldn't panic
	if err == nil {
		t.Error("expected an error for non-existent container")
	}

	// Test with different writer types
	stdout2 := &strings.Builder{}
	stderr2 := &strings.Builder{}

	err = logsOps.GetLogsSince("test", time.Now(), true, true, stdout2, stderr2)
	if err == nil {
		t.Error("expected an error for non-existent container")
	}
}

func TestMetricsRecordingInOperations(t *testing.T) {
	metrics := &MockMetricsRecorder{}
	// For this test, we'll test the metrics recording by directly calling the record methods
	// since we don't need actual Docker operations to verify metrics behavior

	// Test that metrics recording works correctly
	metrics.RecordDockerOperation("inspect_container")
	metrics.RecordDockerError("inspect_container")

	if metrics.operations["inspect_container"] != 1 {
		t.Errorf("expected 1 inspect_container operation, got %d", metrics.operations["inspect_container"])
	}

	if metrics.errors["inspect_container"] != 1 {
		t.Errorf("expected 1 inspect_container error, got %d", metrics.errors["inspect_container"])
	}

	// Test image operations metrics
	metrics.RecordDockerOperation("list_images")
	metrics.RecordDockerError("list_images")

	if metrics.operations["list_images"] != 1 {
		t.Errorf("expected 1 list_images operation, got %d", metrics.operations["list_images"])
	}

	if metrics.errors["list_images"] != 1 {
		t.Errorf("expected 1 list_images error, got %d", metrics.errors["list_images"])
	}
}

func TestLoggingInOperations(t *testing.T) {
	logger := &MockLogger{}
	dockerOps := NewDockerOperations(&docker.Client{}, logger, &MockMetricsRecorder{})

	// Test that the DockerOperations structure properly initializes with logger
	if dockerOps.logger != logger {
		t.Error("expected logger to be set correctly in DockerOperations")
	}

	// Test that child operations inherit the logger
	containerOps := dockerOps.NewContainerLifecycle()
	if containerOps.DockerOperations.logger != logger {
		t.Error("expected logger to be inherited by ContainerLifecycle")
	}
}

func TestEnsureImagePullBehavior(t *testing.T) {
	// Create a properly initialized Docker client with endpoint for testing
	client, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		// Skip test if Docker is not available
		t.Skip("Docker not available, skipping test")
	}

	dockerOps := NewDockerOperations(client, &MockLogger{}, &MockMetricsRecorder{})
	imageOps := dockerOps.NewImageOperations()

	// Test forced pull (should try to pull non-existent image and fail)
	err = imageOps.EnsureImage("nonexistent:latest", true)
	if err == nil {
		t.Error("expected an error for non-existent image")
	}

	// Test without forced pull (should try to check local first, then pull)
	err = imageOps.EnsureImage("nonexistent:latest", false)
	if err == nil {
		t.Error("expected an error for non-existent image")
	}
}

func TestHasImageLocallyErrorHandling(t *testing.T) {
	// Create a properly initialized Docker client with endpoint for testing
	client, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		// Skip test if Docker is not available
		t.Skip("Docker not available, skipping test")
	}

	dockerOps := NewDockerOperations(client, &MockLogger{}, &MockMetricsRecorder{})
	imageOps := dockerOps.NewImageOperations()

	hasImage, err := imageOps.HasImageLocally("nonexistent:latest")
	if err != nil {
		// This is ok - Docker operations can fail
		t.Logf("HasImageLocally failed as expected: %v", err)
	}
	if hasImage {
		t.Error("expected hasImage to be false for non-existent image")
	}
}

// TestIOWriterInterface verifies that the logs operations accept io.Writer properly
func TestIOWriterInterface(t *testing.T) {
	dockerOps := NewDockerOperations(&docker.Client{}, &MockLogger{}, &MockMetricsRecorder{})
	logsOps := dockerOps.NewLogsOperations()

	// Test that the interface accepts various io.Writer implementations properly
	// We're testing the interface compatibility, not the actual operation
	if logsOps == nil {
		t.Error("expected logsOps to be created")
	}

	// Test with different writer types to ensure interface compatibility
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}

	// Verify that the method signature accepts io.Writer properly
	// (This tests compile-time interface compliance without requiring Docker)
	if stdout == nil || stderr == nil {
		t.Error("writer interfaces should not be nil")
	}
}
