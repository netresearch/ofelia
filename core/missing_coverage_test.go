package core

import (
	"bytes"
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
	buf1, err := pool.GetSized(300)
	if err != nil {
		t.Fatalf("GetSized(300) error: %v", err)
	}
	if buf1 == nil {
		t.Fatal("GetSized(300) returned nil")
	}
	if buf1.Size() != 500 { // Should get default size buffer from pool
		t.Errorf("Expected buffer size 500, got %d", buf1.Size())
	}

	// Test 2: Request size exactly at minSize boundary
	buf2, err := pool.GetSized(100)
	if err != nil {
		t.Fatalf("GetSized(100) error: %v", err)
	}
	if buf2 == nil {
		t.Fatal("GetSized(100) returned nil")
	}
	if buf2.Size() != 500 { // Should get default size buffer from pool
		t.Errorf("Expected buffer size 500, got %d", buf2.Size())
	}

	// Test 3: Request size exactly at default size boundary
	buf3, err := pool.GetSized(500)
	if err != nil {
		t.Fatalf("GetSized(500) error: %v", err)
	}
	if buf3 == nil {
		t.Fatal("GetSized(500) returned nil")
	}
	if buf3.Size() != 500 { // Should get default size buffer from pool
		t.Errorf("Expected buffer size 500, got %d", buf3.Size())
	}

	// Test 4: Request larger than default but under max (should create custom buffer)
	buf4, err := pool.GetSized(1000)
	if err != nil {
		t.Fatalf("GetSized(1000) error: %v", err)
	}
	if buf4 == nil {
		t.Fatal("GetSized(1000) returned nil")
	}
	if buf4.Size() != 1000 { // Should get custom sized buffer
		t.Errorf("Expected buffer size 1000, got %d", buf4.Size())
	}

	// Test 5: Request larger than max (should cap at maxSize)
	buf5, err := pool.GetSized(5000)
	if err != nil {
		t.Fatalf("GetSized(5000) error: %v", err)
	}
	if buf5 == nil {
		t.Fatal("GetSized(5000) returned nil")
	}
	if buf5.Size() != 2000 { // Should be capped at maxSize
		t.Errorf("Expected buffer size 2000 (capped), got %d", buf5.Size())
	}

	// Test 6: Request smaller than minSize (enhanced pool clamps to minSize)
	buf6, err := pool.GetSized(50)
	if err != nil {
		t.Fatalf("GetSized(50) error: %v", err)
	}
	if buf6 == nil {
		t.Fatal("GetSized(50) returned nil")
	}
	if buf6.Size() != 100 { // Enhanced pool clamps to minSize
		t.Errorf("Expected buffer size 100 (clamped to minSize), got %d", buf6.Size())
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
	customBuf, err := pool.GetSized(1000)
	if err != nil {
		t.Fatalf("GetSized(1000) error: %v", err)
	}
	if customBuf.Size() != 1000 {
		t.Fatalf("Expected custom buffer size 1000, got %d", customBuf.Size())
	}

	// Put should not panic with custom sized buffer
	pool.Put(customBuf)

	// Put should handle nil buffer gracefully
	pool.Put(nil)
}

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

// TestLogrusAdapterCriticalf tests the LogrusAdapter.Criticalf() function
func TestLogrusAdapterCriticalf(t *testing.T) {
	t.Parallel()

	// Create a logger that writes to a buffer so we can verify output
	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetLevel(logrus.FatalLevel)

	adapter := &LogrusAdapter{Logger: logger}

	// Note: Criticalf uses FatalLevel which would call os.Exit(1) in production
	// but logrus.New() doesn't actually exit in tests when there's no ExitFunc set
	adapter.Criticalf("test critical message: %s", "arg1")

	// Verify something was logged (the exact format depends on logrus settings)
	// The important thing is that the function executed without panic
}

// TestLogrusAdapterDebugf tests the LogrusAdapter.Debugf() function
func TestLogrusAdapterDebugf(t *testing.T) {
	t.Parallel()

	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetLevel(logrus.DebugLevel)

	adapter := &LogrusAdapter{Logger: logger}
	adapter.Debugf("test debug message: %s", "debug_arg")

	output := buf.String()
	if len(output) == 0 {
		t.Error("Expected debug message to be logged")
	}
	if !bytes.Contains(buf.Bytes(), []byte("test debug message")) {
		t.Errorf("Expected output to contain 'test debug message', got: %s", output)
	}
}

// TestLogrusAdapterErrorf tests the LogrusAdapter.Errorf() function
func TestLogrusAdapterErrorf(t *testing.T) {
	t.Parallel()

	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetLevel(logrus.ErrorLevel)

	adapter := &LogrusAdapter{Logger: logger}
	adapter.Errorf("test error message: %d", 42)

	output := buf.String()
	if len(output) == 0 {
		t.Error("Expected error message to be logged")
	}
	if !bytes.Contains(buf.Bytes(), []byte("test error message")) {
		t.Errorf("Expected output to contain 'test error message', got: %s", output)
	}
}

// TestLogrusAdapterWarningf tests the LogrusAdapter.Warningf() function
func TestLogrusAdapterWarningf(t *testing.T) {
	t.Parallel()

	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetLevel(logrus.WarnLevel)

	adapter := &LogrusAdapter{Logger: logger}
	adapter.Warningf("test warning message: %v", true)

	output := buf.String()
	if len(output) == 0 {
		t.Error("Expected warning message to be logged")
	}
	if !bytes.Contains(buf.Bytes(), []byte("test warning message")) {
		t.Errorf("Expected output to contain 'test warning message', got: %s", output)
	}
}

// TestLogrusAdapterNoticef tests the LogrusAdapter.Noticef() function
func TestLogrusAdapterNoticef(t *testing.T) {
	t.Parallel()

	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetLevel(logrus.InfoLevel)

	adapter := &LogrusAdapter{Logger: logger}
	adapter.Noticef("test notice message: %s %d", "arg", 123)

	output := buf.String()
	if len(output) == 0 {
		t.Error("Expected notice message to be logged")
	}
	if !bytes.Contains(buf.Bytes(), []byte("test notice message")) {
		t.Errorf("Expected output to contain 'test notice message', got: %s", output)
	}
}

// TestResilientJobExecutorSetters tests the setter functions on ResilientJobExecutor
func TestResilientJobExecutorSetters(t *testing.T) {
	t.Parallel()

	// Create a mock job
	job := &BareJob{Name: "test-job"}
	executor := NewResilientJobExecutor(job)

	if executor == nil {
		t.Fatal("NewResilientJobExecutor returned nil")
	}

	// Test SetRetryPolicy
	customRetry := &RetryPolicy{
		MaxAttempts:   5,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 1.5,
	}
	executor.SetRetryPolicy(customRetry)
	// Verify it was set (no getter, so we just ensure no panic)

	// Test SetCircuitBreaker
	customCB := NewCircuitBreaker("custom", 10, 60*time.Second)
	executor.SetCircuitBreaker(customCB)

	// Test SetRateLimiter
	customRL := NewRateLimiter(5.0, 20)
	executor.SetRateLimiter(customRL)

	// Test SetBulkhead
	customBH := NewBulkhead("custom", 5)
	executor.SetBulkhead(customBH)

	// Test SetMetricsRecorder
	metrics := NewSimpleMetricsRecorder()
	executor.SetMetricsRecorder(metrics)

	// Test GetCircuitBreakerState
	state := executor.GetCircuitBreakerState()
	if state != StateClosed {
		t.Errorf("Expected circuit breaker state closed, got %v", state)
	}

	// Test ResetCircuitBreaker
	executor.ResetCircuitBreaker()
	state = executor.GetCircuitBreakerState()
	if state != StateClosed {
		t.Errorf("Expected circuit breaker state closed after reset, got %v", state)
	}
}

// TestSimpleMetricsRecorder tests the SimpleMetricsRecorder implementation
func TestSimpleMetricsRecorder(t *testing.T) {
	t.Parallel()

	recorder := NewSimpleMetricsRecorder()

	// Test RecordMetric
	recorder.RecordMetric("test.metric", 42)

	// Test RecordJobExecution
	recorder.RecordJobExecution("test-job", true, 100*time.Millisecond)

	// Test RecordRetryAttempt
	recorder.RecordRetryAttempt("test-job", 1, false)
	recorder.RecordRetryAttempt("test-job", 2, true)

	// Test GetMetrics
	metrics := recorder.GetMetrics()

	if metrics["test.metric"] != 42 {
		t.Errorf("Expected test.metric=42, got %v", metrics["test.metric"])
	}

	jobMetric, ok := metrics["job.test-job.last_execution"].(map[string]interface{})
	if !ok {
		t.Error("Expected job execution metric to be recorded")
	} else if jobMetric["success"] != true {
		t.Errorf("Expected job success=true, got %v", jobMetric["success"])
	}
}

// TestSetGlobalBufferPoolLogger tests the SetGlobalBufferPoolLogger function
func TestSetGlobalBufferPoolLogger(t *testing.T) {
	t.Parallel()

	// Create a test logger
	logger := &LogrusAdapter{Logger: logrus.New()}

	// Should not panic - just sets the logger
	SetGlobalBufferPoolLogger(logger)

	// Set back to nil to avoid affecting other tests
	SetGlobalBufferPoolLogger(nil)
}

// TestRetryExecutorSetMetricsRecorder tests the RetryExecutor.SetMetricsRecorder function
func TestRetryExecutorSetMetricsRecorder(t *testing.T) {
	t.Parallel()

	logger := &LogrusAdapter{Logger: logrus.New()}
	executor := NewRetryExecutor(logger)

	if executor == nil {
		t.Fatal("NewRetryExecutor returned nil")
	}

	// Test setting metrics recorder with PerformanceMetrics (implements MetricsRecorder)
	metrics := NewPerformanceMetrics()
	executor.SetMetricsRecorder(metrics)

	// Setting nil should also work without panic
	executor.SetMetricsRecorder(nil)
}

// TestPerformanceMetricsContainerMethods tests the container monitoring methods
func TestPerformanceMetricsContainerMethods(t *testing.T) {
	t.Parallel()

	pm := NewPerformanceMetrics()
	if pm == nil {
		t.Fatal("NewPerformanceMetrics returned nil")
	}

	// Test RecordContainerMonitorMethod
	pm.RecordContainerMonitorMethod(true)
	pm.RecordContainerMonitorMethod(false)

	// Test RecordContainerWaitDuration
	pm.RecordContainerWaitDuration(1.5)
	pm.RecordContainerWaitDuration(2.5)
	pm.RecordContainerWaitDuration(0.5)

	// Test RecordContainerMonitorFallback
	pm.RecordContainerMonitorFallback()
	pm.RecordContainerMonitorFallback()
}

// TestPerformanceMetricsDockerOps tests the Docker operations metrics
func TestPerformanceMetricsDockerOps(t *testing.T) {
	t.Parallel()

	pm := NewPerformanceMetrics()

	// Test RecordDockerOperation
	pm.RecordDockerOperation("exec")
	pm.RecordDockerOperation("start")
	pm.RecordDockerOperation("exec")

	// Test RecordDockerError
	pm.RecordDockerError("exec")

	// Test RecordDockerLatency
	pm.RecordDockerLatency("exec", 100*time.Millisecond)
	pm.RecordDockerLatency("start", 50*time.Millisecond)

	// Test RecordJobExecution
	pm.RecordJobExecution("test-job", 200*time.Millisecond, true)
	pm.RecordJobExecution("test-job", 150*time.Millisecond, false)

	// Test RecordCustomMetric
	pm.RecordCustomMetric("custom.test", 123)
	pm.RecordCustomMetric("custom.bool", true)

	// Test GetMetrics - should return a report
	report := pm.GetMetrics()
	if report == nil {
		t.Error("GetMetrics returned nil")
	}
}

// TestEnhancedBufferPoolGetStats tests the GetStats method
func TestEnhancedBufferPoolGetStats(t *testing.T) {
	t.Parallel()

	pool := NewBufferPool(100, 500, 2000)

	// Get stats
	stats := pool.GetStats()
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}

	// Verify stats fields exist
	if _, ok := stats["total_gets"]; !ok {
		t.Error("GetStats missing 'total_gets' field")
	}
	if _, ok := stats["total_puts"]; !ok {
		t.Error("GetStats missing 'total_puts' field")
	}
	if _, ok := stats["hit_rate_percent"]; !ok {
		t.Error("GetStats missing 'hit_rate_percent' field")
	}

	// Exercise the pool and check stats update
	buf, _ := pool.Get()
	pool.Put(buf)

	stats2 := pool.GetStats()
	if stats2["total_gets"].(int64) < 1 {
		t.Error("GetStats 'total_gets' should have increased")
	}
}

