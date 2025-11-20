package core

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/armon/circbuf"
)

// EnhancedBufferPoolConfig holds configuration for the enhanced buffer pool
type EnhancedBufferPoolConfig struct {
	MinSize          int64         `json:"minSize"`          // Minimum buffer size
	DefaultSize      int64         `json:"defaultSize"`      // Default buffer size
	MaxSize          int64         `json:"maxSize"`          // Maximum buffer size
	PoolSize         int           `json:"poolSize"`         // Number of buffers to pre-allocate
	MaxPoolSize      int           `json:"maxPoolSize"`      // Maximum number of buffers in pool
	GrowthFactor     float64       `json:"growthFactor"`     // Factor to increase pool size when needed
	ShrinkThreshold  float64       `json:"shrinkThreshold"`  // Usage percentage below which to shrink
	ShrinkInterval   time.Duration `json:"shrinkInterval"`   // How often to check for shrinking
	EnableMetrics    bool          `json:"enableMetrics"`    // Enable performance metrics
	EnablePrewarming bool          `json:"enablePrewarming"` // Pre-allocate buffers on startup
}

// DefaultEnhancedBufferPoolConfig returns optimized defaults for high-concurrency scenarios
func DefaultEnhancedBufferPoolConfig() *EnhancedBufferPoolConfig {
	return &EnhancedBufferPoolConfig{
		MinSize:          1024,            // 1KB minimum
		DefaultSize:      256 * 1024,      // 256KB default
		MaxSize:          maxStreamSize,   // 10MB maximum (from existing constant)
		PoolSize:         50,              // Pre-allocate 50 buffers
		MaxPoolSize:      200,             // Maximum 200 buffers in pool
		GrowthFactor:     1.5,             // Grow by 50% when needed
		ShrinkThreshold:  0.3,             // Shrink when usage below 30%
		ShrinkInterval:   5 * time.Minute, // Check for shrinking every 5 minutes
		EnableMetrics:    true,
		EnablePrewarming: true,
	}
}

// EnhancedBufferPool provides high-performance buffer management with adaptive sizing
type EnhancedBufferPool struct {
	config     *EnhancedBufferPoolConfig
	pools      map[int64]*sync.Pool // Separate pools for different sizes
	poolsMutex sync.RWMutex         // Protect pools map

	// Metrics
	totalGets     int64
	totalPuts     int64
	totalMisses   int64 // When we had to create new buffer instead of reusing
	totalShrinks  int64 // Number of times we shrunk the pool
	totalGrows    int64 // Number of times we grew the pool
	customBuffers int64 // Buffers created outside standard sizes

	// Adaptive management
	usageTracking map[int64]int64 // Track usage per size
	usageMutex    sync.RWMutex    // Protect usage tracking
	workerWg      sync.WaitGroup  // Wait for adaptive management worker to exit
	shrinkTicker  *time.Ticker
	shrinkStop    chan struct{}

	logger Logger
}

// NewEnhancedBufferPool creates a new enhanced buffer pool with adaptive management
func NewEnhancedBufferPool(config *EnhancedBufferPoolConfig, logger Logger) *EnhancedBufferPool {
	if config == nil {
		config = DefaultEnhancedBufferPoolConfig()
	}

	ebp := &EnhancedBufferPool{
		config:        config,
		pools:         make(map[int64]*sync.Pool),
		usageTracking: make(map[int64]int64),
		shrinkStop:    make(chan struct{}),
		logger:        logger,
	}

	// Create initial pools for common sizes
	standardSizes := []int64{
		config.MinSize,
		config.DefaultSize,
		config.MaxSize / 4, // 2.5MB
		config.MaxSize / 2, // 5MB
		config.MaxSize,     // 10MB
	}

	for _, size := range standardSizes {
		ebp.createPoolForSize(size)
	}

	// Pre-warm pools if enabled
	if config.EnablePrewarming {
		ebp.prewarmPools()
	}

	// Start adaptive management
	if config.ShrinkInterval > 0 {
		ebp.shrinkTicker = time.NewTicker(config.ShrinkInterval)
		ebp.workerWg.Add(1)
		go ebp.adaptiveManagementWorker()
	}

	return ebp
}

