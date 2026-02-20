package core

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Mutation tests for core/retry.go
//
// Targets the following LIVED mutants:
//   ARITHMETIC_BASE at 112:26, 112:47, 127:35, 134:73
//   CONDITIONALS_BOUNDARY at 104:14, 146:14, 78:23, 91:15
//   CONDITIONALS_NEGATION at 91:15
// =============================================================================

// captureMetrics is a test MetricsRecorder that records all calls.
type captureMetrics struct {
	retries []retryRecord
}

type retryRecord struct {
	jobName string
	attempt int
	success bool
}

func (m *captureMetrics) RecordJobRetry(jobName string, attempt int, success bool) {
	m.retries = append(m.retries, retryRecord{jobName, attempt, success})
}

func (m *captureMetrics) RecordContainerEvent()                               {}
func (m *captureMetrics) RecordContainerMonitorFallback()                     {}
func (m *captureMetrics) RecordContainerMonitorMethod(usingEvents bool)       {}
func (m *captureMetrics) RecordContainerWaitDuration(seconds float64)         {}
func (m *captureMetrics) RecordDockerOperation(operation string)              {}
func (m *captureMetrics) RecordDockerError(operation string)                  {}
func (m *captureMetrics) RecordJobStart(jobName string)                       {}
func (m *captureMetrics) RecordJobComplete(jobName string, _ float64, _ bool) {}
func (m *captureMetrics) RecordJobScheduled(jobName string)                   {}
func (m *captureMetrics) RecordWorkflowComplete(_ string, _ string)           {}
func (m *captureMetrics) RecordWorkflowJobResult(_ string, _ string)          {}

// retrySlogHandler captures slog records for assertion.
type retrySlogHandler struct {
	warnings []string
	errors   []string
	infos    []string
}

func (h *retrySlogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *retrySlogHandler) Handle(_ context.Context, r slog.Record) error {
	// Format message with attributes for test assertions
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		msg += " " + a.Key + "=" + a.Value.String()
		return true
	})
	switch r.Level { //nolint:exhaustive // only relevant levels captured
	case slog.LevelWarn:
		h.warnings = append(h.warnings, msg)
	case slog.LevelError:
		h.errors = append(h.errors, msg)
	case slog.LevelInfo:
		h.infos = append(h.infos, msg)
	}
	return nil
}

func (h *retrySlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *retrySlogHandler) WithGroup(_ string) slog.Handler      { return h }

func newRetryCaptureLogger() (*slog.Logger, *retrySlogHandler) {
	h := &retrySlogHandler{}
	return slog.New(h), h
}

// --- MaxRetries boundary (line 78) ---

// TestRetryExecutor_MaxRetriesBoundary targets:
//
//	CONDITIONALS_BOUNDARY at line 78:23 (config.MaxRetries <= 0)
//
// If <= is mutated to <, then MaxRetries=0 would NOT short-circuit
// and would enter the retry loop.
func TestRetryExecutor_MaxRetriesBoundary(t *testing.T) {
	t.Parallel()

	logger, handler := newRetryCaptureLogger()
	executor := NewRetryExecutor(logger)
	_ = handler

	t.Run("MaxRetries_Zero_RunsOnce", func(t *testing.T) {
		t.Parallel()
		job := &testRetryJob{
			BareJob: BareJob{
				Name:       "boundary-zero",
				MaxRetries: 0,
			},
		}
		ctx := &Context{Execution: &Execution{}}

		calls := 0
		err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
			calls++
			return errors.New("fail")
		})

		// With MaxRetries=0, should run exactly once (no retries)
		if calls != 1 {
			t.Errorf("expected 1 call with MaxRetries=0, got %d", calls)
		}
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("MaxRetries_Negative_RunsOnce", func(t *testing.T) {
		t.Parallel()
		job := &testRetryJob{
			BareJob: BareJob{
				Name:       "boundary-neg",
				MaxRetries: -1,
			},
		}
		ctx := &Context{Execution: &Execution{}}

		calls := 0
		err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
			calls++
			return errors.New("fail")
		})

		// With MaxRetries=-1 (<=0 is true), should run once
		if calls != 1 {
			t.Errorf("expected 1 call with MaxRetries=-1, got %d", calls)
		}
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("MaxRetries_One_RunsTwice", func(t *testing.T) {
		t.Parallel()
		job := &testRetryJob{
			BareJob: BareJob{
				Name:         "boundary-one",
				MaxRetries:   1,
				RetryDelayMs: 1,
			},
		}
		ctx := &Context{Execution: &Execution{}}

		calls := 0
		err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
			calls++
			return errors.New("fail")
		})

		// With MaxRetries=1, should run 1 + 1 = 2 times
		if calls != 2 {
			t.Errorf("expected 2 calls with MaxRetries=1, got %d", calls)
		}
		if err == nil {
			t.Error("expected error")
		}
	})
}

