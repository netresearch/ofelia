package core

import (
	"runtime"
	"sync"
	"testing"

	"github.com/armon/circbuf"
)

// BenchmarkExecutionMemoryWithPool benchmarks memory usage with buffer pooling
func BenchmarkExecutionMemoryWithPool(b *testing.B) {
	// Force GC to get clean baseline
	runtime.GC()
	runtime.GC()

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		e, err := NewExecution()
		if err != nil {
			b.Fatal(err)
		}

		// Simulate some work
		e.OutputStream.Write([]byte("test output"))
		e.ErrorStream.Write([]byte("test error"))

		// Clean up - returns buffers to pool
		e.Cleanup()
	}

	b.StopTimer()

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	allocatedBytes := memAfter.Alloc - memBefore.Alloc
	b.ReportMetric(float64(allocatedBytes)/float64(b.N), "bytes/op")
	b.ReportMetric(float64(memAfter.Mallocs-memBefore.Mallocs)/float64(b.N), "allocs/op")
}

// BenchmarkExecutionMemoryWithoutPool benchmarks memory usage without pooling (old way)
func BenchmarkExecutionMemoryWithoutPool(b *testing.B) {
	// Force GC to get clean baseline
	runtime.GC()
	runtime.GC()

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Simulate old way - direct allocation
		bufOut, _ := circbuf.NewBuffer(maxStreamSize)
		bufErr, _ := circbuf.NewBuffer(maxStreamSize)

		// Simulate some work
		bufOut.Write([]byte("test output"))
		bufErr.Write([]byte("test error"))

		// No cleanup in old version - relies on GC
	}

	b.StopTimer()

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	allocatedBytes := memAfter.Alloc - memBefore.Alloc
	b.ReportMetric(float64(allocatedBytes)/float64(b.N), "bytes/op")
	b.ReportMetric(float64(memAfter.Mallocs-memBefore.Mallocs)/float64(b.N), "allocs/op")
}

// TestMemoryUsageComparison provides a direct comparison
func TestMemoryUsageComparison(t *testing.T) {
	const iterations = 100

	// Test OLD way (without pool)
	runtime.GC()
	runtime.GC()
	var memOldBefore runtime.MemStats
	runtime.ReadMemStats(&memOldBefore)

	// Keep references to prevent GC
	oldBuffers := make([]*circbuf.Buffer, 0, iterations*2)

	for i := 0; i < iterations; i++ {
		bufOut, _ := circbuf.NewBuffer(maxStreamSize) // 10MB
		bufErr, _ := circbuf.NewBuffer(maxStreamSize) // 10MB
		bufOut.Write([]byte("test"))
		bufErr.Write([]byte("test"))
		oldBuffers = append(oldBuffers, bufOut, bufErr)
	}

	var memOldAfter runtime.MemStats
	runtime.ReadMemStats(&memOldAfter)
	oldAllocated := memOldAfter.TotalAlloc - memOldBefore.TotalAlloc

	// Test NEW way (with pool)
	runtime.GC()
	runtime.GC()
	var memNewBefore runtime.MemStats
	runtime.ReadMemStats(&memNewBefore)

	for i := 0; i < iterations; i++ {
		e, _ := NewExecution()
		e.OutputStream.Write([]byte("test"))
		e.ErrorStream.Write([]byte("test"))
		e.Cleanup()
	}

	var memNewAfter runtime.MemStats
	runtime.ReadMemStats(&memNewAfter)
	newAllocated := memNewAfter.TotalAlloc - memNewBefore.TotalAlloc

	// Calculate improvement
	var improvement float64
	if oldAllocated > 0 {
		improvement = float64(oldAllocated-newAllocated) / float64(oldAllocated) * 100
	}

	t.Logf("Memory Usage Comparison for %d executions:", iterations)
	t.Logf("OLD (without pool): %d bytes (%.2f MB)", oldAllocated, float64(oldAllocated)/1024/1024)
	t.Logf("NEW (with pool):    %d bytes (%.2f MB)", newAllocated, float64(newAllocated)/1024/1024)
	t.Logf("Improvement:        %.2f%% reduction", improvement)
	t.Logf("Per execution OLD:  %.2f MB", float64(oldAllocated)/float64(iterations)/1024/1024)
	t.Logf("Per execution NEW:  %.2f MB", float64(newAllocated)/float64(iterations)/1024/1024)

	// Ensure we have significant improvement
	if improvement < 50 {
		t.Errorf("Expected at least 50%% memory reduction, got %.2f%%", improvement)
	}
}

// TestBufferPoolConcurrency ensures pool works correctly under concurrent load
func TestBufferPoolConcurrency(t *testing.T) {
	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Track memory before
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				e, err := NewExecution()
				if err != nil {
					t.Error(err)
					return
				}

				// Simulate work
				e.OutputStream.Write([]byte("concurrent test"))
				e.ErrorStream.Write([]byte("concurrent error"))

				// Return to pool
				e.Cleanup()
			}
		}()
	}

	wg.Wait()

	// Check memory after
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	totalOps := goroutines * iterations
	bytesPerOp := float64(memAfter.Alloc-memBefore.Alloc) / float64(totalOps)

	t.Logf("Concurrent test: %d goroutines, %d iterations each", goroutines, iterations)
	t.Logf("Memory per operation: %.2f bytes", bytesPerOp)

	// With pooling, memory per op should be very low
	if bytesPerOp > 10000 { // 10KB max per operation
		t.Errorf("Memory usage too high under concurrent load: %.2f bytes/op", bytesPerOp)
	}
}
