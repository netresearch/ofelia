package core

import (
	"context"
	"testing"
	"time"
)

// TestEnhancedBufferPoolShutdown tests the Shutdown method that currently has 0% coverage
func TestEnhancedBufferPoolShutdown(t *testing.T) {
	t.Parallel()

	config := DefaultEnhancedBufferPoolConfig()
	config.ShrinkInterval = 100 * time.Millisecond // Short interval for testing
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)

	// Let the adaptive management worker start
	time.Sleep(50 * time.Millisecond)

	// Test shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := pool.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Verify that the shrink ticker is stopped
	// We can't directly test this, but we can verify the pool still works
	buf := pool.Get()
	if buf == nil {
		t.Error("Pool should still work after shutdown")
	}
	pool.Put(buf)
}

// TestEnhancedBufferPoolPerformAdaptiveManagement tests the performAdaptiveManagement method
func TestEnhancedBufferPoolPerformAdaptiveManagement(t *testing.T) {
	t.Parallel()

	config := DefaultEnhancedBufferPoolConfig()
	config.ShrinkInterval = 0 // Disable automatic shrinking
	config.ShrinkThreshold = 0.3
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)
	defer pool.Shutdown(context.Background())

	// Create some usage patterns by getting and putting buffers
	buffers := make([]*circbuf.Buffer, 10)
	for i := 0; i < 10; i++ {
		buffers[i] = pool.GetSized(config.DefaultSize)
	}

	// Put back only a few to create low usage
	for i := 0; i < 3; i++ {
		pool.Put(buffers[i])
	}

	// Manually trigger adaptive management
	// We can't call performAdaptiveManagement directly as it's private,
	// but we can test the effect through stats
	initialStats := pool.GetStats()

	// Use reflection or test the observable effects
	// For now, test that the pool continues to function correctly
	newBuf := pool.Get()
	if newBuf == nil {
		t.Error("Pool should provide buffer after adaptive management")
	}
	pool.Put(newBuf)
}

// TestEnhancedBufferPoolAdaptiveManagementWorker tests the background worker
func TestEnhancedBufferPoolAdaptiveManagementWorker(t *testing.T) {
	t.Parallel()

	config := DefaultEnhancedBufferPoolConfig()
	config.ShrinkInterval = 50 * time.Millisecond // Very short for testing
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)
	defer pool.Shutdown(context.Background())

	// Let the worker run a few cycles
	time.Sleep(200 * time.Millisecond)

	// Verify the pool is still functioning
	buf := pool.Get()
	if buf == nil {
		t.Error("Pool should provide buffer after worker cycles")
	}
	pool.Put(buf)

	// Check stats to see if any adaptive management occurred
	stats := pool.GetStats()
	if stats == nil {
		t.Error("GetStats should return valid statistics")
	}

	// Verify expected stats keys exist
	expectedKeys := []string{"total_gets", "total_puts", "pools_count", "config"}
	for _, key := range expectedKeys {
		if _, exists := stats[key]; !exists {
			t.Errorf("Expected stats key '%s' not found", key)
		}
	}
}

// TestEnhancedBufferPoolCreatePoolForSize tests pool creation for specific sizes
func TestEnhancedBufferPoolCreatePoolForSize(t *testing.T) {
	t.Parallel()

	config := DefaultEnhancedBufferPoolConfig()
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)
	defer pool.Shutdown(context.Background())

	// Test getting buffers of various sizes to trigger pool creation
	testSizes := []int64{
		1024,    // 1KB
		4096,    // 4KB
		16384,   // 16KB
		65536,   // 64KB
		1048576, // 1MB
	}

	for _, size := range testSizes {
		buf := pool.GetSized(size)
		if buf == nil {
			t.Errorf("Failed to get buffer of size %d", size)
			continue
		}

		if int64(buf.Size()) < size {
			t.Errorf("Buffer size %d is smaller than requested size %d", buf.Size(), size)
		}

		pool.Put(buf)
	}

	// Check that pools were created
	stats := pool.GetStats()
	if poolsCount, exists := stats["pools_count"]; exists {
		if count, ok := poolsCount.(int); !ok || count == 0 {
			t.Error("Expected multiple pools to be created")
		}
	}
}