// --- attempt > 0 check (line 91) ---

// TestRetryExecutor_SuccessNoticeAfterRetry targets:
//
//	CONDITIONALS_BOUNDARY at line 91:15 (attempt > 0)
//	CONDITIONALS_NEGATION at line 91:15 (negation of attempt > 0)
//
// When a job succeeds on the first attempt (attempt=0), no notice should be logged.
// When it succeeds after retries (attempt>0), a notice SHOULD be logged.
// If > is mutated to >= or the condition is negated, logging behavior changes.
func TestRetryExecutor_SuccessNoticeAfterRetry(t *testing.T) {
	t.Parallel()

	t.Run("FirstAttempt_NoNotice", func(t *testing.T) {
		t.Parallel()
		logger, handler := newRetryCaptureLogger()
		executor := NewRetryExecutor(logger)
		metrics := &captureMetrics{}
		executor.SetMetricsRecorder(metrics)

		job := &testRetryJob{
			BareJob: BareJob{
				Name:         "notice-first",
				MaxRetries:   3,
				RetryDelayMs: 1,
			},
		}
		ctx := &Context{Execution: &Execution{}}

		err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
			return nil // succeed immediately
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// On first attempt (attempt=0), "attempt > 0" is false, so no notice
		if len(handler.infos) != 0 {
			t.Errorf("expected no notices on first-attempt success, got %v", handler.infos)
		}
		// No retry metrics should be recorded on first success
		if len(metrics.retries) != 0 {
			t.Errorf("expected no retry metrics on first success, got %v", metrics.retries)
		}
	})

	t.Run("SecondAttempt_Notice", func(t *testing.T) {
		t.Parallel()
		logger, handler := newRetryCaptureLogger()
		executor := NewRetryExecutor(logger)
		metrics := &captureMetrics{}
		executor.SetMetricsRecorder(metrics)

		job := &testRetryJob{
			BareJob: BareJob{
				Name:         "notice-second",
				MaxRetries:   3,
				RetryDelayMs: 1,
			},
		}
		ctx := &Context{Execution: &Execution{}}

		calls := 0
		err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
			calls++
			if calls == 1 {
				return errors.New("first fail")
			}
			return nil // succeed on second attempt
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// On second attempt (attempt=1), "attempt > 0" is true, so notice is logged
		if len(handler.infos) != 1 {
			t.Errorf("expected 1 notice after retry success, got %d: %v", len(handler.infos), handler.infos)
		}
		// Should record success metric
		foundSuccess := false
		for _, r := range metrics.retries {
			if r.success {
				foundSuccess = true
			}
		}
		if !foundSuccess {
			t.Error("expected a success retry metric to be recorded")
		}
	})
}

// --- attempt >= config.MaxRetries boundary (line 104) ---

// TestRetryExecutor_AttemptBoundary targets:
//
//	CONDITIONALS_BOUNDARY at line 104:14 (attempt >= config.MaxRetries)
//
// If >= is mutated to >, the loop would execute one extra iteration.
func TestRetryExecutor_AttemptBoundary(t *testing.T) {
	t.Parallel()

	logger, handler := newRetryCaptureLogger()
	executor := NewRetryExecutor(logger)
	_ = handler

	job := &testRetryJob{
		BareJob: BareJob{
			Name:         "boundary-attempts",
			MaxRetries:   2,
			RetryDelayMs: 1,
		},
	}
	ctx := &Context{Execution: &Execution{}}

	calls := 0
	err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
		calls++
		return errors.New("fail")
	})

	// MaxRetries=2 means: attempt 0, 1, 2 (total 3 calls)
	// If >= mutated to >: attempt 0, 1, 2, 3 (total 4 calls)
	if calls != 3 {
		t.Errorf("expected exactly 3 calls (initial + 2 retries), got %d", calls)
	}
	if err == nil {
		t.Error("expected error")
	}
}

