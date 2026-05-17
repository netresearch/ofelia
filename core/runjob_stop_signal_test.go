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

// TestRunJob_StopContainer_PropagatesSignal is the integration test for
// the #234 stop-signal feature: when an operator configures StopSignal
// on a RunJob, the value reaches the Docker provider's StopContainer
// call as domain.StopOptions.Signal — and from there the daemon's
// `signal` query parameter on POST /containers/{id}/stop.
func TestRunJob_StopContainer_PropagatesSignal(t *testing.T) {
	t.Parallel()

	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	j := NewRunJob(provider)
	j.BareJob = BareJob{Name: "test-run"}
	j.StopSignal = "SIGINT"
	j.setContainerID("test-container")

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

	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	j := NewRunJob(provider)
	j.BareJob = BareJob{Name: "test-run-default"}
	// StopSignal intentionally left empty.
	j.setContainerID("test-container")

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

	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	j := NewRunJob(provider)
	j.BareJob = BareJob{Name: "test-run-both"}
	j.StopSignal = "SIGUSR1"
	j.setContainerID("test-container")

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

	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	j := NewRunJob(provider)
	j.BareJob = BareJob{Name: "test-run-bare"}
	j.StopSignal = "INT" // bare suffix, no SIG prefix
	j.setContainerID("test-container")

	require.NoError(t, j.stopContainer(context.Background(), 10*time.Second))
	require.Len(t, containers.StopCalls, 1)
	assert.Equal(t, "INT", containers.StopCalls[0].Options.Signal,
		"bare-suffix form 'INT' must reach the daemon verbatim — Docker accepts both 'INT' and 'SIGINT' and Ofelia should not normalize one form to the other")
}

// TestRunJob_CleanupOnDeadline_HonorsStopTimeout pins that a
// configured RunJob.StopTimeout overrides the legacy 10s hardcoded
// grace period in the deadline-cleanup path — the only path that
// reads the field today. The docstring promises this behavior; this
// test guards against a future refactor silently reverting it.
func TestRunJob_CleanupOnDeadline_HonorsStopTimeout(t *testing.T) {
	t.Parallel()

	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	j := NewRunJob(provider)
	j.BareJob = BareJob{Name: "test-run-timeout"}
	j.StopTimeout = 45 * time.Second
	j.setContainerID("test-container")

	// Drive cleanupOnDeadline directly with an already-expired ctx —
	// the function ignores the passed ctx (it's only there for
	// symmetry/diagnostics per #655) and builds its own.
	expiredCtx, cancel := context.WithCancel(context.Background())
	cancel()
	logCtx := &Context{Logger: test.NewTestLogger(), Job: j}
	j.cleanupOnDeadline(expiredCtx, logCtx)

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

	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	j := NewRunJob(provider)
	j.BareJob = BareJob{Name: "test-run-timeout-default"}
	// StopTimeout intentionally left zero.
	j.setContainerID("test-container")

	expiredCtx, cancel := context.WithCancel(context.Background())
	cancel()
	logCtx := &Context{Logger: test.NewTestLogger(), Job: j}
	j.cleanupOnDeadline(expiredCtx, logCtx)

	require.Len(t, containers.StopCalls, 1)
	require.NotNil(t, containers.StopCalls[0].Options.Timeout)
	assert.Equal(t, 10*time.Second, *containers.StopCalls[0].Options.Timeout,
		"unset StopTimeout must preserve the pre-#234 hardcoded 10s grace period")
}

// Compile-time field guard for domain.StopOptions. If a future
// refactor renames Signal or Timeout this fails fast at build instead
// of at first integration-test invocation.
var _ = domain.StopOptions{Timeout: nil, Signal: ""} //nolint:exhaustruct // compile-time field guard
