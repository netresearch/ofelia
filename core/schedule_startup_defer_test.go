// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/netresearch/go-cron"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/test"
)

// The run-on-startup run of a job with durable schedule state waits while a
// run that its registration did not start is in progress: a workload from
// before the restart, or the run of the registration it replaced. A run the
// scheduler starts itself at registration (a catch-up, a resumed or queued
// occurrence) takes the place of the run-on-startup run.

// fireStartup delivers the run-on-startup fire, as go-cron does for an entry
// added with WithRunImmediately.
func fireStartup(h *durableHarness, name string) {
	h.t.Helper()
	require.NoError(h.t, h.s.cron.TriggerEntryWithInfoByName(name,
		cron.RunInfo{ScheduledTime: h.s.now(), Trigger: cron.TriggerImmediate}))
}

// waitStartupDue waits until the run-on-startup fire has arrived and waits
// for the entry to become idle. go-cron delivers a fire asynchronously.
func waitStartupDue(h *durableHarness, name string) {
	h.t.Helper()
	require.Eventually(h.t, func() bool {
		h.s.durableMu.Lock()
		defer h.s.durableMu.Unlock()
		e, ok := h.s.durable[name]
		return ok && e.startupDue
	}, 5*time.Second, 5*time.Millisecond, "the run-on-startup run of %s waits", name)
}

// startWithOrphan registers j over a record whose run from before the restart
// still runs, and returns a function that ends that run.
func startWithOrphan(h *durableHarness, j *durableJob, cursor persist.Schedule) (end func()) {
	h.t.Helper()
	h.s.orphanPollInterval = 10 * time.Millisecond
	gone := make(chan struct{})
	j.setRunning(func(id string) (bool, error) {
		select {
		case <-gone:
			return false, nil
		default:
			return id == "interrupted", nil
		}
	})
	rec := cursor
	rec.Revision = j.policy.Revision
	rec.Dispatch = &persist.Dispatch{
		Occurrence: persist.Occurrence{Scheduled: rec.Cursor, Trigger: triggerScheduled},
		Execution:  "interrupted", State: dispatchStarted,
	}
	h.put(j.GetName(), rec)
	require.NoError(h.t, h.s.AddJob(j))
	h.start()
	var once sync.Once
	return func() { once.Do(func() { close(gone) }) }
}

func TestStartupDefer_WaitsForTheRunFromBeforeTheRestart(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", false, false)
	end := startWithOrphan(h, j, persist.Schedule{Activated: at(19, 10), Cursor: at(23, 2)})

	fireStartup(h, "shop.backup")
	waitStartupDue(h, "shop.backup")
	noRun(t, j)

	end()
	run := nextRun(t, j)
	assert.Equal(t, cron.TriggerImmediate, run.info.Trigger)
	noRun(t, j)
}

func TestStartupDefer_QueuedOccurrenceTakesItsPlace(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, true)
	end := startWithOrphan(h, j, persist.Schedule{Activated: at(19, 10), Cursor: at(22, 2)})
	require.NotNil(t, h.record("shop.backup").Pending, "the catch-up waits in the queue slot")

	fireStartup(h, "shop.backup")
	waitStartupDue(h, "shop.backup")
	noRun(t, j)

	end()
	run := nextRun(t, j)
	assert.Equal(t, cron.TriggerCatchUp, run.info.Trigger)
	h.eventually("shop.backup", ended, "the catch-up ends")
	noRun(t, j)
}

func TestStartupDefer_CatchUpAtRegistrationTakesItsPlace(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	logger, logs := test.NewTestLoggerWithHandler()
	h.s.Logger = logger
	j := newDurableJob("shop.backup", true, false)
	release := j.hold()
	h.put("shop.backup", persist.Schedule{Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(20, 2)})
	require.NoError(t, h.s.AddJob(j))
	h.start()
	run := nextRun(t, j)
	require.Equal(t, cron.TriggerCatchUp, run.info.Trigger)

	fireStartup(h, "shop.backup")
	require.Eventually(t, func() bool {
		h.s.durableMu.Lock()
		defer h.s.durableMu.Unlock()
		return logs.HasMessage("takes the place of the run-on-startup run") || h.s.durable["shop.backup"].startupDue
	}, 5*time.Second, 5*time.Millisecond, "the run-on-startup fire arrives while the catch-up runs")
	release()
	h.eventually("shop.backup", ended, "the catch-up ends")
	noRun(t, j)
}

