// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// RunJob refuses a manual run of a job without durable schedule state that
// the global job limit or a no-overlap middleware would skip. The caller gets
// ErrJobBusy (409 on the web API) instead of a success followed by a skipped
// run in the job's history.

// overlapGuardStub is a middleware that reports a no-overlap setting.
type overlapGuardStub struct{ skip bool }

func (m *overlapGuardStub) ContinueOnStop() bool   { return false }
func (m *overlapGuardStub) Run(ctx *Context) error { return ctx.Next() }
func (m *overlapGuardStub) SkipsOverlap() bool     { return m.skip }

func TestRunJob_RefusesARunTheGlobalLimitWouldSkip(t *testing.T) {
	t.Parallel()

	notifier := &skipRecordingMiddleware{continueOnStop: true}
	limited := newLimitedJob("limited")
	limited.Use(notifier)
	s, holder := startWithFullLimit(t, newDiscardLogger(), limited)

	err := s.RunJob(context.Background(), limited.GetName())
	require.ErrorIs(t, err, ErrJobLimitReached)
	require.ErrorIs(t, err, ErrJobBusy)

	// Once the slot is free, a manual run is accepted. It is the only run in
	// the job's history, so the refused run was not triggered.
	close(holder.release)
	require.Eventually(t, func() bool {
		return s.RunJob(context.Background(), limited.GetName()) == nil
	}, 5*time.Second, 10*time.Millisecond)
	last := waitForLastRun(t, limited)
	require.False(t, last.Skipped, "the accepted run was recorded as skipped")
	require.Len(t, limited.GetHistory(), 1)
	require.Len(t, notifier.executions(), 1, "a notification middleware saw the refused run")
	require.Equal(t, 1, limited.Called())
}

// A job with durable schedule state waits for a slot, so a full limit does
// not refuse its manual run.
func TestRunJob_DurableJobIsAcceptedAtAFullLimit(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.SetMaxConcurrentJobs(1)
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	releaseSlot := occupySlot(t, h.s)
	defer releaseSlot()

	require.NoError(t, h.s.RunJob(context.Background(), "shop.backup"))
}

// startHeldRun starts a scheduler with job j and a manual run of j that holds
// until j.release is closed.
func startHeldRun(t *testing.T, j *limitHolderJob) *Scheduler {
	t.Helper()
	s := NewScheduler(newDiscardLogger())
	require.NoError(t, s.AddJob(j))
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.StopWithTimeout(5 * time.Second) })
	require.NoError(t, s.RunJob(context.Background(), j.GetName()))
	select {
	case <-j.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first run did not start")
	}
	return s
}

func TestRunJob_RefusesARunANoOverlapMiddlewareWouldSkip(t *testing.T) {
	t.Parallel()
	j := newLimitHolderJob("single")
	j.Use(&overlapGuardStub{skip: true})
	s := startHeldRun(t, j)
	defer close(j.release)

	err := s.RunJob(context.Background(), j.GetName())
	require.ErrorIs(t, err, ErrJobBusy)
	require.NotErrorIs(t, err, ErrJobLimitReached)
}

func TestRunJob_AcceptsAnOverlappingRunWithoutNoOverlap(t *testing.T) {
	t.Parallel()
	j := newLimitHolderJob("parallel")
	j.Use(&overlapGuardStub{skip: false})
	s := startHeldRun(t, j)
	defer close(j.release)

	require.NoError(t, s.RunJob(context.Background(), j.GetName()))
}
