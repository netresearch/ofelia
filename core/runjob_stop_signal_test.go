// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// newRunJobWithMock builds a RunJob wired to a fresh mock Docker
// client and pre-populates the container ID so callers can immediately
// drive stop/cleanup paths. Returned containers handle exposes the
// recorded StopCalls. Configured by mutating the returned RunJob
// (StopSignal, StopTimeout, etc.) before invoking the method under
// test. Shared across the #234 stop-signal/timeout tests to keep the
// per-test arrange section to the one or two lines that actually vary
// (SonarCloud duplication budget on new code is 3%).
func newRunJobWithMock(t *testing.T, jobName string) (*RunJob, *mock.ContainerService) {
	t.Helper()
	mc := mock.NewDockerClient()
	containers, ok := mc.Containers().(*mock.ContainerService)
	require.True(t, ok, "mock client must expose *mock.ContainerService")
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)
	j := NewRunJob(provider)
	j.BareJob = BareJob{Name: jobName}
	j.setContainerID("test-container")
	return j, containers
}

// TestRunJob_StopContainer_PropagatesSignal is the integration test for
// the #234 stop-signal feature: when an operator configures StopSignal
// on a RunJob, the value reaches the Docker provider's StopContainer
// call as domain.StopOptions.Signal — and from there the daemon's
// `signal` query parameter on POST /containers/{id}/stop.
func TestRunJob_StopContainer_PropagatesSignal(t *testing.T) {
	t.Parallel()

	j, containers := newRunJobWithMock(t, "test-run")
	j.StopSignal = "SIGINT"

	require.NoError(t, j.stopContainer(context.Background(), 10*time.Second))
	require.Len(t, containers.StopCalls, 1)
	assert.Equal(t, "SIGINT", containers.StopCalls[0].Options.Signal,
		"RunJob.StopSignal must propagate to domain.StopOptions.Signal (#234)")
}

// TestRunJob_StopContainer_EmptySignalPreservesPreFixBehavior pins
// that an unset StopSignal produces an empty domain.StopOptions.Signal
// — meaning the Docker daemon falls back to the container image's
// STOPSIGNAL (which itself defaults to SIGTERM). The pre-#234 behavior
// is preserved when operators don't opt into the new field.
func TestRunJob_StopContainer_EmptySignalPreservesPreFixBehavior(t *testing.T) {
	t.Parallel()

	j, containers := newRunJobWithMock(t, "test-run-default")
	// StopSignal intentionally left empty.

	require.NoError(t, j.stopContainer(context.Background(), 10*time.Second))
	require.Len(t, containers.StopCalls, 1)
	assert.Empty(t, containers.StopCalls[0].Options.Signal,
		"unset StopSignal must yield an empty domain.StopOptions.Signal — Docker then honors the image's STOPSIGNAL (pre-#234 behavior)")
}

// TestRunJob_StopContainer_PropagatesTimeoutAlongsideSignal pins that
// adding Signal to the options struct doesn't break the existing
// timeout plumbing. Both fields must arrive at the same call.
func TestRunJob_StopContainer_PropagatesTimeoutAlongsideSignal(t *testing.T) {
	t.Parallel()

	j, containers := newRunJobWithMock(t, "test-run-both")
	j.StopSignal = "SIGUSR1"

	const wantTimeout = 30 * time.Second
	require.NoError(t, j.stopContainer(context.Background(), wantTimeout))
	require.Len(t, containers.StopCalls, 1)
	got := containers.StopCalls[0].Options
	require.NotNil(t, got.Timeout)
	assert.Equal(t, wantTimeout, *got.Timeout,
		"StopOptions.Timeout must arrive intact even when Signal is set — both fields are independent")
	assert.Equal(t, "SIGUSR1", got.Signal)
}

// TestRunJob_StopContainer_BareSignalSuffix pins the docstring claim
// that "INT" (without the SIG prefix) is forwarded to the Docker
// daemon verbatim — Docker accepts both forms and Ofelia should not
// transform the operator's input. If a future contributor adds
// client-side normalization (e.g., prepending "SIG"), this test
// catches the silently-broken docstring contract.
func TestRunJob_StopContainer_BareSignalSuffix(t *testing.T) {
	t.Parallel()

	j, containers := newRunJobWithMock(t, "test-run-bare")
	j.StopSignal = "INT" // bare suffix, no SIG prefix

	require.NoError(t, j.stopContainer(context.Background(), 10*time.Second))
	require.Len(t, containers.StopCalls, 1)
	assert.Equal(t, "INT", containers.StopCalls[0].Options.Signal,
		"bare-suffix form 'INT' must reach the daemon verbatim — Docker accepts both 'INT' and 'SIGINT' and Ofelia should not normalize one form to the other")
}

// driveCleanupOnDeadline invokes cleanupOnDeadline with an
// already-expired ctx (mirroring the production trigger — the parent
// ran out of budget) and a minimal logger-bearing Context. Extracted
// from the two cleanup tests below to keep duplication under SonarCloud's
// 3% budget on new code.
func driveCleanupOnDeadline(j *RunJob) {
	expiredCtx, cancel := context.WithCancel(context.Background())
	cancel()
	logCtx := &Context{Logger: test.NewTestLogger(), Job: j}
	j.cleanupOnDeadline(expiredCtx, logCtx)
}

// TestRunJob_CleanupOnDeadline_HonorsStopTimeout pins that a
// configured RunJob.StopTimeout overrides the legacy 10s hardcoded
// grace period in the deadline-cleanup path — the only path that
// reads the field today. The docstring promises this behavior; this
// test guards against a future refactor silently reverting it.
func TestRunJob_CleanupOnDeadline_HonorsStopTimeout(t *testing.T) {
	t.Parallel()

	j, containers := newRunJobWithMock(t, "test-run-timeout")
	j.StopTimeout = 45 * time.Second

	driveCleanupOnDeadline(j)

	require.Len(t, containers.StopCalls, 1, "cleanupOnDeadline must invoke Stop exactly once")
	require.NotNil(t, containers.StopCalls[0].Options.Timeout)
	assert.Equal(t, 45*time.Second, *containers.StopCalls[0].Options.Timeout,
		"configured RunJob.StopTimeout must override the 10s default in cleanupOnDeadline")
}

// TestRunJob_CleanupOnDeadline_UnsetTimeoutDefaultsTo10s pins the
// inverse: an unset StopTimeout preserves the pre-#234 hardcoded 10s
// behavior. Unconfigured RunJobs see no change.
func TestRunJob_CleanupOnDeadline_UnsetTimeoutDefaultsTo10s(t *testing.T) {
	t.Parallel()

	j, containers := newRunJobWithMock(t, "test-run-timeout-default")
	// StopTimeout intentionally left zero.

	driveCleanupOnDeadline(j)

	require.Len(t, containers.StopCalls, 1)
	require.NotNil(t, containers.StopCalls[0].Options.Timeout)
	assert.Equal(t, 10*time.Second, *containers.StopCalls[0].Options.Timeout,
		"unset StopTimeout must preserve the pre-#234 hardcoded 10s grace period")
}

// Compile-time field guard for domain.StopOptions. If a future
// refactor renames Signal or Timeout this fails fast at build instead
// of at first integration-test invocation.
var _ = domain.StopOptions{Timeout: nil, Signal: ""} //nolint:exhaustruct // compile-time field guard