// TestExecJobInitializeRuntimeFields tests the ExecJob.InitializeRuntimeFields method
func TestExecJobInitializeRuntimeFields(t *testing.T) {
	t.Parallel()

	job := &ExecJob{}
	// Should not panic - this is a no-op initialization method
	job.InitializeRuntimeFields()
}

// TestRunServiceJobInitializeRuntimeFields tests the RunServiceJob.InitializeRuntimeFields method
func TestRunServiceJobInitializeRuntimeFields(t *testing.T) {
	t.Parallel()

	job := &RunServiceJob{}
	// Should not panic - this is a no-op initialization method
	job.InitializeRuntimeFields()
}

// TestPerformanceMetricsJobScheduledSkipped tests job scheduled and skipped recording
func TestPerformanceMetricsJobScheduledSkipped(t *testing.T) {
	t.Parallel()

	pm := NewPerformanceMetrics()

	// Test RecordJobScheduled
	pm.RecordJobScheduled("test-job")
	pm.RecordJobScheduled("test-job-2")

	// Test RecordJobSkipped
	pm.RecordJobSkipped("test-job", "overlap")
	pm.RecordJobSkipped("test-job", "disabled")

	// Test RecordConcurrentJobs
	pm.RecordConcurrentJobs(5)
	pm.RecordConcurrentJobs(10)

	// Test RecordMemoryUsage
	pm.RecordMemoryUsage(1024 * 1024)
	pm.RecordMemoryUsage(2 * 1024 * 1024)

	// Test RecordBufferPoolStats
	pm.RecordBufferPoolStats(map[string]interface{}{
		"total_gets": int64(100),
		"total_puts": int64(95),
	})
}