// --- Warning log format (line 112) ---

// TestRetryExecutor_WarningLogFormat targets:
//
//	ARITHMETIC_BASE at line 112:26 (attempt+1 in warning message)
//	ARITHMETIC_BASE at line 112:47 (config.MaxRetries+1 in warning message)
//
// If +1 is changed to -1 or *1 etc., the log message will contain wrong numbers.
func TestRetryExecutor_WarningLogFormat(t *testing.T) {
	t.Parallel()

	logger, handler := newRetryCaptureLogger()
	executor := NewRetryExecutor(logger)
	_ = handler
	metrics := &captureMetrics{}
	executor.SetMetricsRecorder(metrics)

	job := &testRetryJob{
		BareJob: BareJob{
			Name:         "log-format-test",
			MaxRetries:   3,
			RetryDelayMs: 1,
		},
	}
	ctx := &Context{Execution: &Execution{}}

	calls := 0
	executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
		calls++
		return errors.New("test-error")
	})

	// Should have 3 warning messages (for attempts 0, 1, 2 before retrying)
	// Warning format: "Job %s failed (attempt %d/%d): %v. Retrying in %v"
	// For attempt=0: "attempt 1/4" (attempt+1=1, MaxRetries+1=4)
	// For attempt=1: "attempt 2/4"
	// For attempt=2: "attempt 3/4"
	if len(handler.warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %d: %v", len(handler.warnings), handler.warnings)
	}

	// Check first warning: attempt+1=1, MaxRetries+1=4
	if !strings.Contains(handler.warnings[0], "attempt=1") || !strings.Contains(handler.warnings[0], "maxRetries=4") {
		t.Errorf("first warning should contain attempt=1 and maxRetries=4, got: %s", handler.warnings[0])
	}
	// Check second warning
	if !strings.Contains(handler.warnings[1], "attempt=2") || !strings.Contains(handler.warnings[1], "maxRetries=4") {
		t.Errorf("second warning should contain attempt=2 and maxRetries=4, got: %s", handler.warnings[1])
	}
	// Check third warning
	if !strings.Contains(handler.warnings[2], "attempt=3") || !strings.Contains(handler.warnings[2], "maxRetries=4") {
		t.Errorf("third warning should contain attempt=3 and maxRetries=4, got: %s", handler.warnings[2])
	}

	// Also verify metrics record the correct attempt numbers
	// Metrics record: attempt+1 for failures
	if len(metrics.retries) < 3 {
		t.Fatalf("expected at least 3 retry metrics, got %d", len(metrics.retries))
	}
	// The retry metrics during the loop should have attempt values 1, 2, 3
	for i, r := range metrics.retries[:3] {
		expectedAttempt := i + 1
		if r.attempt != expectedAttempt {
			t.Errorf("retry metric %d: expected attempt=%d, got %d", i, expectedAttempt, r.attempt)
		}
	}
}

// --- Error log format (line 127) ---

// TestRetryExecutor_ErrorLogFormat targets:
//
//	ARITHMETIC_BASE at line 127:35 (config.MaxRetries+1 in error message)
//
// Final error log: "Job %s failed after %d retries: %v"
// The %d should be config.MaxRetries+1 (total attempts including the initial one).
func TestRetryExecutor_ErrorLogFormat(t *testing.T) {
	t.Parallel()

	logger, handler := newRetryCaptureLogger()
	executor := NewRetryExecutor(logger)
	_ = handler
	metrics := &captureMetrics{}
	executor.SetMetricsRecorder(metrics)

	job := &testRetryJob{
		BareJob: BareJob{
			Name:         "error-log-test",
			MaxRetries:   2,
			RetryDelayMs: 1,
		},
	}
	ctx := &Context{Execution: &Execution{}}

	executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
		return errors.New("persistent-error")
	})

	// Should have 1 error message
	if len(handler.errors) != 1 {
		t.Fatalf("expected 1 error log, got %d: %v", len(handler.errors), handler.errors)
	}

	// Error format: "Job %s failed after %d retries: %v"
	// MaxRetries=2, so %d should be 3 (MaxRetries+1)
	if !strings.Contains(handler.errors[0], "failed after 3 retries") {
		t.Errorf("error log should contain 'failed after 3 retries', got: %s", handler.errors[0])
	}

	// Also check the final metrics recording: MaxRetries+1 = 3
	lastMetric := metrics.retries[len(metrics.retries)-1]
	if lastMetric.attempt != 3 {
		t.Errorf("final retry metric should have attempt=3 (MaxRetries+1), got %d", lastMetric.attempt)
	}
}

