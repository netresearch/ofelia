package core

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// MockDockerClient provides a mock implementation for testing
type MockDockerClient struct {
	client.APIClient
	infoCalled         bool
	listContainersCalled bool
	createContainerCalled bool
	startContainerCalled bool
	stopContainerCalled bool
	closeCalled        bool
	shouldFail         bool
}

func (m *MockDockerClient) Info(ctx context.Context) (types.Info, error) {
	m.infoCalled = true
	if m.shouldFail {
		return types.Info{}, &mockError{message: "mock info error"}
	}
	return types.Info{
		ID:   "test-docker-info",
		Name: "test-docker",
	}, nil
}

func (m *MockDockerClient) ContainerList(ctx context.Context, options types.ContainerListOptions) ([]types.Container, error) {
	m.listContainersCalled = true
	if m.shouldFail {
		return nil, &mockError{message: "mock container list error"}
	}
	return []types.Container{
		{
			ID:    "test-container-1",
			Names: []string{"/test1"},
			Image: "test-image:latest",
		},
	}, nil
}

func (m *MockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *container.NetworkingConfig, platform *container.Platform, containerName string) (container.CreateResponse, error) {
	m.createContainerCalled = true
	if m.shouldFail {
		return container.CreateResponse{}, &mockError{message: "mock container create error"}
	}
	return container.CreateResponse{
		ID: "test-created-container",
	}, nil
}

func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options types.ContainerStartOptions) error {
	m.startContainerCalled = true
	if m.shouldFail {
		return &mockError{message: "mock container start error"}
	}
	return nil
}

func (m *MockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	m.stopContainerCalled = true
	if m.shouldFail {
		return &mockError{message: "mock container stop error"}
	}
	return nil
}

func (m *MockDockerClient) Close() error {
	m.closeCalled = true
	if m.shouldFail {
		return &mockError{message: "mock close error"}
	}
	return nil
}

type mockError struct {
	message string
}

func (e *mockError) Error() string {
	return e.message
}

// TestNewOptimizedDockerClient tests the constructor for OptimizedDockerClient
func TestNewOptimizedDockerClient(t *testing.T) {
	t.Parallel()

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()

	client, err := NewOptimizedDockerClient(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewOptimizedDockerClient failed: %v", err)
	}

	if client == nil {
		t.Fatal("NewOptimizedDockerClient returned nil client")
	}

	if client.config != config {
		t.Error("Client config not set correctly")
	}

	if client.logger != logger {
		t.Error("Client logger not set correctly")
	}

	if client.metrics != metrics {
		t.Error("Client metrics not set correctly")
	}

	if client.circuitBreaker == nil {
		t.Error("Circuit breaker not initialized")
	}

	// Test with nil config (should use default)
	clientDefault, err := NewOptimizedDockerClient(nil, logger, metrics)
	if err != nil {
		t.Fatalf("NewOptimizedDockerClient with nil config failed: %v", err)
	}
	if clientDefault == nil {
		t.Fatal("NewOptimizedDockerClient with nil config returned nil")
	}
}

