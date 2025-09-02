package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsCollector(t *testing.T) {
	mc := NewMetricsCollector()
	
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
	mc := NewMetricsCollector()
	
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
	mc := NewMetricsCollector()
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
	mc := NewMetricsCollector()
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
	mc := NewMetricsCollector()
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
	mc := NewMetricsCollector()
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
	mc := NewMetricsCollector()
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