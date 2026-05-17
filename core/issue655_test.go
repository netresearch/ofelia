// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// Tests for issue #655: when the wrapper-level deadline (from #651's
// boundJobContext) fires mid-run, the inner SDK calls return but the
// container / swarm service is left orphaned. These tests pin the
// best-effort cleanup added on the deadline path.

// --------------------------------------------------------------------------
// 1. RunJob: deadline must trigger stopContainer + deleteContainer on a
//    fresh (non-expired) context so the container is actually torn down.
// --------------------------------------------------------------------------

// TestRunJob_DeadlineFiresStopAndDelete asserts that when the parent
// context expires while WaitContainer is hanging, the deferred cleanup
// path stops AND removes the container — both with a fresh (non-expired)
// context so the daemon call actually completes. Regression for #655.
func TestRunJob_DeadlineFiresStopAndDelete(t *testing.T) {
	t.Parallel()

	k := newTestRunJobKit(t)
	k.job.Delete = "true"

	// Wait blocks until ctx is canceled — simulates a real container
	// where the inner SDK call returns when the deadline fires.
	k.containers.OnWait = func(waitCtx context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		r := make(chan domain.WaitResponse, 1)
		e := make(chan error, 1)
		go func() {
			<-waitCtx.Done()
			e <- waitCtx.Err()
			close(r)
			close(e)
		}()
		return r, e
	}

	// Track the context passed to Stop / Remove so we can assert it is
	// NOT the expired parent — cleanup must be best-effort against a
	// fresh deadline.
	var stopCtxLive, removeCtxLive bool
	k.containers.OnStop = func(stopCtx context.Context, _ string, _ domain.StopOptions) error {
		stopCtxLive = stopCtx.Err() == nil
		return nil
	}
	k.containers.OnRemove = func(remCtx context.Context, _ string, _ domain.RemoveOptions) error {
		removeCtxLive = remCtx.Err() == nil
		return nil
	}

	jobCtx := newRunJobContext(t, k.job)
	parent, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	jobCtx.Ctx = parent

	err := k.job.Run(jobCtx)
	if !errors.Is(err, ErrMaxTimeRunning) {
		t.Fatalf("expected ErrMaxTimeRunning, got %v", err)
	}

	if len(k.containers.StopCalls) == 0 {
		t.Fatal("expected stopContainer to be called on deadline path; got 0 calls")
	}
	if !stopCtxLive {
		t.Error("stopContainer must be invoked with a fresh (non-expired) context")
	}

	if len(k.containers.RemoveCalls) == 0 {
		t.Fatal("expected deleteContainer to be called on deadline path; got 0 calls")
	}
	if !removeCtxLive {
		t.Error("deleteContainer must be invoked with a fresh (non-expired) context")
	}
}

// TestRunJob_DeadlineCleanupContinuesOnStopError ensures a Stop failure
// does not block Remove — both are best-effort and independent.
func TestRunJob_DeadlineCleanupContinuesOnStopError(t *testing.T) {
	t.Parallel()

	k := newTestRunJobKit(t)
	k.job.Delete = "true"

	k.containers.OnWait = func(waitCtx context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		r := make(chan domain.WaitResponse, 1)
		e := make(chan error, 1)
		go func() {
			<-waitCtx.Done()
			e <- waitCtx.Err()
			close(r)
			close(e)
		}()
		return r, e
	}
	k.containers.OnStop = func(_ context.Context, _ string, _ domain.StopOptions) error {
		return errors.New("stop failed")
	}

	jobCtx := newRunJobContext(t, k.job)
	parent, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	jobCtx.Ctx = parent

	_ = k.job.Run(jobCtx)

	if len(k.containers.RemoveCalls) == 0 {
		t.Error("Remove must still be attempted even when Stop errored")
	}
}

// --------------------------------------------------------------------------
// 2. RunServiceJob: ticker watcher must honor ctx.Done() and the wrapper
//    must remove the swarm service on deadline using a fresh context.
// --------------------------------------------------------------------------

// TestRunServiceJob_WatcherHonorsContextCancellation pins that the
// ticker-driven watcher returns promptly when the parent context is
// canceled — without this, a wrapper-level deadline only stops the
// SDK call but the watcher loop keeps spinning until j.MaxRuntime
// elapses (or, when MaxRuntime is unset, forever).
func TestRunServiceJob_WatcherHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	k := newTestRunServiceKit(t)
	// No j.MaxRuntime — only the parent ctx deadline should bound the loop.
	jobCtx := newRunServiceJobContext(t, k.job)

	// Always-running task — the loop never exits via findTaskStatus.
	k.services.SetTasks([]domain.Task{
		{ID: "t-running", Status: domain.TaskStatus{State: domain.TaskStateRunning}},
	})

	parent, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- k.job.watchContainer(parent, jobCtx, "svc-1")
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchContainer did not return after parent ctx was canceled")
	}
}

// TestRunServiceJob_DeadlineRemovesService asserts that when Run() exits
// because the parent context expired, the swarm service is removed using
// a fresh (non-expired) context. Without this the operator is left with
// a phantom task scheduled in the swarm.
func TestRunServiceJob_DeadlineRemovesService(t *testing.T) {
	t.Parallel()

	k := newTestRunServiceKit(t)
	k.job.Delete = "true"

	// Always-running task so watchContainer only exits via ctx cancellation.
	k.services.SetTasks([]domain.Task{
		{ID: "t-running", Status: domain.TaskStatus{State: domain.TaskStateRunning}},
	})

	var removeCtxLive bool
	k.services.OnRemove = func(remCtx context.Context, _ string) error {
		removeCtxLive = remCtx.Err() == nil
		return nil
	}

	jobCtx := newRunServiceJobContext(t, k.job)
	parent, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	jobCtx.Ctx = parent

	_ = k.job.Run(jobCtx)

	if len(k.services.RemoveCalls) == 0 {
		t.Fatal("expected swarm service to be removed on deadline path; got 0 calls")
	}
	if !removeCtxLive {
		t.Error("RemoveService must be invoked with a fresh (non-expired) context")
	}
}
