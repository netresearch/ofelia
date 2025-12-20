package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()
	policy := DefaultRetryPolicy()

	if policy.MaxAttempts != 3 {
		t.Errorf("Expected MaxAttempts=3, got %d", policy.MaxAttempts)
	}
	if policy.InitialDelay != 1*time.Second {
		t.Errorf("Expected InitialDelay=1s, got %v", policy.InitialDelay)
	}
	if policy.MaxDelay != 30*time.Second {
		t.Errorf("Expected MaxDelay=30s, got %v", policy.MaxDelay)
	}
	if policy.BackoffFactor != 2.0 {
		t.Errorf("Expected BackoffFactor=2.0, got %f", policy.BackoffFactor)
	}
	if policy.JitterFactor != 0.1 {
		t.Errorf("Expected JitterFactor=0.1, got %f", policy.JitterFactor)
	}
	if policy.RetryableErrors == nil {
		t.Error("RetryableErrors function should not be nil")
	}

	// Test default retryable errors function
	if !policy.RetryableErrors(errors.New("test error")) {
		t.Error("Default retryable errors function should return true for any error")
	}
}

func TestRetrySuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	policy := DefaultRetryPolicy()

	attempts := 0
	err := Retry(ctx, policy, func() error {
		attempts++
		return nil // Success on first attempt
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}
}

func TestRetryEventualSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	policy := DefaultRetryPolicy()
	policy.InitialDelay = 1 * time.Millisecond // Speed up test

	attempts := 0
	err := Retry(ctx, policy, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil // Success on third attempt
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestRetryMaxAttemptsExceeded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	policy := DefaultRetryPolicy()
	policy.InitialDelay = 1 * time.Millisecond // Speed up test

	attempts := 0
	testErr := errors.New("persistent failure")
	err := Retry(ctx, policy, func() error {
		attempts++
		return testErr
	})

	if err == nil {
		t.Error("Expected error after max attempts exceeded")
	}
	if attempts != policy.MaxAttempts {
		t.Errorf("Expected %d attempts, got %d", policy.MaxAttempts, attempts)
	}
}

func TestRetryNonRetryableError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	policy := DefaultRetryPolicy()
	policy.RetryableErrors = func(err error) bool {
		return err.Error() != "non-retryable"
	}

	attempts := 0
	err := Retry(ctx, policy, func() error {
		attempts++
		return errors.New("non-retryable")
	})

	if err == nil {
		t.Error("Expected error for non-retryable error")
	}
	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}
}

func TestRetryContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	policy := DefaultRetryPolicy()
	policy.InitialDelay = 100 * time.Millisecond

	attempts := 0
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := Retry(ctx, policy, func() error {
		attempts++
		return errors.New("failure")
	})

	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
	if attempts < 1 {
		t.Error("Should have at least one attempt before cancellation")
	}
}

func TestRetryNilPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	attempts := 0
	err := Retry(ctx, nil, func() error {
		attempts++
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error with nil policy, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}
}

func TestCircuitBreakerStateString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state    CircuitBreakerState
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitBreakerState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("State %d: expected %s, got %s", int(tt.state), tt.expected, got)
		}
	}
}

func TestNewCircuitBreaker(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker("test", 5, 10*time.Second)

	if cb.name != "test" {
		t.Errorf("Expected name 'test', got '%s'", cb.name)
	}
	if cb.maxFailures != 5 {
		t.Errorf("Expected maxFailures=5, got %d", cb.maxFailures)
	}
	if cb.resetTimeout != 10*time.Second {
		t.Errorf("Expected resetTimeout=10s, got %v", cb.resetTimeout)
	}
	if cb.state != StateClosed {
		t.Errorf("Expected initial state closed, got %v", cb.state)
	}
}

func TestCircuitBreakerExecuteSuccess(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker("test", 3, 5*time.Second)

	calls := 0
	err := cb.Execute(func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("Expected 1 call, got %d", calls)
	}
	if cb.GetState() != StateClosed {
		t.Errorf("Expected state closed, got %v", cb.GetState())
	}
}

