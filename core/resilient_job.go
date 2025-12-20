package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ResilientJobExecutor wraps job execution with resilience patterns
type ResilientJobExecutor struct {
	job            Job
	retryPolicy    *RetryPolicy
	circuitBreaker *CircuitBreaker
	rateLimiter    *RateLimiter
	bulkhead       *Bulkhead
	metrics        JobMetricsRecorder
	mu             sync.RWMutex
}

// NewResilientJobExecutor creates a new resilient job executor
func NewResilientJobExecutor(job Job) *ResilientJobExecutor {
	jobName := job.GetName()

	// Create retry policy with job-specific configuration
	retryPolicy := &RetryPolicy{
		MaxAttempts:   3,
		InitialDelay:  2 * time.Second,
		MaxDelay:      60 * time.Second,
		BackoffFactor: 2.0,
		JitterFactor:  0.1,
		RetryableErrors: func(err error) bool {
			// Don't retry on certain errors
			if errors.Is(err, context.Canceled) {
				return false
			}
			if errors.Is(err, ErrSkippedExecution) {
				return false
			}

			// Check for non-retryable error conditions
			errStr := err.Error()
			// Don't retry on resource not found errors
			if strings.Contains(errStr, "404") || strings.Contains(errStr, "not found") {
				return false
			}
			// Don't retry on invalid parameter errors
			if strings.Contains(errStr, "400") || strings.Contains(errStr, "invalid") {
				return false
			}

			// Retry on other errors by default
			return true
		},
	}

	// Create circuit breaker
	circuitBreaker := NewCircuitBreaker(
		fmt.Sprintf("job_%s", jobName),
		5,              // Open after 5 failures
		30*time.Second, // Reset timeout
	)

	// Create rate limiter (prevent job from running too frequently)
	rateLimiter := NewRateLimiter(
		1.0, // 1 execution per second max
		10,  // Burst capacity of 10
	)

	// Create bulkhead (limit concurrent executions)
	bulkhead := NewBulkhead(
		fmt.Sprintf("job_%s", jobName),
		3, // Max 3 concurrent executions of the same job
	)

	return &ResilientJobExecutor{
		job:            job,
		retryPolicy:    retryPolicy,
		circuitBreaker: circuitBreaker,
		rateLimiter:    rateLimiter,
		bulkhead:       bulkhead,
	}
}

// Execute runs the job with resilience patterns
func (rje *ResilientJobExecutor) Execute(ctx *Context) error {
	// Check rate limit
	if !rje.rateLimiter.Allow() {
		ctx.Warn("Job execution rate limited")
		return fmt.Errorf("%w: %s", ErrRateLimitExceeded, rje.job.GetName())
	}

	// Create context with timeout if needed
	var cancel context.CancelFunc
	execCtx := context.Background()

	// Add timeout if configured (default to 5 minutes)
	timeout := 5 * time.Minute
	execCtx, cancel = context.WithTimeout(execCtx, timeout)
	defer cancel()

	// Execute with bulkhead protection
	bulkheadErr := rje.bulkhead.Execute(execCtx, func() error {
		// Execute with circuit breaker
		return rje.circuitBreaker.Execute(func() error {
			// Execute with retry logic
			return Retry(execCtx, rje.retryPolicy, func() error {
				return rje.executeJob(ctx)
			})
		})
	})

	// Record metrics
	if rje.metrics != nil {
		rje.recordMetrics(bulkheadErr == nil)
	}

	return bulkheadErr
}

// executeJob performs the actual job execution
func (rje *ResilientJobExecutor) executeJob(ctx *Context) error {
	startTime := time.Now()

	// Log execution start
	ctx.Log(fmt.Sprintf("Starting resilient execution of job: %s", rje.job.GetName()))

	// Execute the job
	err := rje.job.Run(ctx)

	// Log execution result
	duration := time.Since(startTime)
	if err != nil {
		ctx.Warn(fmt.Sprintf("Job %s failed after %v: %v", rje.job.GetName(), duration, err))
		return fmt.Errorf("resilient job %s execution failed: %w", rje.job.GetName(), err)
	} else {
		ctx.Log(fmt.Sprintf("Job %s completed successfully in %v", rje.job.GetName(), duration))
	}

	return nil
}

