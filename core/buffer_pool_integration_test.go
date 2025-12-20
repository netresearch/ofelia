package core

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/netresearch/ofelia/test/testutil"
)

// TestEnhancedBufferPoolIntegration verifies the enhanced buffer pool is properly integrated
func TestEnhancedBufferPoolIntegration(t *testing.T) {
	// Verify DefaultBufferPool is the enhanced version
	if DefaultBufferPool == nil {
		t.Fatal("DefaultBufferPool is nil")
	}

	// Verify it's the correct type
	ebp := DefaultBufferPool
	if ebp == nil {
		t.Fatal("DefaultBufferPool is nil after assignment")
	}

	// Verify configuration
	if ebp.config == nil {
		t.Fatal("Enhanced buffer pool config is nil")
	}

	if ebp.config.DefaultSize != 256*1024 {
		t.Errorf("Expected default size 256KB, got %d", ebp.config.DefaultSize)
	}

	if ebp.config.MaxSize != maxStreamSize {
		t.Errorf("Expected max size %d, got %d", maxStreamSize, ebp.config.MaxSize)
	}
}

// TestEnhancedBufferPoolGetPut verifies Get/Put operations work correctly
func TestEnhancedBufferPoolGetPut(t *testing.T) {
	// Get a buffer
	buf, err := DefaultBufferPool.Get()
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if buf == nil {
		t.Fatal("Get() returned nil buffer")
	}

	// Verify size
	if buf.Size() != 256*1024 {
		t.Errorf("Expected buffer size 256KB, got %d", buf.Size())
	}

	// Write some data
	testData := []byte("test data")
	n, err := buf.Write(testData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(testData), n)
	}

	// Put it back
	DefaultBufferPool.Put(buf)

	// Get another one (should be the same from pool after Reset)
	buf2, err := DefaultBufferPool.Get()
	if err != nil {
		t.Fatalf("Get() error on second call: %v", err)
	}
	if buf2 == nil {
		t.Fatal("Get() returned nil buffer on second call")
	}

	// Verify it was reset
	if buf2.TotalWritten() != 0 {
		t.Errorf("Buffer was not reset, TotalWritten=%d", buf2.TotalWritten())
	}
}

// TestEnhancedBufferPoolGetSized verifies sized buffer allocation
func TestEnhancedBufferPoolGetSized(t *testing.T) {
	tests := []struct {
		name          string
		requestedSize int64
		minExpected   int64
	}{
		{"Small buffer", 1024, 1024},
		{"Default size", 256 * 1024, 256 * 1024},
		{"Large buffer", 1024 * 1024, 512 * 1024}, // Should round up
		{"Max buffer", maxStreamSize, maxStreamSize},
		{"Over max", maxStreamSize * 2, maxStreamSize}, // Should cap at max
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := DefaultBufferPool.GetSized(tt.requestedSize)
			if err != nil {
				t.Fatalf("GetSized() error: %v", err)
			}
			if buf == nil {
				t.Fatal("GetSized() returned nil")
			}

			if buf.Size() < tt.minExpected {
				t.Errorf("Expected buffer size >= %d, got %d", tt.minExpected, buf.Size())
			}

			DefaultBufferPool.Put(buf)
		})
	}
}

// TestEnhancedBufferPoolMetrics verifies metrics are being tracked
func TestEnhancedBufferPoolMetrics(t *testing.T) {
	t.Parallel()
	// Create a new pool with metrics enabled for testing
	config := DefaultEnhancedBufferPoolConfig()
	config.EnableMetrics = true
	ebp := NewEnhancedBufferPool(config, nil)

	// Reset metrics to start fresh
	atomic.StoreInt64(&ebp.totalGets, 0)
	atomic.StoreInt64(&ebp.totalPuts, 0)
	atomic.StoreInt64(&ebp.totalMisses, 0)

	// Perform some operations
	buf1, _ := ebp.Get()
	buf2, _ := ebp.Get()
	buf3, _ := ebp.GetSized(512 * 1024)

	ebp.Put(buf1)
	ebp.Put(buf2)
	ebp.Put(buf3)

	// Verify metrics
	stats := ebp.GetStats()
	if stats == nil {
		t.Fatal("GetStats() returned nil")
	}

	totalGets, ok := stats["total_gets"].(int64)
	if !ok || totalGets != 3 {
		t.Errorf("Expected total_gets=3, got %v", stats["total_gets"])
	}

	totalPuts, ok := stats["total_puts"].(int64)
	if !ok || totalPuts != 3 {
		t.Errorf("Expected total_puts=3, got %v", stats["total_puts"])
	}

	poolCount, ok := stats["pool_count"].(int)
	if !ok || poolCount == 0 {
		t.Errorf("Expected pool_count>0, got %v", stats["pool_count"])
	}

	ebp.Shutdown()
}

