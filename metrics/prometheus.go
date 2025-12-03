package metrics

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	// Metric type constants
	MetricTypeGauge = "gauge"
)

// Collector handles Prometheus-style metrics
type Collector struct {
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

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		metrics: make(map[string]*Metric),
	}
}

// RegisterCounter registers a new counter metric
func (mc *Collector) RegisterCounter(name, help string) {
	mc.registerMetric(name, "counter", help, nil)
}

// RegisterGauge registers a new gauge metric
func (mc *Collector) RegisterGauge(name, help string) {
	mc.registerMetric(name, MetricTypeGauge, help, nil)
}

// registerMetric is a helper function to register a new metric
func (mc *Collector) registerMetric(name, metricType, help string, histogram *Histogram) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.metrics[name] = &Metric{
		Name:        name,
		Type:        metricType,
		Help:        help,
		Value:       0,
		Labels:      make(map[string]string),
		Histogram:   histogram,
		LastUpdated: time.Now(),
	}
}

// RegisterHistogram registers a new histogram metric
func (mc *Collector) RegisterHistogram(name, help string, buckets []float64) {
	hist := &Histogram{
		Count:  0,
		Sum:    0,
		Bucket: make(map[float64]int64),
	}

	// Initialize buckets
	for _, b := range buckets {
		hist.Bucket[b] = 0
	}

	mc.registerMetric(name, "histogram", help, hist)
}

// IncrementCounter increments a counter metric
func (mc *Collector) IncrementCounter(name string, value float64) {
	mc.updateMetric(name, func(metric *Metric) {
		if metric.Type == "counter" {
			metric.Value += value
			metric.LastUpdated = time.Now()
		}
	})
}

// SetGauge sets a gauge metric value
func (mc *Collector) SetGauge(name string, value float64) {
	mc.updateMetric(name, func(metric *Metric) {
		if metric.Type == MetricTypeGauge {
			metric.Value = value
			metric.LastUpdated = time.Now()
		}
	})
}

// updateMetric is a helper function to safely update a metric
func (mc *Collector) updateMetric(name string, updateFn func(*Metric)) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if metric, exists := mc.metrics[name]; exists {
		updateFn(metric)
	}
}

// ObserveHistogram records a value in a histogram
func (mc *Collector) ObserveHistogram(name string, value float64) {
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
func (mc *Collector) RecordJobRetry(jobName string, attempt int, success bool) {
	// Increment total retries counter
	mc.IncrementCounter("ofelia_job_retries_total", 1)

	// Record success or failure
	if success {
		mc.IncrementCounter("ofelia_job_retry_success_total", 1)
	} else {
		mc.IncrementCounter("ofelia_job_retry_failed_total", 1)
	}

	// Record attempt number in histogram (simplified - just track the attempt count)
	// Use attempt number as a proxy for delay (higher attempts = longer delays)
	delaySeconds := float64(attempt) // Simplified: each attempt represents increasing delay
	mc.ObserveHistogram("ofelia_job_retry_delay_seconds", delaySeconds)
}

// RecordContainerEvent records a container event received
func (mc *Collector) RecordContainerEvent() {
	mc.IncrementCounter("ofelia_container_monitor_events_total", 1)
}

// RecordContainerMonitorFallback records a fallback to polling
func (mc *Collector) RecordContainerMonitorFallback() {
	mc.IncrementCounter("ofelia_container_monitor_fallbacks_total", 1)
}

// RecordContainerMonitorMethod records the monitoring method being used
func (mc *Collector) RecordContainerMonitorMethod(usingEvents bool) {
	if usingEvents {
		mc.SetGauge("ofelia_container_monitor_method", 1)
	} else {
		mc.SetGauge("ofelia_container_monitor_method", 0)
	}
}

// RecordContainerWaitDuration records the time spent waiting for a container
func (mc *Collector) RecordContainerWaitDuration(seconds float64) {
	mc.ObserveHistogram("ofelia_container_wait_duration_seconds", seconds)
}

// RecordDockerOperation records a Docker API operation
func (mc *Collector) RecordDockerOperation(operation string) {
	mc.IncrementCounter("ofelia_docker_operations_total", 1)
}

// RecordDockerError records a Docker API error
func (mc *Collector) RecordDockerError(operation string) {
	mc.IncrementCounter("ofelia_docker_errors_total", 1)
}

// RecordJobStart records a job starting (from go-cron ObservabilityHooks)
func (mc *Collector) RecordJobStart(jobName string) {
	mc.IncrementCounter("ofelia_cron_jobs_started_total", 1)
	mc.SetGauge("ofelia_jobs_running",
		mc.getGaugeValue("ofelia_jobs_running")+1)
}

// RecordJobComplete records a job completing (from go-cron ObservabilityHooks)
func (mc *Collector) RecordJobComplete(jobName string, durationSeconds float64, panicked bool) {
	mc.IncrementCounter("ofelia_cron_jobs_completed_total", 1)
	mc.ObserveHistogram("ofelia_job_duration_seconds", durationSeconds)

	if panicked {
		mc.IncrementCounter("ofelia_cron_jobs_panicked_total", 1)
	}

	mc.SetGauge("ofelia_jobs_running",
		mc.getGaugeValue("ofelia_jobs_running")-1)
}

// RecordJobScheduled records when a job's next run is scheduled (from go-cron ObservabilityHooks)
func (mc *Collector) RecordJobScheduled(jobName string) {
	mc.IncrementCounter("ofelia_cron_jobs_scheduled_total", 1)
}

// Export formats metrics in Prometheus text format
func (mc *Collector) Export() string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	var output string

	for _, metric := range mc.metrics {
		// Add HELP and TYPE comments
		output += fmt.Sprintf("# HELP %s %s\n", metric.Name, metric.Help)
		output += fmt.Sprintf("# TYPE %s %s\n", metric.Name, metric.Type)

		switch metric.Type {
		case "counter", MetricTypeGauge:
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
func (mc *Collector) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, mc.Export())
	}
}