func TestCircuitBreakerExecuteFailure(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker("test", 2, 5*time.Second)

	testErr := errors.New("test failure")

	// First failure
	err1 := cb.Execute(func() error {
		return testErr
	})
	if !errors.Is(err1, testErr) {
		t.Errorf("Expected test error, got %v", err1)
	}
	if cb.GetState() != StateClosed {
		t.Errorf("Expected state closed after 1 failure, got %v", cb.GetState())
	}

	// Second failure (should open circuit)
	err2 := cb.Execute(func() error {
		return testErr
	})
	if !errors.Is(err2, testErr) {
		t.Errorf("Expected test error, got %v", err2)
	}
	if cb.GetState() != StateOpen {
		t.Errorf("Expected state open after 2 failures, got %v", cb.GetState())
	}
}

func TestCircuitBreakerOpenState(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker("test", 1, 100*time.Millisecond)

	// Cause failure to open circuit
	cb.Execute(func() error {
		return errors.New("failure")
	})

	if cb.GetState() != StateOpen {
		t.Errorf("Expected state open, got %v", cb.GetState())
	}

	// Should reject calls in open state
	calls := 0
	err := cb.Execute(func() error {
		calls++
		return nil
	})

	if err == nil {
		t.Error("Expected error in open state")
	}
	if calls != 0 {
		t.Errorf("Expected 0 calls in open state, got %d", calls)
	}
}

func TestCircuitBreakerHalfOpenState(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker("test", 1, 50*time.Millisecond)

	// Cause failure to open circuit
	cb.Execute(func() error {
		return errors.New("failure")
	})

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// First call should transition to half-open
	calls := 0
	err := cb.Execute(func() error {
		calls++
		return nil // Success
	})
	if err != nil {
		t.Errorf("Expected no error in half-open state, got %v", err)
	}
	if calls != 1 {
		t.Errorf("Expected 1 call, got %d", calls)
	}
	if cb.GetState() != StateClosed {
		t.Errorf("Expected state closed after success in half-open, got %v", cb.GetState())
	}
}

func TestCircuitBreakerMetrics(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker("test-metrics", 1, 5*time.Second) // Lower threshold to ensure it opens

	// Execute some operations
	cb.Execute(func() error { return nil })                 // Success
	cb.Execute(func() error { return errors.New("fail1") }) // Failure (should open circuit)

	metrics := cb.GetMetrics()

	if metrics["name"] != "test-metrics" {
		t.Errorf("Expected name 'test-metrics', got %v", metrics["name"])
	}
	if metrics["state"] != "open" {
		t.Errorf("Expected state 'open', got %v", metrics["state"])
	}
	if metrics["total_calls"] != uint64(2) {
		t.Errorf("Expected 2 total calls, got %v", metrics["total_calls"])
	}
	if metrics["total_successes"] != uint64(1) {
		t.Errorf("Expected 1 success, got %v", metrics["total_successes"])
	}
	if metrics["total_failures"] != uint64(1) {
		t.Errorf("Expected 1 failure, got %v", metrics["total_failures"])
	}
}

func TestNewRateLimiter(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(10.0, 100)

	if rl.rate != 10.0 {
		t.Errorf("Expected rate=10.0, got %f", rl.rate)
	}
	if rl.capacity != 100 {
		t.Errorf("Expected capacity=100, got %d", rl.capacity)
	}
	if rl.tokens != 100.0 {
		t.Errorf("Expected initial tokens=100.0, got %f", rl.tokens)
	}
}

func TestRateLimiterAllow(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(1.0, 2)

	// Should allow first request
	if !rl.Allow() {
		t.Error("Expected first request to be allowed")
	}

	// Should allow second request
	if !rl.Allow() {
		t.Error("Expected second request to be allowed")
	}

	// Should not allow third request (no tokens left)
	if rl.Allow() {
		t.Error("Expected third request to be rejected")
	}
}

func TestRateLimiterAllowN(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(10.0, 5)

	// Should allow 3 tokens
	if !rl.AllowN(3) {
		t.Error("Expected 3 tokens to be allowed")
	}

	// Should allow 2 more tokens
	if !rl.AllowN(2) {
		t.Error("Expected 2 more tokens to be allowed")
	}

	// Should not allow 1 more token (capacity exceeded)
	if rl.AllowN(1) {
		t.Error("Expected 1 more token to be rejected")
	}
}