// --- Return error format (line 134) ---

// TestRetryExecutor_ReturnErrorFormat targets:
//
//	ARITHMETIC_BASE at line 134:73 (config.MaxRetries+1 in error return)
//
// Return: fmt.Errorf("job failed after %d attempts: %w", config.MaxRetries+1, lastErr)
func TestRetryExecutor_ReturnErrorFormat(t *testing.T) {
	t.Parallel()

	logger, handler := newRetryCaptureLogger()
	executor := NewRetryExecutor(logger)
	_ = handler

	job := &testRetryJob{
		BareJob: BareJob{
			Name:         "return-error-test",
			MaxRetries:   2,
			RetryDelayMs: 1,
		},
	}
	ctx := &Context{Execution: &Execution{}}

	err := executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
		return errors.New("base-error")
	})

	if err == nil {
		t.Fatal("expected error")
	}

	// Error should say "job failed after 3 attempts" (MaxRetries+1 = 3)
	if !strings.Contains(err.Error(), "job failed after 3 attempts") {
		t.Errorf("error should contain 'job failed after 3 attempts', got: %s", err.Error())
	}

	// Verify the wrapped error
	if !strings.Contains(err.Error(), "base-error") {
		t.Errorf("error should contain original error, got: %s", err.Error())
	}
}

// --- Exponential delay cap (line 146) ---

// TestRetryExecutor_ExponentialDelayCap targets:
//
//	CONDITIONALS_BOUNDARY at line 146:14 (comparison in min for cap)
//
// The calculateDelay uses min() to cap at RetryMaxDelayMs.
// We verify the cap is applied exactly at the boundary.
func TestRetryExecutor_ExponentialDelayCap(t *testing.T) {
	t.Parallel()

	logger, handler := newRetryCaptureLogger()
	executor := NewRetryExecutor(logger)
	_ = handler

	t.Run("ExactCap", func(t *testing.T) {
		t.Parallel()
		config := RetryConfig{
			RetryDelayMs:     100,
			RetryExponential: true,
			RetryMaxDelayMs:  400,
		}

		// attempt=0: 100 * 2^0 = 100ms
		d0 := executor.calculateDelay(config, 0)
		if d0 != 100*time.Millisecond {
			t.Errorf("attempt 0: expected 100ms, got %v", d0)
		}

		// attempt=1: 100 * 2^1 = 200ms
		d1 := executor.calculateDelay(config, 1)
		if d1 != 200*time.Millisecond {
			t.Errorf("attempt 1: expected 200ms, got %v", d1)
		}

		// attempt=2: 100 * 2^2 = 400ms == cap, should be exactly 400ms
		d2 := executor.calculateDelay(config, 2)
		if d2 != 400*time.Millisecond {
			t.Errorf("attempt 2: expected 400ms (at cap), got %v", d2)
		}

		// attempt=3: 100 * 2^3 = 800ms > cap, should be capped at 400ms
		d3 := executor.calculateDelay(config, 3)
		if d3 != 400*time.Millisecond {
			t.Errorf("attempt 3: expected 400ms (capped), got %v", d3)
		}

		// attempt=10: 100 * 2^10 = 102400ms >> cap, should be capped at 400ms
		d10 := executor.calculateDelay(config, 10)
		if d10 != 400*time.Millisecond {
			t.Errorf("attempt 10: expected 400ms (capped), got %v", d10)
		}
	})

	t.Run("BelowCap", func(t *testing.T) {
		t.Parallel()
		config := RetryConfig{
			RetryDelayMs:     50,
			RetryExponential: true,
			RetryMaxDelayMs:  1000,
		}

		// attempt=0: 50 * 1 = 50
		d0 := executor.calculateDelay(config, 0)
		if d0 != 50*time.Millisecond {
			t.Errorf("attempt 0: expected 50ms, got %v", d0)
		}

		// attempt=1: 50 * 2 = 100
		d1 := executor.calculateDelay(config, 1)
		if d1 != 100*time.Millisecond {
			t.Errorf("attempt 1: expected 100ms, got %v", d1)
		}

		// attempt=4: 50 * 16 = 800 (below cap)
		d4 := executor.calculateDelay(config, 4)
		if d4 != 800*time.Millisecond {
			t.Errorf("attempt 4: expected 800ms, got %v", d4)
		}

		// attempt=5: 50 * 32 = 1600 > 1000, capped at 1000
		d5 := executor.calculateDelay(config, 5)
		if d5 != 1000*time.Millisecond {
			t.Errorf("attempt 5: expected 1000ms (capped), got %v", d5)
		}
	})

	t.Run("NonExponential", func(t *testing.T) {
		t.Parallel()
		config := RetryConfig{
			RetryDelayMs:     200,
			RetryExponential: false,
			RetryMaxDelayMs:  500,
		}

		// Non-exponential should always return RetryDelayMs regardless of attempt
		for attempt := range 5 {
			d := executor.calculateDelay(config, attempt)
			if d != 200*time.Millisecond {
				t.Errorf("attempt %d: expected 200ms (non-exponential), got %v", attempt, d)
			}
		}
	})
}