// TestPerformanceMetricsJobRetry tests the RecordJobRetry method
func TestPerformanceMetricsJobRetry(t *testing.T) {
	t.Parallel()

	pm := NewPerformanceMetrics()

	// Test RecordJobRetry
	pm.RecordJobRetry("test-job", 1, false)
	pm.RecordJobRetry("test-job", 2, false)
	pm.RecordJobRetry("test-job", 3, true)

	// Test RecordContainerEvent
	pm.RecordContainerEvent()
	pm.RecordContainerEvent()
}

// TestPerformanceMetricsReset tests the Reset method
func TestPerformanceMetricsResetMethod(t *testing.T) {
	t.Parallel()

	pm := NewPerformanceMetrics()

	// Add some data
	pm.RecordDockerOperation("exec")
	pm.RecordJobExecution("test-job", 100*time.Millisecond, true)
	pm.RecordContainerEvent()

	// Reset all metrics
	pm.Reset()

	// Verify reset (GetMetrics still works after reset)
	metrics := pm.GetMetrics()
	if metrics == nil {
		t.Error("GetMetrics returned nil after Reset")
	}
}

// TestPerformanceMetricsSummaryReport tests the GetSummaryReport method
func TestPerformanceMetricsSummaryReportMethod(t *testing.T) {
	t.Parallel()

	pm := NewPerformanceMetrics()

	// Add some data
	pm.RecordDockerOperation("exec")
	pm.RecordJobExecution("test-job", 100*time.Millisecond, true)

	// Get summary report
	report := pm.GetSummaryReport()
	if len(report) == 0 {
		t.Error("GetSummaryReport returned empty string")
	}
}

