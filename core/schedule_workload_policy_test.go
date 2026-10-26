// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/persist"
)

type nonrecordWorkloadJob struct{ *durableJob }

// SchedulePolicy opts the test replacement out of durable scheduling while
// leaving it able to observe a workload inherited from its former registration.
func (j *nonrecordWorkloadJob) SchedulePolicy() (SchedulePolicy, bool) {
	return SchedulePolicy{}, false
}

// Switching to skip/forbid must retain both modern and legacy live-workload
// guards while dropping old queue entries and preserving disable/enable behavior.
func TestScheduleState_UnresolvedWorkloadSurvivesPolicyDowngrade(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, state, outcome string
		workload             bool
	}{
		{"tracked", dispatchEnded, outcomeUnknown, true},
		{"legacy started", dispatchStarted, "", false},
		{"legacy unknown", dispatchEnded, outcomeUnknown, false},
		{"legacy failed", dispatchEnded, outcomeFailed, false},
		{"legacy canceled", dispatchEnded, outcomeCanceled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newDurableHarness(t, at(23, 10))
			h.s.orphanPollInterval = 5 * time.Millisecond
			old := newDurableJob("shop.backup", true, true)
			rec := interruptedRecord(old.policy.Revision)
			rec.Dispatch.State, rec.Dispatch.Outcome = tc.state, tc.outcome
			if tc.workload {
				rec.Workload = rec.Dispatch.Execution
			}
			rec.Pending = &persist.Occurrence{Scheduled: at(24, 2), Trigger: triggerScheduled}
			h.put(old.Name, rec)
			job := &nonrecordWorkloadJob{durableJob: newDurableJob(old.Name, false, false)}
			var gone atomic.Bool
			job.setRunning(func(string) (bool, error) { return !gone.Load(), nil })
			require.NoError(t, h.s.AddJob(job))
			h.start()
			require.ErrorIs(t, h.s.RunJob(context.Background(), job.Name), ErrJobBusy,
				"switching to skip/forbid must retain the live workload guard")
			h.s.PruneScheduleRecords(func(string) bool { return false })
			assert.Nil(t, h.record(job.Name).Pending, "old schedule occurrences must be dropped")
			require.NoError(t, h.s.DisableJob(job.Name))
			require.NoError(t, h.s.EnableJob(job.Name))
			require.ErrorIs(t, h.s.RunJob(context.Background(), job.Name), ErrJobBusy)
			noRun(t, job.durableJob)
			gone.Store(true)
			require.Eventually(t, func() bool { return !h.s.retainedWorkloadBusy(job.Name) }, time.Second, time.Millisecond)
			require.NoError(t, h.s.RunJob(context.Background(), job.Name))
			nextRun(t, job.durableJob)
			noRun(t, job.durableJob)
			assert.Empty(t, h.store.ScheduleNames(), "the new skip/forbid job keeps no schedule record")
		})
	}
}

// A reload that abandons durable scheduling cannot dispatch beside an old
// workload whose cancellation cleanup has returned without confirming its end.
func TestScheduleState_LivePolicyDowngradeKeepsWorkloadGuard(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.orphanPollInterval = 5 * time.Millisecond
	old := &unconfirmedDurableJob{durableJob: newDurableJob("shop.backup", true, true)}
	var gone atomic.Bool
	old.setRunning(func(string) (bool, error) { return !gone.Load(), nil })
	release := old.hold()
	t.Cleanup(release)
	require.NoError(t, h.s.AddJob(old))
	h.start()
	h.fire(old.Name, at(23, 2))
	nextRun(t, old.durableJob)
	h.fire(old.Name, at(24, 2))
	h.eventually(old.Name, func(rec persist.Schedule) bool { return rec.Pending != nil }, "old occurrence queued")
	require.NoError(t, h.s.RemoveJob(old))
	h.eventually(old.Name, ended, "cleanup finishes with an unconfirmed workload")
	job := &nonrecordWorkloadJob{durableJob: newDurableJob(old.Name, false, false)}
	job.setRunning(func(string) (bool, error) { return !gone.Load(), nil })
	require.NoError(t, h.s.AddJob(job))
	h.s.PruneScheduleRecords(func(string) bool { return false })
	require.ErrorIs(t, h.s.RunJob(context.Background(), job.Name), ErrJobBusy)
	h.fire(job.Name, at(25, 2))
	noRun(t, job.durableJob)
	gone.Store(true)
	require.Eventually(t, func() bool { return !h.s.retainedWorkloadBusy(job.Name) }, time.Second, time.Millisecond)
	require.NoError(t, h.s.RunJob(context.Background(), job.Name))
	nextRun(t, job.durableJob)
	noRun(t, job.durableJob)
	assert.Empty(t, h.store.ScheduleNames())
}

