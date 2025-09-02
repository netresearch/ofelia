package metrics

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// MetricsCollector handles Prometheus-style metrics
type MetricsCollector struct {
	mu      sync.RWMutex
	metrics map[string]*Metric
}

// Metric represents a single metric with its type and values
type Metric struct {
	Name        string
	Type        string // counter, gauge, histogram
	Help        string
	Value       float64
	Labels      map[string]string
	Histogram   *Histogram
	LastUpdated time.Time
}

// Histogram for tracking distributions
type Histogram struct {
	Count  int64
	Sum    float64
	Bucket map[float64]int64 // bucket threshold -> count
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		metrics: make(map[string]*Metric),
	}
}

// RegisterCounter registers a new counter metric
func (mc *MetricsCollector) RegisterCounter(name, help string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.metrics[name] = &Metric{
		Name:        name,
		Type:        "counter",
		Help:        help,
		Value:       0,
		Labels:      make(map[string]string),
		LastUpdated: time.Now(),
	}
}

// RegisterGauge registers a new gauge metric
func (mc *MetricsCollector) RegisterGauge(name, help string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.metrics[name] = &Metric{
		Name:        name,
		Type:        "gauge",
		Help:        help,
		Value:       0,
		Labels:      make(map[string]string),
		LastUpdated: time.Now(),
	}
}

// RegisterHistogram registers a new histogram metric
func (mc *MetricsCollector) RegisterHistogram(name, help string, buckets []float64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	hist := &Histogram{
		Count:  0,
		Sum:    0,
		Bucket: make(map[float64]int64),
	}
	
	// Initialize buckets
	for _, b := range buckets {
		hist.Bucket[b] = 0
	}
	
	mc.metrics[name] = &Metric{
		Name:        name,
		Type:        "histogram",
		Help:        help,
		Histogram:   hist,
		Labels:      make(map[string]string),
		LastUpdated: time.Now(),
	}
}

// IncrementCounter increments a counter metric
func (mc *MetricsCollector) IncrementCounter(name string, value float64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	if metric, exists := mc.metrics[name]; exists && metric.Type == "counter" {
		metric.Value += value
		metric.LastUpdated = time.Now()
	}
}

// SetGauge sets a gauge metric value
func (mc *MetricsCollector) SetGauge(name string, value float64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	if metric, exists := mc.metrics[name]; exists && metric.Type == "gauge" {
		metric.Value = value
		metric.LastUpdated = time.Now()
	}
}

// ObserveHistogram records a value in a histogram
func (mc *MetricsCollector) ObserveHistogram(name string, value float64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	if metric, exists := mc.metrics[name]; exists && metric.Type == "histogram" {
		hist := metric.Histogram
		hist.Count++
		hist.Sum += value
		
		// Update buckets
		for bucket := range hist.Bucket {
			if value <= bucket {
				hist.Bucket[bucket]++
			}
		}
		
		metric.LastUpdated = time.Now()
	}
}

// RecordJobRetry records a job retry attempt
func (mc *MetricsCollector) RecordJobRetry(jobName string, attempt int, success bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	// Increment total retries counter
	if counter, exists := mc.metrics["ofelia_job_retries_total"]; exists {
		counter.Value++
		counter.LastUpdated = time.Now()
	}
	
	// Record success or failure
	if success {
		if counter, exists := mc.metrics["ofelia_job_retry_success_total"]; exists {
			counter.Value++
			counter.LastUpdated = time.Now()
		}
	} else {
		if counter, exists := mc.metrics["ofelia_job_retry_failed_total"]; exists {
			counter.Value++
			counter.LastUpdated = time.Now()
		}
	}
	
	// Record attempt number in histogram (simplified - just track the attempt count)
	if hist, exists := mc.metrics["ofelia_job_retry_delay_seconds"]; exists && hist.Histogram != nil {
		// Use attempt number as a proxy for delay (higher attempts = longer delays)
		delaySeconds := float64(attempt) // Simplified: each attempt represents increasing delay
		mc.ObserveHistogram("ofelia_job_retry_delay_seconds", delaySeconds)
	}
}

// Export formats metrics in Prometheus text format
func (mc *MetricsCollector) Export() string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	var output string
	
	for _, metric := range mc.metrics {
		// Add HELP and TYPE comments
		output += fmt.Sprintf("# HELP %s %s\n", metric.Name, metric.Help)
		output += fmt.Sprintf("# TYPE %s %s\n", metric.Name, metric.Type)
		
		switch metric.Type {
		case "counter", "gauge":
			output += fmt.Sprintf("%s %f\n", metric.Name, metric.Value)
			
		case "histogram":
			if metric.Histogram != nil {
				// Export histogram buckets
				for bucket, count := range metric.Histogram.Bucket {
					output += fmt.Sprintf("%s_bucket{le=\"%g\"} %d\n", metric.Name, bucket, count)
				}
				output += fmt.Sprintf("%s_bucket{le=\"+Inf\"} %d\n", metric.Name, metric.Histogram.Count)
				output += fmt.Sprintf("%s_count %d\n", metric.Name, metric.Histogram.Count)
				output += fmt.Sprintf("%s_sum %f\n", metric.Name, metric.Histogram.Sum)
			}
		}
		
		output += "\n"
	}
	
	return output
}