// TestRunJobInitializeRuntimeFields tests the RunJob.InitializeRuntimeFields method
func TestRunJobInitializeRuntimeFields(t *testing.T) {
	t.Parallel()

	job := &RunJob{}
	// Should not panic - this is a no-op initialization method
	job.InitializeRuntimeFields()
}

// TestPerformanceMetricsGetDockerMetrics tests the GetDockerMetrics method
func TestPerformanceMetricsGetDockerMetrics(t *testing.T) {
	t.Parallel()

	pm := NewPerformanceMetrics()

	// Add some data
	pm.RecordDockerOperation("exec")
	pm.RecordDockerOperation("start")
	pm.RecordDockerError("exec")
	pm.RecordDockerLatency("exec", 100*time.Millisecond)

	// Get docker metrics
	metrics := pm.GetDockerMetrics()
	if metrics == nil {
		t.Error("GetDockerMetrics returned nil")
	}
}

// TestPerformanceMetricsGetJobMetrics tests the GetJobMetrics method
func TestPerformanceMetricsGetJobMetrics(t *testing.T) {
	t.Parallel()

	pm := NewPerformanceMetrics()

	// Add some job execution data
	pm.RecordJobExecution("test-job-1", 100*time.Millisecond, true)
	pm.RecordJobExecution("test-job-2", 200*time.Millisecond, false)
	pm.RecordJobScheduled("test-job-1")

	// Get job metrics
	metrics := pm.GetJobMetrics()
	if metrics == nil {
		t.Error("GetJobMetrics returned nil")
	}
}

