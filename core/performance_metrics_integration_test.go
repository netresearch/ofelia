package core

import (
	"strings"
	"testing"
	"time"
)

// TestGlobalPerformanceMetrics verifies global metrics instance exists
func TestGlobalPerformanceMetrics(t *testing.T) {
	t.Parallel()
	if GlobalPerformanceMetrics == nil {
		t.Fatal("GlobalPerformanceMetrics is nil")
	}

	// Verify it implements PerformanceRecorder interface
	var _ PerformanceRecorder = GlobalPerformanceMetrics
}

// TestPerformanceMetricsDockerOperations verifies Docker operation tracking
func TestPerformanceMetricsDockerOperations(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record some Docker operations
	pm.RecordDockerOperation("info")
	pm.RecordDockerOperation("list_containers")
	pm.RecordDockerOperation("info")

	pm.RecordDockerError("create_container")

	pm.RecordDockerLatency("info", 10*time.Millisecond)
	pm.RecordDockerLatency("info", 15*time.Millisecond)
	pm.RecordDockerLatency("list_containers", 20*time.Millisecond)

	// Get Docker metrics
	dockerMetrics := pm.GetDockerMetrics()

	totalOps, ok := dockerMetrics["total_operations"].(int64)
	if !ok || totalOps != 3 {
		t.Errorf("Expected total_operations=3, got %v", dockerMetrics["total_operations"])
	}

	totalErrors, ok := dockerMetrics["total_errors"].(int64)
	if !ok || totalErrors != 1 {
		t.Errorf("Expected total_errors=1, got %v", dockerMetrics["total_errors"])
	}

	errorRate, ok := dockerMetrics["error_rate_percent"].(float64)
	if !ok {
		t.Fatal("error_rate_percent not found")
	}
	// 1 error / 3 operations = 33.33%
	if errorRate < 33.0 || errorRate > 34.0 {
		t.Errorf("Expected error_rate ~33%%, got %.2f%%", errorRate)
	}

	// Verify latencies
	latencies, ok := dockerMetrics["latencies"].(map[string]map[string]any)
	if !ok {
		t.Fatal("Latencies not found")
	}

	infoLatency, exists := latencies["info"]
	if !exists {
		t.Fatal("Info latency not found")
	}

	count, ok := infoLatency["count"].(int64)
	if !ok || count != 2 {
		t.Errorf("Expected info latency count=2, got %v", infoLatency["count"])
	}

	avg, ok := infoLatency["average"].(time.Duration)
	if !ok {
		t.Fatal("Average latency not found")
	}
	// Average of 10ms and 15ms = 12.5ms
	if avg != 12*time.Millisecond+500*time.Microsecond {
		t.Errorf("Expected average latency 12.5ms, got %v", avg)
	}
}

// TestPerformanceMetricsJobExecution verifies job execution tracking
func TestPerformanceMetricsJobExecution(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Schedule some jobs
	pm.RecordJobScheduled("job1")
	pm.RecordJobScheduled("job2")

	// Execute jobs
	pm.RecordJobExecution("job1", 100*time.Millisecond, true)
	pm.RecordJobExecution("job1", 150*time.Millisecond, true)
	pm.RecordJobExecution("job2", 200*time.Millisecond, false)

	// Skip a job
	pm.RecordJobSkipped("job3", "disabled")

	// Get job metrics
	jobMetrics := pm.GetJobMetrics()

	totalScheduled, ok := jobMetrics["total_scheduled"].(int64)
	if !ok || totalScheduled != 2 {
		t.Errorf("Expected total_scheduled=2, got %v", jobMetrics["total_scheduled"])
	}

	totalExecuted, ok := jobMetrics["total_executed"].(int64)
	if !ok || totalExecuted != 3 {
		t.Errorf("Expected total_executed=3, got %v", jobMetrics["total_executed"])
	}

	totalFailed, ok := jobMetrics["total_failed"].(int64)
	if !ok || totalFailed != 1 {
		t.Errorf("Expected total_failed=1, got %v", jobMetrics["total_failed"])
	}

	totalSkipped, ok := jobMetrics["total_skipped"].(int64)
	if !ok || totalSkipped != 1 {
		t.Errorf("Expected total_skipped=1, got %v", jobMetrics["total_skipped"])
	}

	// Verify success rate (2 success / 3 executed = 66.67%)
	successRate, ok := jobMetrics["success_rate_percent"].(float64)
	if !ok {
		t.Fatal("success_rate_percent not found")
	}
	if successRate < 66.0 || successRate > 67.0 {
		t.Errorf("Expected success_rate ~66%%, got %.2f%%", successRate)
	}

	// Verify job details
	jobDetails, ok := jobMetrics["job_details"].(map[string]any)
	if !ok {
		t.Fatal("job_details not found")
	}

	job1, exists := jobDetails["job1"]
	if !exists {
		t.Fatal("job1 details not found")
	}

	job1Map, ok := job1.(map[string]any)
	if !ok {
		t.Fatal("job1 is not a map")
	}

	execCount, ok := job1Map["executions"].(int64)
	if !ok || execCount != 2 {
		t.Errorf("Expected job1 executions=2, got %v", job1Map["executions"])
	}

	avgDuration, ok := job1Map["avg_duration"].(time.Duration)
	if !ok {
		t.Fatal("job1 avg_duration not found")
	}
	// Average of 100ms and 150ms = 125ms
	if avgDuration != 125*time.Millisecond {
		t.Errorf("Expected avg_duration 125ms, got %v", avgDuration)
	}
}

