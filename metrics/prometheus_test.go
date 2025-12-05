package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCollector(t *testing.T) {
	mc := NewCollector()

	// Test counter registration and increment
	mc.RegisterCounter("test_counter", "A test counter")
	mc.IncrementCounter("test_counter", 1)
	mc.IncrementCounter("test_counter", 2)

	if mc.metrics["test_counter"].Value != 3 {
		t.Errorf("Expected counter value 3, got %f", mc.metrics["test_counter"].Value)
	}

	// Test gauge registration and set
	mc.RegisterGauge("test_gauge", "A test gauge")
	mc.SetGauge("test_gauge", 42.5)

	if mc.metrics["test_gauge"].Value != 42.5 {
		t.Errorf("Expected gauge value 42.5, got %f", mc.metrics["test_gauge"].Value)
	}

	// Test histogram registration and observe
	mc.RegisterHistogram("test_histogram", "A test histogram", []float64{1, 5, 10})
	mc.ObserveHistogram("test_histogram", 3)
	mc.ObserveHistogram("test_histogram", 7)
	mc.ObserveHistogram("test_histogram", 12)

	hist := mc.metrics["test_histogram"].Histogram
	if hist.Count != 3 {
		t.Errorf("Expected histogram count 3, got %d", hist.Count)
	}
	if hist.Sum != 22 {
		t.Errorf("Expected histogram sum 22, got %f", hist.Sum)
	}

	t.Log("Basic metrics operations test passed")
}

func TestMetricsExport(t *testing.T) {
	mc := NewCollector()

	// Register and set some metrics
	mc.RegisterCounter("requests_total", "Total requests")
	mc.IncrementCounter("requests_total", 100)

	mc.RegisterGauge("temperature", "Current temperature")
	mc.SetGauge("temperature", 23.5)

	mc.RegisterHistogram("response_time", "Response time", []float64{0.1, 0.5, 1})
	mc.ObserveHistogram("response_time", 0.3)
	mc.ObserveHistogram("response_time", 0.7)

	// Export metrics
	output := mc.Export()

	// Check output contains expected metrics
	expectedStrings := []string{
		"# HELP requests_total Total requests",
		"# TYPE requests_total counter",
		"requests_total 100",
		"# HELP temperature Current temperature",
		"# TYPE temperature gauge",
		"temperature 23.5",
		"# HELP response_time Response time",
		"# TYPE response_time histogram",
		"response_time_count 2",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain '%s'", expected)
		}
	}

	t.Log("Metrics export test passed")
}

func TestJobMetrics(t *testing.T) {
	mc := NewCollector()
	mc.InitDefaultMetrics()

	jm := NewJobMetrics(mc)

	// Test job start
	jm.JobStarted("job1")

	// Check metrics
	if mc.getGaugeValue("ofelia_jobs_running") != 1 {
		t.Error("Expected 1 running job")
	}

	// Simulate job execution
	time.Sleep(10 * time.Millisecond)

	// Test job completion (success)
	jm.JobCompleted("job1", true)

	if mc.getGaugeValue("ofelia_jobs_running") != 0 {
		t.Error("Expected 0 running jobs after completion")
	}

	// Test failed job
	jm.JobStarted("job2")
	time.Sleep(10 * time.Millisecond)
	jm.JobCompleted("job2", false)

	// Check failed counter
	if mc.metrics["ofelia_jobs_failed_total"].Value != 1 {
		t.Error("Expected 1 failed job")
	}

	// Check total jobs
	if mc.metrics["ofelia_jobs_total"].Value != 2 {
		t.Error("Expected 2 total jobs")
	}

	t.Log("Job metrics test passed")
}

func TestHTTPMetricsMiddleware(t *testing.T) {
	mc := NewCollector()
	mc.InitDefaultMetrics()

	// Create test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond) // Simulate work
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with metrics middleware
	handler := HTTPMetrics(mc)(testHandler)

	// Make test requests
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Check metrics
	if mc.metrics["ofelia_http_requests_total"].Value != 5 {
		t.Errorf("Expected 5 HTTP requests, got %f",
			mc.metrics["ofelia_http_requests_total"].Value)
	}

	// Check histogram was updated
	hist := mc.metrics["ofelia_http_request_duration_seconds"].Histogram
	if hist.Count != 5 {
		t.Errorf("Expected 5 observations in histogram, got %d", hist.Count)
	}

	t.Log("HTTP metrics middleware test passed")
}

func TestMetricsHandler(t *testing.T) {
	mc := NewCollector()
	mc.InitDefaultMetrics()

	// Set some values
	mc.IncrementCounter("ofelia_jobs_total", 42)
	mc.SetGauge("ofelia_jobs_running", 3)

	// Create request to metrics endpoint
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	handler := mc.Handler()
	handler(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Expected text/plain content type, got %s", contentType)
	}

	body := w.Body.String()
	if !strings.Contains(body, "ofelia_jobs_total 42") {
		t.Error("Response should contain job total metric")
	}
	if !strings.Contains(body, "ofelia_jobs_running 3") {
		t.Error("Response should contain running jobs metric")
	}

	t.Log("Metrics handler test passed")
}

