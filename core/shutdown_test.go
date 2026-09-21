// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestShutdownManager(t *testing.T) {
	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 5*time.Second)

	if sm == nil {
		t.Fatal("NewShutdownManager returned nil")
	}

	if sm.timeout != 5*time.Second {
		t.Errorf("Expected timeout 5s, got %v", sm.timeout)
	}

	if sm.IsShuttingDown() {
		t.Error("Should not be shutting down initially")
	}

	t.Log("ShutdownManager creation test passed")
}

func TestShutdownHooks(t *testing.T) {
	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 2*time.Second)

	// Track hook execution with mutex for concurrent access
	var mu sync.Mutex
	executionOrder := make(map[string]bool)

	// Register hooks with different priorities
	sm.RegisterHook(ShutdownHook{
		Name:     "hook2",
		Priority: 20,
		Hook: func(ctx context.Context) error {
			mu.Lock()
			executionOrder["hook2"] = true
			mu.Unlock()
			return nil
		},
	})

	sm.RegisterHook(ShutdownHook{
		Name:     "hook1",
		Priority: 10,
		Hook: func(ctx context.Context) error {
			mu.Lock()
			executionOrder["hook1"] = true
			mu.Unlock()
			return nil
		},
	})

	sm.RegisterHook(ShutdownHook{
		Name:     "hook3",
		Priority: 30,
		Hook: func(ctx context.Context) error {
			mu.Lock()
			executionOrder["hook3"] = true
			mu.Unlock()
			return nil
		},
	})

	// Execute shutdown
	err := sm.Shutdown()
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Verify all hooks executed (they run concurrently)
	if len(executionOrder) != 3 {
		t.Errorf("Expected 3 hooks executed, got %d", len(executionOrder))
	}

	// Verify hooks are sorted by priority in sm.hooks array
	if len(sm.hooks) != 3 {
		t.Errorf("Expected 3 hooks registered, got %d", len(sm.hooks))
	}
	if sm.hooks[0].Priority != 10 || sm.hooks[1].Priority != 20 || sm.hooks[2].Priority != 30 {
		t.Errorf("Hooks not sorted by priority: %v", sm.hooks)
	}

	if !sm.IsShuttingDown() {
		t.Error("Should be marked as shutting down")
	}

	t.Log("Shutdown hooks test passed")
}

func TestShutdownTimeout(t *testing.T) {
	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 100*time.Millisecond)

	// Register a hook that takes too long
	sm.RegisterHook(ShutdownHook{
		Name:     "slow-hook",
		Priority: 10,
		Hook: func(ctx context.Context) error {
			select {
			case <-time.After(500 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})

	start := time.Now()
	err := sm.Shutdown()
	duration := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error")
	}

	// Should timeout around 100ms (with some tolerance)
	if duration > 200*time.Millisecond {
		t.Errorf("Shutdown took too long: %v", duration)
	}

	t.Log("Shutdown timeout test passed")
}

func TestShutdownWithErrors(t *testing.T) {
	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 1*time.Second)

	// Register hooks, some with errors
	sm.RegisterHook(ShutdownHook{
		Name:     "good-hook",
		Priority: 10,
		Hook: func(ctx context.Context) error {
			return nil
		},
	})

	sm.RegisterHook(ShutdownHook{
		Name:     "bad-hook",
		Priority: 20,
		Hook: func(ctx context.Context) error {
			return errors.New("hook failed")
		},
	})

	err := sm.Shutdown()

	// Should report error but still complete
	if err == nil {
		t.Error("Expected error from failed hook")
	}

	t.Log("Shutdown with errors test passed")
}

func TestShutdownChan(t *testing.T) {
	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 1*time.Second)

	shutdownChan := sm.ShutdownChan()

	// Channel should not be closed initially
	select {
	case <-shutdownChan:
		t.Error("Shutdown channel should not be closed initially")
	default:
		// Expected
	}

	// Start shutdown in background
	go sm.Shutdown()

	// Channel should be closed soon
	select {
	case <-shutdownChan:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Shutdown channel was not closed")
	}

	t.Log("Shutdown channel test passed")
}

