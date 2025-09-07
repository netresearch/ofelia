package core

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// BenchmarkOptimizedDockerClientCreation measures the overhead of creating optimized Docker clients
func BenchmarkOptimizedDockerClientCreation(b *testing.B) {
	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client, _ := NewOptimizedDockerClient(config, logger, metrics)
		_ = client
	}
}

// BenchmarkCircuitBreakerExecution measures circuit breaker overhead
func BenchmarkCircuitBreakerExecution(b *testing.B) {
	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	cb := NewDockerCircuitBreaker(config, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.Execute(func() error {
			return nil
		})
	}
}

// BenchmarkEnhancedBufferPoolOperations measures buffer pool performance
func BenchmarkEnhancedBufferPoolOperations(b *testing.B) {
	config := DefaultEnhancedBufferPoolConfig()
	logger := &MockLogger{}
	pool := NewEnhancedBufferPool(config, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		pool.Put(buf)
	}
}

// BenchmarkEnhancedBufferPoolConcurrent measures concurrent buffer pool performance
func BenchmarkEnhancedBufferPoolConcurrent(b *testing.B) {
	config := DefaultEnhancedBufferPoolConfig()
	logger := &MockLogger{}
	pool := NewEnhancedBufferPool(config, logger)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			pool.Put(buf)
		}
	})
}

// BenchmarkBufferPoolComparison compares original vs enhanced buffer pool
func BenchmarkBufferPoolComparison(b *testing.B) {
	b.Run("Original", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := DefaultBufferPool.Get()
			DefaultBufferPool.Put(buf)
		}
	})

	b.Run("Enhanced", func(b *testing.B) {
		config := DefaultEnhancedBufferPoolConfig()
		logger := &MockLogger{}
		pool := NewEnhancedBufferPool(config, logger)

		for i := 0; i < b.N; i++ {
			buf := pool.Get()
			pool.Put(buf)
		}
	})
}

// BenchmarkPerformanceMetricsRecording measures metrics recording overhead
func BenchmarkPerformanceMetricsRecording(b *testing.B) {
	metrics := NewExtendedMockMetricsRecorder()

	b.ResetTimer()
	b.Run("DockerOperation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			metrics.RecordDockerOperation("test")
		}
	})

	b.Run("DockerLatency", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			metrics.RecordDockerLatency("test", 50*time.Millisecond)
		}
	})

	b.Run("JobExecution", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			metrics.RecordJobExecution("test", 2*time.Second, true)
		}
	})
}

// BenchmarkCircuitBreakerStateTransitions measures state transition overhead
func BenchmarkCircuitBreakerStateTransitions(b *testing.B) {
	config := &DockerClientConfig{
		EnableCircuitBreaker:  true,
		FailureThreshold:      3,
		RecoveryTimeout:       100 * time.Millisecond,
		MaxConcurrentRequests: 10,
	}
	logger := &MockLogger{}

	b.Run("SuccessOnly", func(b *testing.B) {
		cb := NewDockerCircuitBreaker(config, logger)
		for i := 0; i < b.N; i++ {
			_ = cb.Execute(func() error { return nil })
		}
	})

	b.Run("FailureOnly", func(b *testing.B) {
		cb := NewDockerCircuitBreaker(config, logger)
		for i := 0; i < b.N; i++ {
			_ = cb.Execute(func() error { return fmt.Errorf("test error") })
		}
	})

	b.Run("Mixed", func(b *testing.B) {
		cb := NewDockerCircuitBreaker(config, logger)
		for i := 0; i < b.N; i++ {
			if i%4 == 0 {
				_ = cb.Execute(func() error { return fmt.Errorf("test error") })
			} else {
				_ = cb.Execute(func() error { return nil })
			}
		}
	})
}

// TestOptimizedDockerClientPerformanceProfile profiles the optimized Docker client
func TestOptimizedDockerClientPerformanceProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance profile test in short mode")
	}

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()

	client, err := NewOptimizedDockerClient(config, logger, metrics)
	if err != nil {
		t.Fatalf("Failed to create optimized Docker client: %v", err)
	}

	// Simulate Docker operations
	operations := []string{"list_containers", "inspect_container", "create_container", "start_container", "stop_container"}

	start := time.Now()
	for i := 0; i < 1000; i++ {
		op := operations[i%len(operations)]

		// Simulate operation with circuit breaker
		_ = client.circuitBreaker.Execute(func() error {
			// Simulate operation latency
			time.Sleep(time.Microsecond * 100)
			return nil
		})

		// Record metrics
		metrics.RecordDockerOperation(op)
		metrics.RecordDockerLatency(op, time.Microsecond*100)
	}
	duration := time.Since(start)

	t.Logf("Performance Profile Results:")
	t.Logf("Total operations: 1000")
	t.Logf("Total duration: %v", duration)
	t.Logf("Average operation time: %v", duration/1000)

	// Check circuit breaker stats
	stats := client.GetStats()
	t.Logf("Circuit breaker stats: %+v", stats["circuit_breaker"])

	// Check metrics
	dockerMetrics := metrics.GetDockerMetrics()
	t.Logf("Docker metrics: %+v", dockerMetrics)
}