// TestOptimizedDockerClientInfo tests the Info method
func TestOptimizedDockerClientInfo(t *testing.T) {
	t.Parallel()

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	mockClient := &MockDockerClient{}

	client := &OptimizedDockerClient{
		client:         mockClient,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	// Test successful Info call
	info, err := client.Info(context.Background())
	if err != nil {
		t.Fatalf("Info failed: %v", err)
	}

	if !mockClient.infoCalled {
		t.Error("Underlying client Info method was not called")
	}

	if info.ID != "test-docker-info" {
		t.Errorf("Expected info ID 'test-docker-info', got '%s'", info.ID)
	}

	// Test Info call with error
	mockClientFail := &MockDockerClient{shouldFail: true}
	clientFail := &OptimizedDockerClient{
		client:         mockClientFail,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	_, err = clientFail.Info(context.Background())
	if err == nil {
		t.Error("Expected Info to fail with mock error")
	}
}

// TestOptimizedDockerClientListContainers tests the ListContainers method
func TestOptimizedDockerClientListContainers(t *testing.T) {
	t.Parallel()

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	mockClient := &MockDockerClient{}

	client := &OptimizedDockerClient{
		client:         mockClient,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	// Test successful ListContainers call
	containers, err := client.ListContainers(context.Background(), types.ContainerListOptions{})
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}

	if !mockClient.listContainersCalled {
		t.Error("Underlying client ContainerList method was not called")
	}

	if len(containers) != 1 {
		t.Errorf("Expected 1 container, got %d", len(containers))
	}

	if containers[0].ID != "test-container-1" {
		t.Errorf("Expected container ID 'test-container-1', got '%s'", containers[0].ID)
	}

	// Test ListContainers call with error
	mockClientFail := &MockDockerClient{shouldFail: true}
	clientFail := &OptimizedDockerClient{
		client:         mockClientFail,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	_, err = clientFail.ListContainers(context.Background(), types.ContainerListOptions{})
	if err == nil {
		t.Error("Expected ListContainers to fail with mock error")
	}
}

// TestOptimizedDockerClientCreateContainer tests the CreateContainer method
func TestOptimizedDockerClientCreateContainer(t *testing.T) {
	t.Parallel()

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	mockClient := &MockDockerClient{}

	client := &OptimizedDockerClient{
		client:         mockClient,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	// Test successful CreateContainer call
	containerConfig := &container.Config{
		Image: "test-image:latest",
		Cmd:   []string{"echo", "test"},
	}

	response, err := client.CreateContainer(context.Background(), containerConfig, nil, nil, nil, "test-container")
	if err != nil {
		t.Fatalf("CreateContainer failed: %v", err)
	}

	if !mockClient.createContainerCalled {
		t.Error("Underlying client ContainerCreate method was not called")
	}

	if response.ID != "test-created-container" {
		t.Errorf("Expected container ID 'test-created-container', got '%s'", response.ID)
	}

	// Test CreateContainer call with error
	mockClientFail := &MockDockerClient{shouldFail: true}
	clientFail := &OptimizedDockerClient{
		client:         mockClientFail,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	_, err = clientFail.CreateContainer(context.Background(), containerConfig, nil, nil, nil, "test-container")
	if err == nil {
		t.Error("Expected CreateContainer to fail with mock error")
	}
}

// TestOptimizedDockerClientStartContainer tests the StartContainer method
func TestOptimizedDockerClientStartContainer(t *testing.T) {
	t.Parallel()

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	mockClient := &MockDockerClient{}

	client := &OptimizedDockerClient{
		client:         mockClient,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	// Test successful StartContainer call
	err := client.StartContainer(context.Background(), "test-container-id", types.ContainerStartOptions{})
	if err != nil {
		t.Fatalf("StartContainer failed: %v", err)
	}

	if !mockClient.startContainerCalled {
		t.Error("Underlying client ContainerStart method was not called")
	}

	// Test StartContainer call with error
	mockClientFail := &MockDockerClient{shouldFail: true}
	clientFail := &OptimizedDockerClient{
		client:         mockClientFail,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	err = clientFail.StartContainer(context.Background(), "test-container-id", types.ContainerStartOptions{})
	if err == nil {
		t.Error("Expected StartContainer to fail with mock error")
	}
}

// TestOptimizedDockerClientStopContainer tests the StopContainer method
func TestOptimizedDockerClientStopContainer(t *testing.T) {
	t.Parallel()

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	mockClient := &MockDockerClient{}

	client := &OptimizedDockerClient{
		client:         mockClient,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	// Test successful StopContainer call
	timeout := 10
	err := client.StopContainer(context.Background(), "test-container-id", container.StopOptions{
		Timeout: &timeout,
	})
	if err != nil {
		t.Fatalf("StopContainer failed: %v", err)
	}

	if !mockClient.stopContainerCalled {
		t.Error("Underlying client ContainerStop method was not called")
	}

	// Test StopContainer call with error
	mockClientFail := &MockDockerClient{shouldFail: true}
	clientFail := &OptimizedDockerClient{
		client:         mockClientFail,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	err = clientFail.StopContainer(context.Background(), "test-container-id", container.StopOptions{
		Timeout: &timeout,
	})
	if err == nil {
		t.Error("Expected StopContainer to fail with mock error")
	}
}

// TestOptimizedDockerClientClose tests the Close method
func TestOptimizedDockerClientClose(t *testing.T) {
	t.Parallel()

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	mockClient := &MockDockerClient{}

	client := &OptimizedDockerClient{
		client:         mockClient,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	// Test successful Close call
	err := client.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !mockClient.closeCalled {
		t.Error("Underlying client Close method was not called")
	}

	// Test Close call with error
	mockClientFail := &MockDockerClient{shouldFail: true}
	clientFail := &OptimizedDockerClient{
		client:         mockClientFail,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	err = clientFail.Close()
	if err == nil {
		t.Error("Expected Close to fail with mock error")
	}
}

// TestOptimizedDockerClientGetClient tests the GetClient method
func TestOptimizedDockerClientGetClient(t *testing.T) {
	t.Parallel()

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	mockClient := &MockDockerClient{}

	client := &OptimizedDockerClient{
		client:         mockClient,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	// Test GetClient returns the underlying client
	underlying := client.GetClient()
	if underlying != mockClient {
		t.Error("GetClient did not return the underlying Docker client")
	}
}

// TestOptimizedDockerClientGetStats tests the GetStats method
func TestOptimizedDockerClientGetStats(t *testing.T) {
	t.Parallel()

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	mockClient := &MockDockerClient{}

	client := &OptimizedDockerClient{
		client:         mockClient,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	// Test GetStats returns statistics
	stats := client.GetStats()
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}

	// Check for expected keys in stats
	if _, exists := stats["circuit_breaker"]; !exists {
		t.Error("Stats should contain 'circuit_breaker' key")
	}

	if _, exists := stats["config"]; !exists {
		t.Error("Stats should contain 'config' key")
	}
}

// TestOptimizedDockerClientCircuitBreakerIntegration tests circuit breaker integration
func TestOptimizedDockerClientCircuitBreakerIntegration(t *testing.T) {
	t.Parallel()

	config := &DockerClientConfig{
		EnableCircuitBreaker:  true,
		FailureThreshold:      2,
		RecoveryTimeout:       100 * time.Millisecond,
		MaxConcurrentRequests: 10,
	}
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	mockClient := &MockDockerClient{shouldFail: true}

	client := &OptimizedDockerClient{
		client:         mockClient,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		circuitBreaker: NewDockerCircuitBreaker(config, logger),
	}

	// Generate failures to trigger circuit breaker
	for i := 0; i < 3; i++ {
		_, err := client.Info(context.Background())
		if err == nil {
			t.Error("Expected Info to fail")
		}
	}

	// Check circuit breaker stats after failures
	stats := client.GetStats()
	cbStats, exists := stats["circuit_breaker"]
	if !exists {
		t.Fatal("Circuit breaker stats not found")
	}

	if cbMap, ok := cbStats.(map[string]interface{}); ok {
		if state, exists := cbMap["state"]; exists {
			if state != "open" {
				t.Error("Circuit breaker should be open after multiple failures")
			}
		}
	}
}