// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/netresearch/go-cron"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/test"
)

const durableSchedule = "0 2 * * *" // daily at 02:00 in the local zone

// observedRun is what a durableJob saw of one of its runs.
type observedRun struct {
	info      cron.RunInfo
	execution string
}

// durableJob is a job with durable schedule state that records its runs and,
// while block is open, runs until block is closed or its context ends.
type durableJob struct {
	BareJob
	policy SchedulePolicy
	runs   chan observedRun

	mu      sync.Mutex
	block   chan struct{}
	running func(executionID string) (bool, error)
}

func newDurableJob(name string, catchUp, queue bool) *durableJob {
	return &durableJob{
		BareJob: BareJob{Name: name, Schedule: durableSchedule, Command: "true"},
		policy:  SchedulePolicy{CatchUp: catchUp, Queue: queue, Revision: ScheduleRevision(durableSchedule, catchUp, queue)},
		runs:    make(chan observedRun, 16),
	}
}

func (j *durableJob) SchedulePolicy() (SchedulePolicy, bool) { return j.policy, true }

func (j *durableJob) WorkloadRunning(_ context.Context, executionID string) (bool, error) {
	j.mu.Lock()
	running := j.running
	j.mu.Unlock()
	if running == nil {
		return false, nil
	}
	return running(executionID)
}

func (j *durableJob) setRunning(f func(string) (bool, error)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.running = f
}

// hold makes the next runs block until the returned function is called.
func (j *durableJob) hold() (release func()) {
	ch := make(chan struct{})
	j.mu.Lock()
	j.block = ch
	j.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

func (j *durableJob) Run(ctx *Context) error {
	info, _ := cron.RunInfoFromContext(ctx.RunContext())
	j.runs <- observedRun{info: info, execution: ctx.Execution.ID}
	j.mu.Lock()
	block := j.block
	j.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.RunContext().Done():
		}
	}
	return nil
}

// durableHarness is a scheduler on a fake clock with a state file.
type durableHarness struct {
	t     *testing.T
	clock *CronClock
	store *persist.Store
	s     *Scheduler
}

func newDurableHarness(t *testing.T, now time.Time) *durableHarness {
	t.Helper()
	store := persist.NewStore(filepath.Join(t.TempDir(), "state.json"))
	return newDurableHarnessWithStore(t, now, store)
}

func newDurableHarnessWithStore(t *testing.T, now time.Time, store *persist.Store) *durableHarness {
	t.Helper()
	clock := NewCronClock(now)
	s := NewSchedulerWithClock(test.NewTestLogger(), clock)
	s.SetScheduleStore(store)
	t.Cleanup(func() { _ = s.StopWithTimeout(5 * time.Second) })
	return &durableHarness{t: t, clock: clock, store: store, s: s}
}

func (h *durableHarness) start() {
	h.t.Helper()
	require.NoError(h.t, h.s.Start())
}

func (h *durableHarness) record(name string) persist.Schedule {
	h.t.Helper()
	r, ok := h.store.GetSchedule(name)
	require.True(h.t, ok, "no schedule record for %s", name)
	return r
}

func (h *durableHarness) put(name string, r persist.Schedule) {
	h.t.Helper()
	require.NoError(h.t, h.store.PutSchedule(name, r))
}

// fire delivers a scheduled fire for t, as go-cron does when the schedule
// reaches t.
func (h *durableHarness) fire(name string, t time.Time) {
	h.t.Helper()
	require.NoError(h.t, h.s.cron.TriggerEntryWithInfoByName(name, cron.RunInfo{ScheduledTime: t, Trigger: cron.TriggerScheduled}))
}

// eventually waits until the record satisfies cond.
func (h *durableHarness) eventually(name string, cond func(persist.Schedule) bool, msg string) {
	h.t.Helper()
	require.Eventually(h.t, func() bool {
		r, ok := h.store.GetSchedule(name)
		return ok && cond(r)
	}, 5*time.Second, 5*time.Millisecond, msg)
}

func nextRun(t *testing.T, j *durableJob) observedRun {
	t.Helper()
	select {
	case r := <-j.runs:
		return r
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not run", j.GetName())
		return observedRun{}
	}
}

