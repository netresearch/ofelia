// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netresearch/ofelia/test"
)

const drainSkipWarning = "still stopping"

// stoppingJob runs until its context ends and then until release is closed,
// the way a job removes its container after a cancellation. With
// ignoreCancel it does not look at the cancellation at all.
type stoppingJob struct {
	BareJob
	ignoreCancel bool
	runs         atomic.Int32
	started      chan struct{}
	canceled     chan struct{}
	release      chan struct{}
	ended        chan struct{}
}

func newStoppingJob(name string) *stoppingJob {
	return &stoppingJob{
		BareJob:  BareJob{Name: name, Schedule: TriggeredSchedule, Command: "true"},
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		release:  make(chan struct{}),
		ended:    make(chan struct{}),
	}
}

func (j *stoppingJob) Run(ctx *Context) error {
	if j.runs.Add(1) > 1 {
		return nil
	}
	defer close(j.ended)
	close(j.started)
	if !j.ignoreCancel {
		<-ctx.RunContext().Done()
		close(j.canceled)
	}
	<-j.release
	return nil
}

// countingJob counts its runs and reports each on ran.
type countingJob struct {
	BareJob
	runs atomic.Int32
	ran  chan struct{}
}

func newCountingJob(name string) *countingJob {
	return &countingJob{
		BareJob: BareJob{Name: name, Schedule: TriggeredSchedule, Command: "true"},
		ran:     make(chan struct{}, 8),
	}
}

func (j *countingJob) Run(*Context) error {
	j.runs.Add(1)
	j.ran <- struct{}{}
	return nil
}

func startedScheduler(t *testing.T) (*Scheduler, *test.Handler) {
	t.Helper()
	logger, handler := test.NewTestLoggerWithHandler()
	s := NewScheduler(logger)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s, handler
}

func startStoppingJob(t *testing.T, s *Scheduler, j *stoppingJob) {
	t.Helper()
	if err := s.AddJob(j); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if err := s.RunJob(context.Background(), j.GetName()); err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	waitFor(t, j.started, "the job never started")
}

func waitFor(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(msg)
	}
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// RemoveJob returns while the canceled run is still stopping, and the removed
// job reports that run until it has ended.
func TestRemoveJob_DoesNotWaitForTheCanceledRun(t *testing.T) {
	s, _ := startedScheduler(t)
	j := newStoppingJob("stopping")
	startStoppingJob(t, s, j)

	if err := s.RemoveJob(j); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}
	select {
	case <-j.ended:
		t.Fatal("the run ended before release; the test proves nothing")
	default:
	}
	if s.GetAnyJob("stopping") != nil {
		t.Fatal("the job is still registered")
	}
	if removed := s.GetRemovedJobs(); len(removed) != 1 || removed[0] != j {
		t.Fatalf("Removed = %v, want the removed job", removed)
	}
	waitFor(t, j.canceled, "the removal did not cancel the run")
	if !s.IsRemovedJobRunning(j) {
		t.Fatal("IsRemovedJobRunning = false while the canceled run is stopping")
	}

	close(j.release)
	waitFor(t, j.ended, "the run did not end")
	eventually(t, func() bool { return !s.IsRemovedJobRunning(j) },
		"IsRemovedJobRunning stayed true after the run ended")
}

// A job added under the name of a removed one skips its fires until the
// removed job's canceled run has stopped, and runs again afterwards.
func TestRemoveJob_ReplacementSkipsWhileTheOldRunStops(t *testing.T) {
	s, logs := startedScheduler(t)
	old := newStoppingJob("dup")
	old.ignoreCancel = true
	startStoppingJob(t, s, old)

	if err := s.RemoveJob(old); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}
	repl := newCountingJob("dup")
	if err := s.AddJob(repl); err != nil {
		t.Fatalf("AddJob replacement: %v", err)
	}
	if err := s.RunJob(context.Background(), "dup"); !errors.Is(err, ErrJobBusy) {
		t.Fatalf("RunJob replacement = %v, want ErrJobBusy while the old run stops", err)
	}
	if err := s.cron.TriggerEntryByName("dup"); err != nil {
		t.Fatalf("TriggerEntryByName: %v", err)
	}
	eventually(t, func() bool { return logs.HasWarning(drainSkipWarning) },
		"the replacement's fire was not skipped while the old run was stopping")
	if n := repl.runs.Load(); n != 0 {
		t.Fatalf("the replacement ran %d times beside the old run", n)
	}

	close(old.release)
	waitFor(t, old.ended, "the old run did not end")
	eventually(t, func() bool { return !s.IsRemovedJobRunning(old) }, "the old run is still counted")
	if err := s.RunJob(context.Background(), "dup"); err != nil {
		t.Fatalf("RunJob after the drain: %v", err)
	}
	waitFor(t, repl.ran, "the replacement did not run after the old run had ended")
}

