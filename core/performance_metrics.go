package core

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// PerformanceRecorder defines the interface for recording comprehensive performance metrics
// This extends the existing MetricsRecorder interface with additional capabilities
type PerformanceRecorder interface {
	MetricsRecorder // Embed existing interface

	// Extended Docker operations
	RecordDockerError(operation string)
	RecordDockerLatency(operation string, duration time.Duration)

	// Job operations
	RecordJobExecution(jobName string, duration time.Duration, success bool)
	RecordJobScheduled(jobName string)
	RecordJobSkipped(jobName string, reason string)

	// System metrics
	RecordConcurrentJobs(count int64)
	RecordMemoryUsage(bytes int64)
	RecordBufferPoolStats(stats map[string]interface{})

	// Custom metrics
	RecordCustomMetric(name string, value interface{})

	// Retrieval
	GetMetrics() map[string]interface{}
	GetDockerMetrics() map[string]interface{}
	GetJobMetrics() map[string]interface{}
	Reset()
}

// PerformanceMetrics implements comprehensive performance tracking
type PerformanceMetrics struct {
	// Docker metrics
	dockerOpsCount    map[string]int64
	dockerErrorsCount map[string]int64
	dockerLatencies   map[string]*LatencyTracker
	dockerMutex       sync.RWMutex

	// Job metrics
	jobExecutions      map[string]*JobMetrics
	jobMutex           sync.RWMutex
	totalJobsScheduled int64
	totalJobsExecuted  int64
	totalJobsSkipped   int64
	totalJobsFailed    int64

	// System metrics
	maxConcurrentJobs  int64
	currentJobs        int64
	peakMemoryUsage    int64
	currentMemoryUsage int64

	// Buffer pool metrics
	bufferPoolStats map[string]interface{}
	bufferMutex     sync.RWMutex

	// Custom metrics
	customMetrics map[string]interface{}
	customMutex   sync.RWMutex

	// Retry metrics (to satisfy existing MetricsRecorder interface)
	retryMetrics map[string]*RetryMetrics
	retryMutex   sync.RWMutex

	// Container metrics (to satisfy existing MetricsRecorder interface)
	containerEvents           int64
	containerMonitorFallbacks int64
	containerWaitDurations    []float64
	containerMutex            sync.RWMutex

	// Timestamps
	startTime time.Time
}

// RetryMetrics holds retry-specific metrics
type RetryMetrics struct {
	TotalAttempts     int64
	SuccessfulRetries int64
	FailedRetries     int64
	LastRetry         time.Time
}

// JobMetrics holds metrics for individual jobs
type JobMetrics struct {
	ExecutionCount  int64
	TotalDuration   time.Duration
	AverageDuration time.Duration
	MinDuration     time.Duration
	MaxDuration     time.Duration
	SuccessCount    int64
	FailureCount    int64
	LastExecution   time.Time
	LastSuccess     time.Time
	LastFailure     time.Time
}

// LatencyTracker tracks latency statistics for operations
type LatencyTracker struct {
	Count   int64
	Total   time.Duration
	Min     time.Duration
	Max     time.Duration
	Average time.Duration
	mutex   sync.RWMutex
}

// NewPerformanceMetrics creates a new performance metrics recorder
func NewPerformanceMetrics() *PerformanceMetrics {
	return &PerformanceMetrics{
		dockerOpsCount:         make(map[string]int64),
		dockerErrorsCount:      make(map[string]int64),
		dockerLatencies:        make(map[string]*LatencyTracker),
		jobExecutions:          make(map[string]*JobMetrics),
		bufferPoolStats:        make(map[string]interface{}),
		customMetrics:          make(map[string]interface{}),
		retryMetrics:           make(map[string]*RetryMetrics),
		containerWaitDurations: make([]float64, 0),
		startTime:              time.Now(),
	}
}

// Implement existing MetricsRecorder interface methods

// RecordJobRetry records job retry attempts
func (pm *PerformanceMetrics) RecordJobRetry(jobName string, attempt int, success bool) {
	pm.retryMutex.Lock()
	defer pm.retryMutex.Unlock()

	metrics, exists := pm.retryMetrics[jobName]
	if !exists {
		metrics = &RetryMetrics{}
		pm.retryMetrics[jobName] = metrics
	}

	metrics.TotalAttempts++
	metrics.LastRetry = time.Now()

	if success {
		metrics.SuccessfulRetries++
	} else {
		metrics.FailedRetries++
	}
}