// Get retrieves a buffer from the pool, optimized for high concurrency
func (ebp *EnhancedBufferPool) Get() (*circbuf.Buffer, error) {
	return ebp.GetSized(ebp.config.DefaultSize)
}

// GetSized retrieves a buffer with a specific size requirement, with intelligent size selection
func (ebp *EnhancedBufferPool) GetSized(requestedSize int64) (*circbuf.Buffer, error) {
	atomic.AddInt64(&ebp.totalGets, 1)

	// Find the best matching size
	targetSize := ebp.selectOptimalSize(requestedSize)

	// Track usage for adaptive management
	ebp.trackUsage(targetSize)

	// Get pool for this size
	pool := ebp.getPoolForSize(targetSize)
	if pool == nil {
		// Create custom buffer
		atomic.AddInt64(&ebp.customBuffers, 1)
		buf, err := circbuf.NewBuffer(targetSize)
		if err != nil {
			return nil, fmt.Errorf("failed to create buffer of size %d: %w", targetSize, err)
		}
		return buf, nil
	}

	// Try to get from pool
	if pooledItem := pool.Get(); pooledItem != nil {
		if buf, ok := pooledItem.(*circbuf.Buffer); ok {
			return buf, nil
		}
	}

	// Pool miss - create new buffer
	atomic.AddInt64(&ebp.totalMisses, 1)
	buf, err := circbuf.NewBuffer(targetSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create buffer of size %d: %w", targetSize, err)
	}
	return buf, nil
}

// Put returns a buffer to the appropriate pool
func (ebp *EnhancedBufferPool) Put(buf *circbuf.Buffer) {
	if buf == nil {
		return
	}

	atomic.AddInt64(&ebp.totalPuts, 1)

	// Reset the buffer
	buf.Reset()

	// Find appropriate pool
	size := buf.Size()
	pool := ebp.getPoolForSize(size)

	if pool != nil {
		pool.Put(buf)
	}
	// If no pool exists for this size, let GC handle it
}

// selectOptimalSize chooses the best buffer size for the request
func (ebp *EnhancedBufferPool) selectOptimalSize(requestedSize int64) int64 {
	// Clamp to bounds
	if requestedSize < ebp.config.MinSize {
		return ebp.config.MinSize
	}
	if requestedSize > ebp.config.MaxSize {
		return ebp.config.MaxSize
	}

	// If within default size, use default
	if requestedSize <= ebp.config.DefaultSize {
		return ebp.config.DefaultSize
	}

	// Find next power-of-2-like size for efficiency
	// This helps with pool reuse and memory alignment
	sizes := []int64{
		ebp.config.DefaultSize,
		ebp.config.DefaultSize * 2,
		ebp.config.DefaultSize * 4,
		ebp.config.DefaultSize * 8,
		ebp.config.MaxSize,
	}

	for _, size := range sizes {
		if requestedSize <= size {
			return size
		}
	}

	return ebp.config.MaxSize
}

// getPoolForSize returns the pool for a given size, creating if necessary
func (ebp *EnhancedBufferPool) getPoolForSize(size int64) *sync.Pool {
	// Try read lock first for common case
	ebp.poolsMutex.RLock()
	if pool, exists := ebp.pools[size]; exists {
		ebp.poolsMutex.RUnlock()
		return pool
	}
	ebp.poolsMutex.RUnlock()

	// Need to create pool - take write lock
	ebp.poolsMutex.Lock()
	defer ebp.poolsMutex.Unlock()

	// Double-check after acquiring write lock
	if pool, exists := ebp.pools[size]; exists {
		return pool
	}

	// Create new pool only for standard sizes
	if ebp.isStandardSize(size) {
		return ebp.createPoolForSize(size)
	}

	return nil
}