// DefaultMetrics initializes common metrics
func (mc *Collector) InitDefaultMetrics() {
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

	// Container monitoring metrics
	mc.RegisterCounter("ofelia_container_monitor_events_total", "Total container events received")
	mc.RegisterCounter("ofelia_container_monitor_fallbacks_total", "Total fallbacks to polling")
	mc.RegisterGauge("ofelia_container_monitor_method", "Container monitoring method (1=events, 0=polling)")
	mc.RegisterHistogram("ofelia_container_wait_duration_seconds", "Container wait duration in seconds",
		[]float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10})

	// Retry metrics
	mc.RegisterCounter("ofelia_job_retries_total", "Total job retry attempts")
	mc.RegisterCounter("ofelia_job_retry_success_total", "Total successful job retries")
	mc.RegisterCounter("ofelia_job_retry_failed_total", "Total failed job retries")
	mc.RegisterHistogram("ofelia_job_retry_delay_seconds", "Retry delay in seconds",
		[]float64{0.1, 0.5, 1, 2, 5, 10, 30, 60})

	// go-cron ObservabilityHooks metrics
	mc.RegisterCounter("ofelia_cron_jobs_started_total", "Total cron jobs started")
	mc.RegisterCounter("ofelia_cron_jobs_completed_total", "Total cron jobs completed")
	mc.RegisterCounter("ofelia_cron_jobs_panicked_total", "Total cron jobs that panicked")
	mc.RegisterCounter("ofelia_cron_jobs_scheduled_total", "Total cron job scheduling events")

	// Set initial values
	mc.SetGauge("ofelia_up", 1)
	mc.SetGauge("ofelia_jobs_running", 0)
}

// JobMetrics tracks job execution metrics
type JobMetrics struct {
	collector *Collector
	startTime map[string]time.Time
	mu        sync.Mutex
}

// NewJobMetrics creates a job metrics tracker
func NewJobMetrics(collector *Collector) *JobMetrics {
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
		jm.collector.getGaugeValue("ofelia_jobs_running")+1)
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
		jm.collector.getGaugeValue("ofelia_jobs_running")-1)
}

// Helper method to get gauge value
//
//nolint:unparam // name is parameterized for future gauge types
func (mc *Collector) getGaugeValue(name string) float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if metric, exists := mc.metrics[name]; exists && metric.Type == MetricTypeGauge {
		return metric.Value
	}
	return 0
}

// HTTPMetrics middleware for tracking HTTP requests
func HTTPMetrics(mc *Collector) func(http.Handler) http.Handler {
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
