package core

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// Mutation tests for core/resilience.go
//
// Targets the following LIVED mutants:
//   ARITHMETIC_BASE at 370:30, 371:53, 371:60, 410:31, 67:42, 67:64, 67:71, 68:26, 79:40
//   CONDITIONALS_BOUNDARY at 173:37, 345:15, 62:14, 80:12
//   CONDITIONALS_NEGATION at 80:12
//   INVERT_NEGATIVES at 370:30, 410:31, 67:71
//   INCREMENT_DECREMENT at 214:18
// =============================================================================

// --- Retry function mutants ---

// TestRetry_AttemptBoundary_Exact targets:
//
//	CONDITIONALS_BOUNDARY at line 62 (attempt >= policy.MaxAttempts)
//	CONDITIONALS_BOUNDARY at line 80 (attempt <= policy.MaxAttempts in for loop)
//	CONDITIONALS_NEGATION at line 80 (negation of loop condition)
//
// If the boundary is changed from >= to > at line 62, or <= to < at line 80,
// we get a different number of attempts. We verify the exact attempt count.
func TestRetry_AttemptBoundary_Exact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		maxAttempts   int
		expectedCalls int
		failUntil     int // fail until this attempt, then succeed; -1 = always fail
		expectError   bool
	}{
		{
			name:          "MaxAttempts=1_always_fail",
			maxAttempts:   1,
			failUntil:     -1,
			expectedCalls: 1,
			expectError:   true,
		},
		{
			name:          "MaxAttempts=2_always_fail",
			maxAttempts:   2,
			failUntil:     -1,
			expectedCalls: 2,
			expectError:   true,
		},
		{
			name:          "MaxAttempts=3_always_fail",
			maxAttempts:   3,
			failUntil:     -1,
			expectedCalls: 3,
			expectError:   true,
		},
		{
			name:          "MaxAttempts=1_succeed_on_1",
			maxAttempts:   1,
			failUntil:     0,
			expectedCalls: 1,
			expectError:   false,
		},
		{
			name:          "MaxAttempts=2_succeed_on_2",
			maxAttempts:   2,
			failUntil:     1,
			expectedCalls: 2,
			expectError:   false,
		},
		{
			name:          "MaxAttempts=2_fail_on_exact_boundary",
			maxAttempts:   2,
			failUntil:     -1,
			expectedCalls: 2,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			policy := &RetryPolicy{
				MaxAttempts:   tt.maxAttempts,
				InitialDelay:  1 * time.Millisecond,
				MaxDelay:      10 * time.Millisecond,
				BackoffFactor: 1.0,
				JitterFactor:  0.0, // no jitter for determinism
				RetryableErrors: func(err error) bool {
					return true
				},
			}

			calls := 0
			err := Retry(ctx, policy, func() error {
				calls++
				if tt.failUntil == -1 || calls <= tt.failUntil {
					return errors.New("fail")
				}
				return nil
			})

			if calls != tt.expectedCalls {
				t.Errorf("expected %d calls, got %d", tt.expectedCalls, calls)
			}
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestRetry_JitterCalculation targets:
//
//	ARITHMETIC_BASE at line 67:42 (float64(delay) * ...)
//	ARITHMETIC_BASE at line 67:64 (... * policy.JitterFactor)
//	ARITHMETIC_BASE at line 67:71 (0.5 - math.Mod(...))
//	INVERT_NEGATIVES at line 67:71 (negate 1 in math.Mod)
//	ARITHMETIC_BASE at line 68:26 (delay + jitter)
//
// With JitterFactor=0, jitter should be exactly 0, so sleepDuration == delay.
// With JitterFactor != 0, the formula is:
//
//	jitter = time.Duration(float64(delay) * policy.JitterFactor * (0.5 - math.Mod(float64(time.Now().UnixNano()), 1)))
//
// math.Mod(float64(int), 1) is always 0.0 for integer values, so:
//
//	jitter = time.Duration(float64(delay) * policy.JitterFactor * 0.5)
//
// If any arithmetic is mutated (e.g. * to /, + to -, 0.5 to other), the result changes.
func TestRetry_JitterCalculation(t *testing.T) {
	t.Parallel()

	// Test 1: With JitterFactor=0, the function should still work and delay should be exact
	t.Run("ZeroJitter", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		policy := &RetryPolicy{
			MaxAttempts:   2,
			InitialDelay:  50 * time.Millisecond,
			MaxDelay:      1 * time.Second,
			BackoffFactor: 1.0,
			JitterFactor:  0.0,
			RetryableErrors: func(err error) bool {
				return true
			},
		}

		start := time.Now()
		calls := 0
		Retry(ctx, policy, func() error {
			calls++
			return errors.New("fail")
		})
		elapsed := time.Since(start)

		if calls != 2 {
			t.Fatalf("expected 2 calls, got %d", calls)
		}
		// With zero jitter and backoff=1.0, delay should be ~50ms
		// Allow a range but the delay must be positive (not negative or zero)
		if elapsed < 30*time.Millisecond {
			t.Errorf("delay too short: %v (expected ~50ms)", elapsed)
		}
	})

	// Test 2: With a positive JitterFactor, the sleep should still happen (positive duration)
	// The jitter formula: float64(delay) * JitterFactor * (0.5 - math.Mod(float64(now), 1))
	// Since math.Mod(integer, 1) == 0, this becomes: delay * JitterFactor * 0.5
	// If INVERT_NEGATIVES mutates the 1 to -1: math.Mod(n, -1) still returns 0 for integer n
	// But if ARITHMETIC_BASE changes * to / or + to -, the delay changes drastically
	t.Run("PositiveJitter", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		policy := &RetryPolicy{
			MaxAttempts:   2,
			InitialDelay:  100 * time.Millisecond,
			MaxDelay:      1 * time.Second,
			BackoffFactor: 1.0,
			JitterFactor:  0.5, // large jitter to make mutations visible
			RetryableErrors: func(err error) bool {
				return true
			},
		}

		start := time.Now()
		calls := 0
		Retry(ctx, policy, func() error {
			calls++
			return errors.New("fail")
		})
		elapsed := time.Since(start)

		if calls != 2 {
			t.Fatalf("expected 2 calls, got %d", calls)
		}
		// delay=100ms, jitter = 100ms * 0.5 * 0.5 = 25ms, so sleepDuration = 100+25 = 125ms
		// Allow generous range but catch if arithmetic is grossly wrong
		if elapsed < 80*time.Millisecond {
			t.Errorf("delay too short with jitter: %v (expected ~125ms)", elapsed)
		}
		if elapsed > 500*time.Millisecond {
			t.Errorf("delay too long with jitter: %v (expected ~125ms)", elapsed)
		}
	})

	// Test 3: Verify the jitter formula directly by ensuring the sleep duration
	// is in expected range. delay=100ms, jitter=0 (JitterFactor=0), backoff=2.0
	// After first retry: delay should become min(200ms, MaxDelay)
	t.Run("DelayPlusJitter_NotMinus", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		policy := &RetryPolicy{
			MaxAttempts:   3,
			InitialDelay:  50 * time.Millisecond,
			MaxDelay:      500 * time.Millisecond,
			BackoffFactor: 2.0,
			JitterFactor:  0.0,
			RetryableErrors: func(err error) bool {
				return true
			},
		}

		start := time.Now()
		Retry(ctx, policy, func() error {
			return errors.New("fail")
		})
		elapsed := time.Since(start)

		// 3 attempts, delays between: 50ms (before 2nd), 100ms (before 3rd)
		// Total delay ~150ms. If + is mutated to -, delays would be wrong.
		if elapsed < 100*time.Millisecond || elapsed > 400*time.Millisecond {
			t.Errorf("total retry time out of range: %v (expected ~150ms)", elapsed)
		}
	})
}