// TestRetryExecutor_ExponentialFormulaExact verifies the exact exponential formula:
//
//	delayMs = int(float64(config.RetryDelayMs) * math.Pow(2, float64(attempt)))
//
// This kills the ARITHMETIC_BASE mutation at the multiplication/power operations.
func TestRetryExecutor_ExponentialFormulaExact(t *testing.T) {
	t.Parallel()

	logger, handler := newRetryCaptureLogger()
	executor := NewRetryExecutor(logger)
	_ = handler

	config := RetryConfig{
		RetryDelayMs:     10,
		RetryExponential: true,
		RetryMaxDelayMs:  100000, // very high cap so we test the formula itself
	}

	for attempt := range 8 {
		got := executor.calculateDelay(config, attempt)
		expected := time.Duration(int(float64(10)*math.Pow(2, float64(attempt)))) * time.Millisecond

		if got != expected {
			t.Errorf("attempt %d: expected %v, got %v", attempt, expected, got)
		}
	}
}

// TestRetryExecutor_MetricsAttemptValues verifies that metrics record the correct
// attempt values (attempt+1 for in-loop failures, MaxRetries+1 for final failure).
// This targets ARITHMETIC_BASE at lines 112 and 127/134.
func TestRetryExecutor_MetricsAttemptValues(t *testing.T) {
	t.Parallel()

	logger, handler := newRetryCaptureLogger()
	executor := NewRetryExecutor(logger)
	_ = handler
	metrics := &captureMetrics{}
	executor.SetMetricsRecorder(metrics)

	job := &testRetryJob{
		BareJob: BareJob{
			Name:         "metrics-test",
			MaxRetries:   3,
			RetryDelayMs: 1,
		},
	}
	ctx := &Context{Execution: &Execution{}}

	executor.ExecuteWithRetry(job, ctx, func(c *Context) error {
		return errors.New("always-fail")
	})

	// Expected retry records:
	// Loop: attempt=0 -> RecordJobRetry("metrics-test", 1, false)
	// Loop: attempt=1 -> RecordJobRetry("metrics-test", 2, false)
	// Loop: attempt=2 -> RecordJobRetry("metrics-test", 3, false)
	// Final: RecordJobRetry("metrics-test", 4, false)  (MaxRetries+1 = 4)
	expectedAttempts := []int{1, 2, 3, 4}

	if len(metrics.retries) != len(expectedAttempts) {
		t.Fatalf("expected %d retry records, got %d: %+v",
			len(expectedAttempts), len(metrics.retries), metrics.retries)
	}

	for i, expected := range expectedAttempts {
		if metrics.retries[i].attempt != expected {
			t.Errorf("retry record %d: expected attempt=%d, got %d",
				i, expected, metrics.retries[i].attempt)
		}
		if metrics.retries[i].success {
			t.Errorf("retry record %d: expected success=false", i)
		}
	}
}
