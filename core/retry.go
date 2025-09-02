package core

import (
	"fmt"
	"math"
	"time"
)

// RetryConfig contains retry configuration for a job
type RetryConfig struct {
	MaxRetries       int
	RetryDelayMs     int
	RetryExponential bool
	RetryMaxDelayMs  int
}

// RetryableJob interface for jobs that support retries
type RetryableJob interface {
	Job
	GetRetryConfig() RetryConfig
}

// GetRetryConfig returns the retry configuration for the job
func (j *BareJob) GetRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:       j.MaxRetries,
		RetryDelayMs:     j.RetryDelayMs,
		RetryExponential: j.RetryExponential,
		RetryMaxDelayMs:  j.RetryMaxDelayMs,
	}
}

// MetricsRecorder interface for recording retry metrics
type MetricsRecorder interface {
	RecordJobRetry(jobName string, attempt int, success bool)
}

// RetryExecutor wraps job execution with retry logic
type RetryExecutor struct {
	logger  Logger
	metrics MetricsRecorder
}

// NewRetryExecutor creates a new retry executor
func NewRetryExecutor(logger Logger) *RetryExecutor {
	return &RetryExecutor{
		logger: logger,
	}
}

// SetMetricsRecorder sets the metrics recorder for the retry executor
func (re *RetryExecutor) SetMetricsRecorder(metrics MetricsRecorder) {
	re.metrics = metrics
}

// ExecuteWithRetry executes a job with retry logic
func (re *RetryExecutor) ExecuteWithRetry(job Job, ctx *Context, runFunc func(*Context) error) error {
	// Check if job supports retries
	retryableJob, ok := job.(RetryableJob)
	if !ok {
		// No retry support, execute once
		return runFunc(ctx)
	}
	
	config := retryableJob.GetRetryConfig()
	
	// If no retries configured, execute once
	if config.MaxRetries <= 0 {
		return runFunc(ctx)
	}
	
	var lastErr error
	attempt := 0
	
	for attempt <= config.MaxRetries {
		// Execute the job
		err := runFunc(ctx)
		
		// Success
		if err == nil {
			if attempt > 0 {
				re.logger.Noticef("Job %s succeeded after %d retries", job.GetName(), attempt)
				// Record retry success in metrics
				if re.metrics != nil {
					re.metrics.RecordJobRetry(job.GetName(), attempt, true)
				}
			}
			return nil
		}
		
		lastErr = err
		
		// Check if we have retries left
		if attempt >= config.MaxRetries {
			break
		}
		
		// Calculate delay
		delay := re.calculateDelay(config, attempt)
		
		re.logger.Warningf("Job %s failed (attempt %d/%d): %v. Retrying in %v", 
			job.GetName(), attempt+1, config.MaxRetries+1, err, delay)
		
		// Record retry attempt in metrics
		if re.metrics != nil {
			re.metrics.RecordJobRetry(job.GetName(), attempt+1, false)
		}
		
		// Wait before retry
		time.Sleep(delay)
		
		attempt++
	}
	
	// All retries exhausted
	re.logger.Errorf("Job %s failed after %d retries: %v", 
		job.GetName(), config.MaxRetries+1, lastErr)
	
	// Record final failure in metrics
	if re.metrics != nil {
		re.metrics.RecordJobRetry(job.GetName(), config.MaxRetries+1, false)
	}
	
	return fmt.Errorf("job failed after %d attempts: %w", config.MaxRetries+1, lastErr)
}

// calculateDelay calculates the retry delay based on configuration
func (re *RetryExecutor) calculateDelay(config RetryConfig, attempt int) time.Duration {
	delayMs := config.RetryDelayMs
	
	if config.RetryExponential {
		// Exponential backoff: delay * 2^attempt
		delayMs = int(float64(config.RetryDelayMs) * math.Pow(2, float64(attempt)))
		
		// Cap at maximum delay
		if delayMs > config.RetryMaxDelayMs {
			delayMs = config.RetryMaxDelayMs
		}
	}
	
	return time.Duration(delayMs) * time.Millisecond
}

// RetryStats tracks retry statistics for a job
type RetryStats struct {
	JobName       string
	TotalAttempts int
	SuccessAfter  int // Number of retries before success (0 if first attempt succeeded)
	Failed        bool
	LastError     error
}