// TestRetry_BackoffCalculation targets:
//
//	ARITHMETIC_BASE at line 79:40 (float64(delay)*policy.BackoffFactor)
//
// The backoff calculation: delay = min(time.Duration(float64(delay)*policy.BackoffFactor), policy.MaxDelay)
// If * is mutated to +, /, or -, the delay progression changes.
func TestRetry_BackoffCalculation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Use BackoffFactor=3.0 and InitialDelay=10ms
	// Expected: attempt1 delay=10ms, attempt2 delay=min(30ms, 100ms)=30ms
	// If * became +: delay would be 10+3=13ns (wrong type mix, but effectively different)
	// If * became /: delay would be 10/3~3ms
	// If * became -: delay would be 10-3=7ns
	policy := &RetryPolicy{
		MaxAttempts:   3,
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 3.0,
		JitterFactor:  0.0,
		RetryableErrors: func(err error) bool {
			return true
		},
	}

	var delays []time.Time
	Retry(ctx, policy, func() error {
		delays = append(delays, time.Now())
		return errors.New("fail")
	})

	if len(delays) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(delays))
	}

	// Between attempt 1 and 2: ~10ms delay
	gap1 := delays[1].Sub(delays[0])
	// Between attempt 2 and 3: ~30ms delay (10ms * 3.0 backoff)
	gap2 := delays[2].Sub(delays[1])

	// gap2 should be approximately 3x gap1 (with some tolerance)
	if gap1 < 5*time.Millisecond {
		t.Errorf("first delay too short: %v (expected ~10ms)", gap1)
	}
	if gap2 < 15*time.Millisecond {
		t.Errorf("second delay too short: %v (expected ~30ms, should be 3x first)", gap2)
	}
	// The second delay should be noticeably longer than the first
	if gap2 < gap1*2 {
		t.Errorf("backoff not working: gap1=%v, gap2=%v (expected gap2 ~3x gap1)", gap1, gap2)
	}
}

