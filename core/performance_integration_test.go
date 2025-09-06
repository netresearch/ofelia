package core

import (
	"fmt"
	"testing"
	"time"
)

// ExtendedMockMetricsRecorder implements PerformanceRecorder for testing
type ExtendedMockMetricsRecorder struct {
	MockMetricsRecorder
	dockerLatencies  map[string][]time.Duration
	jobExecutions    map[string][]JobExecutionRecord
	customMetrics    map[string]interface{}
}

type JobExecutionRecord struct {
	Duration time.Duration
	Success  bool
}

func NewExtendedMockMetricsRecorder() *ExtendedMockMetricsRecorder {
	return &ExtendedMockMetricsRecorder{
		MockMetricsRecorder: MockMetricsRecorder{},
		dockerLatencies:     make(map[string][]time.Duration),
		jobExecutions:       make(map[string][]JobExecutionRecord),
		customMetrics:       make(map[string]interface{}),
	}
}

func (m *ExtendedMockMetricsRecorder) RecordDockerLatency(operation string, duration time.Duration) {
	m.MockMetricsRecorder.mu.Lock()
	defer m.MockMetricsRecorder.mu.Unlock()
	if m.dockerLatencies == nil {
		m.dockerLatencies = make(map[string][]time.Duration)
	}
	m.dockerLatencies[operation] = append(m.dockerLatencies[operation], duration)
}

func (m *ExtendedMockMetricsRecorder) RecordJobExecution(jobName string, duration time.Duration, success bool) {
	m.MockMetricsRecorder.mu.Lock()
	defer m.MockMetricsRecorder.mu.Unlock()
	if m.jobExecutions == nil {
		m.jobExecutions = make(map[string][]JobExecutionRecord)
	}
	m.jobExecutions[jobName] = append(m.jobExecutions[jobName], JobExecutionRecord{
		Duration: duration,
		Success:  success,
	})
}

func (m *ExtendedMockMetricsRecorder) RecordJobScheduled(jobName string) {}
func (m *ExtendedMockMetricsRecorder) RecordJobSkipped(jobName string, reason string) {}
func (m *ExtendedMockMetricsRecorder) RecordConcurrentJobs(count int64) {}
func (m *ExtendedMockMetricsRecorder) RecordMemoryUsage(bytes int64) {}
func (m *ExtendedMockMetricsRecorder) RecordBufferPoolStats(stats map[string]interface{}) {}

func (m *ExtendedMockMetricsRecorder) RecordCustomMetric(name string, value interface{}) {
	m.MockMetricsRecorder.mu.Lock()
	defer m.MockMetricsRecorder.mu.Unlock()
	if m.customMetrics == nil {
		m.customMetrics = make(map[string]interface{})
	}
	m.customMetrics[name] = value
}

func (m *ExtendedMockMetricsRecorder) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"docker":     m.GetDockerMetrics(),
		"jobs":       m.GetJobMetrics(),
		"custom":     m.customMetrics,
	}
}

func (m *ExtendedMockMetricsRecorder) GetDockerMetrics() map[string]interface{} {
	return map[string]interface{}{
		"operations": m.MockMetricsRecorder.operations, // Use the inherited operations field
		"errors":     m.MockMetricsRecorder.errors,     // Use the inherited errors field
		"latencies":  m.dockerLatencies,
	}
}

func (m *ExtendedMockMetricsRecorder) GetJobMetrics() map[string]interface{} {
	return map[string]interface{}{
		"executions": m.jobExecutions,
	}
}

func (m *ExtendedMockMetricsRecorder) Reset() {
	m.MockMetricsRecorder.mu.Lock()
	defer m.MockMetricsRecorder.mu.Unlock()
	m.MockMetricsRecorder.operations = make(map[string]int)
	m.MockMetricsRecorder.errors = make(map[string]int)
	m.dockerLatencies = make(map[string][]time.Duration)
	m.jobExecutions = make(map[string][]JobExecutionRecord)
	m.customMetrics = make(map[string]interface{})
}

func TestOptimizedDockerClientCreation(t *testing.T) {
	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	
	client, err := NewOptimizedDockerClient(config, logger, metrics)
	if err != nil {
		t.Fatalf("Failed to create optimized Docker client: %v", err)
	}
	
	if client == nil {
		t.Error("Expected optimized Docker client to be created")
	}
	
	if client.config != config {
		t.Error("Expected config to be set correctly")
	}
	
	if client.logger != logger {
		t.Error("Expected logger to be set correctly")
	}
	
	// Test circuit breaker initialization
	if client.circuitBreaker == nil {
		t.Error("Expected circuit breaker to be initialized")
	}
	
	if client.circuitBreaker.config != config {
		t.Error("Expected circuit breaker config to match client config")
	}
}