// The run-on-startup run of a job added while a removed job's canceled run
// stops is deferred, not lost: a @triggered job gets no other fire that
// would make up for it. It runs once the old run has ended, exactly once.
func TestRemoveJob_ReplacementRunOnStartupRunsAfterTheOldRunStops(t *testing.T) {
	s, logs := startedScheduler(t)
	old := newStoppingJob("boot")
	old.ignoreCancel = true
	startStoppingJob(t, s, old)

	if err := s.RemoveJob(old); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}
	repl := newCountingJob("boot")
	repl.RunOnStartup = true
	if err := s.AddJob(repl); err != nil {
		t.Fatalf("AddJob replacement: %v", err)
	}
	eventually(t, func() bool { return logs.HasWarning("defer: the run-on-startup run") },
		"the replacement's run-on-startup fire was not deferred")
	if n := repl.runs.Load(); n != 0 {
		t.Fatalf("the replacement ran %d times beside the old run", n)
	}

	close(old.release)
	waitFor(t, repl.ran, "the deferred run-on-startup run did not start after the old run had ended")
	time.Sleep(50 * time.Millisecond)
	if n := repl.runs.Load(); n != 1 {
		t.Fatalf("the replacement ran %d times, want once", n)
	}
}

// The deferred run-on-startup run survives a web API update of the job
// while the old run stops: go-cron fires no run-on-startup run for a job
// swapped in by UpdateJob, so the updated job takes the deferred one over.
func TestUpdateJob_TakesOverTheDeferredRunOnStartup(t *testing.T) {
	s, logs := startedScheduler(t)
	old := newStoppingJob("boot")
	old.ignoreCancel = true
	startStoppingJob(t, s, old)

	if err := s.RemoveJob(old); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}
	repl := newCountingJob("boot")
	repl.RunOnStartup = true
	if err := s.AddJob(repl); err != nil {
		t.Fatalf("AddJob replacement: %v", err)
	}
	eventually(t, func() bool { return logs.HasWarning("defer: the run-on-startup run") },
		"the replacement's run-on-startup fire was not deferred")
	upd := newCountingJob("boot")
	upd.RunOnStartup = true
	if err := s.UpdateJob("boot", TriggeredSchedule, upd); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}

	close(old.release)
	waitFor(t, upd.ran, "the updated job did not run its deferred run-on-startup run")
	time.Sleep(50 * time.Millisecond)
	if n, m := repl.runs.Load(), upd.runs.Load(); n != 0 || m != 1 {
		t.Fatalf("runs: replacement %d, updated %d; want 0 and 1", n, m)
	}
}

// The end of a run waits for Scheduler.regMu before it leaves the name
// lock, so no registration change (AddJob, UpdateJob) falls between the
// moment leave picks the registration whose deferred run-on-startup run is
// due and the moment that run is fired.
func TestRunEnd_WaitsForRegistrationChanges(t *testing.T) {
	s, _ := startedScheduler(t)
	old := newStoppingJob("held")
	old.ignoreCancel = true
	startStoppingJob(t, s, old)
	if err := s.RemoveJob(old); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}

	s.regMu.Lock()
	close(old.release)
	waitFor(t, old.ended, "the old run did not end")
	time.Sleep(50 * time.Millisecond)
	stillCounted := s.IsRemovedJobRunning(old)
	s.regMu.Unlock()
	if !stillCounted {
		t.Fatal("the run left the name lock while regMu was held")
	}
	eventually(t, func() bool { return !s.IsRemovedJobRunning(old) }, "the run did not leave after regMu was released")
}

