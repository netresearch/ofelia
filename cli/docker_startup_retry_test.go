// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// flakyPingProvider is a minimal core.DockerProvider that records ping
// attempts and returns a configurable failure pattern. Used by the
// startup-retry tests to verify the (count+1)-attempt budget and the
// "succeed after N attempts" recovery path. Only Ping and Close are
// exercised; the other methods panic so an accidental call surfaces in
// the test output.
type flakyPingProvider struct {
	attempts     atomic.Int32
	failUntilNth int32 // return pingErr for attempts 1..failUntilNth, nil after
	pingErr      error
	mockDockerProviderForHandler
}

func (p *flakyPingProvider) Ping(_ context.Context) error {
	n := p.attempts.Add(1)
	if n <= p.failUntilNth {
		return p.pingErr
	}
	return nil
}

// TestPingWithRetry_NoRetriesPreservesPreFixBehavior pins that the
// default (StartupRetryCount=0) collapses to a single Ping attempt — no
// behavior change for operators who haven't opted into the new feature.
// Closes the regression-risk concern that adding the retry helper would
// silently change the "exit on first failure" startup contract.
func TestPingWithRetry_NoRetriesPreservesPreFixBehavior(t *testing.T) {
	t.Parallel()

	provider := &flakyPingProvider{pingErr: errors.New("boom"), failUntilNth: 999}
	err := pingWithRetry(context.Background(), provider, 0, time.Second, test.NewTestLogger())

	require.Error(t, err)
	assert.Equal(t, "boom", err.Error(),
		"count=0 must return the single Ping error directly (no retry wrapping)")
	assert.Equal(t, int32(1), provider.attempts.Load(),
		"count=0 must produce exactly 1 attempt (the pre-#523 single-ping behavior)")
}

// TestPingWithRetry_SucceedsAfterNAttempts pins the recovery path: when
// the Docker daemon comes up partway through the retry budget, the
// helper returns nil and the daemon proceeds normally. The fix for
// #523's reported "socket proxy not yet ready" scenario.
func TestPingWithRetry_SucceedsAfterNAttempts(t *testing.T) {
	t.Parallel()

	provider := &flakyPingProvider{pingErr: errors.New("not ready yet"), failUntilNth: 2}
	// Tiny interval so the test doesn't pay the full backoff budget.
	err := pingWithRetry(context.Background(), provider, 5, time.Millisecond, test.NewTestLogger())

	require.NoError(t, err,
		"daemon should become reachable on attempt 3 (after 2 failed attempts) and the helper must return nil")
	assert.Equal(t, int32(3), provider.attempts.Load(),
		"exactly 3 attempts: 2 failures + 1 success")
}

// TestPingWithRetry_ExhaustsBudgetAndReturnsLastError pins the failure
// path: if every attempt fails, the helper returns the last error
// (preserving the existing "exit with the failure reason" UX) and the
// attempt budget is exactly count+1 (not count, not count+2).
func TestPingWithRetry_ExhaustsBudgetAndReturnsLastError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("daemon down")
	provider := &flakyPingProvider{pingErr: wantErr, failUntilNth: 999}
	err := pingWithRetry(context.Background(), provider, 3, time.Millisecond, test.NewTestLogger())

	require.Error(t, err)
	assert.Equal(t, wantErr, err,
		"on exhaustion the helper must return the last Ping error verbatim — no wrapping that would obscure the operator-facing reason")
	assert.Equal(t, int32(4), provider.attempts.Load(),
		"count=3 must produce exactly 4 attempts (initial + 3 retries)")
}

// TestPingWithRetry_HonorsContextCancellation pins that ctx cancellation
// during the backoff window interrupts the retry budget instead of
// blocking the full Σ baseInterval × 2^attempt sum. Same shape as the
// retry-loop ctx-cancellation fixes in #685 (webhook) and #687 (job
// retries) — daemon SIGTERM during startup must drain promptly.
func TestPingWithRetry_HonorsContextCancellation(t *testing.T) {
	t.Parallel()

	provider := &flakyPingProvider{pingErr: errors.New("never reachable"), failUntilNth: 999}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Big base interval so without ctx cancellation the test would
	// block ~60s waiting on the backoff schedule.
	start := time.Now()
	err := pingWithRetry(ctx, provider, 5, 30*time.Second, test.NewTestLogger())
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled,
		"the returned error must wrap context.Canceled so callers can branch on shutdown vs. real Docker errors")
	assert.Less(t, elapsed, 5*time.Second,
		"ctx cancellation during backoff must drain promptly (elapsed=%v); pre-fix this would block ~30s on the first time.After", elapsed)
}

// TestNewDockerHandler_StartupRetrySucceedsAfterFailures is the end-to-
// end integration test for the #523 fix: NewDockerHandler reads
// StartupRetryCount/Interval from DockerConfig and the daemon comes up
// after a flaky-start period. Pre-#523 the daemon would have exited on
// the first ping failure.
func TestNewDockerHandler_StartupRetrySucceedsAfterFailures(t *testing.T) {
	provider := &flakyPingProvider{pingErr: errors.New("warming up"), failUntilNth: 2}
	notifier := &dummyNotifier{}

	handler, err := NewDockerHandler(context.Background(), notifier, test.NewTestLogger(),
		&DockerConfig{
			StartupRetryCount:    5,
			StartupRetryInterval: time.Millisecond,
		}, provider)
	t.Cleanup(func() {
		if handler != nil {
			_ = handler.Shutdown(context.Background())
		}
	})

	require.NoError(t, err,
		"NewDockerHandler must succeed when Docker comes up within the retry budget — the #523 fix")
	assert.NotNil(t, handler)
	assert.GreaterOrEqual(t, provider.attempts.Load(), int32(3),
		"at least 3 ping attempts must have happened (2 failures + the success that let startup proceed)")
}

// Verify flakyPingProvider satisfies the core.DockerProvider interface
// expected by NewDockerHandler. Compile-time assertion — if the
// interface changes, this fails fast at build instead of at first use.
var _ core.DockerProvider = (*flakyPingProvider)(nil)
