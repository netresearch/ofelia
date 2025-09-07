package core

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// DockerClientConfig holds configuration for the optimized Docker client
type DockerClientConfig struct {
	// Connection pooling settings
	MaxIdleConns        int           `json:"maxIdleConns"`
	MaxIdleConnsPerHost int           `json:"maxIdleConnsPerHost"`
	MaxConnsPerHost     int           `json:"maxConnsPerHost"`
	IdleConnTimeout     time.Duration `json:"idleConnTimeout"`

	// Timeouts
	DialTimeout           time.Duration `json:"dialTimeout"`
	ResponseHeaderTimeout time.Duration `json:"responseHeaderTimeout"`
	RequestTimeout        time.Duration `json:"requestTimeout"`

	// Circuit breaker settings
	EnableCircuitBreaker  bool          `json:"enableCircuitBreaker"`
	FailureThreshold      int           `json:"failureThreshold"`
	RecoveryTimeout       time.Duration `json:"recoveryTimeout"`
	MaxConcurrentRequests int           `json:"maxConcurrentRequests"`
}

// DefaultDockerClientConfig returns sensible defaults for high-performance Docker operations
func DefaultDockerClientConfig() *DockerClientConfig {
	return &DockerClientConfig{
		// Connection pooling - optimized for concurrent job execution
		MaxIdleConns:        100, // Support up to 100 idle connections
		MaxIdleConnsPerHost: 50,  // 50 idle connections per Docker daemon
		MaxConnsPerHost:     100, // Total 100 connections per Docker daemon
		IdleConnTimeout:     90 * time.Second,

		// Timeouts - balanced for responsiveness vs reliability
		DialTimeout:           5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		RequestTimeout:        30 * time.Second,

		// Circuit breaker - protect against Docker daemon issues
		EnableCircuitBreaker:  true,
		FailureThreshold:      10, // Trip after 10 consecutive failures
		RecoveryTimeout:       30 * time.Second,
		MaxConcurrentRequests: 200, // Limit concurrent requests to prevent overload
	}
}

// DockerCircuitBreakerState represents the state of the circuit breaker
type DockerCircuitBreakerState int

const (
	DockerCircuitClosed DockerCircuitBreakerState = iota
	DockerCircuitOpen
	DockerCircuitHalfOpen
)

// DockerCircuitBreaker implements a simple circuit breaker pattern for Docker API calls
type DockerCircuitBreaker struct {
	config          *DockerClientConfig
	state           DockerCircuitBreakerState
	failureCount    int
	lastFailureTime time.Time
	mutex           sync.RWMutex
	concurrentReqs  int64
	logger          Logger
}

// NewDockerCircuitBreaker creates a new circuit breaker
func NewDockerCircuitBreaker(config *DockerClientConfig, logger Logger) *DockerCircuitBreaker {
	return &DockerCircuitBreaker{
		config: config,
		state:  DockerCircuitClosed,
		logger: logger,
	}
}

// Execute runs the given function if the circuit breaker allows it
func (cb *DockerCircuitBreaker) Execute(fn func() error) error {
	if !cb.config.EnableCircuitBreaker {
		return fn()
	}

	// Check if we can execute
	if !cb.canExecute() {
		return fmt.Errorf("docker circuit breaker is open")
	}

	// Track concurrent requests
	atomic.AddInt64(&cb.concurrentReqs, 1)
	defer atomic.AddInt64(&cb.concurrentReqs, -1)

	// Execute the function
	err := fn()

	// Record the result
	cb.recordResult(err)

	return err
}

