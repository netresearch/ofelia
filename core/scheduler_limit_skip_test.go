// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// A fire that the global job limit drops used to leave a single Debug line
// without the job's name. These tests pin what an operator sees instead: a
// skipped run in the job's history, a Warn line naming the job and the limit,
// and the notification middlewares (the ones that run after an execution
// stopped) receiving that skipped run. The fires go through go-cron directly,
// because RunJob refuses a manual run at a full limit (ErrJobLimitReached).

// limitHolderJob takes a slot of the global limit and keeps it until released.
type limitHolderJob struct {
	BareJob
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newLimitHolderJob(name string) *limitHolderJob {
	j := &limitHolderJob{started: make(chan struct{}), release: make(chan struct{})}
	j.Name = name
	j.Schedule = "@every 1h"
	j.Command = "hold"
	return j
}

func (j *limitHolderJob) Run(_ *Context) error {
	j.once.Do(func() { close(j.started) })
	<-j.release
	return nil
}

// skipRecordingMiddleware records the executions it sees.
type skipRecordingMiddleware struct {
	continueOnStop bool
	mu             sync.Mutex
	seen           []Execution
}

func (m *skipRecordingMiddleware) ContinueOnStop() bool { return m.continueOnStop }

func (m *skipRecordingMiddleware) Run(ctx *Context) error {
	err := ctx.Next()
	m.mu.Lock()
	m.seen = append(m.seen, *ctx.Execution)
	m.mu.Unlock()
	return err
}

func (m *skipRecordingMiddleware) executions() []Execution {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Execution(nil), m.seen...)
}

// attrRecord is one captured log record with its attributes.
type attrRecord struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

// attrCapture is a slog.Handler that keeps every record with its attributes.
type attrCapture struct {
	mu      sync.Mutex
	records []attrRecord
}

func (h *attrCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *attrCapture) Handle(_ context.Context, r slog.Record) error {
	rec := attrRecord{level: r.Level, message: r.Message, attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, rec)
	h.mu.Unlock()
	return nil
}

func (h *attrCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *attrCapture) WithGroup(string) slog.Handler      { return h }

func (h *attrCapture) snapshot() []attrRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]attrRecord(nil), h.records...)
}

// startWithFullLimit starts a scheduler whose limit of one slot is taken by a
// held job, and returns it with the held job. The caller releases the job.
func startWithFullLimit(t *testing.T, logger *slog.Logger, others ...Job) (*Scheduler, *limitHolderJob) {
	t.Helper()
	s := NewScheduler(logger)
	s.SetMaxConcurrentJobs(1)
	holder := newLimitHolderJob("holder")
	if err := s.AddJob(holder); err != nil {
		t.Fatalf("add holder: %v", err)
	}
	for _, j := range others {
		if err := s.AddJob(j); err != nil {
			t.Fatalf("add %s: %v", j.GetName(), err)
		}
	}
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.StopWithTimeout(5 * time.Second) })
	if err := s.RunJob(context.Background(), holder.GetName()); err != nil {
		t.Fatalf("run holder: %v", err)
	}
	select {
	case <-holder.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the holder job did not start")
	}
	return s, holder
}