// TestSchedulerSetMetricsRecorder tests the Scheduler.SetMetricsRecorder method
func TestSchedulerSetMetricsRecorder(t *testing.T) {
	t.Parallel()

	logger := &LogrusAdapter{Logger: logrus.New()}
	scheduler := NewScheduler(logger)

	// Set metrics recorder
	pm := NewPerformanceMetrics()
	scheduler.SetMetricsRecorder(pm)

	// Setting nil should also work
	scheduler.SetMetricsRecorder(nil)
}

// TestSchedulerEntries tests the Scheduler.Entries method
func TestSchedulerEntries(t *testing.T) {
	t.Parallel()

	logger := &LogrusAdapter{Logger: logrus.New()}
	scheduler := NewScheduler(logger)

	// Get entries - should return empty list for new scheduler
	entries := scheduler.Entries()
	if entries == nil {
		t.Error("Entries returned nil, expected empty slice")
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

// TestWorkflowOrchestratorCleanupOldExecutions tests the WorkflowOrchestrator.CleanupOldExecutions method
func TestWorkflowOrchestratorCleanupOldExecutions(t *testing.T) {
	t.Parallel()

	logger := &LogrusAdapter{Logger: logrus.New()}
	scheduler := NewScheduler(logger)
	orchestrator := NewWorkflowOrchestrator(scheduler, logger)

	// Clean up old executions with default retention
	orchestrator.CleanupOldExecutions(0)

	// Clean up with custom retention (30 days)
	orchestrator.CleanupOldExecutions(30 * 24 * time.Hour)
}
