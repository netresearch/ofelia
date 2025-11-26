//go:build integration
// +build integration

package core

import (
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// safeClose wraps client.Close() with panic recovery to handle upstream go-dockerclient issue #911
// The panic occurs during cleanup in event monitoring goroutines: "send on closed channel"
// This is NOT a test failure - tests complete successfully before the panic occurs
// Issue: https://github.com/fsouza/go-dockerclient/issues/911
func safeClose(t *testing.T, client *OptimizedDockerClient) {
	defer func() {
		if r := recover(); r != nil {
			// Known upstream issue - panic during event listener cleanup
			t.Logf("Recovered from panic during cleanup (known upstream go-dockerclient issue #911): %v", r)
		}
	}()
	if err := client.Close(); err != nil {
		t.Logf("Error during client close: %v", err)
	}
}

// TestOptimizedDockerClientCreation verifies optimized client can be created
func TestOptimizedDockerClientCreation(t *testing.T) {
	config := DefaultDockerClientConfig()
	metrics := NewPerformanceMetrics()

	client, err := NewOptimizedDockerClient(config, nil, metrics)
	if err != nil {
		// FAIL if Docker is not available - integration tests REQUIRE Docker
		t.Fatalf("Docker not available (integration tests require Docker daemon): %v", err)
	}

	if client == nil {
		t.Fatal("NewOptimizedDockerClient returned nil client")
	}

	// Verify config
	if client.config != config {
		t.Error("Client config not set correctly")
	}

	// Verify circuit breaker
	if client.circuitBreaker == nil {
		t.Fatal("Circuit breaker not initialized")
	}

	// Verify metrics
	if client.metrics != metrics {
		t.Error("Metrics not set correctly")
	}

	// Cleanup
	safeClose(t, client)
}

// TestOptimizedDockerClientGetClient verifies GetClient returns underlying client
func TestOptimizedDockerClientGetClient(t *testing.T) {
	config := DefaultDockerClientConfig()
	client, err := NewOptimizedDockerClient(config, nil, nil)
	if err != nil {
		t.Fatalf("Docker not available (integration tests require Docker daemon): %v", err)
	}
	defer safeClose(t, client)

	underlyingClient := client.GetClient()
	if underlyingClient == nil {
		t.Fatal("GetClient() returned nil")
	}

	// Verify it's a real Docker client (just check it's not nil, type is already known)
	if underlyingClient == nil {
		t.Error("GetClient() returned nil underlying client")
	}
}

// TestOptimizedDockerClientInfo verifies Info call works with metrics
func TestOptimizedDockerClientInfo(t *testing.T) {
	metrics := NewPerformanceMetrics()
	config := DefaultDockerClientConfig()

	client, err := NewOptimizedDockerClient(config, nil, metrics)
	if err != nil {
		t.Fatalf("Docker not available (integration tests require Docker daemon): %v", err)
	}
	defer safeClose(t, client)

	// Call Info
	info, err := client.Info()
	if err != nil {
		t.Fatalf("Docker Info failed (integration tests require working Docker daemon): %v", err)
	}

	if info == nil {
		t.Fatal("Info() returned nil")
	}

	// Verify metrics were recorded
	dockerMetrics := metrics.GetDockerMetrics()
	totalOps, ok := dockerMetrics["total_operations"].(int64)
	if !ok || totalOps == 0 {
		t.Errorf("Expected total_operations>0, got %v", dockerMetrics["total_operations"])
	}

	latencies, ok := dockerMetrics["latencies"].(map[string]map[string]interface{})
	if !ok {
		t.Fatal("Latencies not recorded")
	}

	if _, exists := latencies["info"]; !exists {
		t.Error("Info latency not recorded")
	}
}

// TestOptimizedDockerClientListContainers verifies ListContainers works
func TestOptimizedDockerClientListContainers(t *testing.T) {
	metrics := NewPerformanceMetrics()
	config := DefaultDockerClientConfig()

	client, err := NewOptimizedDockerClient(config, nil, metrics)
	if err != nil {
		t.Fatalf("Docker not available (integration tests require Docker daemon): %v", err)
	}
	defer safeClose(t, client)

	// List containers
	containers, err := client.ListContainers(docker.ListContainersOptions{})
	if err != nil {
		t.Fatalf("Docker ListContainers failed (integration tests require working Docker daemon): %v", err)
	}

	// containers can be empty, that's fine
	if containers == nil {
		t.Fatal("ListContainers() returned nil")
	}

	// Verify metrics were recorded
	dockerMetrics := metrics.GetDockerMetrics()
	latencies, ok := dockerMetrics["latencies"].(map[string]map[string]interface{})
	if !ok {
		t.Fatal("Latencies not recorded")
	}

	if _, exists := latencies["list_containers"]; !exists {
		t.Error("ListContainers latency not recorded")
	}
}

// TestOptimizedDockerClientCircuitBreaker verifies circuit breaker behavior
func TestOptimizedDockerClientCircuitBreaker(t *testing.T) {
	config := DefaultDockerClientConfig()
	config.EnableCircuitBreaker = true
	config.FailureThreshold = 3

	metrics := NewPerformanceMetrics()
	client, err := NewOptimizedDockerClient(config, nil, metrics)
	if err != nil {
		t.Fatalf("Docker not available (integration tests require Docker daemon): %v", err)
	}
	defer safeClose(t, client)

	// Get stats
	stats := client.GetStats()
	if stats == nil {
		t.Fatal("GetStats() returned nil")
	}

	cbStats, ok := stats["circuit_breaker"].(map[string]interface{})
	if !ok {
		t.Fatal("Circuit breaker stats not found")
	}

	// Verify initial state is closed (0)
	state, ok := cbStats["state"].(DockerCircuitBreakerState)
	if !ok {
		t.Fatal("Circuit breaker state not found")
	}

	if state != DockerCircuitClosed {
		t.Errorf("Expected circuit breaker to be closed initially, got state %v", state)
	}
}

// TestOptimizedDockerClientMetricsIntegration verifies metrics integration
func TestOptimizedDockerClientMetricsIntegration(t *testing.T) {
	metrics := NewPerformanceMetrics()
	config := DefaultDockerClientConfig()

	client, err := NewOptimizedDockerClient(config, nil, metrics)
	if err != nil {
		t.Fatalf("Docker not available (integration tests require Docker daemon): %v", err)
	}
	defer safeClose(t, client)

	// Verify Docker daemon is responsive
	if _, err := client.Info(); err != nil {
		t.Fatalf("Docker daemon not responsive (integration tests require working Docker daemon): %v", err)
	}

	// Perform multiple operations
	for i := 0; i < 5; i++ {
		if _, err := client.Info(); err != nil {
			t.Fatalf("Docker Info failed (integration tests require working Docker daemon): %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify metrics
	dockerMetrics := metrics.GetDockerMetrics()

	totalOps, ok := dockerMetrics["total_operations"].(int64)
	if !ok || totalOps < 5 {
		t.Errorf("Expected total_operations>=5, got %v", dockerMetrics["total_operations"])
	}

	latencies, ok := dockerMetrics["latencies"].(map[string]map[string]interface{})
	if !ok {
		t.Fatal("Latencies not recorded")
	}

	infoLatency, exists := latencies["info"]
	if !exists {
		t.Fatal("Info latency not found")
	}

	count, ok := infoLatency["count"].(int64)
	if !ok || count < 5 {
		t.Errorf("Expected info latency count>=5, got %v", infoLatency["count"])
	}

	// Verify average, min, max are set
	if _, ok := infoLatency["average"].(time.Duration); !ok {
		t.Error("Average latency not set")
	}
	if _, ok := infoLatency["min"].(time.Duration); !ok {
		t.Error("Min latency not set")
	}
	if _, ok := infoLatency["max"].(time.Duration); !ok {
		t.Error("Max latency not set")
	}
}

// TestOptimizedDockerClientAddEventListener verifies event listening works
func TestOptimizedDockerClientAddEventListener(t *testing.T) {
	config := DefaultDockerClientConfig()
	client, err := NewOptimizedDockerClient(config, nil, nil)
	if err != nil {
		t.Fatalf("Docker not available (integration tests require Docker daemon): %v", err)
	}
	defer safeClose(t, client)

	// Create event channel with larger buffer to prevent blocking
	events := make(chan *docker.APIEvents, 100)

	// Add event listener
	err = client.AddEventListenerWithOptions(docker.EventsOptions{
		Filters: map[string][]string{"type": {"container"}},
	}, events)

	if err != nil {
		t.Fatalf("AddEventListenerWithOptions failed (integration tests require working Docker daemon): %v", err)
	}

	// Just verify the method exists and doesn't crash
	// Don't wait for actual events as we don't know if any will occur

	// IMPORTANT: Remove the event listener BEFORE closing the channel
	// go-dockerclient issue #911: internal goroutine may panic with "send on closed channel"
	// if we close the channel while it's still trying to send events
	if err := client.GetClient().RemoveEventListener(events); err != nil {
		t.Logf("Warning: RemoveEventListener failed: %v", err)
	}

	// Give go-dockerclient time to clean up its internal goroutine
	// The goroutine polls periodically to check if listener was removed
	time.Sleep(200 * time.Millisecond)

	// Drain any pending events before closing
drainLoop:
	for {
		select {
		case <-events:
			// Drain event
		default:
			break drainLoop
		}
	}

	// Close the channel to allow proper cleanup
	// Wrap in function with recover to handle potential panic from go-dockerclient issue #911
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from event channel close (go-dockerclient issue #911): %v", r)
			}
		}()
		close(events)
	}()
}

// TestOptimizedDockerClientConnectionPooling verifies connection pooling config
func TestOptimizedDockerClientConnectionPooling(t *testing.T) {
	config := DefaultDockerClientConfig()

	// Verify sensible defaults
	if config.MaxIdleConns != 100 {
		t.Errorf("Expected MaxIdleConns=100, got %d", config.MaxIdleConns)
	}
	if config.MaxIdleConnsPerHost != 50 {
		t.Errorf("Expected MaxIdleConnsPerHost=50, got %d", config.MaxIdleConnsPerHost)
	}
	if config.MaxConnsPerHost != 100 {
		t.Errorf("Expected MaxConnsPerHost=100, got %d", config.MaxConnsPerHost)
	}

	client, err := NewOptimizedDockerClient(config, nil, nil)
	if err != nil {
		t.Fatalf("Docker not available (integration tests require Docker daemon): %v", err)
	}
	defer safeClose(t, client)

	// Verify stats reflect config
	stats := client.GetStats()
	configStats, ok := stats["config"].(map[string]interface{})
	if !ok {
		t.Fatal("Config stats not found")
	}

	if maxIdle, ok := configStats["max_idle_conns"].(int); !ok || maxIdle != 100 {
		t.Errorf("Expected max_idle_conns=100, got %v", configStats["max_idle_conns"])
	}
}

// TestOptimizedDockerClientConcurrency verifies concurrent safety
func TestOptimizedDockerClientConcurrency(t *testing.T) {
	metrics := NewPerformanceMetrics()
	config := DefaultDockerClientConfig()

	client, err := NewOptimizedDockerClient(config, nil, metrics)
	if err != nil {
		t.Fatalf("Docker not available (integration tests require Docker daemon): %v", err)
	}
	defer safeClose(t, client)

	// Verify Docker daemon is responsive
	if _, err := client.Info(); err != nil {
		t.Fatalf("Docker daemon not responsive (integration tests require working Docker daemon): %v", err)
	}

	const goroutines = 10
	const iterations = 5

	done := make(chan bool, goroutines)
	errChan := make(chan error, goroutines*iterations)

	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				if _, err := client.Info(); err != nil {
					errChan <- err
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	timeout := time.After(10 * time.Second)
	for i := 0; i < goroutines; i++ {
		select {
		case <-done:
			// Success
		case err := <-errChan:
			t.Fatalf("Docker operation failed during concurrent test (integration tests require working Docker daemon): %v", err)
		case <-timeout:
			t.Fatal("Concurrent test timed out")
		}
	}

	// Verify metrics are reasonable
	dockerMetrics := metrics.GetDockerMetrics()
	totalOps, ok := dockerMetrics["total_operations"].(int64)
	if !ok || totalOps < int64(goroutines*iterations) {
		t.Errorf("Expected total_operations>=%d, got %v", goroutines*iterations, dockerMetrics["total_operations"])
	}
}