// TestPerformanceMetricsConcurrentJobs verifies concurrent job tracking
func TestPerformanceMetricsConcurrentJobs(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record concurrent jobs
	pm.RecordConcurrentJobs(5)
	pm.RecordConcurrentJobs(10)
	pm.RecordConcurrentJobs(3)
	pm.RecordConcurrentJobs(7)

	// Get system metrics
	metrics := pm.GetMetrics()
	systemMetrics, ok := metrics["system"].(map[string]any)
	if !ok {
		t.Fatal("system metrics not found")
	}

	maxConcurrent, ok := systemMetrics["max_concurrent_jobs"].(int64)
	if !ok || maxConcurrent != 10 {
		t.Errorf("Expected max_concurrent_jobs=10, got %v", systemMetrics["max_concurrent_jobs"])
	}

	currentJobs, ok := systemMetrics["concurrent_jobs"].(int64)
	if !ok || currentJobs != 7 {
		t.Errorf("Expected concurrent_jobs=7 (last value), got %v", systemMetrics["concurrent_jobs"])
	}
}

// TestPerformanceMetricsMemoryUsage verifies memory tracking
func TestPerformanceMetricsMemoryUsage(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record memory usage
	pm.RecordMemoryUsage(1024 * 1024)     // 1MB
	pm.RecordMemoryUsage(5 * 1024 * 1024) // 5MB
	pm.RecordMemoryUsage(2 * 1024 * 1024) // 2MB

	metrics := pm.GetMetrics()
	systemMetrics, ok := metrics["system"].(map[string]any)
	if !ok {
		t.Fatal("system metrics not found")
	}

	peakMemory, ok := systemMetrics["peak_memory_usage"].(int64)
	if !ok || peakMemory != 5*1024*1024 {
		t.Errorf("Expected peak_memory_usage=5MB, got %v", systemMetrics["peak_memory_usage"])
	}

	currentMemory, ok := systemMetrics["current_memory_usage"].(int64)
	if !ok || currentMemory != 2*1024*1024 {
		t.Errorf("Expected current_memory_usage=2MB (last value), got %v", systemMetrics["current_memory_usage"])
	}
}

// TestPerformanceMetricsBufferPoolStats verifies buffer pool stats recording
func TestPerformanceMetricsBufferPoolStats(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record buffer pool stats
	stats := map[string]any{
		"total_gets": int64(100),
		"total_puts": int64(95),
		"hit_rate":   float64(85.5),
		"pool_count": 5,
	}

	pm.RecordBufferPoolStats(stats)

	// Get buffer pool metrics
	metrics := pm.GetMetrics()
	bufferMetrics, ok := metrics["buffer_pool"].(map[string]any)
	if !ok {
		t.Fatal("buffer_pool metrics not found")
	}

	totalGets, ok := bufferMetrics["total_gets"].(int64)
	if !ok || totalGets != 100 {
		t.Errorf("Expected total_gets=100, got %v", bufferMetrics["total_gets"])
	}

	hitRate, ok := bufferMetrics["hit_rate"].(float64)
	if !ok || hitRate != 85.5 {
		t.Errorf("Expected hit_rate=85.5, got %v", bufferMetrics["hit_rate"])
	}
}

// TestPerformanceMetricsCustomMetrics verifies custom metrics
func TestPerformanceMetricsCustomMetrics(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record custom metrics
	pm.RecordCustomMetric("test_metric", "test_value")
	pm.RecordCustomMetric("count_metric", int64(42))

	metrics := pm.GetMetrics()
	customMetrics, ok := metrics["custom"].(map[string]any)
	if !ok {
		t.Fatal("custom metrics not found")
	}

	testValue, ok := customMetrics["test_metric"].(string)
	if !ok || testValue != "test_value" {
		t.Errorf("Expected test_metric='test_value', got %v", customMetrics["test_metric"])
	}

	countValue, ok := customMetrics["count_metric"].(int64)
	if !ok || countValue != 42 {
		t.Errorf("Expected count_metric=42, got %v", customMetrics["count_metric"])
	}
}