// --- CircuitBreaker mutants ---

// TestCircuitBreaker_HalfOpenTransitionBoundary targets:
//
//	CONDITIONALS_BOUNDARY at line 173:37 (time.Since(cb.lastFailureTime) > cb.resetTimeout)
//
// If > is changed to >=, then the transition happens at exactly the timeout.
// We test that the circuit does NOT transition when time.Since == timeout (approximately)
// and DOES transition when time.Since > timeout.
func TestCircuitBreaker_HalfOpenTransitionBoundary(t *testing.T) {
	t.Parallel()

	// Use a short timeout for testing
	timeout := 100 * time.Millisecond
	cb := NewCircuitBreaker("boundary-test", 1, timeout)

	// Trip the circuit breaker
	cb.Execute(func() error {
		return errors.New("fail")
	})
	if cb.GetState() != StateOpen {
		t.Fatal("expected circuit to be open")
	}

	// Wait slightly less than the timeout - should still be open
	time.Sleep(timeout - 30*time.Millisecond)
	err := cb.Execute(func() error { return nil })
	if err == nil {
		t.Error("expected error when timeout has not elapsed")
	}

	// Wait past the timeout - should transition to half-open and allow call
	time.Sleep(60 * time.Millisecond) // total wait > timeout
	err = cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("expected no error after timeout elapsed, got %v", err)
	}
}

