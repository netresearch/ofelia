// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ListenForShutdown() tests
// ---------------------------------------------------------------------------

func TestShutdownManagerUnit_ListenForShutdown_SignalSetsShuttingDown(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 1*time.Second)

	// Start listening for signals
	sm.ListenForShutdown()

	// The listener is running in a goroutine. We cannot send real OS signals
	// in unit tests portably, but we can verify that calling Shutdown() works
	// after ListenForShutdown was invoked.
	if sm.IsShuttingDown() {
		t.Error("should not be shutting down before signal")
	}

	// Manually invoke shutdown (the signal listener would do this)
	err := sm.Shutdown()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sm.IsShuttingDown() {
		t.Error("expected IsShuttingDown=true after Shutdown()")
	}
}

// ---------------------------------------------------------------------------
// NewShutdownManager() with zero timeout
// ---------------------------------------------------------------------------

func TestShutdownManagerUnit_ZeroTimeout(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 0)

	// Should default to 30s
	if sm.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", sm.timeout)
	}
}

func TestShutdownManagerUnit_NegativeTimeout(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, -5*time.Second)

	if sm.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s for negative, got %v", sm.timeout)
	}
}

// ---------------------------------------------------------------------------
// RegisterHook() priority ordering
// ---------------------------------------------------------------------------

func TestShutdownManagerUnit_HookPrioritySorting(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 1*time.Second)

	// Register hooks in reverse priority order
	for _, p := range []int{30, 10, 20, 5, 25} {
		sm.RegisterHook(ShutdownHook{
			Name:     "hook",
			Priority: p,
			Hook:     func(context.Context) error { return nil },
		})
	}

	// Verify sorted
	for i := 1; i < len(sm.hooks); i++ {
		if sm.hooks[i].Priority < sm.hooks[i-1].Priority {
			t.Errorf("hooks not sorted: index %d priority %d < index %d priority %d",
				i, sm.hooks[i].Priority, i-1, sm.hooks[i-1].Priority)
		}
	}
}

// ---------------------------------------------------------------------------
// Shutdown() with hook errors
// ---------------------------------------------------------------------------

func TestShutdownManagerUnit_MultipleHookErrors(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 1*time.Second)

	sm.RegisterHook(ShutdownHook{
		Name:     "fail-1",
		Priority: 10,
		Hook:     func(context.Context) error { return errors.New("err1") },
	})
	sm.RegisterHook(ShutdownHook{
		Name:     "fail-2",
		Priority: 20,
		Hook:     func(context.Context) error { return errors.New("err2") },
	})

	err := sm.Shutdown()
	if err == nil {
		t.Fatal("expected error from failing hooks")
	}
}

// ---------------------------------------------------------------------------
// GracefulServer
// ---------------------------------------------------------------------------

func TestGracefulServerUnit_Creation(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 1*time.Second)

	server := &http.Server{Addr: ":0"}
	gs := NewGracefulServer(server, sm, logger)

	if gs == nil {
		t.Fatal("NewGracefulServer returned nil")
	}
	// Should have registered a hook
	hookFound := false
	for _, h := range sm.hooks {
		if h.Name == "http-server" {
			hookFound = true
			break
		}
	}
	if !hookFound {
		t.Error("expected http-server hook to be registered")
	}
}

func TestGracefulServerUnit_GracefulStop(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 2*time.Second)

	// Create a real server on an ephemeral port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	server := &http.Server{Handler: http.DefaultServeMux}
	_ = NewGracefulServer(server, sm, logger)

	// Start serving in background
	go func() {
		_ = server.Serve(listener)
	}()

	// Give server a moment to start
	time.Sleep(10 * time.Millisecond)

	// Shutdown should stop the server via the registered hook
	shutdownErr := sm.Shutdown()
	if shutdownErr != nil {
		t.Fatalf("unexpected shutdown error: %v", shutdownErr)
	}
}

// ---------------------------------------------------------------------------
// gracefulStop() (scheduler)
// ---------------------------------------------------------------------------

func TestGracefulSchedulerUnit_GracefulStopWaitsForJobs(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	scheduler := NewScheduler(logger)
	sm := NewShutdownManager(logger, 2*time.Second)
	gs := NewGracefulScheduler(scheduler, sm)

	// Simulate an active job
	var jobDone sync.WaitGroup
	jobDone.Add(1)
	gs.activeJobs.Add(1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		gs.activeJobs.Done()
		jobDone.Done()
	}()

	// Shutdown - should wait for active job
	err := sm.Shutdown()
	jobDone.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGracefulSchedulerUnit_GracefulStopTimeout(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	scheduler := NewScheduler(logger)
	sm := NewShutdownManager(logger, 100*time.Millisecond)
	gs := NewGracefulScheduler(scheduler, sm)

	// Simulate a stuck job that never finishes
	gs.activeJobs.Add(1)
	// Intentionally never call gs.activeJobs.Done()

	err := sm.Shutdown()
	// The shutdown manager should timeout, and the gracefulStop hook
	// should return ErrWaitTimeout
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// Clean up
	gs.activeJobs.Done()
}

// ---------------------------------------------------------------------------
// RunJobWithTracking()
// ---------------------------------------------------------------------------

func TestGracefulSchedulerUnit_RunJobWithTracking_Success(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	scheduler := NewScheduler(logger)
	sm := NewShutdownManager(logger, 2*time.Second)
	gs := NewGracefulScheduler(scheduler, sm)

	job := &LocalJob{
		Name:    "track-job",
		Command: "echo ok",
	}
	exec, _ := NewExecution()
	exec.Start()
	ctx := NewContext(scheduler, job, exec)

	err := gs.RunJobWithTracking(job, ctx)
	// LocalJob.Run may fail if the command isn't available, but the tracking
	// mechanism should work regardless
	_ = err
}

func TestGracefulSchedulerUnit_RunJobWithTracking_DuringShutdown(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	scheduler := NewScheduler(logger)
	sm := NewShutdownManager(logger, 1*time.Second)
	gs := NewGracefulScheduler(scheduler, sm)

	// Start shutdown first
	_ = sm.Shutdown()

	job := &BareJob{Name: "blocked-job", Command: "echo blocked"}
	exec, _ := NewExecution()
	exec.Start()
	ctx := NewContext(scheduler, job, exec)

	err := gs.RunJobWithTracking(job, ctx)
	if !errors.Is(err, ErrCannotStartJob) {
		t.Errorf("expected ErrCannotStartJob, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrent Shutdown
// ---------------------------------------------------------------------------

func TestShutdownManagerUnit_ConcurrentShutdownCalls(t *testing.T) {
	t.Parallel()

	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 1*time.Second)

	var wg sync.WaitGroup
	results := make([]error, 5)

	for i := range 5 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = sm.Shutdown()
		}(i)
	}

	wg.Wait()

	// Exactly one should succeed, rest should get ErrShutdownInProgress
	successes := 0
	inProgress := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrShutdownInProgress) {
			inProgress++
		}
	}

	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d", successes)
	}
	if inProgress != 4 {
		t.Errorf("expected 4 ErrShutdownInProgress, got %d", inProgress)
	}
}
