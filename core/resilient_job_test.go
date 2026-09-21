// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netresearch/ofelia/test"
)

// mockRunJob implements Job and records Run() calls.
type mockRunJob struct {
	BareJob
	runFunc func(*Context) error
	calls   atomic.Int32
}

func (m *mockRunJob) Run(ctx *Context) error {
	m.calls.Add(1)
	if m.runFunc != nil {
		return m.runFunc(ctx)
	}
	return nil
}

// newResilientTestContext creates a *Context for resilient job executor tests.
func newResilientTestContext(t *testing.T, job Job) *Context {
	t.Helper()
	logger := test.NewTestLogger()
	scheduler := NewScheduler(logger)
	exec, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution: %v", err)
	}
	exec.Start()
	return NewContext(scheduler, job, exec)
}

// ---------------------------------------------------------------------------
// Execute() tests
// ---------------------------------------------------------------------------

func TestResilientJobExecutor_Execute_Success(t *testing.T) {
	t.Parallel()

	job := &mockRunJob{Name: "success-job"}
	rje := NewResilientJobExecutor(job)
	ctx := newResilientTestContext(t, job)

	err := rje.Execute(ctx)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if job.calls.Load() != 1 {
		t.Errorf("expected 1 call, got %d", job.calls.Load())
	}
}

func TestResilientJobExecutor_Execute_JobFailure(t *testing.T) {
	t.Parallel()

	job := &mockRunJob{
		Name:    "fail-job",
		runFunc: func(*Context) error { return errors.New("boom") },
	}
	rje := NewResilientJobExecutor(job)
	// Use fast retry to speed up test
	rje.SetRetryPolicy(&RetryPolicy{
		MaxAttempts:   2,
		InitialDelay:  1 * time.Millisecond,
		MaxDelay:      5 * time.Millisecond,
		BackoffFactor: 1.0,
		JitterFactor:  0.0,
		RetryableErrors: func(err error) bool {
			return true
		},
	})

	ctx := newResilientTestContext(t, job)
	err := rje.Execute(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if job.calls.Load() < 2 {
		t.Errorf("expected at least 2 retry attempts, got %d", job.calls.Load())
	}
}

func TestResilientJobExecutor_Execute_RateLimited(t *testing.T) {
	t.Parallel()

	job := &mockRunJob{Name: "rl-job"}
	rje := NewResilientJobExecutor(job)
	// Exhaust rate limiter
	rje.SetRateLimiter(NewRateLimiter(0.001, 0)) // 0 burst means no tokens available

	ctx := newResilientTestContext(t, job)
	err := rje.Execute(ctx)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Errorf("expected ErrRateLimitExceeded, got %v", err)
	}
}

func TestResilientJobExecutor_Execute_NonRetryableError(t *testing.T) {
	t.Parallel()

	job := &mockRunJob{
		Name:    "nonretry-job",
		runFunc: func(*Context) error { return fmt.Errorf("404 not found") },
	}
	rje := NewResilientJobExecutor(job)
	// The default retry policy skips "404"/"not found" errors
	rje.SetRetryPolicy(&RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
		RetryableErrors: func(err error) bool {
			// "404" matches the non-retryable check
			return false
		},
	})

	ctx := newResilientTestContext(t, job)
	err := rje.Execute(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	// Should only be called once (no retries for non-retryable)
	if job.calls.Load() != 1 {
		t.Errorf("expected 1 call (no retries), got %d", job.calls.Load())
	}
}

// ---------------------------------------------------------------------------
// executeJob() edge cases
// ---------------------------------------------------------------------------

func TestResilientJobExecutor_ExecuteJob_LogsDuration(t *testing.T) {
	t.Parallel()

	_, handler := test.NewTestLoggerWithHandler()
	logger := test.NewTestLogger()
	scheduler := NewScheduler(logger)

	job := &mockRunJob{Name: "log-dur"}
	rje := NewResilientJobExecutor(job)

	exec, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution: %v", err)
	}
	exec.Start()
	ctx := NewContext(scheduler, job, exec)

	if execErr := rje.executeJob(ctx); execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}

	// We can't directly check handler (different logger), but the function should
	// not panic and should complete successfully
	_ = handler
}

