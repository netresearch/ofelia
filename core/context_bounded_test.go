// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// TestContext_RunContext_NilFallback verifies that RunContext() returns a
// non-nil, usable context.Context even when ctx.Ctx is nil. This guarantees
// the four production fallbacks in execjob/runjob/runservice/localjob can
// be replaced with a single helper without panicking on legacy callers
// that constructed *Context literals without setting Ctx.
func TestContext_RunContext_NilFallback(t *testing.T) {
	t.Parallel()

	c := &Context{} // legacy literal: Ctx is nil
	got := c.RunContext()
	if got == nil {
		t.Fatal("RunContext() returned nil; want non-nil fallback")
	}
	if got.Err() != nil {
		t.Errorf("RunContext() fallback returned a finished context: %v", got.Err())
	}
}

// TestContext_RunContext_PassThrough verifies that when ctx.Ctx is set,
// RunContext() returns it unchanged (no wrapping, no copy).
func TestContext_RunContext_PassThrough(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	parent := t.Context()
	parent = context.WithValue(parent, ctxKey{}, "marker")

	c := &Context{Ctx: parent}
	got := c.RunContext()
	if got == nil {
		t.Fatal("RunContext() returned nil")
	}
	if v, ok := got.Value(ctxKey{}).(string); !ok || v != "marker" {
		t.Errorf("RunContext() did not return ctx.Ctx unchanged; value lookup got %q", v)
	}
}

// TestNewContext_NeverNilCtx verifies the NewContext factory always
// produces a Context whose Ctx is non-nil so that downstream RunContext()
// calls never have to fall back.
func TestNewContext_NeverNilCtx(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	scheduler := NewScheduler(logger)
	exec, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution: %v", err)
	}
	job := &BareJob{Name: "ctx-default", Schedule: "@every 1h", Command: "true"}

	c := NewContext(scheduler, job, exec)
	if c.Ctx == nil {
		t.Fatal("NewContext(): Ctx is nil; want non-nil default context")
	}
}

// TestExecJob_RespectsBoundedParent verifies that ExecJob.Run honors a
// deadline-bounded parent context: a wedged Docker exec call (blocked on
// ctx.Done) must unblock within the parent's deadline + slack. Sister to
// TestJobWrapper_BoundsExecJobByDefaultMaxRuntime below, which exercises
// the wrapper-level bounding (the actual fix for issue #638). This test
// just locks in the contract that the executor surfaces context
// cancellation.
func TestExecJob_RespectsBoundedParent(t *testing.T) {
	t.Parallel()

	const maxRuntime = 80 * time.Millisecond
	const slack = 2 * time.Second // CI is slow; cancellation must happen well under this

	mc := mock.NewDockerClient()
	logger := test.NewTestLogger()
	provider := NewSDKDockerProviderFromClient(mc, logger, nil)

	job := NewExecJob(provider)
	job.BareJob = BareJob{Name: "ctx-deadline", Command: "echo hello"}
	job.Container = "test-container"

	execSvc := mc.Exec().(*mock.ExecService)
	execSvc.OnRun = func(ctx context.Context, _ string, _ *domain.ExecConfig, _, _ io.Writer) (int, error) {
		<-ctx.Done()
		return -1, ctx.Err()
	}

	scheduler := NewScheduler(logger)

	parent, cancel := context.WithTimeout(context.Background(), maxRuntime)
	defer cancel()

	exec, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution: %v", err)
	}
	exec.Start()
	jobCtx := NewContextWithContext(parent, scheduler, job, exec)

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- job.Run(jobCtx)
	}()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if elapsed > maxRuntime+slack {
			t.Errorf("job took %v to cancel; want <= %v", elapsed, maxRuntime+slack)
		}
		if err == nil {
			t.Error("expected non-nil error from canceled job; got nil")
		}
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Logf("note: error was %v (not DeadlineExceeded/Canceled); acceptable if wrapped", err)
		}
	case <-time.After(maxRuntime + slack):
		t.Fatalf("job did not cancel within %v; bounded context not propagated", maxRuntime+slack)
	}
}

