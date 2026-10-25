// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
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
	if err := s.RunJob(context.Background(), "dup"); err != nil {
		t.Fatalf("RunJob replacement: %v", err)
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
	if err := s.RunJob(context.Background(), "tagged"); err != nil {
		t.Fatalf("RunJob replacement: %v", err)
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
	if err := s.RunJob(context.Background(), "upd"); err != nil {
		t.Fatalf("RunJob: %v", err)
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
	if v := g.enter(w, false); v != gateRetired {
		t.Fatalf("enter = %v, want gateRetired", v)
	}
	if g.retiredRunning(w.j) {
		t.Fatal("a refused fire counts as running")
	}
}