func TestStartupDefer_WaitsForTheReplacedRegistrationsRun(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	old := &stubbornDurableJob{durableJob: newDurableJob("shop.backup", false, false), release: make(chan struct{})}
	require.NoError(t, h.s.AddJob(old))
	h.start()
	h.fire("shop.backup", at(23, 2))
	nextRun(t, old.durableJob)
	require.NoError(t, h.s.RemoveJob(old))

	replacement := newDurableJob("shop.backup", false, false)
	require.NoError(t, h.s.AddJob(replacement))
	fireStartup(h, "shop.backup")
	waitStartupDue(h, "shop.backup")
	noRun(t, replacement)

	close(old.release)
	run := nextRun(t, replacement)
	assert.Equal(t, cron.TriggerImmediate, run.info.Trigger)
	noRun(t, replacement)
}

func TestStartupDefer_DisablingDropsIt(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", false, false)
	end := startWithOrphan(h, j, persist.Schedule{Activated: at(19, 10), Cursor: at(23, 2)})

	fireStartup(h, "shop.backup")
	waitStartupDue(h, "shop.backup")
	require.NoError(t, h.s.DisableJob("shop.backup"))
	end()
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Workload == "" }, "the old run's end is recorded")
	require.NoError(t, h.s.EnableJob("shop.backup"))
	noRun(t, j)

	// The next idle entry, after a scheduled run, starts no run-on-startup
	// run either.
	h.fire("shop.backup", at(24, 2))
	run := nextRun(t, j)
	assert.Equal(t, cron.TriggerScheduled, run.info.Trigger)
	h.eventually("shop.backup", ended, "the scheduled run ends")
	noRun(t, j)
}

func TestStartupDefer_ReregistrationDropsIt(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", false, false)
	end := startWithOrphan(h, j, persist.Schedule{Activated: at(19, 10), Cursor: at(23, 2)})

	fireStartup(h, "shop.backup")
	waitStartupDue(h, "shop.backup")
	require.NoError(t, h.s.RemoveJob(j))
	again := newDurableJob("shop.backup", false, false)
	again.setRunning(func(id string) (bool, error) { return j.WorkloadRunning(context.Background(), id) })
	require.NoError(t, h.s.AddJob(again))
	end()
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Workload == "" }, "the old run's end is recorded")
	noRun(t, again)
}

// Only the run-on-startup run waits; a manual or dependency run is refused,
// also when the registration found a run it did not start.
func TestStartupDefer_OnlyTheStartupRunWaits(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	rec := persist.Schedule{Workload: "interrupted"}
	for _, trigger := range []cron.TriggerKind{cron.TriggerManual, cron.TriggerDependency} {
		e := &durableEntry{startupDefers: true}
		_, admitted := h.s.arrivalLocked("shop.backup", &rec, e, SchedulePolicy{},
			cron.RunInfo{ScheduledTime: at(23, 10), Trigger: trigger}, at(23, 10))
		assert.False(t, admitted, "trigger %v", trigger)
		assert.False(t, e.startupDue, "trigger %v must not wait like a run-on-startup run", trigger)
	}
	e := &durableEntry{startupDefers: true}
	_, admitted := h.s.arrivalLocked("shop.backup", &rec, e, SchedulePolicy{},
		cron.RunInfo{ScheduledTime: at(23, 10), Trigger: cron.TriggerImmediate}, at(23, 10))
	assert.False(t, admitted)
	assert.True(t, e.startupDue, "the run-on-startup run waits")
}
