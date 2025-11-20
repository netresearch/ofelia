package core

import (
	"runtime"
	"testing"
)

// TestMemoryRegressionPrevention ensures memory usage stays optimized
// This test will fail if someone accidentally removes buffer pooling
func TestMemoryRegressionPrevention(t *testing.T) {
	const iterations = 50

	// Get baseline memory
	runtime.GC()
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Create and cleanup executions
	for i := 0; i < iterations; i++ {
		e, err := NewExecution()
		if err != nil {
			t.Fatal(err)
		}

		// Simulate typical usage
		e.OutputStream.Write([]byte("test output data"))
		e.ErrorStream.Write([]byte("test error data"))

		// Must cleanup to return buffers to pool
		e.Cleanup()
	}

	// Check memory after
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	totalAllocated := memAfter.TotalAlloc - memBefore.TotalAlloc
	perOperation := float64(totalAllocated) / float64(iterations)
	perOperationMB := perOperation / 1024 / 1024

	t.Logf("Memory regression test: %.4f MB per execution", perOperationMB)

	// REGRESSION CHECK: Should be much less than 1MB per operation
	// (With pooling it should be ~0.01 MB, without pooling it would be ~20 MB)
	maxAllowedMB := 1.0
	if perOperationMB > maxAllowedMB {
		t.Errorf("MEMORY REGRESSION DETECTED! Using %.4f MB per execution (max allowed: %.2f MB). "+
			"Buffer pooling may have been removed or broken.", perOperationMB, maxAllowedMB)
	}
}

// TestSchedulerConcurrencyLimit ensures job concurrency limiting works
func TestSchedulerConcurrencyLimit(t *testing.T) {
	// Use a simple logger implementation for testing
	s := NewScheduler(&LogrusAdapter{})

	// Verify default limit is set
	if s.maxConcurrentJobs == 0 {
		t.Error("Scheduler must have a default concurrency limit")
	}

	if s.jobSemaphore == nil {
		t.Error("Scheduler must initialize job semaphore")
	}

	// Test setting custom limit
	s.SetMaxConcurrentJobs(5)
	if s.maxConcurrentJobs != 5 {
		t.Errorf("Expected max concurrent jobs to be 5, got %d", s.maxConcurrentJobs)
	}

	// Test minimum limit enforcement
	s.SetMaxConcurrentJobs(0)
	if s.maxConcurrentJobs != 1 {
		t.Errorf("SetMaxConcurrentJobs(0) should set limit to 1, got %d", s.maxConcurrentJobs)
	}
}

// TestBufferPoolExists ensures buffer pool is properly initialized
func TestBufferPoolExists(t *testing.T) {
	if DefaultBufferPool == nil {
		t.Fatal("DefaultBufferPool must be initialized")
	}

	// Test that pool returns buffers
	buf, err := DefaultBufferPool.Get()
	if err != nil {
		t.Fatalf("Buffer pool Get() error: %v", err)
	}
	if buf == nil {
		t.Fatal("Buffer pool must return valid buffers")
	}

	// Test that buffer has reasonable size (not 10MB)
	if buf.Size() > 1024*1024 { // 1MB
		t.Errorf("Default buffer size is too large: %d bytes (should be optimized)", buf.Size())
	}

	// Return buffer to pool
	DefaultBufferPool.Put(buf)
}

// TestExecutionCleanup ensures Cleanup method exists and works
func TestExecutionCleanup(t *testing.T) {
	e, err := NewExecution()
	if err != nil {
		t.Fatal(err)
	}

	// Write some data
	e.OutputStream.Write([]byte("test"))
	e.ErrorStream.Write([]byte("test"))

	// Cleanup should work without panic
	e.Cleanup()

	// After cleanup, buffers should be nil
	if e.OutputStream != nil {
		t.Error("OutputStream should be nil after Cleanup")
	}
	if e.ErrorStream != nil {
		t.Error("ErrorStream should be nil after Cleanup")
	}

	// Double cleanup should not panic
	e.Cleanup()
}