func TestDefaultMetricsInitialization(t *testing.T) {
	mc := NewCollector()
	mc.InitDefaultMetrics()

	// Check all default metrics are registered
	expectedMetrics := []string{
		"ofelia_jobs_total",
		"ofelia_jobs_failed_total",
		"ofelia_jobs_running",
		"ofelia_job_duration_seconds",
		"ofelia_up",
		"ofelia_restarts_total",
		"ofelia_http_requests_total",
		"ofelia_http_request_duration_seconds",
		"ofelia_docker_operations_total",
		"ofelia_docker_errors_total",
		"ofelia_container_monitor_events_total",
		"ofelia_container_monitor_fallbacks_total",
		"ofelia_container_monitor_method",
		"ofelia_container_wait_duration_seconds",
	}

	for _, name := range expectedMetrics {
		if _, exists := mc.metrics[name]; !exists {
			t.Errorf("Expected metric '%s' to be registered", name)
		}
	}

	// Check initial values
	if mc.getGaugeValue("ofelia_up") != 1 {
		t.Error("ofelia_up should be initialized to 1")
	}

	if mc.getGaugeValue("ofelia_jobs_running") != 0 {
		t.Error("ofelia_jobs_running should be initialized to 0")
	}

	t.Log("Default metrics initialization test passed")
}

func TestContainerMonitorMetrics(t *testing.T) {
	mc := NewCollector()
	mc.InitDefaultMetrics()

	// Test recording container monitor events
	mc.RecordContainerEvent()
	if mc.metrics["ofelia_container_monitor_events_total"].Value != 1 {
		t.Error("Expected container monitor event counter to be 1")
	}

	// Test recording fallbacks
	mc.RecordContainerMonitorFallback()
	if mc.metrics["ofelia_container_monitor_fallbacks_total"].Value != 1 {
		t.Error("Expected container monitor fallback counter to be 1")
	}

	// Test setting monitor method
	mc.RecordContainerMonitorMethod(true) // events API
	if mc.getGaugeValue("ofelia_container_monitor_method") != 1 {
		t.Error("Expected container monitor method to be 1 (events)")
	}

	mc.RecordContainerMonitorMethod(false) // polling
	if mc.getGaugeValue("ofelia_container_monitor_method") != 0 {
		t.Error("Expected container monitor method to be 0 (polling)")
	}

	// Test recording wait duration
	mc.RecordContainerWaitDuration(0.5)
	mc.RecordContainerWaitDuration(1.5)
	mc.RecordContainerWaitDuration(2.5)

	hist := mc.metrics["ofelia_container_wait_duration_seconds"].Histogram
	if hist.Count != 3 {
		t.Errorf("Expected 3 observations, got %d", hist.Count)
	}
	if hist.Sum != 4.5 {
		t.Errorf("Expected sum of 4.5, got %f", hist.Sum)
	}

	t.Log("Container monitor metrics test passed")
}

// New comprehensive tests for missing coverage

func TestJobRetryMetrics(t *testing.T) {
	mc := NewCollector()
	mc.InitDefaultMetrics()

	// Test successful retry
	mc.RecordJobRetry("test-job", 1, true)
	if mc.metrics["ofelia_job_retries_total"].Value != 1 {
		t.Error("Expected job_retries_total counter to be 1")
	}
	if mc.metrics["ofelia_job_retry_success_total"].Value != 1 {
		t.Error("Expected job_retry_success_total counter to be 1")
	}

	// Test failed retry
	mc.RecordJobRetry("test-job", 2, false)
	if mc.metrics["ofelia_job_retries_total"].Value != 2 {
		t.Error("Expected job_retries_total counter to be 2")
	}
	if mc.metrics["ofelia_job_retry_failed_total"].Value != 1 {
		t.Error("Expected job_retry_failed_total counter to be 1")
	}

	// Test histogram recording
	hist := mc.metrics["ofelia_job_retry_delay_seconds"].Histogram
	if hist.Count != 2 {
		t.Errorf("Expected 2 histogram observations, got %d", hist.Count)
	}
	// Sum should be 1 + 2 = 3 (attempt numbers used as proxy for delay)
	if hist.Sum != 3 {
		t.Errorf("Expected histogram sum of 3, got %f", hist.Sum)
	}

	t.Log("Job retry metrics test passed")
}