func noRun(t *testing.T, j *durableJob) {
	t.Helper()
	select {
	case r := <-j.runs:
		t.Fatalf("%s ran unexpectedly: %+v", j.GetName(), r)
	case <-time.After(100 * time.Millisecond):
	}
}

func ended(r persist.Schedule) bool { return r.Dispatch != nil && r.Dispatch.State == dispatchEnded }

// at returns the local time on day d of October 2026, the zone the daily
// schedule is evaluated in.
func at(day, hour int) time.Time {
	return time.Date(2026, 10, day, hour, 0, 0, 0, time.Local)
}

// A first registration writes the record with activation = cursor = now and
// starts nothing: fires before activation are never missed.
func TestScheduleState_FirstRegistration(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(20, 10))
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()

	r := h.record("shop.backup")
	assert.Equal(t, j.policy.Revision, r.Revision)
	assert.True(t, r.Activated.Equal(at(20, 10)), "activated %v", r.Activated)
	assert.True(t, r.Cursor.Equal(at(20, 10)), "cursor %v", r.Cursor)
	noRun(t, j)
}

// After a downtime one catch-up runs, for the most recent missed fire, and a
// second start does not run it again.
func TestScheduleState_DowntimeRunsOneCatchUp(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	h.put("shop.backup", persist.Schedule{Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(20, 2)})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	run := nextRun(t, j)
	assert.Equal(t, cron.TriggerCatchUp, run.info.Trigger)
	assert.True(t, run.info.ScheduledTime.Equal(at(23, 2)), "catch-up for %v, want the most recent missed fire", run.info.ScheduledTime)
	noRun(t, j)
	h.eventually("shop.backup", ended, "the catch-up's end is recorded")
	r := h.record("shop.backup")
	assert.True(t, r.Cursor.Equal(at(23, 2)), "cursor %v", r.Cursor)
	assert.Equal(t, outcomeSuccess, r.Dispatch.Outcome)
	assert.Equal(t, triggerCatchUp, r.Dispatch.Trigger)
	assert.Equal(t, run.execution, r.Dispatch.Execution)
	require.NoError(t, h.s.StopWithTimeout(5*time.Second))

	again := newDurableHarnessWithStore(t, at(23, 11), h.store)
	j2 := newDurableJob("shop.backup", true, false)
	require.NoError(t, again.s.AddJob(j2))
	again.start()
	noRun(t, j2)
}

func TestScheduleState_NothingMissed(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	h.put("shop.backup", persist.Schedule{Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(23, 2)})
	require.NoError(t, h.s.AddJob(j))
	h.start()
	noRun(t, j)
}

// missed_fires: skip with concurrency: queue keeps a record but runs no
// catch-up.
func TestScheduleState_QueueWithoutCatchUp(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", false, true)
	h.put("shop.backup", persist.Schedule{Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(20, 2)})
	require.NoError(t, h.s.AddJob(j))
	h.start()
	noRun(t, j)
}

// A changed schedule starts the record afresh: a time the old expression
// produced is no missed fire of the new one, and its queued and waiting
// occurrences are dropped.
func TestScheduleState_ScheduleChangeStartsAfresh(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, true)
	h.put("shop.backup", persist.Schedule{
		Revision: "old-revision", Activated: at(19, 10), Cursor: at(20, 2),
		Pending:  &persist.Occurrence{Scheduled: at(20, 2), Trigger: triggerScheduled},
		Dispatch: &persist.Dispatch{Occurrence: persist.Occurrence{Scheduled: at(19, 2), Trigger: triggerScheduled}, State: dispatchWaiting},
	})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	noRun(t, j)
	r := h.record("shop.backup")
	assert.Equal(t, j.policy.Revision, r.Revision)
	assert.True(t, r.Cursor.Equal(at(23, 10)))
	assert.Nil(t, r.Pending, "the queued occurrence of the old schedule is dropped")
	assert.Nil(t, r.Dispatch, "the waiting dispatch of the old schedule is dropped")
}