// RemoveJobsByTag retires the removed jobs like RemoveJob does.
func TestRemoveJobsByTag_ReplacementSkipsWhileTheOldRunStops(t *testing.T) {
	s, logs := startedScheduler(t)
	old := newStoppingJob("tagged")
	if err := s.AddJobWithTags(old, "group"); err != nil {
		t.Fatalf("AddJobWithTags: %v", err)
	}
	if err := s.RunJob(context.Background(), "tagged"); err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	waitFor(t, old.started, "the job never started")

	if n := s.RemoveJobsByTag("group"); n != 1 {
		t.Fatalf("RemoveJobsByTag removed %d jobs, want 1", n)
	}
	waitFor(t, old.canceled, "the removal did not cancel the run")
	if !s.IsRemovedJobRunning(old) {
		t.Fatal("IsRemovedJobRunning = false while the canceled run is stopping")
	}
	repl := newCountingJob("tagged")
	if err := s.AddJob(repl); err != nil {
		t.Fatalf("AddJob replacement: %v", err)
	}
	if err := s.RunJob(context.Background(), "tagged"); !errors.Is(err, ErrJobBusy) {
		t.Fatalf("RunJob replacement = %v, want ErrJobBusy while the old run stops", err)
	}
	if err := s.cron.TriggerEntryByName("tagged"); err != nil {
		t.Fatalf("TriggerEntryByName: %v", err)
	}
	eventually(t, func() bool { return logs.HasWarning(drainSkipWarning) },
		"the replacement's fire was not skipped while the old run was stopping")
	if n := repl.runs.Load(); n != 0 {
		t.Fatalf("the replacement ran %d times beside the old run", n)
	}
	close(old.release)
	waitFor(t, old.ended, "the old run did not end")
}

// UpdateJob cancels the old job's run like RemoveJob does, and the new job
// skips its fires until that run has stopped.
func TestUpdateJob_NewJobSkipsWhileTheOldRunStops(t *testing.T) {
	s, logs := startedScheduler(t)
	old := newStoppingJob("upd")
	startStoppingJob(t, s, old)

	repl := newCountingJob("upd")
	if err := s.UpdateJob("upd", TriggeredSchedule, repl); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	waitFor(t, old.canceled, "the update did not cancel the old run")
	if err := s.RunJob(context.Background(), "upd"); !errors.Is(err, ErrJobBusy) {
		t.Fatalf("RunJob = %v, want ErrJobBusy while the old run stops", err)
	}
	if err := s.cron.TriggerEntryByName("upd"); err != nil {
		t.Fatalf("TriggerEntryByName: %v", err)
	}
	eventually(t, func() bool { return logs.HasWarning(drainSkipWarning) },
		"the new job's fire was not skipped while the old run was stopping")
	if n := repl.runs.Load(); n != 0 {
		t.Fatalf("the new job ran %d times beside the old run", n)
	}

	close(old.release)
	waitFor(t, old.ended, "the old run did not end")
	eventually(t, func() bool { return !s.IsRemovedJobRunning(old) }, "the old run is still counted")
	if err := s.RunJob(context.Background(), "upd"); err != nil {
		t.Fatalf("RunJob after the drain: %v", err)
	}
	waitFor(t, repl.ran, "the new job did not run after the old run had ended")
}

// An update that fails keeps the old job, and the old job keeps running.
func TestUpdateJob_FailedUpdateKeepsTheJobRunnable(t *testing.T) {
	s, _ := startedScheduler(t)
	j := newCountingJob("keep")
	if err := s.AddJob(j); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if err := s.UpdateJob("keep", "not a schedule", newCountingJob("keep")); err == nil {
		t.Fatal("UpdateJob accepted an invalid schedule")
	}
	if err := s.RunJob(context.Background(), "keep"); err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	waitFor(t, j.ran, "the job did not run after the failed update")
}

// A failed UpdateJob restores the registration it retired. A RemoveJob of the
// same name running at the same time must not be undone by that restore:
// the removed job keeps reporting its stopping run, and a replacement skips
// its fires until the run has ended.
func TestUpdateJob_FailedUpdateDoesNotReviveARemovedJob(t *testing.T) {
	s, _ := startedScheduler(t)
	for i := range 2000 {
		old := newStoppingJob("raced")
		old.ignoreCancel = true
		startStoppingJob(t, s, old)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.UpdateJob("raced", "not a schedule", newCountingJob("raced"))
		}()
		go func() {
			defer wg.Done()
			_ = s.RemoveJob(old)
		}()
		wg.Wait()

		if !s.IsRemovedJobRunning(old) {
			close(old.release)
			t.Fatalf("round %d: the removed job's stopping run is no longer reported", i)
		}
		close(old.release)
		waitFor(t, old.ended, "the old run did not end")
		eventually(t, func() bool { return !s.IsRemovedJobRunning(old) }, "the old run is still counted")
	}
}

