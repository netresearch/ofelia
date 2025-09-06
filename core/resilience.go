package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// RetryPolicy defines retry behavior
type RetryPolicy struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	JitterFactor    float64
	RetryableErrors func(error) bool
}

// DefaultRetryPolicy returns a default retry policy
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:   3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		JitterFactor:  0.1,
		RetryableErrors: func(err error) bool {
			// By default, retry on any error
			// Can be customized for specific error types
			return true
		},
	}
}

// Retry executes a function with retry logic
func Retry(ctx context.Context, policy *RetryPolicy, fn func() error) error {
	if policy == nil {
		policy = DefaultRetryPolicy()
	}

	var lastErr error
	delay := policy.InitialDelay

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		// Execute the function
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !policy.RetryableErrors(err) {
			return fmt.Errorf("non-retryable error: %w", err)
		}

		// Check if we've exhausted attempts
		if attempt >= policy.MaxAttempts {
			break
		}

		// Apply jitter to delay
		jitter := time.Duration(float64(delay) * policy.JitterFactor * (0.5 - math.Mod(float64(time.Now().UnixNano()), 1)))
		sleepDuration := delay + jitter

		// Wait before next attempt
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry canceled: %w", ctx.Err())
		case <-time.After(sleepDuration):
			// Continue to next attempt
		}

		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * policy.BackoffFactor)
		if delay > policy.MaxDelay {
			delay = policy.MaxDelay
		}
	}

	return fmt.Errorf("max retry attempts (%d) exceeded: %w", policy.MaxAttempts, lastErr)
}

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name             string
	maxFailures      uint32
	resetTimeout     time.Duration
	halfOpenMaxCalls uint32

	mu              sync.Mutex
	state           CircuitBreakerState
	failures        uint32
	lastFailureTime time.Time
	successCount    uint32
	halfOpenCalls   uint32

	// Metrics
	totalCalls     uint64
	totalFailures  uint64
	totalSuccesses uint64
	lastOpenedAt   time.Time
	openDuration   time.Duration
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, maxFailures uint32, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:             name,
		maxFailures:      maxFailures,
		resetTimeout:     resetTimeout,
		halfOpenMaxCalls: 1,
		state:            StateClosed,
	}
}

// Execute runs a function through the circuit breaker
func (cb *CircuitBreaker) Execute(fn func() error) error {
	// Check if we can execute
	if err := cb.beforeCall(); err != nil {
		return err
	}

	// Execute the function
	err := fn()

	// Record the result
	cb.afterCall(err == nil)

	return err
}

// beforeCall checks if the circuit breaker allows the call
func (cb *CircuitBreaker) beforeCall() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.AddUint64(&cb.totalCalls, 1)

	switch cb.state {
	case StateClosed:
		// Allow the call
		return nil

	case StateOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.transitionToHalfOpen()
			return nil
		}
		return fmt.Errorf("circuit breaker '%s' is open", cb.name)

	case StateHalfOpen:
		// Allow limited calls in half-open state
		if cb.halfOpenCalls >= cb.halfOpenMaxCalls {
			return fmt.Errorf("circuit breaker '%s' is half-open but max calls reached", cb.name)
		}
		cb.halfOpenCalls++
		return nil

	default:
		return fmt.Errorf("circuit breaker '%s' is in unknown state", cb.name)
	}
}

// afterCall records the result of a call
func (cb *CircuitBreaker) afterCall(success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if success {
		cb.onSuccess()
	} else {
		cb.onFailure()
	}
}

// onSuccess handles successful calls
func (cb *CircuitBreaker) onSuccess() {
	atomic.AddUint64(&cb.totalSuccesses, 1)

	switch cb.state {
	case StateClosed:
		// Reset failure count on success
		cb.failures = 0

	case StateHalfOpen:
		cb.successCount++
		// Transition to closed if we've had enough successes
		if cb.successCount >= cb.halfOpenMaxCalls {
			cb.transitionToClosed()
		}

	case StateOpen:
		// No action needed in open state for success
		// State transitions are handled by the timeout mechanism
	}
}

// onFailure handles failed calls
func (cb *CircuitBreaker) onFailure() {
	atomic.AddUint64(&cb.totalFailures, 1)

	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.maxFailures {
			cb.transitionToOpen()
		}

	case StateHalfOpen:
		// Immediately open on failure in half-open state
		cb.transitionToOpen()

	case StateOpen:
		// Already open, no action needed
	}
}

// transitionToOpen transitions the circuit breaker to open state
func (cb *CircuitBreaker) transitionToOpen() {
	cb.state = StateOpen
	cb.lastOpenedAt = time.Now()
	cb.failures = 0
	cb.successCount = 0
	cb.halfOpenCalls = 0
}