func TestDockerOperationMetrics(t *testing.T) {
	mc := NewCollector()
	mc.InitDefaultMetrics()

	// Test recording Docker operations
	mc.RecordDockerOperation("list_containers")
	mc.RecordDockerOperation("inspect_container")
	mc.RecordDockerOperation("create_container")

	if mc.metrics["ofelia_docker_operations_total"].Value != 3 {
		t.Errorf("Expected 3 Docker operations, got %f",
			mc.metrics["ofelia_docker_operations_total"].Value)
	}

	// Test recording Docker errors
	mc.RecordDockerError("list_containers")
	mc.RecordDockerError("inspect_container")

	if mc.metrics["ofelia_docker_errors_total"].Value != 2 {
		t.Errorf("Expected 2 Docker errors, got %f",
			mc.metrics["ofelia_docker_errors_total"].Value)
	}

	t.Log("Docker operation metrics test passed")
}

func TestGetGaugeValueEdgeCases(t *testing.T) {
	mc := NewCollector()

	// Test getting value from non-existent gauge
	value := mc.getGaugeValue("non_existent_gauge")
	if value != 0 {
		t.Errorf("Expected 0 for non-existent gauge, got %f", value)
	}

	// Test getting value from non-gauge metric
	mc.RegisterCounter("test_counter", "Test counter")
	mc.IncrementCounter("test_counter", 10)

	value = mc.getGaugeValue("test_counter")
	if value != 0 {
		t.Errorf("Expected 0 for non-gauge metric, got %f", value)
	}

	// Test getting actual gauge value
	mc.RegisterGauge("test_gauge", "Test gauge")
	mc.SetGauge("test_gauge", 42.5)

	value = mc.getGaugeValue("test_gauge")
	if value != 42.5 {
		t.Errorf("Expected 42.5 for gauge value, got %f", value)
	}

	t.Log("Gauge value edge cases test passed")
}

func TestIncrementCounterOnNonExistent(t *testing.T) {
	mc := NewCollector()

	// Attempt to increment non-existent counter (should not panic)
	mc.IncrementCounter("non_existent", 1)

	// Verify it wasn't created
	if _, exists := mc.metrics["non_existent"]; exists {
		t.Error("Non-existent counter should not be auto-created")
	}
}

func TestSetGaugeOnNonExistent(t *testing.T) {
	mc := NewCollector()

	// Attempt to set non-existent gauge (should not panic)
	mc.SetGauge("non_existent", 42)

	// Verify it wasn't created
	if _, exists := mc.metrics["non_existent"]; exists {
		t.Error("Non-existent gauge should not be auto-created")
	}
}

func TestObserveHistogramOnNonExistent(t *testing.T) {
	mc := NewCollector()

	// Attempt to observe non-existent histogram (should not panic)
	mc.ObserveHistogram("non_existent", 1.5)

	// Verify it wasn't created
	if _, exists := mc.metrics["non_existent"]; exists {
		t.Error("Non-existent histogram should not be auto-created")
	}
}

func TestHistogramBuckets(t *testing.T) {
	mc := NewCollector()

	buckets := []float64{1, 5, 10, 50}
	mc.RegisterHistogram("test_hist", "Test histogram", buckets)

	// Observe values that fall into different buckets
	mc.ObserveHistogram("test_hist", 0.5) // Below first bucket
	mc.ObserveHistogram("test_hist", 3)   // Between bucket 1 and 5
	mc.ObserveHistogram("test_hist", 7)   // Between bucket 5 and 10
	mc.ObserveHistogram("test_hist", 25)  // Between bucket 10 and 50
	mc.ObserveHistogram("test_hist", 100) // Above last bucket

	hist := mc.metrics["test_hist"].Histogram

	// Check bucket counts
	expectedBuckets := map[float64]int64{
		1:  1, // 0.5 is <= 1
		5:  2, // 0.5, 3 are <= 5
		10: 3, // 0.5, 3, 7 are <= 10
		50: 4, // 0.5, 3, 7, 25 are <= 50
	}

	for bucket, expectedCount := range expectedBuckets {
		if hist.Bucket[bucket] != expectedCount {
			t.Errorf("Bucket %f: expected count %d, got %d",
				bucket, expectedCount, hist.Bucket[bucket])
		}
	}

	// Check total count
	if hist.Count != 5 {
		t.Errorf("Expected total count 5, got %d", hist.Count)
	}

	// Check sum
	expectedSum := 0.5 + 3 + 7 + 25 + 100
	if hist.Sum != expectedSum {
		t.Errorf("Expected sum %f, got %f", expectedSum, hist.Sum)
	}

	t.Log("Histogram buckets test passed")
}

func TestJobMetricsWithoutStartTime(t *testing.T) {
	mc := NewCollector()
	mc.InitDefaultMetrics()

	jm := NewJobMetrics(mc)

	// Complete a job that was never started (no start time recorded)
	jm.JobCompleted("unknown_job", true)

	// Should still decrement the running gauge
	if mc.getGaugeValue("ofelia_jobs_running") != -1 {
		t.Errorf("Expected -1 running jobs, got %f",
			mc.getGaugeValue("ofelia_jobs_running"))
	}

	t.Log("Job metrics without start time test passed")
}