func TestResilientJobExecutor_ExecuteJob_FailureWrapsError(t *testing.T) {
	t.Parallel()

	origErr := errors.New("docker timeout")
	job := &mockRunJob{
		Name:    "wrap-err",
		runFunc: func(*Context) error { return origErr },
	}
	rje := NewResilientJobExecutor(job)
	ctx := newResilientTestContext(t, job)

	err := rje.executeJob(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, origErr) {
		t.Errorf("expected wrapped original error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// recordMetrics()
// ---------------------------------------------------------------------------

func TestResilientJobExecutor_RecordMetrics_NilRecorder(t *testing.T) {
	t.Parallel()

	job := &mockRunJob{Name: "no-metrics"}
	rje := NewResilientJobExecutor(job)
	// metrics is nil by default
	// Should not panic
	rje.recordMetrics(true)
	rje.recordMetrics(false)
}

func TestResilientJobExecutor_RecordMetrics_WithRecorder(t *testing.T) {
	t.Parallel()

	job := &mockRunJob{Name: "with-metrics"}
	rje := NewResilientJobExecutor(job)

	recorder := NewSimpleMetricsRecorder()
	rje.SetMetricsRecorder(recorder)

	rje.recordMetrics(true)

	metrics := recorder.GetMetrics()
	key := "job.with-metrics.last_execution"
	if _, ok := metrics[key]; !ok {
		t.Errorf("expected metric %q to be recorded", key)
	}

	// Verify circuit breaker and bulkhead metrics are recorded
	hasCBMetric := false
	hasBHMetric := false
	for k := range metrics {
		if len(k) > 16 && k[:16] == "circuit_breaker." {
			hasCBMetric = true
		}
		if len(k) > 9 && k[:9] == "bulkhead." {
			hasBHMetric = true
		}
	}
	if !hasCBMetric {
		t.Error("expected circuit_breaker.* metrics to be recorded")
	}
	if !hasBHMetric {
		t.Error("expected bulkhead.* metrics to be recorded")
	}
}

func TestResilientJobExecutor_RecordMetrics_FailurePath(t *testing.T) {
	t.Parallel()

	job := &mockRunJob{Name: "fail-metrics"}
	rje := NewResilientJobExecutor(job)

	recorder := NewSimpleMetricsRecorder()
	rje.SetMetricsRecorder(recorder)

	rje.recordMetrics(false)

	metrics := recorder.GetMetrics()
	key := "job.fail-metrics.last_execution"
	execMetric, ok := metrics[key].(map[string]any)
	if !ok {
		t.Fatalf("expected metric %q to be recorded", key)
	}
	if execMetric["success"] != false {
		t.Errorf("expected success=false, got %v", execMetric["success"])
	}
}

// ---------------------------------------------------------------------------
// Execute() with metrics recorder (integration of recordMetrics)
// ---------------------------------------------------------------------------

func TestResilientJobExecutor_Execute_RecordsMetricsOnSuccess(t *testing.T) {
	t.Parallel()

	job := &mockRunJob{Name: "exec-metrics-ok"}
	rje := NewResilientJobExecutor(job)
	recorder := NewSimpleMetricsRecorder()
	rje.SetMetricsRecorder(recorder)

	ctx := newResilientTestContext(t, job)
	if err := rje.Execute(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := recorder.GetMetrics()
	if _, ok := metrics["job.exec-metrics-ok.last_execution"]; !ok {
		t.Error("expected job execution metric to be recorded")
	}
}

func TestResilientJobExecutor_Execute_RecordsMetricsOnFailure(t *testing.T) {
	t.Parallel()

	job := &mockRunJob{
		Name:    "exec-metrics-fail",
		runFunc: func(*Context) error { return errors.New("permanent failure") },
	}
	rje := NewResilientJobExecutor(job)
	rje.SetRetryPolicy(&RetryPolicy{
		MaxAttempts:     1,
		InitialDelay:    1 * time.Millisecond,
		MaxDelay:        5 * time.Millisecond,
		RetryableErrors: func(err error) bool { return true },
	})
	recorder := NewSimpleMetricsRecorder()
	rje.SetMetricsRecorder(recorder)

	ctx := newResilientTestContext(t, job)
	err := rje.Execute(ctx)
	if err == nil {
		t.Fatal("expected error")
	}

	metrics := recorder.GetMetrics()
	key := "job.exec-metrics-fail.last_execution"
	execMetric, ok := metrics[key].(map[string]any)
	if !ok {
		t.Fatalf("expected metric %q, got keys: %v", key, keysOf(metrics))
	}
	if execMetric["success"] != false {
		t.Errorf("expected success=false, got %v", execMetric["success"])
	}
}

// keysOf returns the keys of a map for diagnostic output.
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