// A disabled job computes no catch-up; enabling moves the cursor to the
// enable time, so fires while it was disabled never count as missed.
func TestScheduleState_DisabledAndEnabled(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	h.put("shop.backup", persist.Schedule{Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(20, 2)})
	require.NoError(t, h.s.AddJobDisabled(j))
	h.start()
	noRun(t, j)
	assert.Nil(t, h.record("shop.backup").Dispatch, "no catch-up is recorded for a disabled job")

	h.clock.Advance(time.Hour)
	require.NoError(t, h.s.EnableJob("shop.backup"))
	r := h.record("shop.backup")
	assert.True(t, r.Cursor.Equal(at(23, 11)), "cursor %v, want the enable time", r.Cursor)
	noRun(t, j)
}

// A run that was in progress when the daemon stopped counts as handled: its
// outcome becomes unknown and it is not started again. A missed fire after it
// is still caught up.
func TestScheduleState_CrashDuringRunIsNotRepeated(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	h.put("shop.backup", persist.Schedule{
		Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(22, 2),
		Dispatch: &persist.Dispatch{
			Occurrence: persist.Occurrence{Scheduled: at(22, 2), Trigger: triggerScheduled},
			Execution:  "interrupted", State: dispatchStarted,
		},
	})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	run := nextRun(t, j)
	assert.True(t, run.info.ScheduledTime.Equal(at(23, 2)), "only the later missed fire runs, got %v", run.info.ScheduledTime)
	noRun(t, j)
}

func TestScheduleState_CrashDuringRunWithNothingMissed(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	h.put("shop.backup", persist.Schedule{
		Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(23, 2),
		Dispatch: &persist.Dispatch{
			Occurrence: persist.Occurrence{Scheduled: at(23, 2), Trigger: triggerScheduled},
			Execution:  "interrupted", State: dispatchStarted,
		},
	})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	noRun(t, j)
	r := h.record("shop.backup")
	assert.Equal(t, dispatchEnded, r.Dispatch.State)
	assert.Equal(t, outcomeUnknown, r.Dispatch.Outcome)
}

// While the workload of an interrupted run still runs, the entry is busy:
// concurrency: queue holds the catch-up in the slot, and it starts once the
// workload is gone.
func TestScheduleState_OrphanKeepsEntryBusy(t *testing.T) {
	prev := orphanPollInterval
	orphanPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { orphanPollInterval = prev })

	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, true)
	var gone sync.WaitGroup
	gone.Add(1)
	stillRunning := make(chan struct{})
	j.setRunning(func(id string) (bool, error) {
		select {
		case <-stillRunning:
			return false, nil
		default:
			return id == "interrupted", nil
		}
	})
	h.put("shop.backup", persist.Schedule{
		Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(22, 2),
		Dispatch: &persist.Dispatch{
			Occurrence: persist.Occurrence{Scheduled: at(22, 2), Trigger: triggerScheduled},
			Execution:  "interrupted", State: dispatchStarted,
		},
	})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	noRun(t, j)
	r := h.record("shop.backup")
	require.NotNil(t, r.Pending, "the catch-up waits in the queue slot")
	assert.True(t, r.Pending.Scheduled.Equal(at(23, 2)))
	require.ErrorIs(t, h.s.RunJob(context.Background(), "shop.backup"), ErrJobBusy, "a manual run is refused while the orphan runs")

	close(stillRunning)
	run := nextRun(t, j)
	assert.Equal(t, cron.TriggerCatchUp, run.info.Trigger)
	assert.True(t, run.info.ScheduledTime.Equal(at(23, 2)))
}

// With concurrency: forbid the catch-up is skipped while the orphan runs.
func TestScheduleState_OrphanForbidSkipsCatchUp(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	j.setRunning(func(string) (bool, error) { return true, nil })
	h.put("shop.backup", persist.Schedule{
		Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(22, 2),
		Dispatch: &persist.Dispatch{
			Occurrence: persist.Occurrence{Scheduled: at(22, 2), Trigger: triggerScheduled},
			Execution:  "interrupted", State: dispatchStarted,
		},
	})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	noRun(t, j)
	r := h.record("shop.backup")
	assert.Nil(t, r.Pending)
	assert.True(t, r.Cursor.Equal(at(23, 2)), "the skipped catch-up still moves the cursor")
}