// A fire that go-cron dispatched before the removal but that reaches the gate
// after it does not run.
func TestRunGate_RetiredRegistrationDoesNotStart(t *testing.T) {
	var g runGate
	w := &jobWrapper{j: newCountingJob("late")}
	g.register(w)
	if got := g.retire("late"); got != w {
		t.Fatalf("retire returned %v, want the registration", got)
	}
	if v := g.enter(w, false, false); v != gateRetired {
		t.Fatalf("enter = %v, want gateRetired", v)
	}
	if g.retiredRunning(w.j) {
		t.Fatal("a refused fire counts as running")
	}
}

// A deferred run-on-startup fire is handed back only when the last stopping
// run that blocks it has ended, and only for the live registration.
func TestRunGate_DeferredStartupWaitsForEveryStoppingRun(t *testing.T) {
	var g runGate
	a := &jobWrapper{j: newCountingJob("chain")}
	g.register(a)
	if v := g.enter(a, true, false); v != gateRun {
		t.Fatalf("enter a = %v, want gateRun", v)
	}
	g.retire("chain")
	b := &jobWrapper{j: newCountingJob("chain")}
	g.register(b)
	if v := g.enter(b, true, false); v != gateRun {
		t.Fatalf("enter b = %v, want gateRun beside a run that kept a record", v)
	}
	g.retire("chain")
	c := &jobWrapper{j: newCountingJob("chain")}
	g.register(c)
	if v := g.enter(c, false, true); v != gateDeferred {
		t.Fatalf("enter c = %v, want gateDeferred", v)
	}

	if g.leave(a) != nil {
		t.Fatal("the deferred fire was handed back while b still stops")
	}
	if due := g.leave(b); due != c {
		t.Fatalf("leave b = %v, want the live registration c", due)
	}
	if _, ok := g.startup["chain"]; ok {
		t.Fatal("the deferred fire is still recorded after it was handed back")
	}
}

// A deferred run-on-startup fire of a registration that was replaced in the
// meantime is dropped: the newer registration has a fire of its own.
func TestRunGate_DeferredStartupOfAReplacedRegistrationIsDropped(t *testing.T) {
	var g runGate
	a := &jobWrapper{j: newCountingJob("chain")}
	g.register(a)
	g.enter(a, false, false)
	g.retire("chain")
	c := &jobWrapper{j: newCountingJob("chain")}
	g.register(c)
	if v := g.enter(c, false, true); v != gateDeferred {
		t.Fatalf("enter c = %v, want gateDeferred", v)
	}
	g.retire("chain")
	g.register(&jobWrapper{j: newCountingJob("chain")})

	if due := g.leave(a); due != nil {
		t.Fatalf("leave a = %v, want nil for a replaced registration", due)
	}
}

// go-cron fires the run-on-startup run of a new entry as soon as AddJob adds
// it, before addJob records the registration on the gate. A stopping run
// that ends in that moment still hands the deferred fire back.
func TestRunGate_DeferredStartupOfANotYetRecordedRegistrationIsHandedBack(t *testing.T) {
	var g runGate
	a := &jobWrapper{j: newCountingJob("boot")}
	g.register(a)
	g.enter(a, false, false)
	g.retire("boot")
	c := &jobWrapper{j: newCountingJob("boot")}
	if v := g.enter(c, false, true); v != gateDeferred {
		t.Fatalf("enter c = %v, want gateDeferred", v)
	}
	if due := g.leave(a); due != c {
		t.Fatalf("leave a = %v, want c", due)
	}
}