// RecordContainerEvent records container events
func (pm *PerformanceMetrics) RecordContainerEvent() {
	atomic.AddInt64(&pm.containerEvents, 1)
}

// RecordContainerMonitorFallback records container monitor fallbacks
func (pm *PerformanceMetrics) RecordContainerMonitorFallback() {
	atomic.AddInt64(&pm.containerMonitorFallbacks, 1)
}

// RecordContainerMonitorMethod records container monitor method usage
func (pm *PerformanceMetrics) RecordContainerMonitorMethod(usingEvents bool) {
	pm.RecordCustomMetric("container_monitor_using_events", usingEvents)
}

// RecordContainerWaitDuration records container wait durations
func (pm *PerformanceMetrics) RecordContainerWaitDuration(seconds float64) {
	pm.containerMutex.Lock()
	defer pm.containerMutex.Unlock()

	pm.containerWaitDurations = append(pm.containerWaitDurations, seconds)

	// Keep only last 1000 durations to prevent memory growth
	if len(pm.containerWaitDurations) > 1000 {
		pm.containerWaitDurations = pm.containerWaitDurations[len(pm.containerWaitDurations)-1000:]
	}
}

// RecordDockerOperation records a successful Docker operation
func (pm *PerformanceMetrics) RecordDockerOperation(operation string) {
	pm.dockerMutex.Lock()
	pm.dockerOpsCount[operation]++
	pm.dockerMutex.Unlock()
}

// RecordDockerError records a Docker operation error
func (pm *PerformanceMetrics) RecordDockerError(operation string) {
	pm.dockerMutex.Lock()
	pm.dockerErrorsCount[operation]++
	pm.dockerMutex.Unlock()
}

// RecordDockerLatency records the latency of a Docker operation
func (pm *PerformanceMetrics) RecordDockerLatency(operation string, duration time.Duration) {
	pm.dockerMutex.Lock()

	tracker, exists := pm.dockerLatencies[operation]
	if !exists {
		tracker = &LatencyTracker{
			Min: duration,
			Max: duration,
		}
		pm.dockerLatencies[operation] = tracker
	}

	pm.dockerMutex.Unlock()

	// Update latency tracker
	tracker.mutex.Lock()
	tracker.Count++
	tracker.Total += duration
	tracker.Average = tracker.Total / time.Duration(tracker.Count)

	if duration < tracker.Min || tracker.Min == 0 {
		tracker.Min = duration
	}
	if duration > tracker.Max {
		tracker.Max = duration
	}
	tracker.mutex.Unlock()
}

// RecordJobExecution records a job execution with timing and success status
func (pm *PerformanceMetrics) RecordJobExecution(jobName string, duration time.Duration, success bool) {
	atomic.AddInt64(&pm.totalJobsExecuted, 1)
	if !success {
		atomic.AddInt64(&pm.totalJobsFailed, 1)
	}

	pm.jobMutex.Lock()

	metrics, exists := pm.jobExecutions[jobName]
	if !exists {
		metrics = &JobMetrics{
			MinDuration: duration,
			MaxDuration: duration,
		}
		pm.jobExecutions[jobName] = metrics
	}

	pm.jobMutex.Unlock()

	// Update job metrics
	now := time.Now()
	metrics.ExecutionCount++
	metrics.TotalDuration += duration
	metrics.AverageDuration = metrics.TotalDuration / time.Duration(metrics.ExecutionCount)
	metrics.LastExecution = now

	if duration < metrics.MinDuration || metrics.MinDuration == 0 {
		metrics.MinDuration = duration
	}
	if duration > metrics.MaxDuration {
		metrics.MaxDuration = duration
	}

	if success {
		metrics.SuccessCount++
		metrics.LastSuccess = now
	} else {
		metrics.FailureCount++
		metrics.LastFailure = now
	}
}

// RecordJobScheduled records when a job is scheduled
func (pm *PerformanceMetrics) RecordJobScheduled(jobName string) {
	atomic.AddInt64(&pm.totalJobsScheduled, 1)
}

