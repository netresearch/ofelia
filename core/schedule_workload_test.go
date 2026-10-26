// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/persist"
)

func TestScheduleState_OrphanSurvivesRepeatedRestarts(t *testing.T) {
	t.Parallel()
	for _, lookupFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("lookup_fails_%t", lookupFails), func(t *testing.T) {
			t.Parallel()
			h := newDurableHarness(t, at(23, 10))
			j := newDurableJob("shop.backup", true, true)
			var gone atomic.Bool
			check := func(id string) (bool, error) {
				assert.Equal(t, "interrupted", id)
				if gone.Load() {
					return false, nil
				}
				if lookupFails {
					return false, errors.New("docker unreachable")
				}
				return true, nil
			}
			h.put(j.Name, persist.Schedule{
				Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(22, 2),
				Dispatch: &persist.Dispatch{
					Occurrence: persist.Occurrence{Scheduled: at(22, 2), Trigger: triggerScheduled},
					Execution:  "interrupted", State: dispatchStarted,
				},
			})
			for restart := range 2 {
				j = newDurableJob("shop.backup", true, true)
				j.setRunning(check)
				h.s.orphanPollInterval = 10 * time.Millisecond
				require.NoError(t, h.s.AddJob(j))
				h.start()
				require.ErrorIs(t, h.s.RunJob(context.Background(), j.Name), ErrJobBusy, "restart %d", restart+1)
				noRun(t, j)
				assert.Equal(t, outcomeUnknown, h.record(j.Name).Dispatch.Outcome)
				if restart == 0 {
					require.NoError(t, h.s.StopWithTimeout(time.Second))
					store := persist.NewStore(h.store.Path())
					require.NoError(t, store.Load(), "reload the persisted state, not its in-memory copy")
					h = newDurableHarnessWithStore(t, at(23, 11), store)
				}
			}
			gone.Store(true)
			run := nextRun(t, j)
			assert.True(t, run.info.ScheduledTime.Equal(at(23, 2)))
			noRun(t, j)
		})
	}
}

type unconfirmedDurableJob struct {
	*durableJob
	attempts atomic.Int32
}

func (j *unconfirmedDurableJob) Run(ctx *Context) error {
	attempt := j.attempts.Add(1)
	if err := j.durableJob.Run(ctx); err != nil {
		return err
	}
	if attempt == 1 {
		return fmt.Errorf("cleanup failed: %w", ErrComposeTerminationUnconfirmed)
	}
	return nil
}

func TestScheduleState_UnconfirmedTerminationKeepsQueuedWorkloadBlocked(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	h.s.orphanPollInterval = 10 * time.Millisecond
	j := &unconfirmedDurableJob{durableJob: newDurableJob("shop.backup", true, true)}
	var gone atomic.Bool
	j.setRunning(func(string) (bool, error) { return !gone.Load(), nil })
	release := j.hold()
	t.Cleanup(release)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	h.fire(j.Name, at(24, 2))
	first := nextRun(t, j.durableJob)
	h.fire(j.Name, at(25, 2))
	h.eventually(j.Name, func(r persist.Schedule) bool { return r.Pending != nil }, "the second fire is queued")
	release()
	h.eventually(j.Name, ended, "the failed cleanup records its outcome")
	require.ErrorIs(t, h.s.RunJob(context.Background(), j.Name), ErrJobBusy)
	noRun(t, j.durableJob)
	assert.Equal(t, outcomeFailed, h.record(j.Name).Dispatch.Outcome)
	assert.Equal(t, first.execution, h.record(j.Name).Dispatch.Execution)
	require.NotNil(t, h.record(j.Name).Pending)

	require.NoError(t, h.s.StopWithTimeout(time.Second))
	store := persist.NewStore(h.store.Path())
	require.NoError(t, store.Load())
	again := newDurableHarnessWithStore(t, at(23, 11), store)
	again.s.orphanPollInterval = 10 * time.Millisecond
	j2 := newDurableJob(j.Name, true, true)
	j2.setRunning(func(id string) (bool, error) {
		assert.Equal(t, first.execution, id)
		return !gone.Load(), nil
	})
	require.NoError(t, again.s.AddJob(j2))
	again.start()
	require.ErrorIs(t, again.s.RunJob(context.Background(), j.Name), ErrJobBusy)
	noRun(t, j2)
	assert.Equal(t, outcomeFailed, again.record(j.Name).Dispatch.Outcome, "recovery preserves the recorded execution result")
	gone.Store(true)
	second := nextRun(t, j2)
	assert.True(t, second.info.ScheduledTime.Equal(at(25, 2)))
}

func TestRetryExecutor_DoesNotRetryUnconfirmedComposeWorkload(t *testing.T) {
	t.Parallel()
	j := newDurableJob("shop.backup", true, true)
	j.MaxRetries, j.RetryDelayMs = 2, 1
	e, err := NewExecution()
	require.NoError(t, err)
	defer e.Cleanup()
	h := newDurableHarness(t, at(23, 10))
	ctx := NewContextWithContext(context.Background(), h.s, j, e)
	attempts := 0
	err = h.s.retryExecutor.ExecuteWithRetry(j, ctx, func(*Context) error {
		attempts++
		return fmt.Errorf("cleanup: %w", ErrComposeTerminationUnconfirmed)
	})
	require.ErrorIs(t, err, ErrComposeTerminationUnconfirmed)
	assert.Equal(t, 1, attempts, "retrying can overlap the workload whose termination was not confirmed")
}