func TestOptimizedDockerClientConfiguration(t *testing.T) {
	config := DefaultDockerClientConfig()
	
	// Validate default configuration values
	if config.MaxIdleConns != 100 {
		t.Errorf("Expected MaxIdleConns to be 100, got %d", config.MaxIdleConns)
	}
	
	if config.MaxIdleConnsPerHost != 50 {
		t.Errorf("Expected MaxIdleConnsPerHost to be 50, got %d", config.MaxIdleConnsPerHost)
	}
	
	if config.MaxConnsPerHost != 100 {
		t.Errorf("Expected MaxConnsPerHost to be 100, got %d", config.MaxConnsPerHost)
	}
	
	if config.IdleConnTimeout != 90*time.Second {
		t.Errorf("Expected IdleConnTimeout to be 90s, got %v", config.IdleConnTimeout)
	}
	
	if config.DialTimeout != 5*time.Second {
		t.Errorf("Expected DialTimeout to be 5s, got %v", config.DialTimeout)
	}
	
	if config.ResponseHeaderTimeout != 10*time.Second {
		t.Errorf("Expected ResponseHeaderTimeout to be 10s, got %v", config.ResponseHeaderTimeout)
	}
	
	if config.RequestTimeout != 30*time.Second {
		t.Errorf("Expected RequestTimeout to be 30s, got %v", config.RequestTimeout)
	}
	
	if !config.EnableCircuitBreaker {
		t.Error("Expected circuit breaker to be enabled by default")
	}
	
	if config.FailureThreshold != 10 {
		t.Errorf("Expected FailureThreshold to be 10, got %d", config.FailureThreshold)
	}
	
	if config.MaxConcurrentRequests != 200 {
		t.Errorf("Expected MaxConcurrentRequests to be 200, got %d", config.MaxConcurrentRequests)
	}
}

func TestDockerCircuitBreakerInitialization(t *testing.T) {
	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	
	cb := NewDockerCircuitBreaker(config, logger)
	
	if cb == nil {
		t.Error("Expected circuit breaker to be created")
	}
	
	if cb.config != config {
		t.Error("Expected circuit breaker config to be set")
	}
	
	if cb.state != DockerCircuitClosed {
		t.Errorf("Expected initial state to be Closed, got %v", cb.state)
	}
	
	if cb.failureCount != 0 {
		t.Errorf("Expected initial failure count to be 0, got %d", cb.failureCount)
	}
	
	if cb.logger != logger {
		t.Error("Expected logger to be set correctly")
	}
}

func TestDockerCircuitBreakerExecution(t *testing.T) {
	config := &DockerClientConfig{
		EnableCircuitBreaker:  true,
		FailureThreshold:      3,
		RecoveryTimeout:       1 * time.Second,
		MaxConcurrentRequests: 5,
	}
	logger := &MockLogger{}
	
	cb := NewDockerCircuitBreaker(config, logger)
	
	// Test successful execution
	err := cb.Execute(func() error {
		return nil
	})
	
	if err != nil {
		t.Errorf("Expected successful execution, got error: %v", err)
	}
	
	// Test execution that fails
	testError := fmt.Errorf("test error")
	err = cb.Execute(func() error {
		return testError
	})
	
	if err != testError {
		t.Errorf("Expected test error to be returned, got: %v", err)
	}
	
	// Verify failure was recorded
	if cb.failureCount != 1 {
		t.Errorf("Expected failure count to be 1, got %d", cb.failureCount)
	}
}

func TestEnhancedBufferPoolInitialization(t *testing.T) {
	config := DefaultEnhancedBufferPoolConfig()
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)
	
	if pool == nil {
		t.Error("Expected enhanced buffer pool to be created")
	}
	
	if pool.config != config {
		t.Error("Expected config to be set correctly")
	}
	
	if pool.logger != logger {
		t.Error("Expected logger to be set correctly")
	}
	
	if pool.pools == nil {
		t.Error("Expected pools map to be initialized")
	}
	
	if pool.usageTracking == nil {
		t.Error("Expected usage tracking to be initialized")
	}
}

func TestEnhancedBufferPoolConfiguration(t *testing.T) {
	config := DefaultEnhancedBufferPoolConfig()
	
	if config.MinSize != 1024 {
		t.Errorf("Expected MinSize to be 1024, got %d", config.MinSize)
	}
	
	if config.DefaultSize != 256*1024 {
		t.Errorf("Expected DefaultSize to be 256KB, got %d", config.DefaultSize)
	}
	
	if config.MaxSize != maxStreamSize {
		t.Errorf("Expected MaxSize to be maxStreamSize, got %d", config.MaxSize)
	}
	
	if config.PoolSize != 50 {
		t.Errorf("Expected PoolSize to be 50, got %d", config.PoolSize)
	}
	
	if config.MaxPoolSize != 200 {
		t.Errorf("Expected MaxPoolSize to be 200, got %d", config.MaxPoolSize)
	}
	
	if !config.EnableMetrics {
		t.Error("Expected metrics to be enabled by default")
	}
	
	if !config.EnablePrewarming {
		t.Error("Expected prewarming to be enabled by default")
	}
}

