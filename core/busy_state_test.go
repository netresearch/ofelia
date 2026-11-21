// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"testing"

	cron "github.com/netresearch/go-cron"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/persist"
)

// Each admission check counts a different part of the busy state; the table
// pins which part each method counts.
func TestBusyState_EachMeaningCountsItsParts(t *testing.T) {
	t.Parallel()
	waiting := &persist.Dispatch{State: dispatchWaiting}
	started := &persist.Dispatch{State: dispatchStarted}
	cases := []struct {
		name                    string
		rec                     persist.Schedule
		e                       *durableEntry
		running, busy, retained bool
	}{
		{"idle", persist.Schedule{}, &durableEntry{}, false, false, false},
		{"no entry", persist.Schedule{}, nil, false, false, false},
		{"in flight", persist.Schedule{}, &durableEntry{inFlight: true}, true, true, false},
		{"orphan", persist.Schedule{}, &durableEntry{orphan: "x"}, true, true, true},
		{"workload", persist.Schedule{Workload: "x"}, nil, true, true, true},
		{"waiting dispatch", persist.Schedule{Dispatch: waiting}, &durableEntry{}, false, true, false},
		{"started dispatch only", persist.Schedule{Dispatch: started}, &durableEntry{}, false, false, false},
	}
	for _, c := range cases {
		b := busyStateLocked(c.rec, c.e)
		assert.Equal(t, c.running, b.running(), "%s: running", c.name)
		assert.Equal(t, c.busy, b.busy(), "%s: busy", c.name)
		assert.Equal(t, c.retained, b.retained(), "%s: retained", c.name)
	}
}

// A waiting dispatch that no run of this process owns yet, such as one
// restored from the record, keeps the entry busy for a later fire: the fire
// is queued behind it and does not replace it. A dispatch this process waits
// on is already covered by inFlight.
func TestBusyState_UnownedWaitingDispatchQueuesALaterFire(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	rec := persist.Schedule{Cursor: at(23, 2), Dispatch: &persist.Dispatch{
		Occurrence: persist.Occurrence{Scheduled: at(23, 2), Trigger: triggerScheduled},
		State:      dispatchWaiting,
	}}
	h.s.durableMu.Lock()
	_, admitted := h.s.arrivalLocked("shop.backup", &rec, &durableEntry{}, SchedulePolicy{Queue: true},
		cron.RunInfo{ScheduledTime: at(23, 3), Trigger: cron.TriggerScheduled}, at(23, 3))
	h.s.durableMu.Unlock()
	assert.False(t, admitted, "the later fire is not admitted")
	require.NotNil(t, rec.Pending, "the later fire is queued")
	assert.True(t, rec.Pending.Scheduled.Equal(at(23, 3)))
	assert.True(t, rec.Dispatch.Scheduled.Equal(at(23, 2)), "the waiting dispatch is kept")
}

// A run started by a dependency, or the run-on-startup run, is refused while
// a dispatch waits for a slot. Checked at the arrival decision itself: the
// refusal leaves no trace a test could wait for.
func TestBusyState_WaitingDispatchRefusesANonScheduledRun(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	for _, trigger := range []cron.TriggerKind{cron.TriggerDependency, cron.TriggerImmediate, cron.TriggerManual} {
		rec := persist.Schedule{Cursor: at(23, 2), Dispatch: &persist.Dispatch{
			Occurrence: persist.Occurrence{Scheduled: at(23, 2), Trigger: triggerScheduled},
			State:      dispatchWaiting,
		}}
		h.s.durableMu.Lock()
		_, admitted := h.s.arrivalLocked("shop.backup", &rec, &durableEntry{}, SchedulePolicy{},
			cron.RunInfo{ScheduledTime: at(23, 3), Trigger: trigger}, at(23, 3))
		h.s.durableMu.Unlock()
		assert.False(t, admitted, "trigger %v is refused while a dispatch waits", trigger)
	}
}