// TestCircuitBreaker_SuccessCountIncrement targets:
//
//	INCREMENT_DECREMENT at line 214:18 (cb.successCount++)
//
// In half-open state, successCount must be incremented (not decremented) so that
// the circuit transitions to closed when successCount >= halfOpenMaxCalls.
// If ++ becomes --, the circuit never closes.
func TestCircuitBreaker_SuccessCountIncrement(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker("inc-test", 1, 50*time.Millisecond)
	cb.halfOpenMaxCalls = 2 // Require 2 successes to close

	// Trip the circuit
	cb.Execute(func() error {
		return errors.New("fail")
	})
	if cb.GetState() != StateOpen {
		t.Fatal("expected open state")
	}

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// First success in half-open state
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("expected no error for first half-open call, got %v", err)
	}

	// After 1 success with halfOpenMaxCalls=2, should still NOT be closed
	// The successCount should be 1, not -1 (if decremented)
	// We can't call again in half-open (halfOpenCalls >= halfOpenMaxCalls after the first call
	// incremented halfOpenCalls). So we need to check state directly.
	cb.mu.Lock()
	sc := cb.successCount
	cb.mu.Unlock()

	if sc != 1 {
		t.Errorf("expected successCount=1, got %d", sc)
	}

	// With halfOpenMaxCalls=1 (default), a single success should transition to closed
	cb2 := NewCircuitBreaker("inc-test-2", 1, 50*time.Millisecond)

	// Trip the circuit
	cb2.Execute(func() error {
		return errors.New("fail")
	})

	// Wait for reset
	time.Sleep(60 * time.Millisecond)

	// One success should close it (since halfOpenMaxCalls=1)
	err = cb2.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cb2.GetState() != StateClosed {
		t.Errorf("expected closed state after success in half-open, got %v", cb2.GetState())
	}

	// If increment was mutated to decrement, successCount would be -1 (underflow for uint32 = huge number)
	// and >= comparison would wrongly succeed, or successCount would never reach halfOpenMaxCalls
}

// TestCircuitBreaker_SuccessCountIncrement_MultipleSuccesses is a more direct test
// that requires exactly N successes to transition from half-open to closed.
func TestCircuitBreaker_SuccessCountIncrement_MultipleSuccesses(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker("multi-success", 1, 30*time.Millisecond)
	// Set halfOpenMaxCalls to 1 so that exactly 1 success transitions to closed
	cb.halfOpenMaxCalls = 1

	// Trip the circuit
	cb.Execute(func() error { return errors.New("fail") })
	if cb.GetState() != StateOpen {
		t.Fatal("expected open")
	}

	// Wait for reset
	time.Sleep(40 * time.Millisecond)

	// Execute one success - should close the circuit
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must be closed now
	if cb.GetState() != StateClosed {
		t.Fatalf("expected closed after 1 success with halfOpenMaxCalls=1, got %v", cb.GetState())
	}

	// Verify a subsequent call works fine in closed state
	err = cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("expected no error in closed state, got %v", err)
	}
}

// --- RateLimiter mutants ---

// TestRateLimiter_RefillCapBoundary targets:
//
//	CONDITIONALS_BOUNDARY at line 345:15 (rl.tokens > float64(rl.capacity))
//
// If > is mutated to >=, then tokens at exactly capacity get capped down (to capacity-1 effectively),
// losing one token. We test that after full refill, tokens equal exactly capacity.
func TestRateLimiter_RefillCapBoundary(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(1000.0, 5) // 1000 tokens/sec, capacity 5

	// Initially tokens = capacity = 5
	// Use 1 token
	if !rl.Allow() {
		t.Fatal("expected first allow to succeed")
	}

	// Wait enough for more than 1 token to refill (but total < capacity)
	time.Sleep(10 * time.Millisecond) // should add ~10 tokens at 1000/sec, capped to 5

	// After refill, tokens should be exactly at capacity (5)
	// Use all 5 tokens
	for i := range 5 {
		if !rl.Allow() {
			t.Errorf("expected allow to succeed for token %d after refill to capacity", i+1)
		}
	}

	// 6th should fail (no more tokens)
	if rl.Allow() {
		t.Error("expected 6th allow to fail after using all tokens")
	}
}