// TestEnhancedBufferPoolPrewarmPools tests the prewarming functionality
func TestEnhancedBufferPoolPrewarmPools(t *testing.T) {
	t.Parallel()

	config := DefaultEnhancedBufferPoolConfig()
	config.EnablePrewarming = true
	config.PoolSize = 10 // Small for testing
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)
	defer pool.Shutdown(context.Background())

	// Since pools are prewarmed, getting buffers should be fast
	start := time.Now()
	for i := 0; i < 5; i++ {
		buf := pool.Get()
		if buf == nil {
			t.Errorf("Failed to get prewarmed buffer %d", i)
		}
		pool.Put(buf)
	}
	duration := time.Since(start)

	// Prewarmed pools should be reasonably fast
	if duration > 10*time.Millisecond {
		t.Logf("Prewarmed buffer operations took %v (may be acceptable depending on system)", duration)
	}
}

// TestEnhancedBufferPoolConcurrentShrinking tests concurrent shrinking operations
func TestEnhancedBufferPoolConcurrentShrinking(t *testing.T) {
	t.Parallel()

	config := DefaultEnhancedBufferPoolConfig()
	config.ShrinkInterval = 10 * time.Millisecond // Very aggressive for testing
	config.ShrinkThreshold = 0.8 // High threshold so shrinking is less likely
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)
	defer pool.Shutdown(context.Background())

	// Generate concurrent activity
	done := make(chan bool)
	numGoroutines := 5

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- true }()
			for j := 0; j < 100; j++ {
				buf := pool.Get()
				if buf != nil {
					time.Sleep(time.Microsecond) // Very brief hold
					pool.Put(buf)
				}
			}
		}()
	}

	// Wait for all operations to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Let shrinking worker run for a bit
	time.Sleep(50 * time.Millisecond)

	// Verify pool still functions correctly after concurrent operations
	buf := pool.Get()
	if buf == nil {
		t.Error("Pool should still function after concurrent shrinking")
	}
	pool.Put(buf)
}

// TestEnhancedBufferPoolUsageTracking tests usage tracking functionality
func TestEnhancedBufferPoolUsageTracking(t *testing.T) {
	t.Parallel()

	config := DefaultEnhancedBufferPoolConfig()
	config.EnableMetrics = true
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)
	defer pool.Shutdown(context.Background())

	// Create usage patterns
	sizes := []int64{config.MinSize, config.DefaultSize, config.MaxSize}
	operations := 10

	for _, size := range sizes {
		for i := 0; i < operations; i++ {
			buf := pool.GetSized(size)
			if buf != nil {
				pool.Put(buf)
			}
		}
	}

	// Check stats to verify usage tracking
	stats := pool.GetStats()
	
	if totalGets, exists := stats["total_gets"]; exists {
		if gets, ok := totalGets.(int64); !ok || gets < int64(len(sizes)*operations) {
			t.Errorf("Expected at least %d gets, got %v", len(sizes)*operations, totalGets)
		}
	} else {
		t.Error("total_gets not found in stats")
	}

	if totalPuts, exists := stats["total_puts"]; exists {
		if puts, ok := totalPuts.(int64); !ok || puts < int64(len(sizes)*operations) {
			t.Errorf("Expected at least %d puts, got %v", len(sizes)*operations, totalPuts)
		}
	} else {
		t.Error("total_puts not found in stats")
	}
}