// createPoolForSize creates a new pool for the given size
func (ebp *EnhancedBufferPool) createPoolForSize(size int64) *sync.Pool {
	pool := &sync.Pool{
		New: func() interface{} {
			buf, err := circbuf.NewBuffer(size)
			if err != nil {
				// Return nil instead of panicking - caller will handle
				if ebp.logger != nil {
					ebp.logger.Errorf("Failed to create buffer of size %d: %v", size, err)
				}
				return nil
			}
			return buf
		},
	}

	ebp.pools[size] = pool

	if ebp.config.EnableMetrics && ebp.logger != nil {
		ebp.logger.Debugf("Created buffer pool for size %d bytes", size)
	}

	return pool
}

// isStandardSize checks if a size is one of our standard pool sizes
func (ebp *EnhancedBufferPool) isStandardSize(size int64) bool {
	standardSizes := []int64{
		ebp.config.MinSize,
		ebp.config.DefaultSize,
		ebp.config.DefaultSize * 2,
		ebp.config.DefaultSize * 4,
		ebp.config.MaxSize / 4,
		ebp.config.MaxSize / 2,
		ebp.config.MaxSize,
	}

	for _, standardSize := range standardSizes {
		if size == standardSize {
			return true
		}
	}

	return false
}

// trackUsage records usage of a particular buffer size for adaptive management
func (ebp *EnhancedBufferPool) trackUsage(size int64) {
	ebp.usageMutex.Lock()
	ebp.usageTracking[size]++
	ebp.usageMutex.Unlock()
}

// prewarmPools pre-allocates buffers in pools to reduce initial allocation overhead
func (ebp *EnhancedBufferPool) prewarmPools() {
	if !ebp.config.EnablePrewarming {
		return
	}

	ebp.poolsMutex.RLock()
	defer ebp.poolsMutex.RUnlock()

	for size, pool := range ebp.pools {
		// Pre-allocate buffers for this pool
		successfulBuffers := 0
		for i := 0; i < ebp.config.PoolSize; i++ {
			buf, err := circbuf.NewBuffer(size)
			if err != nil {
				// Log error but continue with remaining buffers (graceful degradation)
				if ebp.logger != nil {
					ebp.logger.Errorf("Failed to pre-warm buffer %d of size %d: %v", i, size, err)
				}
				continue
			}
			pool.Put(buf)
			successfulBuffers++
		}

		if ebp.logger != nil {
			ebp.logger.Debugf("Pre-warmed pool for size %d with %d/%d buffers", size, successfulBuffers, ebp.config.PoolSize)
		}
	}
}

// adaptiveManagementWorker runs periodic optimization of pool sizes
func (ebp *EnhancedBufferPool) adaptiveManagementWorker() {
	defer ebp.workerWg.Done()
	for {
		select {
		case <-ebp.shrinkStop:
			return
		case <-ebp.shrinkTicker.C:
			ebp.performAdaptiveManagement()
		}
	}
}

// performAdaptiveManagement adjusts pool sizes based on usage patterns
func (ebp *EnhancedBufferPool) performAdaptiveManagement() {
	ebp.usageMutex.RLock()
	usage := make(map[int64]int64)
	for size, count := range ebp.usageTracking {
		usage[size] = count
	}
	ebp.usageMutex.RUnlock()

	// Reset usage tracking
	ebp.usageMutex.Lock()
	ebp.usageTracking = make(map[int64]int64)
	ebp.usageMutex.Unlock()

	totalUsage := int64(0)
	for _, count := range usage {
		totalUsage += count
	}

	if totalUsage == 0 {
		return // No usage to analyze
	}

	// Find underutilized pools and consider shrinking
	ebp.poolsMutex.RLock()
	for size := range ebp.pools {
		usageCount := usage[size]
		utilizationRate := float64(usageCount) / float64(totalUsage)

		if utilizationRate < ebp.config.ShrinkThreshold {
			// This pool is underutilized - could shrink or remove
			if ebp.logger != nil {
				ebp.logger.Debugf("Buffer pool size %d has low utilization: %.2f%%",
					size, utilizationRate*100)
			}
			// For now, just log - in production, could implement actual shrinking
		}
	}
	ebp.poolsMutex.RUnlock()
}