// Removing configuration retires its schedule, not its unresolved workload;
// the persisted tombstone must still protect a later same-name registration.
func TestScheduleState_RemovedWorkloadSurvivesPruneAndRestart(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	h.s.orphanPollInterval = 5 * time.Millisecond
	old := newDurableJob("shop.backup", true, true)
	var gone atomic.Bool
	old.setRunning(func(string) (bool, error) { return !gone.Load(), nil })
	rec := interruptedRecord(old.policy.Revision)
	rec.Workload = rec.Dispatch.Execution
	h.put(old.Name, rec)
	require.NoError(t, h.s.AddJob(old))
	h.start()
	require.NoError(t, h.s.RemoveJob(old))
	h.s.PruneScheduleRecords(func(string) bool { return false })
	assert.NotEmpty(t, h.record(old.Name).Workload, "removing configuration must retain its live workload tombstone")
	require.NoError(t, h.s.StopWithTimeout(time.Second))
	store := persist.NewStore(h.store.Path())
	require.NoError(t, store.Load())
	again := newDurableHarnessWithStore(t, at(23, 11), store)
	again.s.orphanPollInterval = 5 * time.Millisecond
	again.s.PruneScheduleRecords(func(string) bool { return false }) // still absent at restart
	job := &nonrecordWorkloadJob{durableJob: newDurableJob(old.Name, false, false)}
	job.setRunning(func(string) (bool, error) { return !gone.Load(), nil })
	require.NoError(t, again.s.AddJob(job))
	again.start()
	require.ErrorIs(t, again.s.RunJob(context.Background(), job.Name), ErrJobBusy)
	noRun(t, job.durableJob)
	gone.Store(true)
	require.Eventually(t, func() bool { return !again.s.retainedWorkloadBusy(job.Name) }, time.Second, time.Millisecond)
	require.NoError(t, again.s.RunJob(context.Background(), job.Name))
	nextRun(t, job.durableJob)
	assert.Empty(t, again.store.ScheduleNames())
}

// A new durable registration takes ownership of a retained workload record
// so the orphan watcher cannot prune its newly active scheduling state.
func TestScheduleState_DurableReregistrationKeepsRecoveredRecord(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	h.s.orphanPollInterval = 5 * time.Millisecond
	old := newDurableJob("shop.backup", true, true)
	var gone atomic.Bool
	old.setRunning(func(string) (bool, error) { return !gone.Load(), nil })
	h.put(old.Name, interruptedRecord(old.policy.Revision))
	require.NoError(t, h.s.AddJob(old))
	h.start()
	require.NoError(t, h.s.RemoveJob(old))
	h.s.PruneScheduleRecords(func(string) bool { return false })
	job := newDurableJob(old.Name, true, false)
	job.setRunning(func(string) (bool, error) { return !gone.Load(), nil })
	require.NoError(t, h.s.AddJob(job))
	gone.Store(true)
	require.Eventually(t, func() bool { return !h.s.durableBusy(job.Name) }, time.Second, time.Millisecond)
	rec := h.record(job.Name)
	assert.True(t, rec.WorkloadEnded)
	h.fire(job.Name, at(24, 2))
	nextRun(t, job)
	h.eventually(job.Name, ended, "the new durable run records its result")
}

// Recovery guards must not create schedule records or persistence requirements
// for fresh legacy jobs, whether or not the scheduler has a store attached.
func TestScheduleState_FreshNonrecordJobsKeepTheirAdmissionPolicy(t *testing.T) {
	t.Parallel()
	for _, withStore := range []bool{false, true} {
		t.Run(map[bool]string{false: "no store", true: "existing store"}[withStore], func(t *testing.T) {
			t.Parallel()
			h := newDurableHarness(t, at(23, 10))
			if !withStore {
				h.s.SetScheduleStore(nil)
			}
			job := &nonrecordWorkloadJob{durableJob: newDurableJob("shop.backup", false, false)}
			require.NoError(t, h.s.AddJob(job))
			h.start()
			require.NoError(t, h.s.RunJob(context.Background(), job.Name))
			nextRun(t, job.durableJob)
			assert.Empty(t, h.store.ScheduleNames())
		})
	}
}