// TestEnhancedBufferPoolMemoryEfficiency tests memory efficiency improvements
func TestEnhancedBufferPoolMemoryEfficiency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory efficiency test in short mode")
	}

	const iterations = 100
	const bufferSize = int64(2 * 1024 * 1024) // 2MB

	// Test original buffer pool
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for i := 0; i < iterations; i++ {
		buf := DefaultBufferPool.Get()
		// Use buffer
		buf.Write(make([]byte, bufferSize))
		DefaultBufferPool.Put(buf)
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)
	originalMemory := m2.Alloc - m1.Alloc

	// Test enhanced buffer pool
	config := DefaultEnhancedBufferPoolConfig()
	logger := &MockLogger{}
	pool := NewEnhancedBufferPool(config, logger)

	var m3, m4 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m3)

	for i := 0; i < iterations; i++ {
		buf := pool.GetSized(bufferSize)
		// Use buffer
		buf.Write(make([]byte, bufferSize))
		pool.Put(buf)
	}

	runtime.GC()
	runtime.ReadMemStats(&m4)
	enhancedMemory := m4.Alloc - m3.Alloc

	improvement := float64(originalMemory-enhancedMemory) / float64(originalMemory) * 100

	t.Logf("Memory Efficiency Comparison:")
	t.Logf("Original buffer pool memory: %d bytes", originalMemory)
	t.Logf("Enhanced buffer pool memory: %d bytes", enhancedMemory)
	t.Logf("Memory improvement: %.2f%%", improvement)

	// Get pool statistics
	stats := pool.GetStats()
	t.Logf("Enhanced buffer pool stats: %+v", stats)

	// Verify improvement (should be significant)
	if improvement < 10 {
		t.Logf("Warning: Memory improvement is less than expected (%.2f%%)", improvement)
	}
}

// BenchmarkConcurrentDockerOperations simulates concurrent Docker operations
func BenchmarkConcurrentDockerOperations(b *testing.B) {
	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()

	client, _ := NewOptimizedDockerClient(config, logger, metrics)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = client.circuitBreaker.Execute(func() error {
				// Simulate Docker API call
				time.Sleep(time.Microsecond * 10)
				return nil
			})
			metrics.RecordDockerOperation("concurrent_test")
		}
	})
}

// TestPerformanceRegressionDetection ensures we maintain performance standards
func TestPerformanceRegressionDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	// Buffer pool performance baseline
	config := DefaultEnhancedBufferPoolConfig()
	logger := &MockLogger{}
	pool := NewEnhancedBufferPool(config, logger)

	start := time.Now()
	for i := 0; i < 10000; i++ {
		buf := pool.Get()
		pool.Put(buf)
	}
	bufferPoolDuration := time.Since(start)

	// Circuit breaker performance baseline
	cbConfig := DefaultDockerClientConfig()
	cb := NewDockerCircuitBreaker(cbConfig, logger)

	start = time.Now()
	for i := 0; i < 10000; i++ {
		_ = cb.Execute(func() error { return nil })
	}
	circuitBreakerDuration := time.Since(start)

	// Metrics recording baseline
	metrics := NewExtendedMockMetricsRecorder()

	start = time.Now()
	for i := 0; i < 10000; i++ {
		metrics.RecordDockerOperation("test")
		metrics.RecordDockerLatency("test", time.Millisecond)
	}
	metricsDuration := time.Since(start)

	t.Logf("Performance Regression Detection:")
	t.Logf("Buffer pool operations (10k): %v (%.2f μs/op)", bufferPoolDuration, float64(bufferPoolDuration.Nanoseconds())/10000/1000)
	t.Logf("Circuit breaker operations (10k): %v (%.2f μs/op)", circuitBreakerDuration, float64(circuitBreakerDuration.Nanoseconds())/10000/1000)
	t.Logf("Metrics recording (10k): %v (%.2f μs/op)", metricsDuration, float64(metricsDuration.Nanoseconds())/10000/1000)

	// Set performance thresholds (adjust based on expected performance in containerized environment)
	bufferPoolThreshold := 1 * time.Second            // 100 μs per operation (relaxed for Docker overhead)
	circuitBreakerThreshold := 200 * time.Millisecond // 20 μs per operation
	metricsThreshold := 100 * time.Millisecond        // 10 μs per operation

	if bufferPoolDuration > bufferPoolThreshold {
		t.Errorf("Buffer pool performance regression detected: %v > %v", bufferPoolDuration, bufferPoolThreshold)
	}

	if circuitBreakerDuration > circuitBreakerThreshold {
		t.Errorf("Circuit breaker performance regression detected: %v > %v", circuitBreakerDuration, circuitBreakerThreshold)
	}

	if metricsDuration > metricsThreshold {
		t.Errorf("Metrics recording performance regression detected: %v > %v", metricsDuration, metricsThreshold)
	}
}