// A failing check counts as running: a second run beside one that may still
// run is what at-most-once prevents.
func TestScheduleState_OrphanCheckErrorCountsAsRunning(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	j.setRunning(func(string) (bool, error) { return false, errors.New("docker unreachable") })
	h.put("shop.backup", persist.Schedule{
		Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(22, 2),
		Dispatch: &persist.Dispatch{
			Occurrence: persist.Occurrence{Scheduled: at(22, 2), Trigger: triggerScheduled},
			Execution:  "interrupted", State: dispatchStarted,
		},
	})
	require.NoError(t, h.s.AddJob(j))
	h.start()
	noRun(t, j)
}

// A dispatch that was still waiting for a slot never started; it starts as
// the catch-up, with its own scheduled time, and no computed catch-up starts
// beside it.
func TestScheduleState_WaitingDispatchCountsAsCatchUp(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	h.put("shop.backup", persist.Schedule{
		Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(21, 2),
		Dispatch: &persist.Dispatch{Occurrence: persist.Occurrence{Scheduled: at(21, 2), Trigger: triggerScheduled}, State: dispatchWaiting},
	})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	run := nextRun(t, j)
	assert.Equal(t, cron.TriggerScheduled, run.info.Trigger)
	assert.True(t, run.info.ScheduledTime.Equal(at(21, 2)), "the waiting occurrence starts with its own time")
	noRun(t, j)
	h.eventually("shop.backup", ended, "its end is recorded")
	assert.True(t, h.record("shop.backup").Cursor.Equal(at(23, 2)), "the downtime's fires count as handled")
}

// A queued occurrence counts as the catch-up too.
func TestScheduleState_PendingCountsAsCatchUp(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, true)
	h.put("shop.backup", persist.Schedule{
		Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(21, 2),
		Pending: &persist.Occurrence{Scheduled: at(21, 2), Trigger: triggerScheduled},
	})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	run := nextRun(t, j)
	assert.True(t, run.info.ScheduledTime.Equal(at(21, 2)))
	noRun(t, j)
	h.eventually("shop.backup", ended, "its end is recorded")
	assert.Nil(t, h.record("shop.backup").Pending)
}

// A manual dispatch that was waiting is dropped, never started.
func TestScheduleState_WaitingManualIsDropped(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", false, true)
	h.put("shop.backup", persist.Schedule{
		Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(23, 2),
		Dispatch: &persist.Dispatch{Occurrence: persist.Occurrence{Scheduled: at(23, 3), Trigger: triggerManual}, State: dispatchWaiting},
	})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	noRun(t, j)
	assert.Nil(t, h.record("shop.backup").Dispatch)
}

// concurrency: forbid skips a fire while a run is in progress; the cursor
// passes it, so a restart does not count it as missed.
func TestScheduleState_ForbidSkipsFireDuringRun(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	release := j.hold()

	h.fire("shop.backup", at(23, 2))
	first := nextRun(t, j)
	assert.Equal(t, cron.TriggerScheduled, first.info.Trigger)
	h.fire("shop.backup", at(23, 3))
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Cursor.Equal(at(23, 3)) }, "the skipped fire moves the cursor")
	noRun(t, j)

	release()
	h.eventually("shop.backup", ended, "the first run ends")
	noRun(t, j)
	assert.True(t, h.record("shop.backup").Dispatch.Scheduled.Equal(at(23, 2)))
}

// concurrency: queue keeps one fire, drops a second, and runs the kept one
// when the run in progress ends.
func TestScheduleState_QueueKeepsOneFire(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	j := newDurableJob("shop.backup", false, true)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	release := j.hold()

	h.fire("shop.backup", at(23, 2))
	nextRun(t, j)
	// Each fire is evaluated before the next is sent: go-cron starts every
	// invocation in its own goroutine, and real fires are minutes apart.
	h.fire("shop.backup", at(23, 3))
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Cursor.Equal(at(23, 3)) }, "the first fire is evaluated")
	h.fire("shop.backup", at(23, 4))
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Cursor.Equal(at(23, 4)) }, "both fires are evaluated")
	r := h.record("shop.backup")
	require.NotNil(t, r.Pending)
	assert.True(t, r.Pending.Scheduled.Equal(at(23, 3)), "the first fire is kept, the second is coalesced")

	release()
	queued := nextRun(t, j)
	assert.Equal(t, cron.TriggerScheduled, queued.info.Trigger)
	assert.True(t, queued.info.ScheduledTime.Equal(at(23, 3)))
	noRun(t, j)
	h.eventually("shop.backup", func(r persist.Schedule) bool {
		return ended(r) && r.Dispatch.Scheduled.Equal(at(23, 3)) && r.Pending == nil
	}, "the queued run ends and the slot is empty")
}

