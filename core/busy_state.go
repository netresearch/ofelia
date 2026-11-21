// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import "github.com/netresearch/ofelia/core/persist"

// busyState is what can keep a job's schedule entry from starting a run. The
// admission checks differ in which parts they count, so each one names its
// part through a method instead of reading the fields itself.
type busyState struct {
	// inFlight: a run of the entry was admitted in this process and has not
	// ended.
	inFlight bool
	// orphan: a workload inherited from an earlier process is still being
	// observed.
	orphan bool
	// workload: the record holds a workload that was started.
	workload bool
	// waiting: the record holds a dispatch that waits for a slot.
	waiting bool
}

// busyStateLocked reads the busy state of an entry from its record and its
// in-memory entry; e may be nil for a name without an entry. Caller holds
// durableMu.
func busyStateLocked(rec persist.Schedule, e *durableEntry) busyState {
	b := busyState{
		workload: rec.Workload != "",
		waiting:  rec.Dispatch != nil && rec.Dispatch.State == dispatchWaiting,
	}
	if e != nil {
		b.inFlight = e.inFlight
		b.orphan = e.orphan != ""
	}
	return b
}

// running reports a run in progress, in this process or as an orphan. A
// waiting dispatch is not counted: registration and resume start it.
func (b busyState) running() bool { return b.inFlight || b.orphan || b.workload }

// busy reports a run in progress or waiting for a slot. A fire and a manual
// run are not admitted while the entry is busy.
func (b busyState) busy() bool { return b.running() || b.waiting }

// retained reports a workload left by an earlier registration, the part of
// the state a replacement without a schedule record still honors.
func (b busyState) retained() bool { return b.orphan || b.workload }