// RecordJobSkipped records when a job is skipped
func (pm *PerformanceMetrics) RecordJobSkipped(jobName string, reason string) {
	atomic.AddInt64(&pm.totalJobsSkipped, 1)

	pm.customMutex.Lock()
	skipReasons := pm.customMetrics["job_skip_reasons"]
	if skipReasons == nil {
		skipReasons = make(map[string]int64)
		pm.customMetrics["job_skip_reasons"] = skipReasons
	}
	if reasonMap, ok := skipReasons.(map[string]int64); ok {
		reasonMap[reason]++
	}
	pm.customMutex.Unlock()
}

// RecordConcurrentJobs tracks the number of concurrent jobs
func (pm *PerformanceMetrics) RecordConcurrentJobs(count int64) {
	atomic.StoreInt64(&pm.currentJobs, count)

	// Track peak
	for {
		peak := atomic.LoadInt64(&pm.maxConcurrentJobs)
		if count <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&pm.maxConcurrentJobs, peak, count) {
			break
		}
	}
}

// RecordMemoryUsage tracks memory usage
func (pm *PerformanceMetrics) RecordMemoryUsage(bytes int64) {
	atomic.StoreInt64(&pm.currentMemoryUsage, bytes)

	// Track peak
	for {
		peak := atomic.LoadInt64(&pm.peakMemoryUsage)
		if bytes <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&pm.peakMemoryUsage, peak, bytes) {
			break
		}
	}
}

// RecordBufferPoolStats records buffer pool performance statistics
func (pm *PerformanceMetrics) RecordBufferPoolStats(stats map[string]interface{}) {
	pm.bufferMutex.Lock()
	pm.bufferPoolStats = stats
	pm.bufferMutex.Unlock()
}

// RecordCustomMetric records a custom metric
func (pm *PerformanceMetrics) RecordCustomMetric(name string, value interface{}) {
	pm.customMutex.Lock()
	pm.customMetrics[name] = value
	pm.customMutex.Unlock()
}

// GetMetrics returns all performance metrics
func (pm *PerformanceMetrics) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"docker":      pm.GetDockerMetrics(),
		"jobs":        pm.GetJobMetrics(),
		"system":      pm.getSystemMetrics(),
		"buffer_pool": pm.getBufferPoolMetrics(),
		"retries":     pm.getRetryMetrics(),
		"container":   pm.getContainerMetrics(),
		"custom":      pm.getCustomMetrics(),
		"uptime":      time.Since(pm.startTime),
	}
}

// GetDockerMetrics returns Docker-specific metrics
func (pm *PerformanceMetrics) GetDockerMetrics() map[string]interface{} {
	pm.dockerMutex.RLock()
	defer pm.dockerMutex.RUnlock()

	// Calculate totals
	totalOps := int64(0)
	totalErrors := int64(0)

	for _, count := range pm.dockerOpsCount {
		totalOps += count
	}
	for _, count := range pm.dockerErrorsCount {
		totalErrors += count
	}

	// Build latency stats
	latencyStats := make(map[string]map[string]interface{})
	for operation, tracker := range pm.dockerLatencies {
		tracker.mutex.RLock()
		latencyStats[operation] = map[string]interface{}{
			"count":   tracker.Count,
			"average": tracker.Average,
			"min":     tracker.Min,
			"max":     tracker.Max,
			"total":   tracker.Total,
		}
		tracker.mutex.RUnlock()
	}

	errorRate := float64(0)
	if totalOps > 0 {
		errorRate = float64(totalErrors) / float64(totalOps) * 100
	}

	return map[string]interface{}{
		"total_operations":   totalOps,
		"total_errors":       totalErrors,
		"error_rate_percent": errorRate,
		"operations_by_type": pm.dockerOpsCount,
		"errors_by_type":     pm.dockerErrorsCount,
		"latencies":          latencyStats,
	}
}