// TestBoundJobContext_FromJobMaxRuntime verifies that boundJobContext
// derives a deadline from a job's MaxRuntimeProvider value when present.
func TestBoundJobContext_FromJobMaxRuntime(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()

	// A RunJob HAS MaxRuntime — pick a small one.
	mc := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mc, logger, nil)
	job := NewRunJob(provider)
	job.BareJob = BareJob{Name: "deadline-derived", Command: "true"}
	job.MaxRuntime = 50 * time.Millisecond

	parent := t.Context()
	bounded, boundedCancel := boundJobContext(parent, job, defaultJobMaxRuntime)
	defer boundedCancel()

	deadline, ok := bounded.Deadline()
	if !ok {
		t.Fatal("boundJobContext did not set a deadline")
	}
	d := time.Until(deadline)
	if d <= 0 || d > 200*time.Millisecond {
		t.Errorf("derived deadline %v out of expected range (0,200ms]", d)
	}
}

// TestJobWrapper_BoundsExecJobByDefaultMaxRuntime is the end-to-end
// regression test for issue #638. It drives the actual scheduler
// jobWrapper.runWithCtx with an UNBOUNDED parent context (the same
// context.TODO() the cron defensive fallback uses) and asserts that the
// inner Docker provider receives a bounded context — i.e., that the
// scheduler-level boundJobContext call truly wraps the per-run context
// even for job types like ExecJob that do not implement MaxRuntimeProvider
// themselves.
//
// We can't realistically wait the full defaultJobMaxRuntime (24h), so we
// inspect the context the Docker mock receives and assert it carries a
// Deadline. Without the scheduler-level bound, the mock would see an
// unbounded context.
func TestJobWrapper_BoundsExecJobByDefaultMaxRuntime(t *testing.T) {
	t.Parallel()

	mc := mock.NewDockerClient()
	logger := test.NewTestLogger()
	provider := NewSDKDockerProviderFromClient(mc, logger, nil)

	scheduler := NewScheduler(logger)
	if err := scheduler.Start(); err != nil {
		t.Fatalf("scheduler start: %v", err)
	}
	defer func() {
		if err := scheduler.Stop(); err != nil {
			t.Logf("scheduler stop: %v", err)
		}
	}()

	job := NewExecJob(provider)
	job.BareJob = BareJob{Name: "wrapper-bounds-exec", Schedule: "@every 1h", Command: "echo hi"}
	job.Container = "test-container"

	// Capture the context the Docker exec call receives.
	var (
		gotCtx  context.Context
		gotOnce sync.Once
		ready   = make(chan struct{})
	)
	execSvc := mc.Exec().(*mock.ExecService)
	execSvc.OnRun = func(ctx context.Context, _ string, _ *domain.ExecConfig, _, _ io.Writer) (int, error) {
		gotOnce.Do(func() {
			gotCtx = ctx //nolint:fatcontext // capturing for assertion, not nesting
			close(ready)
		})
		return 0, nil
	}

	// Drive the wrapper directly with an UNBOUNDED parent context. This
	// is the exact path the cron defensive fallback (Run -> runWithCtx)
	// takes; it MUST be bounded by the wrapper.
	w := &jobWrapper{s: scheduler, j: job}
	go w.runWithCtx(context.TODO())

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("Docker exec was never invoked; wrapper did not run job")
	}

	if gotCtx == nil {
		t.Fatal("captured ctx is nil")
	}
	deadline, ok := gotCtx.Deadline()
	if !ok {
		t.Fatal("Docker provider received an UNBOUNDED context; wrapper did not apply boundJobContext (regression of #638)")
	}
	until := time.Until(deadline)
	// Default bound is 24h — accept anything in [23h59m, 24h] to allow for scheduling jitter.
	if until < defaultJobMaxRuntime-time.Minute || until > defaultJobMaxRuntime+time.Minute {
		t.Errorf("default-bounded deadline = %v away; want ~%v", until, defaultJobMaxRuntime)
	}
}

// TestBoundJobContext_GlobalDefault verifies that a job without its own
// MaxRuntime gets bounded by the scheduler's default (24h sentinel).
func TestBoundJobContext_GlobalDefault(t *testing.T) {
	t.Parallel()

	job := &BareJob{Name: "no-maxruntime"} // no MaxRuntime accessor
	parent := t.Context()

	bounded, boundedCancel := boundJobContext(parent, job, defaultJobMaxRuntime)
	defer boundedCancel()

	deadline, ok := bounded.Deadline()
	if !ok {
		t.Fatal("boundJobContext did not set a deadline (global default missing)")
	}
	// Should be roughly defaultJobMaxRuntime away.
	want := time.Until(deadline)
	if want < defaultJobMaxRuntime-time.Minute || want > defaultJobMaxRuntime+time.Minute {
		t.Errorf("default-bounded deadline = %v; want ~%v", want, defaultJobMaxRuntime)
	}
}