// transitionToHalfOpen transitions the circuit breaker to half-open state
func (cb *CircuitBreaker) transitionToHalfOpen() {
	cb.state = StateHalfOpen
	cb.successCount = 0
	cb.halfOpenCalls = 0

	// Track how long we were open
	if !cb.lastOpenedAt.IsZero() {
		cb.openDuration += time.Since(cb.lastOpenedAt)
	}
}

// transitionToClosed transitions the circuit breaker to closed state
func (cb *CircuitBreaker) transitionToClosed() {
	cb.state = StateClosed
	cb.failures = 0
	cb.successCount = 0
	cb.halfOpenCalls = 0
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// GetMetrics returns circuit breaker metrics
func (cb *CircuitBreaker) GetMetrics() map[string]interface{} {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return map[string]interface{}{
		"name":             cb.name,
		"state":            cb.state.String(),
		"total_calls":      atomic.LoadUint64(&cb.totalCalls),
		"total_successes":  atomic.LoadUint64(&cb.totalSuccesses),
		"total_failures":   atomic.LoadUint64(&cb.totalFailures),
		"current_failures": cb.failures,
		"open_duration":    cb.openDuration.Seconds(),
	}
}

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	rate       float64 // tokens per second
	capacity   int     // maximum tokens
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rate float64, capacity int) *RateLimiter {
	return &RateLimiter{
		rate:       rate,
		capacity:   capacity,
		tokens:     float64(capacity),
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow() bool {
	return rl.AllowN(1)
}

// AllowN checks if n requests are allowed
func (rl *RateLimiter) AllowN(n int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.refill()

	if rl.tokens >= float64(n) {
		rl.tokens -= float64(n)
		return true
	}

	return false
}

// refill adds tokens based on elapsed time
func (rl *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()

	rl.tokens += elapsed * rl.rate
	if rl.tokens > float64(rl.capacity) {
		rl.tokens = float64(rl.capacity)
	}

	rl.lastRefill = now
}

// Wait blocks until a request is allowed
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.WaitN(ctx, 1)
}

// WaitN blocks until n requests are allowed
func (rl *RateLimiter) WaitN(ctx context.Context, n int) error {
	if n > rl.capacity {
		return errors.New("requested tokens exceed capacity")
	}

	for {
		if rl.AllowN(n) {
			return nil
		}

		// Calculate wait time
		rl.mu.Lock()
		tokensNeeded := float64(n) - rl.tokens
		waitDuration := time.Duration(tokensNeeded/rl.rate*1000) * time.Millisecond
		rl.mu.Unlock()

		select {
		case <-ctx.Done():
			return fmt.Errorf("rate limiter wait canceled: %w", ctx.Err())
		case <-time.After(waitDuration):
			// Continue to retry
		}
	}
}

// Bulkhead implements the bulkhead pattern for resource isolation
type Bulkhead struct {
	name          string
	maxConcurrent int
	semaphore     chan struct{}
	active        int32
	rejected      uint64
	completed     uint64
}

// NewBulkhead creates a new bulkhead
func NewBulkhead(name string, maxConcurrent int) *Bulkhead {
	return &Bulkhead{
		name:          name,
		maxConcurrent: maxConcurrent,
		semaphore:     make(chan struct{}, maxConcurrent),
	}
}

// Execute runs a function with bulkhead protection
func (b *Bulkhead) Execute(ctx context.Context, fn func() error) error {
	select {
	case b.semaphore <- struct{}{}:
		// Acquired a slot
		atomic.AddInt32(&b.active, 1)
		defer func() {
			<-b.semaphore
			atomic.AddInt32(&b.active, -1)
			atomic.AddUint64(&b.completed, 1)
		}()

		return fn()

	case <-ctx.Done():
		atomic.AddUint64(&b.rejected, 1)
		return fmt.Errorf("bulkhead '%s' context canceled: %w", b.name, ctx.Err())

	default:
		atomic.AddUint64(&b.rejected, 1)
		return fmt.Errorf("bulkhead '%s' is full (%d/%d)", b.name, b.maxConcurrent, b.maxConcurrent)
	}
}

// GetMetrics returns bulkhead metrics
func (b *Bulkhead) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"name":           b.name,
		"max_concurrent": b.maxConcurrent,
		"active":         atomic.LoadInt32(&b.active),
		"rejected":       atomic.LoadUint64(&b.rejected),
		"completed":      atomic.LoadUint64(&b.completed),
	}
}