// A fire at or before the cursor has been evaluated already and is ignored.
func TestScheduleState_FireAtOrBeforeCursorIsIgnored(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	h.put("shop.backup", persist.Schedule{Revision: j.policy.Revision, Activated: at(19, 10), Cursor: at(23, 2)})
	require.NoError(t, h.s.AddJob(j))
	h.start()

	h.fire("shop.backup", at(23, 2))
	h.fire("shop.backup", at(22, 2))
	noRun(t, j)
}

// A manual run does not move the cursor, and is refused while a run is in
// progress.
func TestScheduleState_ManualRun(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	cursor := h.record("shop.backup").Cursor
	h.clock.Advance(time.Hour) // a manual run at a later time than the cursor
	release := j.hold()

	require.NoError(t, h.s.RunJob(context.Background(), "shop.backup"))
	run := nextRun(t, j)
	assert.Equal(t, cron.TriggerManual, run.info.Trigger)
	require.ErrorIs(t, h.s.RunJob(context.Background(), "shop.backup"), ErrJobBusy)

	release()
	h.eventually("shop.backup", ended, "the manual run ends")
	r := h.record("shop.backup")
	assert.Equal(t, triggerManual, r.Dispatch.Trigger)
	assert.True(t, r.Cursor.Equal(cursor), "a manual run must not move the cursor")
}

// The per-entry check refuses a manual run that reaches it while the job is
// busy, also when it bypasses RunJob's own check (another job's dependency
// chain, or a race with RunJob).
func TestScheduleState_ManualRunRefusedByTheCheck(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	release := j.hold()
	defer release()

	h.fire("shop.backup", at(23, 2))
	nextRun(t, j)
	require.NoError(t, h.s.cron.TriggerEntryByName("shop.backup"))
	noRun(t, j)
}

// blockerJob is a job without durable state that holds a global slot until
// released.
type blockerJob struct {
	BareJob
	started chan struct{}
	release chan struct{}
}

func (j *blockerJob) Run(ctx *Context) error {
	close(j.started)
	select {
	case <-j.release:
	case <-ctx.RunContext().Done():
	}
	return nil
}

func occupySlot(t *testing.T, s *Scheduler) (release func()) {
	t.Helper()
	b := &blockerJob{
		BareJob: BareJob{Name: "blocker", Schedule: TriggeredSchedule, Command: "true"},
		started: make(chan struct{}), release: make(chan struct{}),
	}
	require.NoError(t, s.AddJob(b))
	require.NoError(t, s.RunJob(context.Background(), "blocker"))
	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the blocker did not start")
	}
	var once sync.Once
	return func() { once.Do(func() { close(b.release) }) }
}

// The global limit delays a durable fire instead of dropping it; while it
// waits, the entry is busy.
func TestScheduleState_GlobalLimitDelays(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.SetMaxConcurrentJobs(1)
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	releaseSlot := occupySlot(t, h.s)

	h.fire("shop.backup", at(23, 2))
	h.eventually("shop.backup", func(r persist.Schedule) bool {
		return r.Dispatch != nil && r.Dispatch.State == dispatchWaiting
	}, "the fire waits for a slot")
	noRun(t, j)
	h.fire("shop.backup", at(23, 3))
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Cursor.Equal(at(23, 3)) }, "a second fire is evaluated")

	releaseSlot()
	run := nextRun(t, j)
	assert.True(t, run.info.ScheduledTime.Equal(at(23, 2)), "the waiting fire runs; the second was skipped")
	h.eventually("shop.backup", ended, "it ends")
	assert.Equal(t, run.execution, h.record("shop.backup").Dispatch.Execution, "the started write carries the execution ID")
	noRun(t, j)
}

