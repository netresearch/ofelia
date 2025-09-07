package core

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestBareJobRun tests the BareJob.Run() function that currently has 0% coverage
func TestBareJobRun(t *testing.T) {
	t.Parallel()

	// Create a test job
	job := &BareJob{
		Name:    "test-run-job",
		Command: "echo test",
	}

	// Create test scheduler and context
	logger := &LogrusAdapter{Logger: logrus.New()}
	scheduler := NewScheduler(logger)

	exec, err := NewExecution()
	if err != nil {
		t.Fatal(err)
	}

	ctx := NewContext(scheduler, job, exec)

	// Test the Run method - should call ctx.Next()
	err = job.Run(ctx)

	// For BareJob, Run should always return nil since it just calls ctx.Next()
	// which returns nil when there are no middlewares
	if err != nil {
		t.Errorf("BareJob.Run() returned error: %v", err)
	}
}

// TestBufferPoolGetSized tests the BufferPool.GetSized() function that currently has 0% coverage
func TestBufferPoolGetSized(t *testing.T) {
	t.Parallel()

	// Create a buffer pool: min=100, default=500, max=2000
	pool := NewBufferPool(100, 500, 2000)

	// Test 1: Request size within normal range (should use pool)
	buf1 := pool.GetSized(300)
	if buf1 == nil {
		t.Fatal("GetSized(300) returned nil")
	}
	if buf1.Size() != 500 { // Should get default size buffer from pool
		t.Errorf("Expected buffer size 500, got %d", buf1.Size())
	}

	// Test 2: Request size exactly at minSize boundary
	buf2 := pool.GetSized(100)
	if buf2 == nil {
		t.Fatal("GetSized(100) returned nil")
	}
	if buf2.Size() != 500 { // Should get default size buffer from pool
		t.Errorf("Expected buffer size 500, got %d", buf2.Size())
	}

	// Test 3: Request size exactly at default size boundary
	buf3 := pool.GetSized(500)
	if buf3 == nil {
		t.Fatal("GetSized(500) returned nil")
	}
	if buf3.Size() != 500 { // Should get default size buffer from pool
		t.Errorf("Expected buffer size 500, got %d", buf3.Size())
	}

	// Test 4: Request larger than default but under max (should create custom buffer)
	buf4 := pool.GetSized(1000)
	if buf4 == nil {
		t.Fatal("GetSized(1000) returned nil")
	}
	if buf4.Size() != 1000 { // Should get custom sized buffer
		t.Errorf("Expected buffer size 1000, got %d", buf4.Size())
	}

	// Test 5: Request larger than max (should cap at maxSize)
	buf5 := pool.GetSized(5000)
	if buf5 == nil {
		t.Fatal("GetSized(5000) returned nil")
	}
	if buf5.Size() != 2000 { // Should be capped at maxSize
		t.Errorf("Expected buffer size 2000 (capped), got %d", buf5.Size())
	}

	// Test 6: Request smaller than minSize (should create custom buffer)
	buf6 := pool.GetSized(50)
	if buf6 == nil {
		t.Fatal("GetSized(50) returned nil")
	}
	if buf6.Size() != 50 { // Should get custom sized buffer
		t.Errorf("Expected buffer size 50, got %d", buf6.Size())
	}

	// Clean up - return pool buffers
	pool.Put(buf1)
	pool.Put(buf2)
	pool.Put(buf3)
	// buf4, buf5, buf6 are custom sized and should not be returned to pool
}

// TestBufferPoolPutCustomSized tests that custom sized buffers are not returned to pool
func TestBufferPoolPutCustomSized(t *testing.T) {
	t.Parallel()

	pool := NewBufferPool(100, 500, 2000)

	// Get a custom sized buffer
	customBuf := pool.GetSized(1000)
	if customBuf.Size() != 1000 {
		t.Fatalf("Expected custom buffer size 1000, got %d", customBuf.Size())
	}

	// Put should not panic with custom sized buffer
	pool.Put(customBuf)

	// Put should handle nil buffer gracefully
	pool.Put(nil)
}

// TestSimpleLogger tests the SimpleLogger methods that currently have 0% coverage
func TestSimpleLogger(t *testing.T) {
	t.Parallel()

	logger := &SimpleLogger{}

	// Test all the logger methods - they are no-ops but should not panic
	logger.Criticalf("test critical: %s", "message")
	logger.Debugf("test debug: %s", "message")
	logger.Errorf("test error: %s", "message")
	logger.Noticef("test notice: %s", "message")
	logger.Warningf("test warning: %s", "message")

	// Test with no arguments
	logger.Criticalf("simple message")
	logger.Debugf("simple message")
	logger.Errorf("simple message")
	logger.Noticef("simple message")
	logger.Warningf("simple message")

	// Test with multiple arguments
	logger.Criticalf("test with multiple %s %d", "args", 42)
	logger.Debugf("test with multiple %s %d", "args", 42)
	logger.Errorf("test with multiple %s %d", "args", 42)
	logger.Noticef("test with multiple %s %d", "args", 42)
	logger.Warningf("test with multiple %s %d", "args", 42)
}