// Handler returns an HTTP handler for the metrics endpoint
func (mc *MetricsCollector) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, mc.Export())
	}
}

// DefaultMetrics initializes common metrics
func (mc *MetricsCollector) InitDefaultMetrics() {
	// Job metrics
	mc.RegisterCounter("ofelia_jobs_total", "Total number of jobs executed")
	mc.RegisterCounter("ofelia_jobs_failed_total", "Total number of failed jobs")
	mc.RegisterGauge("ofelia_jobs_running", "Number of currently running jobs")
	mc.RegisterHistogram("ofelia_job_duration_seconds", "Job execution duration in seconds",
		[]float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300})
	
	// System metrics
	mc.RegisterGauge("ofelia_up", "Ofelia service status (1 = up, 0 = down)")
	mc.RegisterCounter("ofelia_restarts_total", "Total number of service restarts")
	
	// HTTP metrics
	mc.RegisterCounter("ofelia_http_requests_total", "Total number of HTTP requests")
	mc.RegisterHistogram("ofelia_http_request_duration_seconds", "HTTP request duration in seconds",
		[]float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1})
	
	// Docker metrics
	mc.RegisterCounter("ofelia_docker_operations_total", "Total Docker API operations")
	mc.RegisterCounter("ofelia_docker_errors_total", "Total Docker API errors")
	
	// Retry metrics
	mc.RegisterCounter("ofelia_job_retries_total", "Total job retry attempts")
	mc.RegisterCounter("ofelia_job_retry_success_total", "Total successful job retries")
	mc.RegisterCounter("ofelia_job_retry_failed_total", "Total failed job retries")
	mc.RegisterHistogram("ofelia_job_retry_delay_seconds", "Retry delay in seconds",
		[]float64{0.1, 0.5, 1, 2, 5, 10, 30, 60})
	
	// Set initial values
	mc.SetGauge("ofelia_up", 1)
	mc.SetGauge("ofelia_jobs_running", 0)
}

// JobMetrics tracks job execution metrics
type JobMetrics struct {
	collector *MetricsCollector
	startTime map[string]time.Time
	mu        sync.Mutex
}

// NewJobMetrics creates a job metrics tracker
func NewJobMetrics(collector *MetricsCollector) *JobMetrics {
	return &JobMetrics{
		collector: collector,
		startTime: make(map[string]time.Time),
	}
}

// JobStarted records job start
func (jm *JobMetrics) JobStarted(jobID string) {
	jm.mu.Lock()
	jm.startTime[jobID] = time.Now()
	jm.mu.Unlock()
	
	jm.collector.IncrementCounter("ofelia_jobs_total", 1)
	jm.collector.SetGauge("ofelia_jobs_running", 
		jm.collector.getGaugeValue("ofelia_jobs_running") + 1)
}

// JobCompleted records job completion
func (jm *JobMetrics) JobCompleted(jobID string, success bool) {
	jm.mu.Lock()
	startTime, exists := jm.startTime[jobID]
	if exists {
		delete(jm.startTime, jobID)
		duration := time.Since(startTime).Seconds()
		jm.collector.ObserveHistogram("ofelia_job_duration_seconds", duration)
	}
	jm.mu.Unlock()
	
	if !success {
		jm.collector.IncrementCounter("ofelia_jobs_failed_total", 1)
	}
	
	jm.collector.SetGauge("ofelia_jobs_running",
		jm.collector.getGaugeValue("ofelia_jobs_running") - 1)
}

// Helper method to get gauge value
func (mc *MetricsCollector) getGaugeValue(name string) float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	if metric, exists := mc.metrics[name]; exists && metric.Type == "gauge" {
		return metric.Value
	}
	return 0
}

// HTTPMetrics middleware for tracking HTTP requests
func HTTPMetrics(mc *MetricsCollector) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			
			// Increment request counter
			mc.IncrementCounter("ofelia_http_requests_total", 1)
			
			// Call next handler
			next.ServeHTTP(w, r)
			
			// Record duration
			duration := time.Since(start).Seconds()
			mc.ObserveHistogram("ofelia_http_request_duration_seconds", duration)
		})
	}
}