// UpdateJob swaps the job in place, and go-cron fires no run-on-startup run
// for the swapped-in job. The deferred fire moves to it when its job runs on
// startup, and is dropped when it does not.
func TestRunGate_UpdateJobTakesOverTheDeferredStartup(t *testing.T) {
	setup := func() (*runGate, *jobWrapper, *jobWrapper) {
		g := &runGate{}
		a := &jobWrapper{j: newCountingJob("boot")}
		g.register(a)
		g.enter(a, false, false)
		g.retire("boot")
		c := &jobWrapper{j: newCountingJob("boot")}
		g.register(c)
		if v := g.enter(c, false, true); v != gateDeferred {
			t.Fatalf("enter c = %v, want gateDeferred", v)
		}
		return g, a, c
	}

	t.Run("updated job runs on startup", func(t *testing.T) {
		g, a, c := setup()
		g.retire("boot")
		j := newCountingJob("boot")
		j.RunOnStartup = true
		d := &jobWrapper{j: j}
		g.replace(c, d, false)
		if due := g.leave(a); due != d {
			t.Fatalf("leave a = %v, want the updated registration", due)
		}
	})
	t.Run("updated job does not run on startup", func(t *testing.T) {
		g, a, c := setup()
		g.retire("boot")
		g.replace(c, &jobWrapper{j: newCountingJob("boot")}, false)
		if due := g.leave(a); due != nil {
			t.Fatalf("leave a = %v for a job without run-on-startup, want nil", due)
		}
	})
}

// The old run ends at the moment the replacement is added. go-cron fires the
// replacement's run-on-startup run before addJob records it on the gate, so
// over many rounds every interleaving of the two occurs; the run must start
// exactly once in each.
func TestRemoveJob_RunOnStartupSurvivesTheOldRunEndingDuringAddJob(t *testing.T) {
	for i := range 50 {
		s, _ := startedScheduler(t)
		old := newStoppingJob("boot")
		old.ignoreCancel = true
		startStoppingJob(t, s, old)
		if err := s.RemoveJob(old); err != nil {
			t.Fatalf("RemoveJob: %v", err)
		}
		repl := newCountingJob("boot")
		repl.RunOnStartup = true
		go close(old.release)
		if err := s.AddJob(repl); err != nil {
			t.Fatalf("AddJob replacement: %v", err)
		}
		waitFor(t, repl.ran, "round "+strconv.Itoa(i)+": the run-on-startup run was lost")
		time.Sleep(20 * time.Millisecond)
		if n := repl.runs.Load(); n != 1 {
			t.Fatalf("round %d: the replacement ran %d times, want once", i, n)
		}
		_ = s.Stop()
	}
}

// The deferred run-on-startup run is fired while the old run still holds its
// global concurrency slot. With every other slot taken it waits for a slot
// instead of being skipped by the limit. The race the test covers is decided
// in microseconds, so it runs many rounds.
func TestRemoveJob_DeferredRunOnStartupWaitsForAGlobalSlot(t *testing.T) {
	for i := range 30 {
		logger, logs := test.NewTestLoggerWithHandler()
		s := NewScheduler(logger)
		s.SetMaxConcurrentJobs(2)
		if err := s.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		old := newStoppingJob("boot")
		old.ignoreCancel = true
		startStoppingJob(t, s, old)
		if err := s.RemoveJob(old); err != nil {
			t.Fatalf("RemoveJob: %v", err)
		}
		repl := newCountingJob("boot")
		repl.RunOnStartup = true
		if err := s.AddJob(repl); err != nil {
			t.Fatalf("AddJob replacement: %v", err)
		}
		eventually(t, func() bool { return logs.HasWarning("defer: the run-on-startup run") },
			"the replacement's run-on-startup fire was not deferred")
		other := newStoppingJob("other")
		other.ignoreCancel = true
		if err := s.AddJob(other); err != nil {
			t.Fatalf("AddJob other: %v", err)
		}
		// The deferred fire hands its slot back after the gate refused it;
		// only then can the other job take it.
		eventually(t, func() bool {
			return s.RunJob(context.Background(), "other") == nil && func() bool {
				select {
				case <-other.started:
					return true
				case <-time.After(20 * time.Millisecond):
					return false
				}
			}()
		}, "the other job did not take the free slot")

		close(old.release)
		waitFor(t, repl.ran, "round "+strconv.Itoa(i)+": the deferred run-on-startup run was lost")
		close(other.release)
		_ = s.Stop()
	}
}
