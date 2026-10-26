// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"

	"github.com/netresearch/ofelia/core/persist"
)

// Workload tombstones carry no active schedule. Restoring a durable policy
// must activate it afresh instead of treating the tombstone interval as downtime.
const workloadOnlyRevision = "workload-only"

// unresolvedExecution prefers explicit workload state and falls back to
// unsuccessful legacy dispatches only when no confirmed-end marker exists.
func unresolvedExecution(rec persist.Schedule) string {
	if rec.Workload != "" {
		return rec.Workload
	}
	if rec.WorkloadEnded {
		return ""
	}
	if d := rec.Dispatch; d != nil && (d.State == dispatchStarted ||
		d.State == dispatchEnded && d.Outcome != outcomeSuccess) {
		return d.Execution
	}
	return ""
}

// prepareRetainedWorkload reconciles an old workload even when the new job
// no longer keeps schedule state. Such a job gets no new schedule record:
// only the existing workload guard survives, and old queued fires are dropped.
func (s *Scheduler) prepareRetainedWorkload(j Job) {
	name := j.GetName()
	candidate := s.unendedDispatch(name)
	orphan := s.checkOrphan(context.Background(), j, candidate)
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	if s.records == nil {
		return
	}
	rec, ok := s.records.GetSchedule(name)
	if !ok {
		return
	}
	e := s.entryLocked(name)
	current := unresolvedExecution(rec)
	if current == "" && e.orphan == "" {
		return
	}
	e.pruneWhenIdle = true
	s.logDropped(name, rec, "the job no longer keeps schedule state")
	rec.Revision = workloadOnlyRevision
	rec.Pending = nil
	if rec.Dispatch != nil && rec.Dispatch.State == dispatchWaiting {
		rec.Dispatch = nil
	}
	// A run may finish its cleanup between the observation above and this
	// lock. Keep its newer unresolved ID until its watcher can check it.
	if current != candidate {
		orphan = current
	}
	if orphan == "" && e.orphan != "" {
		orphan = e.orphan
	}
	s.settleInterruptedLocked(j, &rec, e, orphan)
	if s.putLocked(name, rec) {
		s.pruneIdleRecordLocked(name, e)
	}
}

// retainedWorkloadBusy applies the guard of a former durable registration to
// its non-durable replacement without changing that job's admission policy.
func (s *Scheduler) retainedWorkloadBusy(name string) bool {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	if s.records == nil {
		return false
	}
	rec, _ := s.records.GetSchedule(name)
	e := s.durable[name]
	return rec.Workload != "" || e != nil && e.orphan != ""
}

// pruneIdleRecordLocked removes a retained tombstone only after its workload
// is confirmed ended. Caller holds durableMu.
func (s *Scheduler) pruneIdleRecordLocked(name string, e *durableEntry) bool {
	if !e.pruneWhenIdle || e.inFlight || e.orphan != "" {
		return false
	}
	rec, _ := s.records.GetSchedule(name)
	if rec.Workload != "" {
		return false
	}
	if err := s.records.RemoveSchedules(name); err != nil {
		s.Logger.Error("could not remove the ended workload record", "job", name, "error", err)
		return true
	}
	delete(s.durable, name)
	return true
}

// stopPrunedWatcherLocked detaches observation only for a record safe to prune.
// In-flight execution bookkeeping remains until its owner finishes.
func (s *Scheduler) stopPrunedWatcherLocked(name string) {
	e, ok := s.durable[name]
	if !ok {
		return
	}
	if e.stopWatch != nil {
		e.stopWatch()
	}
	if !e.inFlight {
		delete(s.durable, name)
	}
}

// retainRemovedWorkloadLocked replaces obsolete scheduling obligations with
// a workload-only tombstone so removal cannot bypass the unresolved guard.
func (s *Scheduler) retainRemovedWorkloadLocked(name string) bool {
	rec, _ := s.records.GetSchedule(name)
	e := s.durable[name]
	if unresolvedExecution(rec) == "" && (e == nil || e.orphan == "") {
		return false
	}
	e = s.entryLocked(name)
	e.pruneWhenIdle = true
	s.logDropped(name, rec, "the schedule was removed")
	rec.Revision = workloadOnlyRevision
	rec.Pending = nil
	if rec.Dispatch != nil && rec.Dispatch.State == dispatchWaiting {
		rec.Dispatch = nil
	}
	s.putLocked(name, rec)
	return true
}