// A dispatch waiting for a slot gives up when its entry is removed, keeps the
// state "waiting", and starts with its own time on the new entry.
func TestScheduleState_WaitingSurvivesReload(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.SetMaxConcurrentJobs(1)
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	releaseSlot := occupySlot(t, h.s)

	h.fire("shop.backup", at(23, 2))
	h.eventually("shop.backup", func(r persist.Schedule) bool {
		return r.Dispatch != nil && r.Dispatch.State == dispatchWaiting
	}, "the fire waits for a slot")
	require.NoError(t, h.s.RemoveJob(j))
	assert.Equal(t, dispatchWaiting, h.record("shop.backup").Dispatch.State)

	releaseSlot()
	replacement := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(replacement))
	run := nextRun(t, replacement)
	assert.True(t, run.info.ScheduledTime.Equal(at(23, 2)))
	noRun(t, j)
}

// A manual run waiting for a slot is dropped when its entry is removed.
func TestScheduleState_WaitingManualDroppedOnRemoval(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.SetMaxConcurrentJobs(1)
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	defer occupySlot(t, h.s)()

	require.NoError(t, h.s.RunJob(context.Background(), "shop.backup"))
	h.eventually("shop.backup", func(r persist.Schedule) bool {
		return r.Dispatch != nil && r.Dispatch.State == dispatchWaiting
	}, "the manual run waits for a slot")
	require.NoError(t, h.s.RemoveJob(j))
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Dispatch == nil }, "the manual run is dropped")
}

// Disabling drops a queued and a waiting occurrence.
func TestScheduleState_DisableDropsQueuedAndWaiting(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.SetMaxConcurrentJobs(1)
	j := newDurableJob("shop.backup", false, true)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	releaseSlot := occupySlot(t, h.s)

	h.fire("shop.backup", at(23, 2))
	h.eventually("shop.backup", func(r persist.Schedule) bool {
		return r.Dispatch != nil && r.Dispatch.State == dispatchWaiting
	}, "the fire waits for a slot")
	h.fire("shop.backup", at(23, 3))
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Pending != nil }, "the second fire is queued")

	require.NoError(t, h.s.DisableJob("shop.backup"))
	r := h.record("shop.backup")
	assert.Nil(t, r.Dispatch, "the waiting occurrence is dropped")
	assert.Nil(t, r.Pending, "the queued occurrence is dropped")
	releaseSlot()
	noRun(t, j)
}

// A reload replaces the job while it runs: the run is canceled, the queued
// occurrence stays in the record and starts on the new entry after the
// canceled run has ended.
func TestScheduleState_ReloadCarriesQueuedOccurrence(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	j := newDurableJob("shop.backup", false, true)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	j.hold()

	h.fire("shop.backup", at(23, 2))
	nextRun(t, j)
	h.fire("shop.backup", at(23, 3))
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Pending != nil }, "the second fire is queued")

	require.NoError(t, h.s.RemoveJob(j))
	h.eventually("shop.backup", func(r persist.Schedule) bool {
		return ended(r) && r.Dispatch.Outcome == outcomeCanceled
	}, "the canceled run ends with the outcome canceled")

	replacement := newDurableJob("shop.backup", false, true)
	require.NoError(t, h.s.AddJob(replacement))
	run := nextRun(t, replacement)
	assert.True(t, run.info.ScheduledTime.Equal(at(23, 3)))
}

// failingStore refuses every write after the first n.
type failingStore struct {
	*persist.Store
	mu     sync.Mutex
	allows int
}

func (f *failingStore) PutSchedule(name string, r persist.Schedule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.allows <= 0 {
		return errors.New("disk full")
	}
	f.allows--
	return f.Store.PutSchedule(name, r)
}