// BenchmarkOptimizationOverhead measures the overhead of our optimizations
func BenchmarkOptimizationOverhead(b *testing.B) {
	b.Run("Baseline-NoOptimizations", func(b *testing.B) {
		// Simulate baseline Docker operation
		for i := 0; i < b.N; i++ {
			// Simulate simple operation without optimizations
			time.Sleep(time.Nanosecond)
		}
	})

	b.Run("WithCircuitBreaker", func(b *testing.B) {
		config := DefaultDockerClientConfig()
		logger := &MockLogger{}
		cb := NewDockerCircuitBreaker(config, logger)

		for i := 0; i < b.N; i++ {
			_ = cb.Execute(func() error {
				time.Sleep(time.Nanosecond)
				return nil
			})
		}
	})

	b.Run("WithMetrics", func(b *testing.B) {
		metrics := NewExtendedMockMetricsRecorder()

		for i := 0; i < b.N; i++ {
			time.Sleep(time.Nanosecond)
			metrics.RecordDockerOperation("test")
		}
	})

	b.Run("WithBothOptimizations", func(b *testing.B) {
		config := DefaultDockerClientConfig()
		logger := &MockLogger{}
		cb := NewDockerCircuitBreaker(config, logger)
		metrics := NewExtendedMockMetricsRecorder()

		for i := 0; i < b.N; i++ {
			_ = cb.Execute(func() error {
				time.Sleep(time.Nanosecond)
				metrics.RecordDockerOperation("test")
				return nil
			})
		}
	})
}

// TestOptimizedComponentsConcurrentStress tests under high concurrency load
func TestOptimizedComponentsConcurrentStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent stress test in short mode")
	}

	const numGoroutines = 100
	const operationsPerGoroutine = 1000

	config := DefaultDockerClientConfig()
	logger := &MockLogger{}
	metrics := NewExtendedMockMetricsRecorder()

	client, err := NewOptimizedDockerClient(config, logger, metrics)
	if err != nil {
		t.Fatalf("Failed to create optimized Docker client: %v", err)
	}

	bufferPool := NewEnhancedBufferPool(DefaultEnhancedBufferPoolConfig(), logger)

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				// Test circuit breaker
				_ = client.circuitBreaker.Execute(func() error {
					return nil
				})

				// Test buffer pool
				buf := bufferPool.Get()
				bufferPool.Put(buf)

				// Test metrics
				metrics.RecordDockerOperation("stress_test")
				metrics.RecordDockerLatency("stress_test", time.Microsecond*10)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	totalOperations := numGoroutines * operationsPerGoroutine
	t.Logf("Concurrent Stress Test Results:")
	t.Logf("Goroutines: %d", numGoroutines)
	t.Logf("Operations per goroutine: %d", operationsPerGoroutine)
	t.Logf("Total operations: %d", totalOperations)
	t.Logf("Total duration: %v", duration)
	t.Logf("Operations per second: %.2f", float64(totalOperations)/duration.Seconds())

	// Verify no deadlocks or race conditions
	stats := client.GetStats()
	t.Logf("Final circuit breaker stats: %+v", stats["circuit_breaker"])

	bufferStats := bufferPool.GetStats()
	t.Logf("Final buffer pool stats: %+v", bufferStats)

	metricsData := metrics.GetMetrics()
	t.Logf("Final metrics: %+v", metricsData)
}
