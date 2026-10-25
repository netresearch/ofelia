// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// inheritingRunJob is a run job without a max-runtime of its own, the shape
// that runs under [global] max-runtime.
func inheritingRunJob() *RunJob {
	j := &RunJob{}
	j.Name = "inherits"
	return j
}

func requireCanceledWithMaxTime(t *testing.T, ctx context.Context, within time.Duration) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(within):
		t.Fatalf("run context still open after %v", within)
	}
	if !errors.Is(context.Cause(ctx), ErrMaxTimeRunning) {
		t.Fatalf("cause = %v, want ErrMaxTimeRunning", context.Cause(ctx))
	}
}

func requireOpen(t *testing.T, ctx context.Context, wait time.Duration) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatalf("run context ended early, cause %v", context.Cause(ctx))
	case <-time.After(wait):
	}
}

// A lowered [global] max-runtime ends a run in progress whose elapsed time
// already reaches the new value; the job is not re-registered for it.
func TestRunContext_LoweredGlobalEndsRunInProgress(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	s.SetGlobalMaxRuntime(time.Hour)

	ctx, cancel := s.runContext(context.Background(), inheritingRunJob())
	defer cancel()
	requireOpen(t, ctx, 30*time.Millisecond)

	s.SetGlobalMaxRuntime(20 * time.Millisecond)
	requireCanceledWithMaxTime(t, ctx, time.Second)
}

// A raised [global] max-runtime extends a run in progress.
func TestRunContext_RaisedGlobalExtendsRunInProgress(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	s.SetGlobalMaxRuntime(60 * time.Millisecond)

	ctx, cancel := s.runContext(context.Background(), inheritingRunJob())
	defer cancel()
	s.SetGlobalMaxRuntime(time.Hour)
	requireOpen(t, ctx, 200*time.Millisecond)
}

// Without a change the inherited bound ends the run like a fixed deadline.
func TestRunContext_InheritedGlobalEndsRun(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	s.SetGlobalMaxRuntime(30 * time.Millisecond)

	ctx, cancel := s.runContext(context.Background(), inheritingRunJob())
	defer cancel()
	requireCanceledWithMaxTime(t, ctx, time.Second)
}

// Only jobs without a value of their own that inherit the global follow it.
// A job's own value is fixed at the start of the run, and exec, local and
// exec-mode compose jobs stay on the 24h default.
func TestRunContext_GlobalReachesOnlyInheritingJobs(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	s.SetGlobalMaxRuntime(time.Minute)

	own := &RunJob{MaxRuntime: 2 * time.Hour}
	composeRun := &ComposeJob{}
	composeExec := &ComposeJob{Exec: true}
	service := &RunServiceJob{}
	cases := map[string]struct {
		job  Job
		want time.Duration
	}{
		"own value":        {job: own, want: 2 * time.Hour},
		"run inherits":     {job: inheritingRunJob(), want: time.Minute},
		"service inherits": {job: service, want: time.Minute},
		"compose run mode": {job: composeRun, want: time.Minute},
		"compose exec":     {job: composeExec, want: defaultJobMaxRuntime},
		"exec job":         {job: &ExecJob{}, want: defaultJobMaxRuntime},
		"local job":        {job: &LocalJob{}, want: defaultJobMaxRuntime},
	}
	for name, tc := range cases {
		if got := s.MaxRuntimeFor(tc.job); got != tc.want {
			t.Errorf("%s: MaxRuntimeFor = %v, want %v", name, got, tc.want)
		}
	}

	// The fixed bounds carry a deadline that a later change does not move.
	for name, j := range map[string]Job{"own value": own, "compose exec": composeExec, "exec job": &ExecJob{}} {
		ctx, cancel := s.runContext(context.Background(), j)
		before, ok := ctx.Deadline()
		if !ok {
			t.Fatalf("%s: no deadline", name)
		}
		s.SetGlobalMaxRuntime(time.Millisecond)
		after, _ := ctx.Deadline()
		if !after.Equal(before) || ctx.Err() != nil {
			t.Errorf("%s: a global change moved the fixed bound", name)
		}
		s.SetGlobalMaxRuntime(time.Minute)
		cancel()
	}
}

// A value <= 0 restores the 24h default.
func TestSetGlobalMaxRuntime_ZeroMeansDefault(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	s.SetGlobalMaxRuntime(time.Minute)
	s.SetGlobalMaxRuntime(0)
	if got := s.MaxRuntimeFor(inheritingRunJob()); got != defaultJobMaxRuntime {
		t.Fatalf("MaxRuntimeFor = %v, want %v", got, defaultJobMaxRuntime)
	}
}

// blockingJob runs until its context ends and reports the cause.
type blockingJob struct {
	BareJob
	started chan struct{}
	cause   chan error
}

func (j *blockingJob) GetMaxRuntime() time.Duration   { return 0 }
func (j *blockingJob) InheritsGlobalMaxRuntime() bool { return true }

func (j *blockingJob) Run(ctx *Context) error {
	close(j.started)
	<-ctx.RunContext().Done()
	j.cause <- context.Cause(ctx.RunContext())
	return ErrMaxTimeRunning
}

// The scheduler's wrapper bounds a run with the live global: lowering it
// while the run is in progress ends that run.
func TestJobWrapper_RunFollowsGlobalChange(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	if err := s.Start(); err != nil {
		t.Fatalf("scheduler start: %v", err)
	}
	defer func() { _ = s.Stop() }()
	s.SetGlobalMaxRuntime(time.Hour)

	j := &blockingJob{
		BareJob: BareJob{Name: "blocking", Schedule: "@every 1h", Command: "true"},
		started: make(chan struct{}),
		cause:   make(chan error, 1),
	}
	go (&jobWrapper{s: s, j: j}).runWithCtx(context.TODO())

	select {
	case <-j.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the job never started")
	}
	s.SetGlobalMaxRuntime(time.Millisecond)

	select {
	case cause := <-j.cause:
		if !errors.Is(cause, ErrMaxTimeRunning) {
			t.Fatalf("cause = %v, want ErrMaxTimeRunning", cause)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not end after the global was lowered")
	}
}

// A service job reports an inherited max-runtime as ErrMaxTimeRunning, as it
// does for its own value, not as a bare context error.
func TestRunServiceJob_InheritedMaxRuntimeIsMaxTimeRunning(t *testing.T) {
	t.Parallel()
	k := newTestRunServiceKit(t)
	jobCtx := newRunServiceJobContext(t, k.job)
	k.services.SetTasks([]domain.Task{
		{ID: "t-running", Status: domain.TaskStatus{State: domain.TaskStateRunning}},
	})

	s := NewScheduler(test.NewTestLogger())
	s.SetGlobalMaxRuntime(50 * time.Millisecond)
	ctx, cancel := s.runContext(context.Background(), k.job)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- k.job.watchContainer(ctx, jobCtx, "svc-1") }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrMaxTimeRunning) {
			t.Fatalf("err = %v, want ErrMaxTimeRunning", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchContainer did not return")
	}
}

// Reloads set the global while runs read it.
func TestSetGlobalMaxRuntime_ConcurrentWithRuns(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for n := range 50 {
				s.SetGlobalMaxRuntime(time.Duration(i*50+n+1) * time.Minute)
			}
		})
		wg.Go(func() {
			for range 50 {
				ctx, cancel := s.runContext(context.Background(), inheritingRunJob())
				_ = s.MaxRuntimeFor(inheritingRunJob())
				cancel()
				<-ctx.Done()
			}
		})
	}
	wg.Wait()
}