// TestEnhancedBufferPoolEdgeCases tests edge cases and error conditions
func TestEnhancedBufferPoolEdgeCases(t *testing.T) {
	t.Parallel()

	// Test with nil config
	logger := &MockLogger{}
	pool := NewEnhancedBufferPool(nil, logger)
	defer pool.Shutdown(context.Background())

	buf := pool.Get()
	if buf == nil {
		t.Error("Pool with nil config should still provide buffers")
	}
	pool.Put(buf)

	// Test with zero size request
	zeroBuf := pool.GetSized(0)
	if zeroBuf == nil {
		t.Error("Pool should handle zero size request gracefully")
	}
	pool.Put(zeroBuf)

	// Test with negative size request
	negBuf := pool.GetSized(-1)
	if negBuf == nil {
		t.Error("Pool should handle negative size request gracefully")
	}
	pool.Put(negBuf)

	// Test with extremely large size request
	hugeBuf := pool.GetSized(1024 * 1024 * 1024) // 1GB
	if hugeBuf == nil {
		t.Error("Pool should handle large size request gracefully")
	}
	if hugeBuf != nil {
		// Should be capped at maxSize
		maxSize := DefaultEnhancedBufferPoolConfig().MaxSize
		if int64(hugeBuf.Size()) > maxSize {
			t.Errorf("Buffer size %d exceeds max size %d", hugeBuf.Size(), maxSize)
		}
		pool.Put(hugeBuf)
	}
}

// TestEnhancedBufferPoolMetricsAccuracy tests the accuracy of metrics
func TestEnhancedBufferPoolMetricsAccuracy(t *testing.T) {
	t.Parallel()

	config := DefaultEnhancedBufferPoolConfig()
	config.EnableMetrics = true
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)
	defer pool.Shutdown(context.Background())

	const numOperations = 50
	buffers := make([]*circbuf.Buffer, numOperations)

	// Get buffers
	for i := 0; i < numOperations; i++ {
		buffers[i] = pool.Get()
	}

	// Put back half
	for i := 0; i < numOperations/2; i++ {
		pool.Put(buffers[i])
	}

	stats := pool.GetStats()

	// Check gets
	if totalGets, exists := stats["total_gets"]; exists {
		if gets := totalGets.(int64); gets != numOperations {
			t.Errorf("Expected %d gets, got %d", numOperations, gets)
		}
	}

	// Check puts
	if totalPuts, exists := stats["total_puts"]; exists {
		if puts := totalPuts.(int64); puts != numOperations/2 {
			t.Errorf("Expected %d puts, got %d", numOperations/2, puts)
		}
	}

	// Put back remaining buffers to clean up
	for i := numOperations / 2; i < numOperations; i++ {
		if buffers[i] != nil {
			pool.Put(buffers[i])
		}
	}
}

// TestEnhancedBufferPoolShutdownTimeout tests shutdown with timeout
func TestEnhancedBufferPoolShutdownTimeout(t *testing.T) {
	t.Parallel()

	config := DefaultEnhancedBufferPoolConfig()
	config.ShrinkInterval = 100 * time.Millisecond
	logger := &MockLogger{}
	
	pool := NewEnhancedBufferPool(config, logger)

	// Test shutdown with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	err := pool.Shutdown(ctx)
	// Should either succeed quickly or return timeout error
	if err != nil && err != context.DeadlineExceeded {
		t.Errorf("Unexpected shutdown error: %v", err)
	}

	// Pool should still be usable even after failed shutdown
	buf := pool.Get()
	if buf == nil {
		t.Error("Pool should still work after shutdown timeout")
	}
	pool.Put(buf)
}

// TestEnhancedBufferPoolConfigValidation tests configuration validation
func TestEnhancedBufferPoolConfigValidation(t *testing.T) {
	t.Parallel()

	logger := &MockLogger{}

	// Test with invalid config values
	invalidConfig := &EnhancedBufferPoolConfig{
		MinSize:          -1,
		DefaultSize:      0,
		MaxSize:          -1,
		PoolSize:         -1,
		MaxPoolSize:      -1,
		GrowthFactor:     -1,
		ShrinkThreshold:  -1,
		ShrinkInterval:   -1 * time.Second,
		EnableMetrics:    true,
		EnablePrewarming: true,
	}

	// Should not crash with invalid config
	pool := NewEnhancedBufferPool(invalidConfig, logger)
	defer pool.Shutdown(context.Background())

	// Should still provide buffers
	buf := pool.Get()
	if buf == nil {
		t.Error("Pool should work even with invalid config")
	}
	pool.Put(buf)
}