func TestConcurrentMetricsAccess(t *testing.T) {
	mc := NewCollector()
	mc.RegisterCounter("concurrent_counter", "Test counter")
	mc.RegisterGauge("concurrent_gauge", "Test gauge")
	mc.RegisterHistogram("concurrent_hist", "Test histogram", []float64{1, 5, 10})

	done := make(chan bool, 30)

	// Concurrent increments
	for i := 0; i < 10; i++ {
		go func() {
			mc.IncrementCounter("concurrent_counter", 1)
			done <- true
		}()
	}

	// Concurrent gauge sets
	for i := 0; i < 10; i++ {
		go func(val float64) {
			mc.SetGauge("concurrent_gauge", val)
			done <- true
		}(float64(i))
	}

	// Concurrent histogram observations
	for i := 0; i < 10; i++ {
		go func(val float64) {
			mc.ObserveHistogram("concurrent_hist", val)
			done <- true
		}(float64(i))
	}

	// Wait for all goroutines with timeout
	const testTimeout = 10 * time.Second // Timeout for mutation testing
	timeout := time.After(testTimeout)
	for i := 0; i < 30; i++ {
		select {
		case <-done:
			// goroutine completed
		case <-timeout:
			t.Fatalf("Test timed out waiting for goroutine %d", i)
		}
	}

	// Counter should be 10
	if mc.metrics["concurrent_counter"].Value != 10 {
		t.Errorf("Expected counter value 10, got %f",
			mc.metrics["concurrent_counter"].Value)
	}

	// Histogram should have 10 observations
	if mc.metrics["concurrent_hist"].Histogram.Count != 10 {
		t.Errorf("Expected 10 histogram observations, got %d",
			mc.metrics["concurrent_hist"].Histogram.Count)
	}

	t.Log("Concurrent metrics access test passed")
}

func TestMetricsTypeValidation(t *testing.T) {
	mc := NewCollector()

	// Register as counter
	mc.RegisterCounter("test_metric", "Test metric")

	// Try to set as gauge (should not work - wrong type)
	mc.SetGauge("test_metric", 42)

	if mc.metrics["test_metric"].Value != 0 {
		t.Error("Setting gauge on counter should not change value")
	}

	// Register as gauge
	mc.RegisterGauge("gauge_metric", "Gauge metric")

	// Try to increment as counter (should not work - wrong type)
	mc.IncrementCounter("gauge_metric", 10)

	if mc.metrics["gauge_metric"].Value != 0 {
		t.Error("Incrementing counter on gauge should not change value")
	}

	t.Log("Metrics type validation test passed")
}

func TestExportWithEmptyHistogram(t *testing.T) {
	mc := NewCollector()

	// Register histogram but don't observe anything
	mc.RegisterHistogram("empty_hist", "Empty histogram", []float64{1, 5, 10})

	output := mc.Export()

	// Should still export with zero counts
	if !strings.Contains(output, "empty_hist_count 0") {
		t.Error("Export should include empty histogram with count 0")
	}
	if !strings.Contains(output, "empty_hist_sum 0.000000") {
		t.Error("Export should include empty histogram with sum 0")
	}

	t.Log("Export with empty histogram test passed")
}

func TestLastUpdatedTimestamp(t *testing.T) {
	mc := NewCollector()
	mc.RegisterCounter("test_counter", "Test counter")

	before := time.Now()
	mc.IncrementCounter("test_counter", 1)
	after := time.Now()

	lastUpdated := mc.metrics["test_counter"].LastUpdated

	if lastUpdated.Before(before) || lastUpdated.After(after) {
		t.Error("LastUpdated timestamp should be between before and after times")
	}

	// Test for gauge
	mc.RegisterGauge("test_gauge", "Test gauge")
	before = time.Now()
	mc.SetGauge("test_gauge", 42)
	after = time.Now()

	lastUpdated = mc.metrics["test_gauge"].LastUpdated

	if lastUpdated.Before(before) || lastUpdated.After(after) {
		t.Error("LastUpdated timestamp should be between before and after times for gauge")
	}

	// Test for histogram
	mc.RegisterHistogram("test_hist", "Test histogram", []float64{1, 5})
	before = time.Now()
	mc.ObserveHistogram("test_hist", 2)
	after = time.Now()

	lastUpdated = mc.metrics["test_hist"].LastUpdated

	if lastUpdated.Before(before) || lastUpdated.After(after) {
		t.Error("LastUpdated timestamp should be between before and after times for histogram")
	}

	t.Log("LastUpdated timestamp test passed")
}