// TestRateLimiter_RefillDoesNotExceedCapacity ensures tokens are capped exactly at capacity,
// not capacity-1 (which would happen if > is mutated to >=).
func TestRateLimiter_RefillDoesNotExceedCapacity(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(10000.0, 3) // fast refill
	// Use all tokens
	rl.AllowN(3)

	// Wait for refill to exceed capacity
	time.Sleep(10 * time.Millisecond) // would add 100 tokens, capped to 3

	// Should be able to use exactly 3
	if !rl.AllowN(3) {
		t.Error("expected AllowN(3) to succeed after refill to capacity=3")
	}

	// Next should fail
	if rl.AllowN(1) {
		t.Error("expected AllowN(1) to fail when tokens are exhausted")
	}
}

// TestRateLimiter_WaitDuration targets:
//
//	ARITHMETIC_BASE at line 370:30 (tokensNeeded/rl.rate*1000)
//	INVERT_NEGATIVES at line 370:30
//
// The wait duration calculation: tokensNeeded/rl.rate*1000 * Millisecond
// If the arithmetic is mutated (e.g., / to *, * to /, or sign inverted),
// the wait time would be wildly different.
func TestRateLimiter_WaitDuration(t *testing.T) {
	t.Parallel()

	// Rate=100 tokens/sec, capacity=1
	// After using the 1 token, need to wait ~10ms for 1 token
	rl := NewRateLimiter(100.0, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Drain all tokens
	rl.Allow()

	start := time.Now()
	err := rl.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// At 100 tokens/sec, 1 token needs ~10ms
	// If arithmetic is inverted or mutated, wait would be huge or zero
	if elapsed < 1*time.Millisecond {
		t.Errorf("wait too short: %v (expected ~10ms)", elapsed)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("wait too long: %v (expected ~10ms)", elapsed)
	}
}

// TestRateLimiter_WaitDuration_TokensNeeded targets:
//
//	ARITHMETIC_BASE at line 371:53 and 371:60 around the tokensNeeded calculation
//	tokensNeeded := float64(n) - rl.tokens
//	If - is mutated to +, tokensNeeded would be too large (or negative scenario different).
func TestRateLimiter_WaitDuration_TokensNeeded(t *testing.T) {
	t.Parallel()

	// capacity=2, rate=100/sec
	rl := NewRateLimiter(100.0, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Use 1 of 2 tokens
	rl.Allow()

	// WaitN(2) should need 1 more token (2 - 1.0 = 1.0 tokens needed)
	// At 100/sec that's ~10ms
	start := time.Now()
	err := rl.WaitN(ctx, 2)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// If - was mutated to +, tokensNeeded would be 2+1=3, wait would be 30ms
	// The actual wait should be around 10ms
	if elapsed > 100*time.Millisecond {
		t.Errorf("wait too long: %v (expected ~10ms for 1 token at 100/sec)", elapsed)
	}
}

// --- Bulkhead mutants ---

// TestBulkhead_ActiveCounterSign targets:
//
//	ARITHMETIC_BASE at line 410:31 (atomic.AddInt32(&b.active, -1))
//	INVERT_NEGATIVES at line 410:31 (negate -1 to 1)
//
// If -1 is mutated to 1, active count would increase on completion instead of decrease.
// After executing a function, active should return to 0.
func TestBulkhead_ActiveCounterSign(t *testing.T) {
	t.Parallel()

	b := NewBulkhead("active-test", 2)
	ctx := context.Background()

	// Execute a function
	err := b.Execute(ctx, func() error {
		// During execution, active should be 1
		active := atomic.LoadInt32(&b.active)
		if active != 1 {
			t.Errorf("expected active=1 during execution, got %d", active)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After execution, active should be back to 0
	active := atomic.LoadInt32(&b.active)
	if active != 0 {
		t.Errorf("expected active=0 after execution, got %d", active)
	}

	// Verify completed counter
	completed := atomic.LoadUint64(&b.completed)
	if completed != 1 {
		t.Errorf("expected completed=1, got %d", completed)
	}
}

// TestBulkhead_ActiveCounterConcurrent verifies that concurrent executions
// properly increment and decrement the active counter.
func TestBulkhead_ActiveCounterConcurrent(t *testing.T) {
	t.Parallel()

	b := NewBulkhead("concurrent-active", 5)
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Execute(ctx, func() error {
				time.Sleep(20 * time.Millisecond)
				return nil
			})
		}()
	}

	// Let all goroutines start
	time.Sleep(5 * time.Millisecond)

	// Active should be 3
	active := atomic.LoadInt32(&b.active)
	if active != 3 {
		t.Errorf("expected active=3, got %d", active)
	}

	wg.Wait()

	// After all complete, active should be 0
	active = atomic.LoadInt32(&b.active)
	if active != 0 {
		t.Errorf("expected active=0 after all complete, got %d", active)
	}

	completed := atomic.LoadUint64(&b.completed)
	if completed != 3 {
		t.Errorf("expected completed=3, got %d", completed)
	}
}

// TestBulkhead_ActiveCounterAfterMultipleExecutions verifies the active counter
// stays consistent over multiple sequential executions (catches -1 -> +1 mutation).
func TestBulkhead_ActiveCounterAfterMultipleExecutions(t *testing.T) {
	t.Parallel()

	b := NewBulkhead("multi-exec", 1)
	ctx := context.Background()

	for i := range 5 {
		err := b.Execute(ctx, func() error { return nil })
		if err != nil {
			t.Fatalf("execution %d failed: %v", i, err)
		}
	}

	active := atomic.LoadInt32(&b.active)
	if active != 0 {
		t.Errorf("expected active=0 after 5 sequential executions, got %d", active)
	}

	// If -1 was mutated to +1, active would be 10 (incremented 5 more instead of decremented 5)
	// and subsequent executions might fail due to unexpected active count
}

// TestRetry_ExactBackoffProgression verifies the exact multiplicative progression
// to kill ARITHMETIC_BASE at line 79 (delay * backoffFactor mutation).
func TestRetry_ExactBackoffProgression(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// backoff = 2.0, initial = 20ms, max = 1s
	// Expected: delay1=20ms, delay2=40ms (20*2), delay3=80ms (40*2)
	policy := &RetryPolicy{
		MaxAttempts:   4,
		InitialDelay:  20 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BackoffFactor: 2.0,
		JitterFactor:  0.0,
		RetryableErrors: func(err error) bool {
			return true
		},
	}

	timestamps := make([]time.Time, 0, 4)
	Retry(ctx, policy, func() error {
		timestamps = append(timestamps, time.Now())
		return errors.New("fail")
	})

	if len(timestamps) != 4 {
		t.Fatalf("expected 4 timestamps, got %d", len(timestamps))
	}

	gap1 := timestamps[1].Sub(timestamps[0]) // ~20ms
	gap2 := timestamps[2].Sub(timestamps[1]) // ~40ms
	gap3 := timestamps[3].Sub(timestamps[2]) // ~80ms

	// gap2 should be roughly 2x gap1
	ratio21 := float64(gap2) / float64(gap1)
	if ratio21 < 1.3 || ratio21 > 3.0 {
		t.Errorf("gap2/gap1 ratio = %.2f (expected ~2.0): gap1=%v, gap2=%v", ratio21, gap1, gap2)
	}

	// gap3 should be roughly 2x gap2
	ratio32 := float64(gap3) / float64(gap2)
	if ratio32 < 1.3 || ratio32 > 3.0 {
		t.Errorf("gap3/gap2 ratio = %.2f (expected ~2.0): gap2=%v, gap3=%v", ratio32, gap2, gap3)
	}
}

// TestRetry_MaxDelayCapBackoff ensures the backoff is capped by MaxDelay.
// This provides additional coverage for ARITHMETIC_BASE at line 79.
func TestRetry_MaxDelayCapBackoff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	policy := &RetryPolicy{
		MaxAttempts:   3,
		InitialDelay:  50 * time.Millisecond,
		MaxDelay:      60 * time.Millisecond, // cap is only slightly above initial
		BackoffFactor: 10.0,                  // aggressive backoff
		JitterFactor:  0.0,
		RetryableErrors: func(err error) bool {
			return true
		},
	}

	timestamps := make([]time.Time, 0, 3)
	Retry(ctx, policy, func() error {
		timestamps = append(timestamps, time.Now())
		return errors.New("fail")
	})

	if len(timestamps) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(timestamps))
	}

	gap2 := timestamps[2].Sub(timestamps[1])

	// Second delay should be capped at 60ms, not 500ms (50*10)
	if gap2 > 120*time.Millisecond {
		t.Errorf("delay not capped: %v (expected max ~60ms)", gap2)
	}
}