func TestRateLimiterRefill(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(50.0, 10) // 50 tokens per second for faster test

	// Use all tokens
	rl.AllowN(10)

	time.Sleep(50 * time.Millisecond) // Should add ~2.5 tokens at 50/s rate

	// Should now allow some requests
	if !rl.Allow() {
		t.Error("Expected request to be allowed after refill")
	}
}

func TestRateLimiterWait(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(100.0, 1)
	ctx := context.Background()

	// Use the token
	rl.Allow()

	// Wait should succeed quickly due to high refill rate
	err := rl.Wait(ctx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestRateLimiterWaitContextCanceled(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(0.1, 1)
	ctx, cancel := context.WithCancel(context.Background())

	// Use the token
	rl.Allow()

	// Cancel context quickly
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := rl.WaitN(ctx, 1)
	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
}

func TestRateLimiterWaitExceedsCapacity(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(10.0, 5)
	ctx := context.Background()

	err := rl.WaitN(ctx, 10) // Request more than capacity
	if err == nil {
		t.Error("Expected error when requesting more tokens than capacity")
	}
}

func TestNewBulkhead(t *testing.T) {
	t.Parallel()
	b := NewBulkhead("test", 5)

	if b.name != "test" {
		t.Errorf("Expected name 'test', got '%s'", b.name)
	}
	if b.maxConcurrent != 5 {
		t.Errorf("Expected maxConcurrent=5, got %d", b.maxConcurrent)
	}
	if len(b.semaphore) != 0 {
		t.Errorf("Expected empty semaphore, got %d", len(b.semaphore))
	}
	if cap(b.semaphore) != 5 {
		t.Errorf("Expected semaphore capacity=5, got %d", cap(b.semaphore))
	}
}

func TestBulkheadExecuteSuccess(t *testing.T) {
	t.Parallel()
	b := NewBulkhead("test", 2)
	ctx := context.Background()

	calls := 0
	err := b.Execute(ctx, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("Expected 1 call, got %d", calls)
	}
}

func TestBulkheadExecuteConcurrencyLimit(t *testing.T) {
	t.Parallel()
	b := NewBulkhead("test", 1)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([]error, 2)

	// Start two concurrent operations
	for i := range 2 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			err := b.Execute(ctx, func() error {
				time.Sleep(50 * time.Millisecond) // Hold the slot
				return nil
			})
			results[index] = err
		}(i)
		time.Sleep(10 * time.Millisecond) // Ensure some ordering
	}

	wg.Wait()

	// One should succeed, one should be rejected
	successes := 0
	rejections := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else {
			rejections++
		}
	}

	if successes != 1 {
		t.Errorf("Expected 1 success, got %d", successes)
	}
	if rejections != 1 {
		t.Errorf("Expected 1 rejection, got %d", rejections)
	}
}

func TestBulkheadExecuteContextCanceled(t *testing.T) {
	t.Parallel()
	b := NewBulkhead("test", 1)
	ctx, cancel := context.WithCancel(context.Background())

	// Fill the bulkhead
	go b.Execute(context.Background(), func() error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	time.Sleep(10 * time.Millisecond) // Let first operation start
	cancel()

	err := b.Execute(ctx, func() error {
		return nil
	})

	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
}

func TestBulkheadGetMetrics(t *testing.T) {
	t.Parallel()
	b := NewBulkhead("test-metrics", 3)
	ctx := context.Background()

	// Execute some operations
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Execute(ctx, func() error {
				time.Sleep(20 * time.Millisecond)
				return nil
			})
		}()
	}

	// Let operations start
	time.Sleep(10 * time.Millisecond)

	metrics := b.GetMetrics()

	if metrics["name"] != "test-metrics" {
		t.Errorf("Expected name 'test-metrics', got %v", metrics["name"])
	}
	if metrics["max_concurrent"] != 3 {
		t.Errorf("Expected max_concurrent=3, got %v", metrics["max_concurrent"])
	}

	wg.Wait()

	// Final metrics after completion
	finalMetrics := b.GetMetrics()
	if finalMetrics["completed"].(uint64) < 2 {
		t.Errorf("Expected at least 2 completed operations, got %v", finalMetrics["completed"])
	}
}