// recordMetrics records execution metrics
func (rje *ResilientJobExecutor) recordMetrics(success bool) {
	if rje.metrics == nil {
		return
	}

	// Record job success/failure metrics
	rje.metrics.RecordJobExecution(rje.job.GetName(), success, time.Duration(0))

	// Record circuit breaker metrics
	cbMetrics := rje.circuitBreaker.GetMetrics()
	for key, value := range cbMetrics {
		rje.metrics.RecordMetric(fmt.Sprintf("circuit_breaker.%s", key), value)
	}

	// Record bulkhead metrics
	bhMetrics := rje.bulkhead.GetMetrics()
	for key, value := range bhMetrics {
		rje.metrics.RecordMetric(fmt.Sprintf("bulkhead.%s", key), value)
	}
}

// SetRetryPolicy updates the retry policy
func (rje *ResilientJobExecutor) SetRetryPolicy(policy *RetryPolicy) {
	rje.mu.Lock()
	defer rje.mu.Unlock()
	rje.retryPolicy = policy
}

// SetCircuitBreaker updates the circuit breaker
func (rje *ResilientJobExecutor) SetCircuitBreaker(cb *CircuitBreaker) {
	rje.mu.Lock()
	defer rje.mu.Unlock()
	rje.circuitBreaker = cb
}

// SetRateLimiter updates the rate limiter
func (rje *ResilientJobExecutor) SetRateLimiter(rl *RateLimiter) {
	rje.mu.Lock()
	defer rje.mu.Unlock()
	rje.rateLimiter = rl
}

// SetBulkhead updates the bulkhead
func (rje *ResilientJobExecutor) SetBulkhead(bh *Bulkhead) {
	rje.mu.Lock()
	defer rje.mu.Unlock()
	rje.bulkhead = bh
}

// SetMetricsRecorder sets the metrics recorder
func (rje *ResilientJobExecutor) SetMetricsRecorder(metrics JobMetricsRecorder) {
	rje.mu.Lock()
	defer rje.mu.Unlock()
	rje.metrics = metrics
}

// GetCircuitBreakerState returns the current state of the circuit breaker
func (rje *ResilientJobExecutor) GetCircuitBreakerState() CircuitBreakerState {
	rje.mu.RLock()
	defer rje.mu.RUnlock()
	return rje.circuitBreaker.GetState()
}

// ResetCircuitBreaker manually resets the circuit breaker
func (rje *ResilientJobExecutor) ResetCircuitBreaker() {
	rje.mu.Lock()
	defer rje.mu.Unlock()
	rje.circuitBreaker.transitionToClosed()
}

// JobMetricsRecorder interface for recording job-specific metrics
type JobMetricsRecorder interface {
	RecordMetric(name string, value interface{})
	RecordJobExecution(jobName string, success bool, duration time.Duration)
	RecordRetryAttempt(jobName string, attempt int, success bool)
}

// SimpleMetricsRecorder provides a basic implementation of JobMetricsRecorder
type SimpleMetricsRecorder struct {
	mu      sync.RWMutex
	metrics map[string]interface{}
}

// NewSimpleMetricsRecorder creates a new simple metrics recorder
func NewSimpleMetricsRecorder() *SimpleMetricsRecorder {
	return &SimpleMetricsRecorder{
		metrics: make(map[string]interface{}),
	}
}

// RecordMetric records a generic metric
func (smr *SimpleMetricsRecorder) RecordMetric(name string, value interface{}) {
	smr.mu.Lock()
	defer smr.mu.Unlock()
	smr.metrics[name] = value
}

// RecordJobExecution records job execution metrics
func (smr *SimpleMetricsRecorder) RecordJobExecution(jobName string, success bool, duration time.Duration) {
	smr.mu.Lock()
	defer smr.mu.Unlock()

	key := fmt.Sprintf("job.%s.last_execution", jobName)
	smr.metrics[key] = map[string]interface{}{
		"success":  success,
		"duration": duration.Seconds(),
		"time":     time.Now(),
	}
}

// RecordRetryAttempt records retry attempt metrics
func (smr *SimpleMetricsRecorder) RecordRetryAttempt(jobName string, attempt int, success bool) {
	smr.mu.Lock()
	defer smr.mu.Unlock()

	key := fmt.Sprintf("job.%s.retry.attempt_%d", jobName, attempt)
	smr.metrics[key] = map[string]interface{}{
		"success": success,
		"time":    time.Now(),
	}
}

// GetMetrics returns all recorded metrics
func (smr *SimpleMetricsRecorder) GetMetrics() map[string]interface{} {
	smr.mu.RLock()
	defer smr.mu.RUnlock()

	// Create a copy to avoid race conditions
	result := make(map[string]interface{})
	for k, v := range smr.metrics {
		result[k] = v
	}
	return result
}