// GetJobMetrics returns job execution metrics
func (pm *PerformanceMetrics) GetJobMetrics() map[string]interface{} {
	pm.jobMutex.RLock()
	defer pm.jobMutex.RUnlock()

	totalScheduled := atomic.LoadInt64(&pm.totalJobsScheduled)
	totalExecuted := atomic.LoadInt64(&pm.totalJobsExecuted)
	totalSkipped := atomic.LoadInt64(&pm.totalJobsSkipped)
	totalFailed := atomic.LoadInt64(&pm.totalJobsFailed)

	successRate := float64(0)
	if totalExecuted > 0 {
		successRate = float64(totalExecuted-totalFailed) / float64(totalExecuted) * 100
	}

	jobStats := make(map[string]interface{})
	for jobName, metrics := range pm.jobExecutions {
		jobSuccessRate := float64(0)
		if metrics.ExecutionCount > 0 {
			jobSuccessRate = float64(metrics.SuccessCount) / float64(metrics.ExecutionCount) * 100
		}

		jobStats[jobName] = map[string]interface{}{
			"executions":     metrics.ExecutionCount,
			"success_count":  metrics.SuccessCount,
			"failure_count":  metrics.FailureCount,
			"success_rate":   jobSuccessRate,
			"avg_duration":   metrics.AverageDuration,
			"min_duration":   metrics.MinDuration,
			"max_duration":   metrics.MaxDuration,
			"total_duration": metrics.TotalDuration,
			"last_execution": metrics.LastExecution,
			"last_success":   metrics.LastSuccess,
			"last_failure":   metrics.LastFailure,
		}
	}

	return map[string]interface{}{
		"total_scheduled":      totalScheduled,
		"total_executed":       totalExecuted,
		"total_skipped":        totalSkipped,
		"total_failed":         totalFailed,
		"success_rate_percent": successRate,
		"job_details":          jobStats,
	}
}

// getSystemMetrics returns system performance metrics
func (pm *PerformanceMetrics) getSystemMetrics() map[string]interface{} {
	return map[string]interface{}{
		"concurrent_jobs":      atomic.LoadInt64(&pm.currentJobs),
		"max_concurrent_jobs":  atomic.LoadInt64(&pm.maxConcurrentJobs),
		"current_memory_usage": atomic.LoadInt64(&pm.currentMemoryUsage),
		"peak_memory_usage":    atomic.LoadInt64(&pm.peakMemoryUsage),
		"uptime_seconds":       time.Since(pm.startTime).Seconds(),
	}
}

// getBufferPoolMetrics returns buffer pool metrics
func (pm *PerformanceMetrics) getBufferPoolMetrics() map[string]interface{} {
	pm.bufferMutex.RLock()
	defer pm.bufferMutex.RUnlock()

	// Return a copy to avoid concurrent access issues
	result := make(map[string]interface{})
	for k, v := range pm.bufferPoolStats {
		result[k] = v
	}
	return result
}

// getRetryMetrics returns retry metrics
func (pm *PerformanceMetrics) getRetryMetrics() map[string]interface{} {
	pm.retryMutex.RLock()
	defer pm.retryMutex.RUnlock()

	retryStats := make(map[string]interface{})
	for jobName, metrics := range pm.retryMetrics {
		successRate := float64(0)
		if metrics.TotalAttempts > 0 {
			successRate = float64(metrics.SuccessfulRetries) / float64(metrics.TotalAttempts) * 100
		}

		retryStats[jobName] = map[string]interface{}{
			"total_attempts":     metrics.TotalAttempts,
			"successful_retries": metrics.SuccessfulRetries,
			"failed_retries":     metrics.FailedRetries,
			"success_rate":       successRate,
			"last_retry":         metrics.LastRetry,
		}
	}

	return retryStats
}

// getContainerMetrics returns container monitoring metrics
func (pm *PerformanceMetrics) getContainerMetrics() map[string]interface{} {
	pm.containerMutex.RLock()
	durations := make([]float64, len(pm.containerWaitDurations))
	copy(durations, pm.containerWaitDurations)
	pm.containerMutex.RUnlock()

	avgWaitDuration := float64(0)
	if len(durations) > 0 {
		sum := float64(0)
		for _, d := range durations {
			sum += d
		}
		avgWaitDuration = sum / float64(len(durations))
	}

	return map[string]interface{}{
		"total_events":          atomic.LoadInt64(&pm.containerEvents),
		"monitor_fallbacks":     atomic.LoadInt64(&pm.containerMonitorFallbacks),
		"avg_wait_duration":     avgWaitDuration,
		"wait_duration_samples": len(durations),
	}
}

