// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRetryExecutor tests the retry mechanism
func TestRetryExecutor(t *testing.T) {
	logger := newDiscardLogger()
	executor := NewRetryExecutor(logger)

	t.Run("SuccessOnFirstTry", func(t *testing.T) {
		attempts := 0
		job := &testRetryJob{
			BareJob: BareJob{
				Name:       "test-job",
				MaxRetries: 3,
			},
		}

		ctx := &Context{
			Execution: &Execution{},
		}

		err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
			attempts++
			return nil // Success
		})
		if err != nil {
			t.Errorf("Expected success, got error: %v", err)
		}

		if attempts != 1 {
			t.Errorf("Expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("RetryOnFailure", func(t *testing.T) {
		attempts := 0
		job := &testRetryJob{
			BareJob: BareJob{
				Name:         "test-job",
				MaxRetries:   3,
				RetryDelayMs: 10, // Short delay for testing
			},
		}

		ctx := &Context{
			Execution: &Execution{},
		}

		err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
			attempts++
			if attempts < 3 {
				return errors.New("temporary failure")
			}
			return nil // Success on third attempt
		})
		if err != nil {
			t.Errorf("Expected success after retries, got error: %v", err)
		}

		if attempts != 3 {
			t.Errorf("Expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("MaxRetriesExceeded", func(t *testing.T) {
		attempts := 0
		job := &testRetryJob{
			BareJob: BareJob{
				Name:         "test-job",
				MaxRetries:   2,
				RetryDelayMs: 10,
			},
		}

		ctx := &Context{
			Execution: &Execution{},
		}

		err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
			attempts++
			return errors.New("persistent failure")
		})

		if err == nil {
			t.Error("Expected error after max retries, got nil")
		}

		// Should try initial + 2 retries = 3 total
		if attempts != 3 {
			t.Errorf("Expected 3 attempts (initial + 2 retries), got %d", attempts)
		}
	})

	t.Run("ExponentialBackoff", func(t *testing.T) {
		job := &testRetryJob{
			BareJob: BareJob{
				Name:             "test-job",
				MaxRetries:       3,
				RetryDelayMs:     100,
				RetryExponential: true,
				RetryMaxDelayMs:  500,
			},
		}

		config := job.GetRetryConfig()

		// Test delay calculation
		delay0 := executor.calculateDelay(config, 0)
		delay1 := executor.calculateDelay(config, 1)
		delay2 := executor.calculateDelay(config, 2)
		delay3 := executor.calculateDelay(config, 3)

		// Verify exponential growth
		if delay0 != 100*time.Millisecond {
			t.Errorf("Expected 100ms for attempt 0, got %v", delay0)
		}
		if delay1 != 200*time.Millisecond {
			t.Errorf("Expected 200ms for attempt 1, got %v", delay1)
		}
		if delay2 != 400*time.Millisecond {
			t.Errorf("Expected 400ms for attempt 2, got %v", delay2)
		}
		// Should be capped at max delay
		if delay3 != 500*time.Millisecond {
			t.Errorf("Expected 500ms (capped) for attempt 3, got %v", delay3)
		}
	})

	t.Run("NoRetryConfiguration", func(t *testing.T) {
		attempts := 0
		job := &testRetryJob{
			BareJob: BareJob{
				Name:       "test-job",
				MaxRetries: 0, // No retries
			},
		}

		ctx := &Context{
			Execution: &Execution{},
		}

		err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
			attempts++
			return errors.New("failure")
		})

		if err == nil {
			t.Error("Expected error, got nil")
		}

		if attempts != 1 {
			t.Errorf("Expected 1 attempt (no retries), got %d", attempts)
		}
	})
}

// testRetryJob implements RetryableJob for testing
type testRetryJob struct {
	BareJob
}

func (j *testRetryJob) Run(ctx *Context) error {
	// Not used in tests - we pass a custom function to ExecuteWithRetry
	return nil
}

// TestRetryConfig tests retry configuration
func TestRetryConfig(t *testing.T) {
	job := &BareJob{
		Name:             "test-job",
		MaxRetries:       5,
		RetryDelayMs:     2000,
		RetryExponential: true,
		RetryMaxDelayMs:  120000,
	}

	config := job.GetRetryConfig()

	if config.MaxRetries != 5 {
		t.Errorf("Expected MaxRetries=5, got %d", config.MaxRetries)
	}

	if config.RetryDelayMs != 2000 {
		t.Errorf("Expected RetryDelayMs=2000, got %d", config.RetryDelayMs)
	}

	if !config.RetryExponential {
		t.Error("Expected RetryExponential=true")
	}

	if config.RetryMaxDelayMs != 120000 {
		t.Errorf("Expected RetryMaxDelayMs=120000, got %d", config.RetryMaxDelayMs)
	}
}

// TestExecuteWithRetry_HonorsContextCancellation pins the fix for
// https://github.com/netresearch/ofelia/issues/687.
//
// Before the fix the inter-attempt backoff used a bare time.Sleep that
// did not observe ctx cancellation, so SIGTERM on a daemon mid-retry kept
// the job goroutine pinned for up to RetryDelay × MaxRetries (compounded
// by exponential backoff) waiting for the sleep to return — keeping the
// scheduler from draining on shutdown. The fix replaces the sleep with a
// select over (time.After, ctx.RunContext().Done()) so retries drain
// promptly. Sibling to #673 / #685 (webhook retry backoff).
//
// We assert two things:
//  1. After cancellation, ExecuteWithRetry returns within a small window
//     (well under the RetryDelay budget) instead of blocking through it.
//  2. The returned error wraps context.Canceled so callers can branch on it.
func TestExecuteWithRetry_HonorsContextCancellation(t *testing.T) {
	logger := newDiscardLogger()
	executor := NewRetryExecutor(logger)

	// Big retry budget so a naive time.Sleep impl would block the test
	// well past its deadline.
	const retryDelay = 30 * time.Second
	job := &testRetryJob{
		BareJob: BareJob{
			Name:         "test-ctx-cancel",
			MaxRetries:   5,
			RetryDelayMs: int(retryDelay / time.Millisecond),
		},
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &Context{
		Execution: &Execution{},
		Ctx:       cancelCtx,
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	attempts := 0
	start := time.Now()
	err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
		attempts++
		return errors.New("persistent failure")
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ExecuteWithRetry should return an error after cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("ExecuteWithRetry should return an error chain containing context.Canceled, got: %v", err)
	}
	// Tolerate CI slowness but stay well under RetryDelay. If the fix
	// regresses to time.Sleep, elapsed will balloon to ~retryDelay.
	if elapsed >= 5*time.Second {
		t.Errorf("ExecuteWithRetry should drain promptly on ctx cancel (elapsed=%v, retryDelay=%v)",
			elapsed, retryDelay)
	}
	// Cancellation hit during the first backoff, so runFunc should have
	// been called exactly once (the initial attempt that failed).
	if attempts != 1 {
		t.Errorf("Expected 1 attempt before cancellation, got %d", attempts)
	}
}