// A modern confirmed end must not be reconsidered as an unresolved legacy
// failure merely because its recorded execution result was unsuccessful.
func TestScheduleState_ConfirmedWorkloadEndDoesNotResurrectGuard(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	job := &nonrecordWorkloadJob{durableJob: newDurableJob("shop.backup", false, false)}
	rec := interruptedRecord("old revision")
	rec.Dispatch.State, rec.Dispatch.Outcome = dispatchEnded, outcomeUnknown
	rec.WorkloadEnded = true
	h.put(job.Name, rec)
	job.setRunning(func(string) (bool, error) {
		t.Error("a confirmed workload end must not be rechecked as an unsuccessful legacy dispatch")
		return true, nil
	})
	require.NoError(t, h.s.AddJob(job))
	h.start()
	require.NoError(t, h.s.RunJob(context.Background(), job.Name))
	nextRun(t, job.durableJob)
	h.s.PruneScheduleRecords(func(string) bool { return false })
	assert.Empty(t, h.store.ScheduleNames())
}

// An adapter without observation support cannot prove its predecessor ended;
// a later compatible adapter can reconcile and release the retained guard.
func TestScheduleState_RetainedWorkloadNeedsACompatibleObserver(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	job := newCountingJob("shop.backup")
	rec := interruptedRecord("old revision")
	rec.Dispatch.State, rec.Dispatch.Outcome = dispatchEnded, outcomeUnknown
	rec.Workload = rec.Dispatch.Execution
	h.put(job.Name, rec)
	require.NoError(t, h.s.AddJob(job))
	h.start()
	require.ErrorIs(t, h.s.RunJob(context.Background(), job.Name), ErrJobBusy,
		"the new adapter cannot confirm the old workload has ended")
	assert.Equal(t, rec.Workload, h.record(job.Name).Workload)
	require.NoError(t, h.s.RemoveJob(job))
	compatible := &nonrecordWorkloadJob{durableJob: newDurableJob(job.Name, false, false)}
	compatible.setRunning(func(string) (bool, error) { return false, nil })
	require.NoError(t, h.s.AddJob(compatible))
	require.NoError(t, h.s.RunJob(context.Background(), compatible.Name))
	nextRun(t, compatible.durableJob)
	assert.Empty(t, h.store.ScheduleNames())
}

// Restoring a retired durable policy must activate it at the new registration
// time rather than catch up deliberately skipped or absent schedule intervals.
func TestScheduleState_RestoredDurablePolicyStartsFresh(t *testing.T) {
	t.Parallel()
	for _, removed := range []bool{false, true} {
		name := "skip interval"
		if removed {
			name = "absent interval"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newDurableHarness(t, at(23, 10))
			old := newDurableJob("shop.backup", true, true)
			rec := interruptedRecord(old.policy.Revision)
			rec.Workload = rec.Dispatch.Execution
			h.put(old.Name, rec)
			if removed {
				h.s.PruneScheduleRecords(func(string) bool { return false })
			} else {
				skip := &nonrecordWorkloadJob{durableJob: newDurableJob(old.Name, false, false)}
				skip.setRunning(func(string) (bool, error) { return true, nil })
				require.NoError(t, h.s.AddJob(skip))
				h.start()
			}
			require.NoError(t, h.s.StopWithTimeout(time.Second))
			store := persist.NewStore(h.store.Path())
			require.NoError(t, store.Load())
			again := newDurableHarnessWithStore(t, at(24, 10), store)
			job := newDurableJob(old.Name, true, true)
			job.setRunning(func(string) (bool, error) { return true, nil })
			require.NoError(t, again.s.AddJob(job))
			again.start()
			r := again.record(job.Name)
			assert.Nil(t, r.Pending, "fires of an inactive schedule must not be caught up")
			assert.True(t, r.Cursor.Equal(at(24, 10)))
			assert.True(t, r.Activated.Equal(at(24, 10)))
			assert.NotEmpty(t, r.Workload, "the old workload still guards the freshly activated schedule")
		})
	}
}