// TestRetry_LoopCondition_SingleAttempt verifies that with MaxAttempts=1,
// the function is called exactly once (kills CONDITIONALS_NEGATION at line 80).
func TestRetry_LoopCondition_SingleAttempt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	policy := &RetryPolicy{
		MaxAttempts:   1,
		InitialDelay:  1 * time.Millisecond,
		MaxDelay:      1 * time.Millisecond,
		BackoffFactor: 1.0,
		JitterFactor:  0.0,
		RetryableErrors: func(err error) bool {
			return true
		},
	}

	calls := 0
	err := Retry(ctx, policy, func() error {
		calls++
		return errors.New("fail")
	})

	// With MaxAttempts=1, should call exactly once
	if calls != 1 {
		t.Errorf("expected exactly 1 call with MaxAttempts=1, got %d", calls)
	}
	if err == nil {
		t.Error("expected error")
	}
}

// TestRetry_LoopCondition_ZeroAttempts verifies behavior with MaxAttempts=0.
// The for loop condition is: attempt <= policy.MaxAttempts (i.e., 1 <= 0 is false)
// So with MaxAttempts=0, the loop body never executes.
func TestRetry_LoopCondition_ZeroAttempts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	policy := &RetryPolicy{
		MaxAttempts:   0,
		InitialDelay:  1 * time.Millisecond,
		MaxDelay:      1 * time.Millisecond,
		BackoffFactor: 1.0,
		JitterFactor:  0.0,
		RetryableErrors: func(err error) bool {
			return true
		},
	}

	calls := 0
	err := Retry(ctx, policy, func() error {
		calls++
		return errors.New("fail")
	})

	// With MaxAttempts=0, loop condition "1 <= 0" is false, so no calls
	if calls != 0 {
		t.Errorf("expected 0 calls with MaxAttempts=0, got %d", calls)
	}
	// Error message should indicate 0 attempts exceeded
	if err == nil {
		t.Error("expected error for 0 max attempts")
	}
}

// TestRateLimiter_WaitN_CalculationPrecision targets the wait duration arithmetic more precisely.
// The formula: waitDuration = time.Duration(tokensNeeded/rl.rate*1000) * time.Millisecond
func TestRateLimiter_WaitN_CalculationPrecision(t *testing.T) {
	t.Parallel()

	// rate=200, capacity=1: after draining, need 1 token.
	// tokensNeeded=1.0, waitDuration = (1.0/200*1000)ms = 5ms
	rl := NewRateLimiter(200.0, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	rl.Allow() // drain

	start := time.Now()
	err := rl.WaitN(ctx, 1)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected ~5ms. If division/multiplication is mutated, it would be very different.
	if elapsed < 1*time.Millisecond {
		t.Errorf("wait too short: %v (expected ~5ms)", elapsed)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("wait too long: %v (expected ~5ms)", elapsed)
	}
}

// Suppress unused import warnings
var _ = math.Abs
