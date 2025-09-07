package core

import (
	"testing"
	"time"
)

// TestNewExtendedMockMetricsRecorder tests the extended mock metrics recorder
func TestNewExtendedMockMetricsRecorder(t *testing.T) {
	t.Parallel()

	recorder := NewExtendedMockMetricsRecorder()
	if recorder == nil {
		t.Fatal("NewExtendedMockMetricsRecorder returned nil")
	}

	if recorder.dockerMetrics == nil {
		t.Fatal("Docker metrics map not initialized")
	}

	if recorder.jobMetrics == nil {
		t.Fatal("Job metrics map not initialized")
	}

	if recorder.systemMetrics == nil {
		t.Fatal("System metrics map not initialized")
	}
}

// TestRecordDockerOperation tests recording Docker operations
func TestRecordDockerOperation(t *testing.T) {
	t.Parallel()

	recorder := NewExtendedMockMetricsRecorder()

	// Test recording various Docker operations
	operations := []string{"list_containers", "create_container", "start_container", "stop_container", "inspect_container"}

	for _, op := range operations {
		recorder.RecordDockerOperation(op)
	}

	dockerMetrics := recorder.GetDockerMetrics()
	if dockerMetrics == nil {
		t.Fatal("GetDockerMetrics returned nil")
	}

	// Check that all operations were recorded
	for _, op := range operations {
		if count, exists := dockerMetrics[op]; !exists {
			t.Errorf("Operation %s not recorded", op)
		} else if count != 1 {
			t.Errorf("Expected count 1 for operation %s, got %v", op, count)
		}
	}

	// Test multiple recordings of same operation
	recorder.RecordDockerOperation("list_containers")
	dockerMetrics = recorder.GetDockerMetrics()
	if count := dockerMetrics["list_containers"]; count != 2 {
		t.Errorf("Expected count 2 for list_containers after second recording, got %v", count)
	}
}

// TestRecordDockerLatency tests recording Docker operation latencies
func TestRecordDockerLatency(t *testing.T) {
	t.Parallel()

	recorder := NewExtendedMockMetricsRecorder()

	// Test recording latencies
	testCases := []struct {
		operation string
		latency   time.Duration
	}{
		{"list_containers", 50 * time.Millisecond},
		{"create_container", 200 * time.Millisecond},
		{"start_container", 100 * time.Millisecond},
		{"list_containers", 75 * time.Millisecond}, // Second recording for same operation
	}

	for _, tc := range testCases {
		recorder.RecordDockerLatency(tc.operation, tc.latency)
	}

	dockerMetrics := recorder.GetDockerMetrics()

	// Check latency recordings
	if latencies, exists := dockerMetrics["list_containers_latency"]; exists {
		if latencySlice, ok := latencies.([]time.Duration); ok {
			if len(latencySlice) != 2 {
				t.Errorf("Expected 2 latency recordings for list_containers, got %d", len(latencySlice))
			}
		} else {
			t.Error("Latency data is not in expected format")
		}
	} else {
		t.Error("list_containers_latency not found in metrics")
	}

	if latencies, exists := dockerMetrics["create_container_latency"]; exists {
		if latencySlice, ok := latencies.([]time.Duration); ok {
			if len(latencySlice) != 1 {
				t.Errorf("Expected 1 latency recording for create_container, got %d", len(latencySlice))
			}
			if latencySlice[0] != 200*time.Millisecond {
				t.Errorf("Expected latency 200ms for create_container, got %v", latencySlice[0])
			}
		} else {
			t.Error("Latency data is not in expected format")
		}
	}
}

// TestRecordJobExecution tests recording job execution metrics
func TestRecordJobExecution(t *testing.T) {
	t.Parallel()

	recorder := NewExtendedMockMetricsRecorder()

	// Test successful job execution
	recorder.RecordJobExecution("test-job-1", 2*time.Second, true)

	jobMetrics := recorder.GetJobMetrics()
	if jobMetrics == nil {
		t.Fatal("GetJobMetrics returned nil")
	}

	if executions, exists := jobMetrics["test-job-1"]; exists {
		if execList, ok := executions.([]JobExecutionMetric); ok {
			if len(execList) != 1 {
				t.Errorf("Expected 1 execution record, got %d", len(execList))
			}
			exec := execList[0]
			if exec.Duration != 2*time.Second {
				t.Errorf("Expected duration 2s, got %v", exec.Duration)
			}
			if !exec.Success {
				t.Error("Expected successful execution")
			}
		} else {
			t.Error("Job execution data is not in expected format")
		}
	} else {
		t.Error("test-job-1 not found in job metrics")
	}

	// Test failed job execution
	recorder.RecordJobExecution("test-job-1", 500*time.Millisecond, false)

	jobMetrics = recorder.GetJobMetrics()
	if executions, exists := jobMetrics["test-job-1"]; exists {
		if execList, ok := executions.([]JobExecutionMetric); ok {
			if len(execList) != 2 {
				t.Errorf("Expected 2 execution records, got %d", len(execList))
			}
			// Check second execution (failed)
			exec := execList[1]
			if exec.Duration != 500*time.Millisecond {
				t.Errorf("Expected duration 500ms, got %v", exec.Duration)
			}
			if exec.Success {
				t.Error("Expected failed execution")
			}
		}
	}
}