func (cb *DockerCircuitBreaker) canExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	// Check concurrent request limit
	if atomic.LoadInt64(&cb.concurrentReqs) >= int64(cb.config.MaxConcurrentRequests) {
		return false
	}

	switch cb.state {
	case DockerCircuitClosed:
		return true
	case DockerCircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailureTime) > cb.config.RecoveryTimeout {
			cb.mutex.RUnlock()
			cb.mutex.Lock()
			if cb.state == DockerCircuitOpen && time.Since(cb.lastFailureTime) > cb.config.RecoveryTimeout {
				cb.state = DockerCircuitHalfOpen
				cb.logger.Noticef("Docker circuit breaker transitioning to half-open state")
			}
			cb.mutex.Unlock()
			cb.mutex.RLock()
		}
		return cb.state == DockerCircuitHalfOpen
	case DockerCircuitHalfOpen:
		return true
	default:
		return false
	}
}

func (cb *DockerCircuitBreaker) recordResult(err error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if err != nil {
		cb.failureCount++
		cb.lastFailureTime = time.Now()

		if cb.state == DockerCircuitHalfOpen {
			// Failed in half-open state, go back to open
			cb.state = DockerCircuitOpen
			cb.logger.Warningf("Docker circuit breaker opening due to failure in half-open state: %v", err)
		} else if cb.failureCount >= cb.config.FailureThreshold {
			// Too many failures, open the circuit
			cb.state = DockerCircuitOpen
			cb.logger.Warningf("Docker circuit breaker opened after %d failures", cb.failureCount)
		}
	} else {
		// Success
		if cb.state == DockerCircuitHalfOpen {
			// Success in half-open state, close the circuit
			cb.state = DockerCircuitClosed
			cb.failureCount = 0
			cb.logger.Noticef("Docker circuit breaker closed after successful recovery")
		} else if cb.state == DockerCircuitClosed {
			// Reset failure count on success
			cb.failureCount = 0
		}
	}
}

// OptimizedDockerClient wraps the Docker client with performance optimizations
type OptimizedDockerClient struct {
	client         *docker.Client
	config         *DockerClientConfig
	circuitBreaker *DockerCircuitBreaker
	metrics        PerformanceRecorder
	logger         Logger
}

// NewOptimizedDockerClient creates a new Docker client with performance optimizations
func NewOptimizedDockerClient(config *DockerClientConfig, logger Logger, metrics PerformanceRecorder) (*OptimizedDockerClient, error) {
	if config == nil {
		config = DefaultDockerClientConfig()
	}

	// Create optimized HTTP transport
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   config.DialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,

		// Connection pooling settings
		MaxIdleConns:        config.MaxIdleConns,
		MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
		MaxConnsPerHost:     config.MaxConnsPerHost,
		IdleConnTimeout:     config.IdleConnTimeout,

		// Performance settings
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,

		// HTTP/2 settings for better performance
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,

		// Disable compression to reduce CPU overhead
		DisableCompression: false, // Keep compression for slower networks
	}

	// Create HTTP client with timeout
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   config.RequestTimeout,
	}

	// Create Docker client with optimized HTTP client
	client, err := docker.NewClientFromEnv()
	if err != nil {
		return nil, fmt.Errorf("create base docker client: %w", err)
	}

	// Replace the HTTP client with our optimized version
	// Note: This requires access to the internal HTTP client, which may need
	// to be done via reflection or by using a custom endpoint
	client.HTTPClient = httpClient

	// Create circuit breaker
	circuitBreaker := NewDockerCircuitBreaker(config, logger)

	optimizedClient := &OptimizedDockerClient{
		client:         client,
		config:         config,
		circuitBreaker: circuitBreaker,
		metrics:        metrics,
		logger:         logger,
	}

	return optimizedClient, nil
}

// GetClient returns the underlying Docker client
func (c *OptimizedDockerClient) GetClient() *docker.Client {
	return c.client
}

// Info wraps the Docker Info call with circuit breaker and metrics
func (c *OptimizedDockerClient) Info() (*docker.DockerInfo, error) {
	var result *docker.DockerInfo
	var err error

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		if c.metrics != nil {
			if err != nil {
				c.metrics.RecordDockerError("info")
			} else {
				c.metrics.RecordDockerOperation("info")
			}
			c.metrics.RecordDockerLatency("info", duration)
		}
	}()

	err = c.circuitBreaker.Execute(func() error {
		result, err = c.client.Info()
		return err
	})

	if err != nil {
		return result, fmt.Errorf("docker info request failed: %w", err)
	}
	return result, nil
}