// GetStats returns comprehensive performance statistics
func (ebp *EnhancedBufferPool) GetStats() map[string]interface{} {
	ebp.poolsMutex.RLock()
	poolCount := len(ebp.pools)
	poolSizes := make([]int64, 0, len(ebp.pools))
	for size := range ebp.pools {
		poolSizes = append(poolSizes, size)
	}
	ebp.poolsMutex.RUnlock()

	ebp.usageMutex.RLock()
	currentUsage := make(map[int64]int64)
	for size, count := range ebp.usageTracking {
		currentUsage[size] = count
	}
	ebp.usageMutex.RUnlock()

	totalGets := atomic.LoadInt64(&ebp.totalGets)
	totalMisses := atomic.LoadInt64(&ebp.totalMisses)

	hitRate := float64(0)
	if totalGets > 0 {
		hitRate = float64(totalGets-totalMisses) / float64(totalGets) * 100
	}

	return map[string]interface{}{
		"total_gets":       totalGets,
		"total_puts":       atomic.LoadInt64(&ebp.totalPuts),
		"total_misses":     totalMisses,
		"hit_rate_percent": hitRate,
		"custom_buffers":   atomic.LoadInt64(&ebp.customBuffers),
		"total_shrinks":    atomic.LoadInt64(&ebp.totalShrinks),
		"total_grows":      atomic.LoadInt64(&ebp.totalGrows),
		"pool_count":       poolCount,
		"pool_sizes":       poolSizes,
		"current_usage":    currentUsage,
		"config": map[string]interface{}{
			"default_size": ebp.config.DefaultSize,
			"max_size":     ebp.config.MaxSize,
			"max_pools":    ebp.config.MaxPoolSize,
		},
	}
}

// Shutdown gracefully stops the enhanced buffer pool
func (ebp *EnhancedBufferPool) Shutdown() {
	if ebp.shrinkTicker != nil {
		ebp.shrinkTicker.Stop()
		close(ebp.shrinkStop)
		ebp.workerWg.Wait() // Wait for worker to exit before clearing pools
	}

	// Clear all pools
	ebp.poolsMutex.Lock()
	ebp.pools = make(map[int64]*sync.Pool)
	ebp.poolsMutex.Unlock()

	if ebp.logger != nil {
		ebp.logger.Noticef("Enhanced buffer pool shutdown complete")
	}
}

// Global enhanced buffer pool instance
var (
	// DefaultBufferPool provides enhanced performance for job execution
	// This replaces the simple buffer pool with multi-tier adaptive pooling
	// Note: ShrinkInterval is set to 0 to prevent background goroutine at package init
	DefaultBufferPool = func() *EnhancedBufferPool {
		cfg := DefaultEnhancedBufferPoolConfig()
		cfg.ShrinkInterval = 0                 // Prevent background goroutine leak at package init
		cfg.EnablePrewarming = false           // Prevent 888MB memory allocation at package init
		cfg.PoolSize = 0                       // Disable initial pool allocation
		return NewEnhancedBufferPool(cfg, nil) // Logger can be set later
	}()
)

// SetGlobalBufferPoolLogger sets the logger for the global buffer pool
func SetGlobalBufferPoolLogger(logger Logger) {
	DefaultBufferPool.logger = logger
}

// NewBufferPool is a compatibility wrapper for tests and old code
// It returns the enhanced buffer pool with custom configuration
func NewBufferPool(minSize, defaultSize, maxSize int64) *EnhancedBufferPool {
	config := &EnhancedBufferPoolConfig{
		MinSize:          minSize,
		DefaultSize:      defaultSize,
		MaxSize:          maxSize,
		PoolSize:         10, // Smaller pool for simple usage
		MaxPoolSize:      50,
		GrowthFactor:     1.5,
		ShrinkThreshold:  0.3,
		ShrinkInterval:   5 * time.Minute,
		EnableMetrics:    false, // Disable metrics for compatibility
		EnablePrewarming: false, // Disable prewarming for compatibility
	}
	return NewEnhancedBufferPool(config, nil)
}