func TestScheduleState_LegacyUnsuccessfulDispatchIsChecked(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{outcomeUnknown, outcomeFailed, outcomeCanceled} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			h := newDurableHarness(t, at(23, 10))
			j := newDurableJob("shop.backup", true, true)
			j.setRunning(func(id string) (bool, error) { return id == "interrupted", nil })
			rec := interruptedRecord("an older schedule")
			rec.Dispatch.State, rec.Dispatch.Outcome = dispatchEnded, outcome
			h.put(j.Name, rec) // Older builds persisted only the ended dispatch.
			store := persist.NewStore(h.store.Path())
			require.NoError(t, store.Load())
			h = newDurableHarnessWithStore(t, at(23, 10), store)
			require.NoError(t, h.s.AddJob(j))
			h.start()
			require.ErrorIs(t, h.s.RunJob(context.Background(), j.Name), ErrJobBusy)
			noRun(t, j)
			assert.Equal(t, "interrupted", h.record(j.Name).Workload)
			assert.Equal(t, outcome, h.record(j.Name).Dispatch.Outcome)
		})
	}
}

func TestScheduleState_UnconfirmedCanceledReplacementStaysBusy(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.orphanPollInterval = 10 * time.Millisecond
	j := &unconfirmedDurableJob{durableJob: newDurableJob("shop.backup", true, true)}
	j.setRunning(func(string) (bool, error) { return true, nil })
	release := j.hold()
	t.Cleanup(release)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	h.fire(j.Name, at(23, 2))
	first := nextRun(t, j.durableJob)
	require.NoError(t, h.s.RemoveJob(j))
	h.eventually(j.Name, ended, "the canceled execution has finished its cleanup attempt")
	assert.Equal(t, outcomeCanceled, h.record(j.Name).Dispatch.Outcome)
	assert.Equal(t, first.execution, h.record(j.Name).Workload)
	j2 := newDurableJob(j.Name, true, true)
	j2.setRunning(func(string) (bool, error) { return true, nil })
	require.NoError(t, h.s.AddJob(j2))
	require.ErrorIs(t, h.s.RunJob(context.Background(), j.Name), ErrJobBusy)
	noRun(t, j2)
}

func TestScheduleState_OrphanReleaseWaitsForDurableWrite(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, true)
	j.setRunning(func(string) (bool, error) { return true, nil })
	h.put(j.Name, interruptedRecord(j.policy.Revision))
	require.NoError(t, h.s.AddJob(j))
	h.start()
	store := &failingStore{Store: h.store}
	h.s.SetScheduleStore(store)
	_, cleared := h.s.orphanEnded(j.Name, "interrupted")
	assert.False(t, cleared, "a failed state write must retain the workload guard")
	require.ErrorIs(t, h.s.RunJob(context.Background(), j.Name), ErrJobBusy)
	assert.Equal(t, "interrupted", h.record(j.Name).Workload)
	store.mu.Lock()
	store.allows = 1
	store.mu.Unlock()
	_, cleared = h.s.orphanEnded(j.Name, "interrupted")
	assert.True(t, cleared)
	assert.Empty(t, h.record(j.Name).Workload)
}

func TestScheduleState_FailedStartWriteDoesNotLeaveAWorkloadBusy(t *testing.T) {
	t.Parallel()
	for _, waitsForSlot := range []bool{false, true} {
		t.Run(fmt.Sprintf("waits_for_slot_%t", waitsForSlot), func(t *testing.T) {
			t.Parallel()
			h := newDurableHarness(t, at(23, 1))
			h.s.orphanPollInterval = 10 * time.Millisecond
			h.s.SetMaxConcurrentJobs(1)
			j := newDurableJob("shop.backup", true, false)
			require.NoError(t, h.s.AddJob(j))
			h.start()
			releaseSlot := func() {}
			if waitsForSlot {
				slot := h.s.concurrencySem.getChan()
				slot <- struct{}{}
				var once sync.Once
				releaseSlot = func() { once.Do(func() { <-slot }) }
				t.Cleanup(releaseSlot)
				h.fire(j.Name, at(23, 2))
				h.eventually(j.Name, func(r persist.Schedule) bool {
					return r.Dispatch != nil && r.Dispatch.State == dispatchWaiting
				}, "the admitted execution waits for its slot")
			}
			// Rename over a directory fails after Store has updated its in-memory
			// record. This exercises the real store's failed-write behavior.
			path := h.store.Path()
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Mkdir(path, 0o700))
			if waitsForSlot {
				releaseSlot()
			} else {
				h.fire(j.Name, at(23, 2))
			}
			h.eventually(j.Name, func(r persist.Schedule) bool { return r.Workload != "" }, "the attempted start updated memory")
			noRun(t, j)
			require.NoError(t, os.Remove(path))
			require.Eventually(t, func() bool { return !h.s.durableBusy(j.Name) }, time.Second, 5*time.Millisecond,
				"a workload that never started must be reconciled after storage recovers")
			h.fire(j.Name, at(23, 3))
			run := nextRun(t, j)
			assert.True(t, run.info.ScheduledTime.Equal(at(23, 3)))
		})
	}
}