// TestPerformanceMetricsReset verifies metrics can be reset
func TestPerformanceMetricsReset(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record some metrics
	pm.RecordDockerOperation("info")
	pm.RecordJobExecution("job1", 100*time.Millisecond, true)
	pm.RecordConcurrentJobs(5)
	pm.RecordMemoryUsage(1024 * 1024)

	// Verify metrics exist
	metrics := pm.GetMetrics()
	dockerMetrics := metrics["docker"].(map[string]any)
	if dockerMetrics["total_operations"].(int64) != 1 {
		t.Error("Metrics not recorded before reset")
	}

	// Reset
	pm.Reset()

	// Verify metrics are cleared
	metrics = pm.GetMetrics()
	dockerMetrics = metrics["docker"].(map[string]any)

	totalOps, ok := dockerMetrics["total_operations"].(int64)
	if !ok || totalOps != 0 {
		t.Errorf("Expected total_operations=0 after reset, got %v", dockerMetrics["total_operations"])
	}

	jobMetrics := metrics["jobs"].(map[string]any)
	totalExecuted, ok := jobMetrics["total_executed"].(int64)
	if !ok || totalExecuted != 0 {
		t.Errorf("Expected total_executed=0 after reset, got %v", jobMetrics["total_executed"])
	}
}

// TestPerformanceMetricsSummaryReport verifies summary report generation
func TestPerformanceMetricsSummaryReport(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record various metrics
	pm.RecordDockerOperation("info")
	pm.RecordDockerOperation("list_containers")
	pm.RecordJobExecution("job1", 100*time.Millisecond, true)
	pm.RecordJobExecution("job2", 200*time.Millisecond, false)

	// Generate summary report
	report := pm.GetSummaryReport()

	if report == "" {
		t.Fatal("GetSummaryReport() returned empty string")
	}

	// Verify report contains expected sections
	if !containsString(report, "Performance Summary") {
		t.Error("Report missing 'Performance Summary' section")
	}
	if !containsString(report, "Docker Operations") {
		t.Error("Report missing 'Docker Operations' section")
	}
	if !containsString(report, "Job Execution") {
		t.Error("Report missing 'Job Execution' section")
	}
	if !containsString(report, "Total Operations") {
		t.Error("Report missing 'Total Operations' field")
	}
}

// TestPerformanceMetricsContainerEvents verifies container event tracking
func TestPerformanceMetricsContainerEvents(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record container events
	pm.RecordContainerEvent()
	pm.RecordContainerEvent()
	pm.RecordContainerMonitorFallback()

	metrics := pm.GetMetrics()
	containerMetrics, ok := metrics["container"].(map[string]any)
	if !ok {
		t.Fatal("container metrics not found")
	}

	totalEvents, ok := containerMetrics["total_events"].(int64)
	if !ok || totalEvents != 2 {
		t.Errorf("Expected total_events=2, got %v", containerMetrics["total_events"])
	}

	fallbacks, ok := containerMetrics["monitor_fallbacks"].(int64)
	if !ok || fallbacks != 1 {
		t.Errorf("Expected monitor_fallbacks=1, got %v", containerMetrics["monitor_fallbacks"])
	}
}

// TestPerformanceMetricsRetries verifies retry tracking
func TestPerformanceMetricsRetries(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record retry attempts
	pm.RecordJobRetry("job1", 1, false) // First attempt failed
	pm.RecordJobRetry("job1", 2, false) // Second attempt failed
	pm.RecordJobRetry("job1", 3, true)  // Third attempt succeeded

	metrics := pm.GetMetrics()
	retryMetrics, ok := metrics["retries"].(map[string]any)
	if !ok {
		t.Fatal("retry metrics not found")
	}

	job1Retries, exists := retryMetrics["job1"]
	if !exists {
		t.Fatal("job1 retry metrics not found")
	}

	job1Map, ok := job1Retries.(map[string]any)
	if !ok {
		t.Fatal("job1 retries is not a map")
	}

	totalAttempts, ok := job1Map["total_attempts"].(int64)
	if !ok || totalAttempts != 3 {
		t.Errorf("Expected total_attempts=3, got %v", job1Map["total_attempts"])
	}

	successful, ok := job1Map["successful_retries"].(int64)
	if !ok || successful != 1 {
		t.Errorf("Expected successful_retries=1, got %v", job1Map["successful_retries"])
	}

	failed, ok := job1Map["failed_retries"].(int64)
	if !ok || failed != 2 {
		t.Errorf("Expected failed_retries=2, got %v", job1Map["failed_retries"])
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// TestPerformanceMetricsConcurrency verifies thread safety
func TestPerformanceMetricsConcurrency(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	const goroutines = 50
	const operations = 100

	done := make(chan bool, goroutines)

	for i := range goroutines {
		go func(id int) {
			for range operations {
				pm.RecordDockerOperation("test")
				pm.RecordDockerLatency("test", time.Millisecond)
				pm.RecordJobExecution("test_job", time.Millisecond, true)
				pm.RecordConcurrentJobs(int64(id))
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	timeout := time.After(10 * time.Second)
	for range goroutines {
		select {
		case <-done:
			// Success
		case <-timeout:
			t.Fatal("Concurrent test timed out")
		}
	}

	// Verify metrics are reasonable
	dockerMetrics := pm.GetDockerMetrics()
	totalOps, ok := dockerMetrics["total_operations"].(int64)
	if !ok || totalOps != int64(goroutines*operations) {
		t.Errorf("Expected total_operations=%d, got %v", goroutines*operations, dockerMetrics["total_operations"])
	}
}