// getCustomMetrics returns custom metrics
func (pm *PerformanceMetrics) getCustomMetrics() map[string]interface{} {
	pm.customMutex.RLock()
	defer pm.customMutex.RUnlock()

	// Return a copy to avoid concurrent access issues
	result := make(map[string]interface{})
	for k, v := range pm.customMetrics {
		result[k] = v
	}
	return result
}

// Reset clears all metrics (useful for testing or periodic resets)
func (pm *PerformanceMetrics) Reset() {
	pm.dockerMutex.Lock()
	pm.dockerOpsCount = make(map[string]int64)
	pm.dockerErrorsCount = make(map[string]int64)
	pm.dockerLatencies = make(map[string]*LatencyTracker)
	pm.dockerMutex.Unlock()

	pm.jobMutex.Lock()
	pm.jobExecutions = make(map[string]*JobMetrics)
	pm.jobMutex.Unlock()

	pm.retryMutex.Lock()
	pm.retryMetrics = make(map[string]*RetryMetrics)
	pm.retryMutex.Unlock()

	pm.containerMutex.Lock()
	pm.containerWaitDurations = make([]float64, 0)
	pm.containerMutex.Unlock()

	atomic.StoreInt64(&pm.totalJobsScheduled, 0)
	atomic.StoreInt64(&pm.totalJobsExecuted, 0)
	atomic.StoreInt64(&pm.totalJobsSkipped, 0)
	atomic.StoreInt64(&pm.totalJobsFailed, 0)
	atomic.StoreInt64(&pm.maxConcurrentJobs, 0)
	atomic.StoreInt64(&pm.currentJobs, 0)
	atomic.StoreInt64(&pm.peakMemoryUsage, 0)
	atomic.StoreInt64(&pm.currentMemoryUsage, 0)
	atomic.StoreInt64(&pm.containerEvents, 0)
	atomic.StoreInt64(&pm.containerMonitorFallbacks, 0)

	pm.bufferMutex.Lock()
	pm.bufferPoolStats = make(map[string]interface{})
	pm.bufferMutex.Unlock()

	pm.customMutex.Lock()
	pm.customMetrics = make(map[string]interface{})
	pm.customMutex.Unlock()

	pm.startTime = time.Now()
}

// GetSummaryReport generates a human-readable performance summary
func (pm *PerformanceMetrics) GetSummaryReport() string {
	metrics := pm.GetMetrics()

	report := "Performance Summary:\n"
	report += "===================\n\n"

	// Docker metrics summary
	if docker, ok := metrics["docker"].(map[string]interface{}); ok {
		report += "Docker Operations:\n"
		if totalOps, ok := docker["total_operations"].(int64); ok {
			report += fmt.Sprintf("  Total Operations: %d\n", totalOps)
		}
		if errorRate, ok := docker["error_rate_percent"].(float64); ok {
			report += fmt.Sprintf("  Error Rate: %.2f%%\n", errorRate)
		}
		report += "\n"
	}

	// Job metrics summary
	if jobs, ok := metrics["jobs"].(map[string]interface{}); ok {
		report += "Job Execution:\n"
		if totalExec, ok := jobs["total_executed"].(int64); ok {
			report += fmt.Sprintf("  Total Executed: %d\n", totalExec)
		}
		if successRate, ok := jobs["success_rate_percent"].(float64); ok {
			report += fmt.Sprintf("  Success Rate: %.2f%%\n", successRate)
		}
		report += "\n"
	}

	// System metrics summary
	if system, ok := metrics["system"].(map[string]interface{}); ok {
		report += "System Performance:\n"
		if maxJobs, ok := system["max_concurrent_jobs"].(int64); ok {
			report += fmt.Sprintf("  Peak Concurrent Jobs: %d\n", maxJobs)
		}
		if uptime, ok := metrics["uptime"].(time.Duration); ok {
			report += fmt.Sprintf("  Uptime: %v\n", uptime)
		}
	}

	return report
}

// Global enhanced metrics instance
var GlobalPerformanceMetrics = NewPerformanceMetrics()