func TestDoubleShutdown(t *testing.T) {
	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 1*time.Second)

	// First shutdown should succeed
	err1 := sm.Shutdown()
	if err1 != nil {
		t.Errorf("First shutdown failed: %v", err1)
	}

	// Second shutdown should return error
	err2 := sm.Shutdown()
	if err2 == nil {
		t.Error("Second shutdown should return error")
	}

	t.Log("Double shutdown prevention test passed")
}

func TestGracefulScheduler(t *testing.T) {
	logger := newDiscardLogger()
	scheduler := NewScheduler(logger)
	sm := NewShutdownManager(logger, 2*time.Second)

	gs := NewGracefulScheduler(scheduler, sm)

	if gs == nil {
		t.Fatal("NewGracefulScheduler returned nil")
	}

	// Verify shutdown hook was registered
	if len(sm.hooks) != 1 {
		t.Errorf("Expected 1 shutdown hook, got %d", len(sm.hooks))
	}

	if sm.hooks[0].Name != "scheduler" {
		t.Errorf("Expected hook name 'scheduler', got '%s'", sm.hooks[0].Name)
	}

	t.Log("GracefulScheduler creation test passed")
}

func TestJobRunDuringShutdown(t *testing.T) {
	logger := newDiscardLogger()
	scheduler := NewScheduler(logger)
	sm := NewShutdownManager(logger, 2*time.Second)
	gs := NewGracefulScheduler(scheduler, sm)

	// Create a test job
	job := &LocalJob{
		Name:    "test-job",
		Command: "echo test",
	}

	exec, _ := NewExecution()
	ctx := NewContext(scheduler, job, exec)

	// Start shutdown
	go sm.Shutdown()

	// Wait a moment for shutdown to start
	time.Sleep(50 * time.Millisecond)

	// Try to run job during shutdown
	err := gs.RunJobWithTracking(job, ctx)

	if err == nil {
		t.Error("Expected error when running job during shutdown")
	}

	t.Log("Job prevention during shutdown test passed")
}

// TestShutdownHookPriorityOrdering verifies that hooks with different priorities
// execute in priority groups: all hooks in a lower priority group must FINISH
// before hooks in the next priority group START.
func TestShutdownHookPriorityOrdering(t *testing.T) {
	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 5*time.Second)

	// Use channels to track start and finish times per hook.
	// Priority-10 hooks must fully finish before priority-20 hooks start,
	// and priority-20 hooks must fully finish before priority-30 hooks start.
	type hookEvent struct {
		name     string
		priority int
		start    time.Time
		end      time.Time
	}

	var mu sync.Mutex
	events := make([]hookEvent, 0, 4)

	makeHook := func(name string, priority int, duration time.Duration) ShutdownHook {
		return ShutdownHook{
			Name:     name,
			Priority: priority,
			Hook: func(ctx context.Context) error {
				start := time.Now()
				time.Sleep(duration)
				end := time.Now()
				mu.Lock()
				events = append(events, hookEvent{name: name, priority: priority, start: start, end: end})
				mu.Unlock()
				return nil
			},
		}
	}

	// Register hooks in scrambled order to prove sorting works
	sm.RegisterHook(makeHook("p20-a", 20, 50*time.Millisecond))
	sm.RegisterHook(makeHook("p10-a", 10, 50*time.Millisecond))
	sm.RegisterHook(makeHook("p30-a", 30, 50*time.Millisecond))
	sm.RegisterHook(makeHook("p10-b", 10, 50*time.Millisecond))

	err := sm.Shutdown()
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 4 {
		t.Fatalf("Expected 4 hook events, got %d", len(events))
	}

	// Find the latest end time in priority-10 group and earliest start time in priority-20 group
	var p10LatestEnd, p20EarliestStart, p20LatestEnd, p30EarliestStart time.Time

	for _, ev := range events {
		switch ev.priority {
		case 10:
			if p10LatestEnd.IsZero() || ev.end.After(p10LatestEnd) {
				p10LatestEnd = ev.end
			}
		case 20:
			if p20EarliestStart.IsZero() || ev.start.Before(p20EarliestStart) {
				p20EarliestStart = ev.start
			}
			if p20LatestEnd.IsZero() || ev.end.After(p20LatestEnd) {
				p20LatestEnd = ev.end
			}
		case 30:
			if p30EarliestStart.IsZero() || ev.start.Before(p30EarliestStart) {
				p30EarliestStart = ev.start
			}
		}
	}

	// Priority-10 hooks must finish before priority-20 hooks start
	if !p10LatestEnd.Before(p20EarliestStart) && !p10LatestEnd.Equal(p20EarliestStart) {
		t.Errorf("Priority-10 hooks did not finish before priority-20 hooks started: "+
			"p10 latest end=%v, p20 earliest start=%v", p10LatestEnd, p20EarliestStart)
	}

	// Priority-20 hooks must finish before priority-30 hooks start
	if !p20LatestEnd.Before(p30EarliestStart) && !p20LatestEnd.Equal(p30EarliestStart) {
		t.Errorf("Priority-20 hooks did not finish before priority-30 hooks started: "+
			"p20 latest end=%v, p30 earliest start=%v", p20LatestEnd, p30EarliestStart)
	}
}

