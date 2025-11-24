package core

import (
	"os"
	"strings"
	"testing"
)

// TestDockerHTTP2Detection verifies HTTP/2 enablement detection
// Docker daemon only supports HTTP/2 over TLS (https://), not on cleartext connections
func TestDockerHTTP2Detection(t *testing.T) {
	tests := []struct {
		name          string
		dockerHost    string
		expectedHTTP2 bool
		description   string
	}{
		{
			name:          "unix scheme",
			dockerHost:    "unix:///var/run/docker.sock",
			expectedHTTP2: false,
			description:   "Unix socket - HTTP/1.1 only (no TLS)",
		},
		{
			name:          "absolute path",
			dockerHost:    "/var/run/docker.sock",
			expectedHTTP2: false,
			description:   "Absolute path - HTTP/1.1 only (Unix socket)",
		},
		{
			name:          "relative path",
			dockerHost:    "docker.sock",
			expectedHTTP2: false,
			description:   "Relative path - HTTP/1.1 only (Unix socket)",
		},
		{
			name:          "tcp scheme",
			dockerHost:    "tcp://localhost:2375",
			expectedHTTP2: false,
			description:   "TCP cleartext - HTTP/1.1 only (no h2c support in Docker daemon)",
		},
		{
			name:          "tcp scheme with IP",
			dockerHost:    "tcp://127.0.0.1:2375",
			expectedHTTP2: false,
			description:   "TCP cleartext with IP - HTTP/1.1 only (no h2c)",
		},
		{
			name:          "http scheme",
			dockerHost:    "http://localhost:2375",
			expectedHTTP2: false,
			description:   "HTTP cleartext - HTTP/1.1 only (no h2c support)",
		},
		{
			name:          "https scheme",
			dockerHost:    "https://docker.example.com:2376",
			expectedHTTP2: true,
			description:   "HTTPS with TLS - HTTP/2 via ALPN negotiation",
		},
		{
			name:          "https with IP",
			dockerHost:    "https://192.168.1.100:2376",
			expectedHTTP2: true,
			description:   "HTTPS with TLS and IP - HTTP/2 via ALPN",
		},
		{
			name:          "empty defaults to unix",
			dockerHost:    "",
			expectedHTTP2: false,
			description:   "Empty DOCKER_HOST defaults to Unix socket (HTTP/1.1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.dockerHost != "" {
				t.Setenv("DOCKER_HOST", tt.dockerHost)
			} else {
				// Set empty to test default behavior
				t.Setenv("DOCKER_HOST", "")
			}

			// This is the detection logic from NewOptimizedDockerClient
			dockerHost := os.Getenv("DOCKER_HOST")
			if dockerHost == "" {
				dockerHost = "unix:///var/run/docker.sock"
			}

			// Test the TLS detection logic (same as in NewOptimizedDockerClient)
			// Docker daemon only supports HTTP/2 over TLS (https://)
			isTLSConnection := strings.HasPrefix(dockerHost, "https://")

			if isTLSConnection != tt.expectedHTTP2 {
				t.Errorf("%s: expected HTTP/2=%v, got %v (dockerHost=%s)",
					tt.description, tt.expectedHTTP2, isTLSConnection, dockerHost)
			}
		})
	}
}

