package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestShutdownManager(t *testing.T) {
	logger := &LogrusAdapter{}
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
	logger := &LogrusAdapter{}
	sm := NewShutdownManager(logger, 2*time.Second)
	
	// Track hook execution order
	var executionOrder []string
	
	// Register hooks with different priorities
	sm.RegisterHook(ShutdownHook{
		Name:     "hook2",
		Priority: 20,
		Hook: func(ctx context.Context) error {
			executionOrder = append(executionOrder, "hook2")
			return nil
		},
	})
	
	sm.RegisterHook(ShutdownHook{
		Name:     "hook1",
		Priority: 10,
		Hook: func(ctx context.Context) error {
			executionOrder = append(executionOrder, "hook1")
			return nil
		},
	})
	
	sm.RegisterHook(ShutdownHook{
		Name:     "hook3",
		Priority: 30,
		Hook: func(ctx context.Context) error {
			executionOrder = append(executionOrder, "hook3")
			return nil
		},
	})
	
	// Execute shutdown
	err := sm.Shutdown()
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
	
	// Verify execution order (should be sorted by priority)
	if len(executionOrder) != 3 {
		t.Errorf("Expected 3 hooks executed, got %d", len(executionOrder))
	}
	
	if executionOrder[0] != "hook1" || executionOrder[1] != "hook2" || executionOrder[2] != "hook3" {
		t.Errorf("Hooks executed in wrong order: %v", executionOrder)
	}
	
	if !sm.IsShuttingDown() {
		t.Error("Should be marked as shutting down")
	}
	
	t.Log("Shutdown hooks test passed")
}

func TestShutdownTimeout(t *testing.T) {
	logger := &LogrusAdapter{}
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
	logger := &LogrusAdapter{}
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
	logger := &LogrusAdapter{}
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
	logger := &LogrusAdapter{}
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
	logger := &LogrusAdapter{}
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
	logger := &LogrusAdapter{}
	scheduler := NewScheduler(logger)
	sm := NewShutdownManager(logger, 2*time.Second)
	gs := NewGracefulScheduler(scheduler, sm)
	
	// Create a test job
	job := &LocalJob{
		BareJob: BareJob{
			Name:    "test-job",
			Command: "echo test",
		},
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