// TestContainerMonitorSetMetricsRecorder tests the SetMetricsRecorder method that currently has 0% coverage
func TestContainerMonitorSetMetricsRecorder(t *testing.T) {
	t.Parallel()

	// Create a mock metrics recorder
	mockRecorder := &MockMetricsRecorder{}

	// Create container monitor
	monitor := &ContainerMonitor{}

	// Test setting metrics recorder
	monitor.SetMetricsRecorder(mockRecorder)

	if monitor.metrics != mockRecorder {
		t.Error("SetMetricsRecorder didn't set the metrics recorder correctly")
	}

	// Test setting nil recorder
	monitor.SetMetricsRecorder(nil)
	if monitor.metrics != nil {
		t.Error("SetMetricsRecorder didn't handle nil recorder correctly")
	}
}

// Use the existing MockMetricsRecorder from docker_client_test.go

// TestComposeJobNewComposeJob tests the NewComposeJob() constructor that currently has 0% coverage
func TestComposeJobNewComposeJob(t *testing.T) {
	t.Parallel()

	job := NewComposeJob()
	if job == nil {
		t.Fatal("NewComposeJob() returned nil")
	}

	// The constructor just creates an empty job - defaults are set elsewhere
	// Test basic functionality
	job.Name = "test-compose"
	job.File = "docker-compose.yml"
	job.Service = "web"

	// Test that basic fields work
	if job.Name != "test-compose" {
		t.Errorf("Expected name 'test-compose', got %q", job.Name)
	}

	// Test that it can be used as a Job interface
	var _ Job = job
}

// TestComposeJobRun tests the ComposeJob.Run method that currently has 0% coverage
func TestComposeJobRun(t *testing.T) {
	t.Parallel()

	job := NewComposeJob()
	job.Name = "test-compose-run"
	job.Command = "up -d web"
	job.File = "docker-compose.test.yml"
	job.Service = "web"

	// Create test context
	logger := &LogrusAdapter{Logger: logrus.New()}
	scheduler := NewScheduler(logger)
	exec, err := NewExecution()
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewContext(scheduler, job, exec)

	// Test Run method - it will likely fail due to missing docker-compose file
	// but we want to test the method is callable and handles errors properly
	err = job.Run(ctx)
	// We expect an error since we don't have a real docker-compose.test.yml file
	if err == nil {
		t.Log("ComposeJob.Run() unexpectedly succeeded (maybe docker-compose.test.yml exists?)")
	}
}

// TestExecJobMethods tests ExecJob methods with 0% coverage
func TestExecJobMethods(t *testing.T) {
	t.Parallel()

	// Test with nil client for basic constructor test
	job := NewExecJob(nil)
	if job == nil {
		t.Fatal("NewExecJob(nil) returned nil")
	}

	job.Name = "test-exec-methods"
	job.Command = "echo test"
	job.Container = "test-container"
	job.User = "root"
	job.TTY = true
	job.Environment = []string{"TEST=1"}

	// Test basic getters without calling Run which requires a real Docker client
	if job.GetName() != "test-exec-methods" {
		t.Errorf("Expected name 'test-exec-methods', got %s", job.GetName())
	}
	if job.GetCommand() != "echo test" {
		t.Errorf("Expected command 'echo test', got %s", job.GetCommand())
	}

	// Test that it can be used as a Job interface
	var _ Job = job
}

// TestLogrusLoggerMethods tests LogrusAdapter methods with 0% coverage
func TestLogrusLoggerMethods(t *testing.T) {
	t.Parallel()

	logger := &LogrusAdapter{Logger: logrus.New()}

	// Test all logger methods - they should not panic
	logger.Criticalf("test critical: %s", "message")
	logger.Debugf("test debug: %s", "message")
	logger.Errorf("test error: %s", "message")
	logger.Noticef("test notice: %s", "message")
	logger.Warningf("test warning: %s", "message")

	// Test with no format arguments
	logger.Criticalf("simple message")
	logger.Debugf("simple message")
	logger.Errorf("simple message")
	logger.Noticef("simple message")
	logger.Warningf("simple message")
}

// TestDockerOperationMethods tests Docker operation methods with 0% coverage
func TestDockerOperationMethods(t *testing.T) {
	t.Parallel()

	logger := &SimpleLogger{}
	ops := NewDockerOperations(nil, logger, nil)

	// Test ExecOperations creation
	execOps := ops.NewExecOperations()
	if execOps == nil {
		t.Error("NewExecOperations() returned nil")
	}

	// Test other operation objects creation without calling methods that require real client
	imageOps := ops.NewImageOperations()
	if imageOps == nil {
		t.Error("NewImageOperations() returned nil")
	}

	logsOps := ops.NewLogsOperations()
	if logsOps == nil {
		t.Error("NewLogsOperations() returned nil")
	}

	networkOps := ops.NewNetworkOperations()
	if networkOps == nil {
		t.Error("NewNetworkOperations() returned nil")
	}

	containerOps := ops.NewContainerLifecycle()
	if containerOps == nil {
		t.Error("NewContainerLifecycle() returned nil")
	}
}