// TestEnhancedBufferPoolConcurrent verifies thread safety
func TestEnhancedBufferPoolConcurrent(t *testing.T) {
	const goroutines = 100
	const iterations = 100

	done := make(chan bool, goroutines)

	for range goroutines {
		go func() {
			for range iterations {
				buf, err := DefaultBufferPool.Get()
				if err != nil {
					t.Errorf("Get() error: %v", err)
					continue
				}
				buf.Write([]byte("concurrent test"))
				DefaultBufferPool.Put(buf)
			}
			done <- true
		}()
	}

	// Wait for all goroutines with timeout
	timeout := time.After(10 * time.Second)
	for range goroutines {
		select {
		case <-done:
			// Success
		case <-timeout:
			t.Fatal("Concurrent test timed out")
		}
	}

	// Verify pool still works
	buf, err := DefaultBufferPool.Get()
	if err != nil {
		t.Fatalf("Get() error after concurrent access: %v", err)
	}
	if buf == nil {
		t.Fatal("Pool broken after concurrent access")
	}
	DefaultBufferPool.Put(buf)
}

// TestEnhancedBufferPoolPrewarming verifies pool pre-warming
func TestEnhancedBufferPoolPrewarming(t *testing.T) {
	t.Parallel()
	// Create a new pool with pre-warming enabled
	config := DefaultEnhancedBufferPoolConfig()
	config.EnablePrewarming = true
	config.PoolSize = 10

	ebp := NewEnhancedBufferPool(config, nil)

	// Get stats - if pre-warming worked, first Get should be a hit not a miss
	stats := ebp.GetStats()
	initialMisses := stats["total_misses"].(int64)

	// Get a buffer
	buf, err := ebp.Get()
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if buf == nil {
		t.Fatal("Get() returned nil")
	}

	// Verify it came from pre-warmed pool (no new allocation)
	stats = ebp.GetStats()
	newMisses := stats["total_misses"].(int64)

	if newMisses > initialMisses {
		t.Errorf("Expected pre-warmed buffer (no miss), got miss count increase from %d to %d",
			initialMisses, newMisses)
	}

	ebp.Put(buf)
	ebp.Shutdown()
}

func TestEnhancedBufferPoolShutdown(t *testing.T) {
	t.Parallel()
	config := DefaultEnhancedBufferPoolConfig()
	config.ShrinkInterval = 10 * time.Millisecond

	ebp := NewEnhancedBufferPool(config, nil)
	ebp.Shutdown()

	testutil.Eventually(t, func() bool {
		stats := ebp.GetStats()
		return stats["pool_count"].(int) == 0
	}, testutil.WithTimeout(200*time.Millisecond), testutil.WithInterval(5*time.Millisecond))
}

// BenchmarkEnhancedBufferPoolVsSimple compares enhanced vs simple pool performance
func BenchmarkEnhancedBufferPoolVsSimple(b *testing.B) {
	b.Run("Enhanced", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf, err := DefaultBufferPool.Get()
				if err != nil {
					b.Fatalf("Get() error: %v", err)
				}
				buf.Write([]byte("benchmark data"))
				DefaultBufferPool.Put(buf)
			}
		})
	})

	b.Run("Simple", func(b *testing.B) {
		simplePool := NewBufferPool(1024, 256*1024, maxStreamSize)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf, err := simplePool.Get()
				if err != nil {
					b.Fatalf("Get() error: %v", err)
				}
				buf.Write([]byte("benchmark data"))
				simplePool.Put(buf)
			}
		})
	})
}