// TestRecordSystemMetric tests recording system metrics
func TestRecordSystemMetric(t *testing.T) {
	t.Parallel()

	recorder := NewExtendedMockMetricsRecorder()

	// Test recording various system metrics
	testMetrics := map[string]interface{}{
		"memory_usage":    uint64(1024 * 1024 * 100), // 100MB
		"cpu_usage":       75.5,                       // 75.5%
		"disk_usage":      uint64(1024 * 1024 * 1024 * 5), // 5GB
		"goroutines":      42,
		"gc_pause_ns":     int64(1000000), // 1ms
	}

	for metric, value := range testMetrics {
		recorder.RecordSystemMetric(metric, value)
	}

	systemMetrics := recorder.GetSystemMetrics()
	if systemMetrics == nil {
		t.Fatal("GetSystemMetrics returned nil")
	}

	// Verify all metrics were recorded
	for metric, expectedValue := range testMetrics {
		if recordedValue, exists := systemMetrics[metric]; !exists {
			t.Errorf("System metric %s not recorded", metric)
		} else if recordedValue != expectedValue {
			t.Errorf("Expected %s = %v, got %v", metric, expectedValue, recordedValue)
		}
	}
}

// TestGetMetrics tests the general metrics getter
func TestGetMetrics(t *testing.T) {
	t.Parallel()

	recorder := NewExtendedMockMetricsRecorder()

	// Record some metrics
	recorder.RecordDockerOperation("test_operation")
	recorder.RecordDockerLatency("test_operation", 100*time.Millisecond)
	recorder.RecordJobExecution("test_job", 1*time.Second, true)
	recorder.RecordSystemMetric("test_metric", 42)

	metrics := recorder.GetMetrics()
	if metrics == nil {
		t.Fatal("GetMetrics returned nil")
	}

	// Check that all metric types are present
	expectedKeys := []string{"docker", "jobs", "system"}
	for _, key := range expectedKeys {
		if _, exists := metrics[key]; !exists {
			t.Errorf("Expected key %s not found in metrics", key)
		}
	}

	// Check docker metrics
	if dockerMetrics, ok := metrics["docker"].(map[string]interface{}); ok {
		if _, exists := dockerMetrics["test_operation"]; !exists {
			t.Error("test_operation not found in docker metrics")
		}
	} else {
		t.Error("Docker metrics not in expected format")
	}

	// Check job metrics
	if jobMetrics, ok := metrics["jobs"].(map[string]interface{}); ok {
		if _, exists := jobMetrics["test_job"]; !exists {
			t.Error("test_job not found in job metrics")
		}
	} else {
		t.Error("Job metrics not in expected format")
	}

	// Check system metrics
	if systemMetrics, ok := metrics["system"].(map[string]interface{}); ok {
		if value, exists := systemMetrics["test_metric"]; !exists {
			t.Error("test_metric not found in system metrics")
		} else if value != 42 {
			t.Errorf("Expected test_metric = 42, got %v", value)
		}
	} else {
		t.Error("System metrics not in expected format")
	}
}