// ListContainers wraps the Docker ListContainers call with optimizations
func (c *OptimizedDockerClient) ListContainers(opts docker.ListContainersOptions) ([]docker.APIContainers, error) {
	var result []docker.APIContainers
	var err error

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		if c.metrics != nil {
			if err != nil {
				c.metrics.RecordDockerError("list_containers")
			} else {
				c.metrics.RecordDockerOperation("list_containers")
			}
			c.metrics.RecordDockerLatency("list_containers", duration)
		}
	}()

	err = c.circuitBreaker.Execute(func() error {
		result, err = c.client.ListContainers(opts)
		return err
	})

	if err != nil {
		return result, fmt.Errorf("docker list containers failed: %w", err)
	}
	return result, nil
}

// CreateContainer wraps container creation with optimizations
func (c *OptimizedDockerClient) CreateContainer(opts docker.CreateContainerOptions) (*docker.Container, error) {
	var result *docker.Container
	var err error

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		if c.metrics != nil {
			if err != nil {
				c.metrics.RecordDockerError("create_container")
			} else {
				c.metrics.RecordDockerOperation("create_container")
			}
			c.metrics.RecordDockerLatency("create_container", duration)
		}
	}()

	err = c.circuitBreaker.Execute(func() error {
		result, err = c.client.CreateContainer(opts)
		return err
	})

	if err != nil {
		return result, fmt.Errorf("docker create container failed: %w", err)
	}
	return result, nil
}

// StartContainer wraps container start with optimizations
func (c *OptimizedDockerClient) StartContainer(id string, hostConfig *docker.HostConfig) error {
	var err error

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		if c.metrics != nil {
			if err != nil {
				c.metrics.RecordDockerError("start_container")
			} else {
				c.metrics.RecordDockerOperation("start_container")
			}
			c.metrics.RecordDockerLatency("start_container", duration)
		}
	}()

	err = c.circuitBreaker.Execute(func() error {
		return c.client.StartContainer(id, hostConfig)
	})

	return err
}

// StopContainer wraps container stop with optimizations
func (c *OptimizedDockerClient) StopContainer(id string, timeout uint) error {
	var err error

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		if c.metrics != nil {
			if err != nil {
				c.metrics.RecordDockerError("stop_container")
			} else {
				c.metrics.RecordDockerOperation("stop_container")
			}
			c.metrics.RecordDockerLatency("stop_container", duration)
		}
	}()

	err = c.circuitBreaker.Execute(func() error {
		return c.client.StopContainer(id, timeout)
	})

	return err
}

// GetStats returns performance statistics about the optimized client
func (c *OptimizedDockerClient) GetStats() map[string]interface{} {
	c.circuitBreaker.mutex.RLock()
	defer c.circuitBreaker.mutex.RUnlock()

	return map[string]interface{}{
		"circuit_breaker": map[string]interface{}{
			"state":               c.circuitBreaker.state,
			"failure_count":       c.circuitBreaker.failureCount,
			"concurrent_requests": atomic.LoadInt64(&c.circuitBreaker.concurrentReqs),
		},
		"config": map[string]interface{}{
			"max_idle_conns":          c.config.MaxIdleConns,
			"max_idle_conns_per_host": c.config.MaxIdleConnsPerHost,
			"max_conns_per_host":      c.config.MaxConnsPerHost,
			"dial_timeout":            c.config.DialTimeout,
			"request_timeout":         c.config.RequestTimeout,
		},
	}
}

// Close closes the optimized Docker client and cleans up resources
func (c *OptimizedDockerClient) Close() error {
	// Close the underlying transport to clean up connection pools
	if transport, ok := c.client.HTTPClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}