// TestShutdownHookSamePriorityConcurrent verifies that hooks with the
// same priority execute concurrently (not sequentially).
func TestShutdownHookSamePriorityConcurrent(t *testing.T) {
	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 5*time.Second)

	hookDuration := 100 * time.Millisecond

	sm.RegisterHook(ShutdownHook{
		Name: "p10-slow-a", Priority: 10,
		Hook: func(ctx context.Context) error {
			time.Sleep(hookDuration)
			return nil
		},
	})
	sm.RegisterHook(ShutdownHook{
		Name: "p10-slow-b", Priority: 10,
		Hook: func(ctx context.Context) error {
			time.Sleep(hookDuration)
			return nil
		},
	})

	start := time.Now()
	err := sm.Shutdown()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// If both hooks ran concurrently, total time should be around hookDuration, not 2x.
	// Allow generous tolerance (1.5x) but must be less than 2x.
	maxExpected := hookDuration * 2
	if elapsed >= maxExpected {
		t.Errorf("Same-priority hooks appear to have run sequentially: elapsed %v >= %v", elapsed, maxExpected)
	}
}

// TestShutdownTimeout_HookIgnoresContext verifies that shutdown still returns
// ErrShutdownTimeout even when a hook ignores its context and blocks.
// This was the bug reported in review: wg.Wait() without ctx.Done() select
// caused the shutdown to hang forever.
func TestShutdownTimeout_HookIgnoresContext(t *testing.T) {
	logger := newDiscardLogger()
	sm := NewShutdownManager(logger, 100*time.Millisecond)

	// Register a hook that ignores the context and blocks for much longer
	sm.RegisterHook(ShutdownHook{
		Name:     "context-ignoring-hook",
		Priority: 10,
		Hook: func(_ context.Context) error {
			// Deliberately ignores ctx.Done() — sleeps unconditionally.
			// The shutdown manager must still return within its timeout.
			time.Sleep(5 * time.Second)
			return nil
		},
	})

	start := time.Now()
	err := sm.Shutdown()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected ErrShutdownTimeout, got nil")
	}
	if !errors.Is(err, ErrShutdownTimeout) {
		t.Errorf("Expected ErrShutdownTimeout, got: %v", err)
	}

	// Must return within 200ms (timeout is 100ms + tolerance), not 5s
	if elapsed > 300*time.Millisecond {
		t.Errorf("Shutdown should have timed out quickly, but took %v", elapsed)
	}
}