// TestJobExecutionMetric tests the JobExecutionMetric struct
func TestJobExecutionMetric(t *testing.T) {
	t.Parallel()

	metric := JobExecutionMetric{
		Duration:  5 * time.Second,
		Success:   true,
		Timestamp: time.Now(),
	}

	if metric.Duration != 5*time.Second {
		t.Errorf("Expected duration 5s, got %v", metric.Duration)
	}

	if !metric.Success {
		t.Error("Expected success to be true")
	}

	if metric.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

// TestConcurrentMetricsRecording tests concurrent access to metrics recorder
func TestConcurrentMetricsRecording(t *testing.T) {
	t.Parallel()

	recorder := NewExtendedMockMetricsRecorder()

	const numGoroutines = 10
	const operationsPerGoroutine = 100

	done := make(chan bool, numGoroutines)

	// Launch concurrent metric recorders
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			for j := 0; j < operationsPerGoroutine; j++ {
				// Record Docker operations
				recorder.RecordDockerOperation("concurrent_operation")
				recorder.RecordDockerLatency("concurrent_operation", time.Duration(j)*time.Millisecond)

				// Record job executions
				recorder.RecordJobExecution("concurrent_job", time.Duration(j)*time.Millisecond, j%2 == 0)

				// Record system metrics
				recorder.RecordSystemMetric("concurrent_metric", goroutineID*operationsPerGoroutine+j)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify final counts
	dockerMetrics := recorder.GetDockerMetrics()
	if count, exists := dockerMetrics["concurrent_operation"]; !exists {
		t.Error("concurrent_operation not found in docker metrics")
	} else if count != numGoroutines*operationsPerGoroutine {
		t.Errorf("Expected %d concurrent operations, got %v", numGoroutines*operationsPerGoroutine, count)
	}

	if latencies, exists := dockerMetrics["concurrent_operation_latency"]; exists {
		if latencySlice, ok := latencies.([]time.Duration); ok {
			if len(latencySlice) != numGoroutines*operationsPerGoroutine {
				t.Errorf("Expected %d latency recordings, got %d", numGoroutines*operationsPerGoroutine, len(latencySlice))
			}
		}
	}

	jobMetrics := recorder.GetJobMetrics()
	if executions, exists := jobMetrics["concurrent_job"]; exists {
		if execList, ok := executions.([]JobExecutionMetric); ok {
			if len(execList) != numGoroutines*operationsPerGoroutine {
				t.Errorf("Expected %d job executions, got %d", numGoroutines*operationsPerGoroutine, len(execList))
			}
		}
	}

	systemMetrics := recorder.GetSystemMetrics()
	if value, exists := systemMetrics["concurrent_metric"]; exists {
		// The last recorded value should be from the last goroutine
		expectedLastValue := (numGoroutines-1)*operationsPerGoroutine + (operationsPerGoroutine - 1)
		if value != expectedLastValue {
			t.Errorf("Expected final concurrent_metric value %d, got %v", expectedLastValue, value)
		}
	}
}

// TestMetricsDataTypes tests various data types in metrics
func TestMetricsDataTypes(t *testing.T) {
	t.Parallel()

	recorder := NewExtendedMockMetricsRecorder()

	// Test different data types
	testCases := []struct {
		metric string
		value  interface{}
	}{
		{"int_metric", 42},
		{"int64_metric", int64(1234567890)},
		{"uint64_metric", uint64(9876543210)},
		{"float64_metric", 3.14159},
		{"string_metric", "test_string"},
		{"bool_metric", true},
	}

	for _, tc := range testCases {
		recorder.RecordSystemMetric(tc.metric, tc.value)
	}

	systemMetrics := recorder.GetSystemMetrics()

	for _, tc := range testCases {
		if value, exists := systemMetrics[tc.metric]; !exists {
			t.Errorf("Metric %s not found", tc.metric)
		} else if value != tc.value {
			t.Errorf("Expected %s = %v, got %v", tc.metric, tc.value, value)
		}
	}
}

// TestMetricsPerformance tests basic performance characteristics
func TestMetricsPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	recorder := NewExtendedMockMetricsRecorder()

	const numOperations = 10000

	// Test Docker operation recording performance
	start := time.Now()
	for i := 0; i < numOperations; i++ {
		recorder.RecordDockerOperation("perf_test")
	}
	dockerDuration := time.Since(start)

	// Test job execution recording performance
	start = time.Now()
	for i := 0; i < numOperations; i++ {
		recorder.RecordJobExecution("perf_job", time.Duration(i)*time.Microsecond, i%2 == 0)
	}
	jobDuration := time.Since(start)

	// Test system metric recording performance
	start = time.Now()
	for i := 0; i < numOperations; i++ {
		recorder.RecordSystemMetric("perf_metric", i)
	}
	systemDuration := time.Since(start)

	t.Logf("Performance Results for %d operations:", numOperations)
	t.Logf("Docker operations: %v (%.2f μs/op)", dockerDuration, float64(dockerDuration.Nanoseconds())/float64(numOperations)/1000)
	t.Logf("Job executions: %v (%.2f μs/op)", jobDuration, float64(jobDuration.Nanoseconds())/float64(numOperations)/1000)
	t.Logf("System metrics: %v (%.2f μs/op)", systemDuration, float64(systemDuration.Nanoseconds())/float64(numOperations)/1000)

	// Verify final counts
	dockerMetrics := recorder.GetDockerMetrics()
	if count := dockerMetrics["perf_test"]; count != numOperations {
		t.Errorf("Expected %d docker operations, got %v", numOperations, count)
	}

	jobMetrics := recorder.GetJobMetrics()
	if executions, exists := jobMetrics["perf_job"]; exists {
		if execList, ok := executions.([]JobExecutionMetric); ok {
			if len(execList) != numOperations {
				t.Errorf("Expected %d job executions, got %d", numOperations, len(execList))
			}
		}
	}

	systemMetrics := recorder.GetSystemMetrics()
	if value := systemMetrics["perf_metric"]; value != numOperations-1 {
		t.Errorf("Expected final system metric value %d, got %v", numOperations-1, value)
	}
}