// A dispatch that cannot be recorded does not run: at most once.
func TestScheduleState_UnrecordedDispatchDoesNotRun(t *testing.T) {
	t.Parallel()
	store := &failingStore{Store: persist.NewStore(filepath.Join(t.TempDir(), "state.json")), allows: 1}
	clock := NewCronClock(at(23, 1))
	s := NewSchedulerWithClock(test.NewTestLogger(), clock)
	s.SetScheduleStore(store)
	t.Cleanup(func() { _ = s.StopWithTimeout(5 * time.Second) })
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, s.AddJob(j))
	require.NoError(t, s.Start()) // the first registration is the one allowed write

	require.NoError(t, s.cron.TriggerEntryWithInfoByName("shop.backup", cron.RunInfo{ScheduledTime: at(23, 2), Trigger: cron.TriggerScheduled}))
	noRun(t, j)
}

// A run that shutdown cancels ends with the outcome canceled.
func TestScheduleState_ShutdownCancels(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	j.hold()

	h.fire("shop.backup", at(23, 2))
	nextRun(t, j)
	require.NoError(t, h.s.StopWithTimeout(5*time.Second))
	r := h.record("shop.backup")
	assert.Equal(t, dispatchEnded, r.Dispatch.State)
	assert.Equal(t, outcomeCanceled, r.Dispatch.Outcome)
}

// Without a store no job keeps a record, and a durable job runs like any
// other: a fire during a run is not refused by the per-entry check.
func TestScheduleState_WithoutStore(t *testing.T) {
	t.Parallel()
	s := NewSchedulerWithClock(test.NewTestLogger(), NewCronClock(at(23, 1)))
	t.Cleanup(func() { _ = s.StopWithTimeout(5 * time.Second) })
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, s.AddJob(j))
	require.NoError(t, s.Start())
	_, durable := s.durablePolicy(j)
	assert.False(t, durable)

	require.NoError(t, s.RunJob(context.Background(), "shop.backup"))
	nextRun(t, j)
}

func TestScheduleState_Prune(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	for _, name := range []string{"shop.backup", "shop.gone", "other.kept"} {
		h.put(name, persist.Schedule{Revision: "r"})
	}
	h.s.PruneScheduleRecords(func(name string) bool { return name != "shop.gone" })
	assert.Equal(t, []string{"other.kept", "shop.backup"}, h.store.ScheduleNames())
}

func TestLatestMissed(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		cursor, now, want time.Time
	}{
		"none":            {at(23, 2), at(23, 10), time.Time{}},
		"one":             {at(22, 2), at(23, 10), at(23, 2)},
		"most recent":     {at(10, 2), at(23, 10), at(23, 2)},
		"exactly now":     {at(22, 2), at(23, 2), at(23, 2)},
		"just before now": {at(22, 2), at(23, 2).Add(-time.Second), time.Time{}},
	}
	for name, tc := range cases {
		got := latestMissed(durableSchedule, tc.cursor, tc.now, time.Local)
		assert.True(t, got.Equal(tc.want), "%s: got %v, want %v", name, got, tc.want)
	}
	// The record keeps the cursor in UTC; the schedule is still evaluated in
	// the scheduler's zone. An explicit zone keeps this independent of the
	// host's.
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	cursor := time.Date(2026, 10, 22, 2, 0, 0, 0, berlin).UTC()
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, berlin).UTC()
	got := latestMissed(durableSchedule, cursor, now, berlin)
	want := time.Date(2026, 10, 23, 2, 0, 0, 0, berlin)
	assert.True(t, got.Equal(want), "a UTC cursor: got %v, want %v", got, want)
	// Longer than go-cron's catch-up limit of 100 missed times.
	got = latestMissed("* * * * *", at(20, 0), at(23, 0), time.Local)
	assert.True(t, got.Equal(at(23, 0)), "after a long downtime the most recent fire is found, got %v", got)
	assert.True(t, latestMissed("not a schedule", at(20, 0), at(23, 0), time.Local).IsZero())
}