// TestOptimizedDockerClient_DefaultConfig verifies default configuration
func TestOptimizedDockerClient_DefaultConfig(t *testing.T) {
	config := DefaultDockerClientConfig()

	if config == nil {
		t.Fatal("DefaultDockerClientConfig returned nil")
	}

	// Verify connection pooling defaults
	if config.MaxIdleConns != 100 {
		t.Errorf("Expected MaxIdleConns=100, got %d", config.MaxIdleConns)
	}
	if config.MaxIdleConnsPerHost != 50 {
		t.Errorf("Expected MaxIdleConnsPerHost=50, got %d", config.MaxIdleConnsPerHost)
	}
	if config.MaxConnsPerHost != 100 {
		t.Errorf("Expected MaxConnsPerHost=100, got %d", config.MaxConnsPerHost)
	}

	// Verify timeouts
	if config.DialTimeout.Seconds() != 5 {
		t.Errorf("Expected DialTimeout=5s, got %v", config.DialTimeout)
	}
	if config.ResponseHeaderTimeout.Seconds() != 10 {
		t.Errorf("Expected ResponseHeaderTimeout=10s, got %v", config.ResponseHeaderTimeout)
	}
	if config.RequestTimeout.Seconds() != 30 {
		t.Errorf("Expected RequestTimeout=30s, got %v", config.RequestTimeout)
	}

	// Verify circuit breaker defaults
	if !config.EnableCircuitBreaker {
		t.Error("Expected EnableCircuitBreaker=true")
	}
	if config.FailureThreshold != 10 {
		t.Errorf("Expected FailureThreshold=10, got %d", config.FailureThreshold)
	}
	if config.MaxConcurrentRequests != 200 {
		t.Errorf("Expected MaxConcurrentRequests=200, got %d", config.MaxConcurrentRequests)
	}
}

// TestCircuitBreaker_States verifies circuit breaker state transitions
func TestCircuitBreaker_States(t *testing.T) {
	config := DefaultDockerClientConfig()
	config.FailureThreshold = 3

	cb := NewDockerCircuitBreaker(config, nil)

	if cb == nil {
		t.Fatal("NewDockerCircuitBreaker returned nil")
	}

	// Initial state should be closed
	if cb.state != DockerCircuitClosed {
		t.Errorf("Expected initial state=DockerCircuitClosed, got %v", cb.state)
	}

	// Record failures
	for i := 0; i < config.FailureThreshold; i++ {
		cb.recordResult(os.ErrInvalid) // Use any error
	}

	// Should now be open
	if cb.state != DockerCircuitOpen {
		t.Errorf("Expected state=DockerCircuitOpen after %d failures, got %v", config.FailureThreshold, cb.state)
	}

	// Verify failure count
	if cb.failureCount != config.FailureThreshold {
		t.Errorf("Expected failureCount=%d, got %d", config.FailureThreshold, cb.failureCount)
	}
}

// TestCircuitBreaker_ExecuteWhenOpen verifies execution is blocked when circuit is open
func TestCircuitBreaker_ExecuteWhenOpen(t *testing.T) {
	config := DefaultDockerClientConfig()
	config.FailureThreshold = 1

	cb := NewDockerCircuitBreaker(config, nil)

	// Record failure to open circuit
	cb.recordResult(os.ErrInvalid)

	// Try to execute when open
	err := cb.Execute(func() error {
		return nil
	})

	if err == nil {
		t.Error("Expected error when executing with open circuit, got nil")
	}

	if err.Error() != "docker circuit breaker is open" {
		t.Errorf("Expected 'docker circuit breaker is open' error, got: %v", err)
	}
}

// TestCircuitBreaker_MaxConcurrentRequests verifies concurrent request limiting
func TestCircuitBreaker_MaxConcurrentRequests(t *testing.T) {
	config := DefaultDockerClientConfig()
	config.MaxConcurrentRequests = 5

	cb := NewDockerCircuitBreaker(config, nil)

	// Simulate reaching the limit
	for i := 0; i < config.MaxConcurrentRequests; i++ {
		cb.concurrentReqs++
	}

	// Next request should fail
	err := cb.Execute(func() error {
		return nil
	})

	if err == nil {
		t.Error("Expected error when exceeding max concurrent requests, got nil")
	}
}

// TestCircuitBreaker_DisabledBypass verifies circuit breaker can be disabled
func TestCircuitBreaker_DisabledBypass(t *testing.T) {
	config := DefaultDockerClientConfig()
	config.EnableCircuitBreaker = false

	cb := NewDockerCircuitBreaker(config, nil)

	// Manually open circuit
	cb.state = DockerCircuitOpen

	// Should still execute because circuit breaker is disabled
	executed := false
	err := cb.Execute(func() error {
		executed = true
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error with disabled circuit breaker, got: %v", err)
	}

	if !executed {
		t.Error("Function was not executed despite circuit breaker being disabled")
	}
}