// waitForLastRun polls until the job has a last run.
func waitForLastRun(t *testing.T, j *TestJob) *Execution {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if e := j.GetLastRun(); e != nil {
			return e
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s got no last run", j.GetName())
	return nil
}

func newLimitedJob(name string) *TestJob {
	j := &TestJob{}
	j.Name = name
	j.Schedule = "@every 1h"
	j.Command = "limited"
	return j
}

func TestLimitSkip_RecordsSkippedRunInHistory(t *testing.T) {
	t.Parallel()

	limited := newLimitedJob("limited")
	s, holder := startWithFullLimit(t, newDiscardLogger(), limited)
	defer close(holder.release)

	if err := s.cron.TriggerEntryByName(limited.GetName()); err != nil {
		t.Fatalf("fire limited: %v", err)
	}

	last := waitForLastRun(t, limited)
	if !last.Skipped {
		t.Errorf("last run of a fire dropped by the limit: Skipped = false, want true")
	}
	if last.Failed {
		t.Errorf("last run of a fire dropped by the limit: Failed = true, want false")
	}
	if last.IsRunning {
		t.Errorf("last run of a fire dropped by the limit: IsRunning = true, want false")
	}
	if got := len(limited.GetHistory()); got != 1 {
		t.Errorf("history length = %d, want 1", got)
	}
	if limited.Called() != 0 {
		t.Errorf("the job ran %d times although the limit dropped its fire", limited.Called())
	}
	if limited.Running() != 0 {
		t.Errorf("in-flight counter = %d after the skipped run, want 0", limited.Running())
	}
}

func TestLimitSkip_LogsWarnWithJobNameAndLimit(t *testing.T) {
	t.Parallel()

	capture := &attrCapture{}
	limited := newLimitedJob("limited")
	s, holder := startWithFullLimit(t, slog.New(capture), limited)
	defer close(holder.release)

	if err := s.cron.TriggerEntryByName(limited.GetName()); err != nil {
		t.Fatalf("fire limited: %v", err)
	}
	waitForLastRun(t, limited)

	for _, r := range capture.snapshot() {
		if r.level == slog.LevelWarn && r.attrs["job"] == "limited" {
			if got, ok := r.attrs["max-concurrent-jobs"].(int64); !ok || got != 1 {
				t.Errorf("Warn record %q: max-concurrent-jobs = %v, want 1", r.message, r.attrs["max-concurrent-jobs"])
			}
			return
		}
	}
	t.Errorf("no Warn record with job=limited; records: %+v", capture.snapshot())
}

func TestLimitSkip_NotificationMiddlewaresSeeTheSkippedRun(t *testing.T) {
	t.Parallel()

	notifier := &skipRecordingMiddleware{continueOnStop: true}
	runningOnly := &skipRecordingMiddleware{continueOnStop: false}
	limited := newLimitedJob("limited")
	limited.Use(notifier, runningOnly)
	s, holder := startWithFullLimit(t, newDiscardLogger(), limited)
	defer close(holder.release)

	if err := s.cron.TriggerEntryByName(limited.GetName()); err != nil {
		t.Fatalf("fire limited: %v", err)
	}
	waitForLastRun(t, limited)

	seen := notifier.executions()
	if len(seen) != 1 {
		t.Fatalf("a middleware that continues on stop saw %d executions, want 1", len(seen))
	}
	if !seen[0].Skipped {
		t.Errorf("the notification middleware saw Skipped = false, want true")
	}
	if n := len(runningOnly.executions()); n != 0 {
		t.Errorf("a middleware that only runs while the execution runs saw %d executions, want 0", n)
	}
	if limited.Called() != 0 {
		t.Errorf("the job ran %d times although the limit dropped its fire", limited.Called())
	}
}

// TestLimitSkip_RemovedJobRecordsNothing covers a fire that go-cron dispatched
// before the job was removed and that reaches the full limit only afterwards.
// The run path refuses such a fire at the name lock (passGate) and records
// nothing; the drop path does the same, so the removed job gets no skipped
// run, no Warn line and no notification.
func TestLimitSkip_RemovedJobRecordsNothing(t *testing.T) {
	t.Parallel()

	capture := &attrCapture{}
	notifier := &skipRecordingMiddleware{continueOnStop: true}
	limited := newLimitedJob("limited")
	limited.Use(notifier)
	s, holder := startWithFullLimit(t, slog.New(capture), limited)
	defer close(holder.release)

	s.gate.mu.Lock()
	w := s.gate.current[limited.GetName()]
	s.gate.mu.Unlock()
	if w == nil {
		t.Fatal("the limited job has no live registration")
	}
	if err := s.RemoveJob(limited); err != nil {
		t.Fatalf("remove: %v", err)
	}

	dropped := &maxConcurrentSkipJob{inner: w, sem: s.concurrencySem, logger: NewCronUtils(newDiscardLogger())}
	dropped.RunWithContext(context.Background())

	if last := limited.GetLastRun(); last != nil {
		t.Errorf("the removed job got a last run: %+v", last)
	}
	if n := len(limited.GetHistory()); n != 0 {
		t.Errorf("history length = %d, want 0", n)
	}
	if n := len(notifier.executions()); n != 0 {
		t.Errorf("a notification middleware saw %d executions of the removed job, want 0", n)
	}
	for _, r := range capture.snapshot() {
		if r.level == slog.LevelWarn && r.attrs["job"] == "limited" {
			t.Errorf("Warn record for the removed job: %q", r.message)
		}
	}
	if limited.Called() != 0 {
		t.Errorf("the removed job ran %d times", limited.Called())
	}
}

func TestScheduler_MaxConcurrentJobsReportsTheLimit(t *testing.T) {
	t.Parallel()

	s := NewScheduler(newDiscardLogger())
	if got := s.MaxConcurrentJobs(); got != DefaultMaxConcurrentJobs {
		t.Errorf("new scheduler: MaxConcurrentJobs() = %d, want %d", got, DefaultMaxConcurrentJobs)
	}
	s.SetMaxConcurrentJobs(7)
	if got := s.MaxConcurrentJobs(); got != 7 {
		t.Errorf("after SetMaxConcurrentJobs(7): MaxConcurrentJobs() = %d, want 7", got)
	}
}

// TestLimitSkip_SlotFreeRunsNormally guards the other branch: with a free
// slot the job runs and nothing is recorded as skipped.
func TestLimitSkip_SlotFreeRunsNormally(t *testing.T) {
	t.Parallel()

	s := NewScheduler(newDiscardLogger())
	s.SetMaxConcurrentJobs(1)
	j := newLimitedJob("free")
	if err := s.AddJob(j); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.StopWithTimeout(5 * time.Second) })

	if err := s.RunJob(context.Background(), j.GetName()); err != nil {
		t.Fatalf("run: %v", err)
	}
	last := waitForLastRun(t, j)
	if last.Skipped {
		t.Errorf("a run with a free slot was recorded as skipped")
	}
	if j.Called() != 1 {
		t.Errorf("job ran %d times, want 1", j.Called())
	}
}