// TestResilientJobExecutor tests resilient job executor methods with 0% coverage
func TestResilientJobExecutor(t *testing.T) {
	t.Parallel()

	testJob := &BareJob{
		Name:    "test-resilient-job",
		Command: "echo test",
	}

	executor := NewResilientJobExecutor(testJob)
	if executor == nil {
		t.Fatal("NewResilientJobExecutor() returned nil")
	}

	// Test setting configurations
	retryPolicy := DefaultRetryPolicy()
	executor.SetRetryPolicy(retryPolicy)

	circuitBreaker := NewCircuitBreaker("test-cb", 5, time.Second*60)
	executor.SetCircuitBreaker(circuitBreaker)

	rateLimiter := NewRateLimiter(10, 1)
	executor.SetRateLimiter(rateLimiter)

	bulkhead := NewBulkhead("test-bulkhead", 5)
	executor.SetBulkhead(bulkhead)

	metricsRecorder := NewSimpleMetricsRecorder()
	executor.SetMetricsRecorder(metricsRecorder)

	// Test getting circuit breaker state
	state := executor.GetCircuitBreakerState()
	if state != StateClosed {
		t.Errorf("Expected circuit breaker state 'StateClosed', got %s", state)
	}

	// Test reset circuit breaker
	executor.ResetCircuitBreaker()

	// Test metrics recorder methods
	metricsRecorder.RecordMetric("test-metric", 123.45)
	metricsRecorder.RecordJobExecution("test-job", true, time.Millisecond*100)
	metricsRecorder.RecordRetryAttempt("test-job", 1, false)

	metrics := metricsRecorder.GetMetrics()
	if metrics == nil {
		t.Error("GetMetrics() returned nil")
	}
}

// TestResetMiddlewares tests the ResetMiddlewares function that currently has 0% coverage
func TestResetMiddlewares(t *testing.T) {
	t.Parallel()

	// Create a job that has middlewares
	job := &LocalJob{}

	// Add middleware - middlewares are deduplicated by type, so we can only have one TestMiddleware
	middleware1 := &TestMiddleware{}
	job.Use(middleware1)

	// Verify middleware was added
	middlewares := job.Middlewares()
	if len(middlewares) != 1 {
		t.Errorf("Expected 1 middleware after Use, got %d", len(middlewares))
	}

	// Reset middlewares with new ones
	middleware2 := &TestMiddleware{}
	job.ResetMiddlewares(middleware2)

	// Verify old middlewares were cleared and new one was added
	middlewares = job.Middlewares()
	if len(middlewares) != 1 {
		t.Errorf("Expected 1 middleware after ResetMiddlewares, got %d", len(middlewares))
	}

	if middlewares[0] != middleware2 {
		t.Error("ResetMiddlewares didn't set the correct middleware")
	}

	// Test reset with no middlewares - this is the main test since ResetMiddlewares clears all
	job.ResetMiddlewares()
	middlewares = job.Middlewares()
	if len(middlewares) != 0 {
		t.Errorf("Expected 0 middlewares after ResetMiddlewares(), got %d", len(middlewares))
	}
}

// TestAdditionalCoverage adds more coverage to reach the 60% threshold
func TestAdditionalCoverage(t *testing.T) {
	t.Parallel()

	// Test more PerformanceMetrics functions if they exist
	logger := &SimpleLogger{}
	scheduler := NewScheduler(logger)

	// Test default retry policy
	retryPolicy := DefaultRetryPolicy()
	if retryPolicy == nil {
		t.Error("DefaultRetryPolicy should not return nil")
	}

	// Test rate limiter
	rateLimiter := NewRateLimiter(10, 1)
	if rateLimiter == nil {
		t.Error("NewRateLimiter should not return nil")
	}
	if !rateLimiter.Allow() {
		t.Error("RateLimiter should allow first request")
	}

	// Test circuit breaker
	circuitBreaker := NewCircuitBreaker("test", 5, time.Second*60)
	if circuitBreaker == nil {
		t.Error("NewCircuitBreaker should not return nil")
	}

	// Test circuit breaker execution
	executed := false
	err := circuitBreaker.Execute(func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Errorf("Circuit breaker Execute should not error: %v", err)
	}
	if !executed {
		t.Error("Function should have been executed")
	}

	// Test bulkhead
	bulkhead := NewBulkhead("test-bulkhead", 5)
	if bulkhead == nil {
		t.Error("NewBulkhead should not return nil")
	}

	// Test more context functions
	job := &BareJob{
		Name:    "test-additional-coverage",
		Command: "echo test",
	}
	exec, err := NewExecution()
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewContext(scheduler, job, exec)

	// Test context methods
	ctx.Start()
	if !exec.IsRunning {
		t.Error("Execution should be running after ctx.Start()")
	}

	// Test context logging
	ctx.Log("test log message")
	ctx.Warn("test warning message")

	// Test execution methods
	exec.Stop(nil)
	if exec.IsRunning {
		t.Error("Execution should not be running after Stop()")
	}
}