func TestScheduleRevision(t *testing.T) {
	t.Parallel()
	base := ScheduleRevision("0 2 * * *", true, false)
	assert.Len(t, base, 12)
	assert.Equal(t, base, ScheduleRevision("0 2 * * *", true, false))
	assert.NotEqual(t, base, ScheduleRevision("0 3 * * *", true, false), "the expression")
	assert.NotEqual(t, base, ScheduleRevision("0 2 * * *", false, false), "missed_fires")
	assert.NotEqual(t, base, ScheduleRevision("0 2 * * *", true, true), "concurrency")
	assert.NotEqual(t, base, ScheduleRevision("CRON_TZ=Europe/Berlin 0 2 * * *", true, false), "the zone")

	// A zone from /etc/localtime is named "Local" whatever it is; a changed
	// host zone must still change the revision.
	plus1 := scheduleRevision("0 2 * * *", true, false, time.FixedZone("Local", 3600))
	utc := scheduleRevision("0 2 * * *", true, false, time.FixedZone("Local", 0))
	assert.NotEqual(t, plus1, utc, "the local zone's offset")
	assert.Equal(t, plus1, scheduleRevision("0 2 * * *", true, false, time.FixedZone("Local", 3600)))
	explicit := "CRON_TZ=Europe/Berlin 0 2 * * *"
	assert.Equal(t, scheduleRevision(explicit, true, false, time.FixedZone("Local", 3600)),
		scheduleRevision(explicit, true, false, time.FixedZone("Local", 0)), "an explicit zone ignores the host's")
}

// panicJob is a durable job whose first run panics.
type panicJob struct {
	*durableJob
	panicked bool
}

func (j *panicJob) Run(ctx *Context) error {
	if !j.panicked {
		j.panicked = true
		panic("boom")
	}
	return j.durableJob.Run(ctx)
}

// A run that panics releases its slot, records the outcome failed and leaves
// the job idle, so the next fire runs.
func TestScheduleState_PanicReleasesTheEntry(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.SetMaxConcurrentJobs(1)
	j := &panicJob{durableJob: newDurableJob("shop.backup", true, false)}
	require.NoError(t, h.s.AddJob(j))
	h.start()

	h.fire("shop.backup", at(23, 2))
	h.eventually("shop.backup", func(r persist.Schedule) bool {
		return ended(r) && r.Dispatch.Outcome == outcomeFailed
	}, "the panicked run ends with the outcome failed")

	h.fire("shop.backup", at(23, 3))
	run := nextRun(t, j.durableJob)
	assert.True(t, run.info.ScheduledTime.Equal(at(23, 3)), "the next fire runs")
}

// interruptedRecord is a record whose last run started before a crash.
func interruptedRecord(revision string) persist.Schedule {
	return persist.Schedule{
		Revision: revision, Activated: at(19, 10), Cursor: at(23, 2),
		Dispatch: &persist.Dispatch{
			Occurrence: persist.Occurrence{Scheduled: at(23, 2), Trigger: triggerScheduled},
			Execution:  "interrupted", State: dispatchStarted,
		},
	}
}

// The crash rules apply to a disabled job too: once it is enabled, its
// container still keeps it busy.
func TestScheduleState_CrashRulesForDisabledJob(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	j.setRunning(func(id string) (bool, error) { return id == "interrupted", nil })
	h.put("shop.backup", interruptedRecord(j.policy.Revision))
	require.NoError(t, h.s.AddJobDisabled(j))
	h.start()

	r := h.record("shop.backup")
	assert.Equal(t, outcomeUnknown, r.Dispatch.Outcome)
	require.NoError(t, h.s.EnableJob("shop.backup"))
	require.ErrorIs(t, h.s.RunJob(context.Background(), "shop.backup"), ErrJobBusy)
}

// A changed schedule starts the record afresh but keeps the crash rules: the
// interrupted run's outcome is unknown and its container keeps the job busy.
func TestScheduleState_CrashRulesForChangedSchedule(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 10))
	j := newDurableJob("shop.backup", true, false)
	j.setRunning(func(id string) (bool, error) { return id == "interrupted", nil })
	h.put("shop.backup", interruptedRecord("old-revision"))
	require.NoError(t, h.s.AddJob(j))
	h.start()

	r := h.record("shop.backup")
	assert.Equal(t, j.policy.Revision, r.Revision)
	require.NotNil(t, r.Dispatch)
	assert.Equal(t, outcomeUnknown, r.Dispatch.Outcome)
	require.ErrorIs(t, h.s.RunJob(context.Background(), "shop.backup"), ErrJobBusy)
	noRun(t, j)
}