func TestEnhancedBufferPoolBasicOperations(t *testing.T) {
	config := DefaultEnhancedBufferPoolConfig()
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)
	
	// Test getting a buffer
	buf := pool.Get()
	if buf == nil {
		t.Error("Expected buffer to be returned")
	}
	
	// Test getting a sized buffer
	buf2 := pool.GetSized(1024)
	if buf2 == nil {
		t.Error("Expected sized buffer to be returned")
	}
	
	if buf2.Size() < 1024 {
		t.Errorf("Expected buffer size to be at least 1024, got %d", buf2.Size())
	}
	
	// Test putting buffers back
	pool.Put(buf)
	pool.Put(buf2)
	
	// Verify metrics are tracked
	stats := pool.GetStats()
	if stats["total_gets"].(int64) < 2 {
		t.Errorf("Expected at least 2 gets, got %v", stats["total_gets"])
	}
	
	if stats["total_puts"].(int64) < 2 {
		t.Errorf("Expected at least 2 puts, got %v", stats["total_puts"])
	}
}

func TestPerformanceMetricsIntegration(t *testing.T) {
	metrics := NewExtendedMockMetricsRecorder()
	
	// Test Docker operation recording
	metrics.RecordDockerOperation("list_containers")
	metrics.RecordDockerLatency("list_containers", 50*time.Millisecond)
	
	// Check using the inherited operations field
	if metrics.MockMetricsRecorder.operations["list_containers"] != 1 {
		t.Errorf("Expected 1 list_containers operation, got %d", 
			metrics.MockMetricsRecorder.operations["list_containers"])
	}
	
	if len(metrics.dockerLatencies["list_containers"]) != 1 {
		t.Errorf("Expected 1 latency record, got %d", 
			len(metrics.dockerLatencies["list_containers"]))
	}
	
	if metrics.dockerLatencies["list_containers"][0] != 50*time.Millisecond {
		t.Errorf("Expected 50ms latency, got %v", 
			metrics.dockerLatencies["list_containers"][0])
	}
	
	// Test job execution recording
	metrics.RecordJobExecution("test-job", 2*time.Second, true)
	
	if len(metrics.jobExecutions["test-job"]) != 1 {
		t.Errorf("Expected 1 job execution record, got %d", 
			len(metrics.jobExecutions["test-job"]))
	}
	
	record := metrics.jobExecutions["test-job"][0]
	if record.Duration != 2*time.Second {
		t.Errorf("Expected 2s duration, got %v", record.Duration)
	}
	
	if !record.Success {
		t.Error("Expected job execution to be marked as successful")
	}
}

func TestOptimizedDockerClientStats(t *testing.T) {
	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()
	
	client, err := NewOptimizedDockerClient(config, logger, metrics)
	if err != nil {
		t.Fatalf("Failed to create optimized Docker client: %v", err)
	}
	
	stats := client.GetStats()
	
	// Verify stats structure
	if stats == nil {
		t.Error("Expected stats to be returned")
	}
	
	cbStats, ok := stats["circuit_breaker"].(map[string]interface{})
	if !ok {
		t.Error("Expected circuit_breaker stats to be present")
	}
	
	if cbStats["state"] != DockerCircuitClosed {
		t.Errorf("Expected circuit breaker state to be Closed, got %v", cbStats["state"])
	}
	
	configStats, ok := stats["config"].(map[string]interface{})
	if !ok {
		t.Error("Expected config stats to be present")
	}
	
	if configStats["max_idle_conns"] != config.MaxIdleConns {
		t.Errorf("Expected max_idle_conns to match config, got %v", 
			configStats["max_idle_conns"])
	}
}

func TestGlobalBufferPoolIntegration(t *testing.T) {
	logger := &MockLogger{}
	
	// Test setting global buffer pool logger
	SetGlobalBufferPoolLogger(logger)
	
	if EnhancedDefaultBufferPool.logger != logger {
		t.Error("Expected global buffer pool logger to be set")
	}
	
	// Test using global buffer pool
	buf := EnhancedDefaultBufferPool.Get()
	if buf == nil {
		t.Error("Expected buffer from global pool")
	}
	
	EnhancedDefaultBufferPool.Put(buf)
	
	stats := EnhancedDefaultBufferPool.GetStats()
	if stats["total_gets"].(int64) < 1 {
		t.Error("Expected at least 1 get from global pool")
	}
}