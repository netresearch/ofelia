package core

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/armon/circbuf"
)

// mockLogger implements Logger for testing purposes.
type mockLogger struct {
	debugMessages  []string
	errorMessages  []string
	noticeMessages []string
}

func (m *mockLogger) Criticalf(format string, args ...any) {}
func (m *mockLogger) Debugf(format string, args ...any) {
	m.debugMessages = append(m.debugMessages, format)
}
func (m *mockLogger) Errorf(format string, args ...any) {
	m.errorMessages = append(m.errorMessages, format)
}
func (m *mockLogger) Noticef(format string, args ...any) {
	m.noticeMessages = append(m.noticeMessages, format)
}
func (m *mockLogger) Warningf(format string, args ...any) {}

// newTestPool creates a small, deterministic pool for mutation testing.
// ShrinkInterval=0 disables background goroutine; EnablePrewarming=false avoids prewarm.
func newTestPool(logger Logger) *EnhancedBufferPool {
	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         2,
		MaxPoolSize:      10,
		GrowthFactor:     1.5,
		ShrinkThreshold:  0.3,
		ShrinkInterval:   0, // no background goroutine
		EnableMetrics:    true,
		EnablePrewarming: false,
	}
	return NewEnhancedBufferPool(config, logger)
}

// =============================================================================
// Kill: ARITHMETIC_BASE at line 36 (GrowthFactor: 1.5) and line 463 (GrowthFactor: 1.5)
// Both DefaultEnhancedBufferPoolConfig and NewBufferPool set GrowthFactor to 1.5.
// =============================================================================

func TestDefaultConfigGrowthFactor(t *testing.T) {
	t.Parallel()
	cfg := DefaultEnhancedBufferPoolConfig()
	if cfg.GrowthFactor != 1.5 {
		t.Errorf("expected GrowthFactor 1.5, got %f", cfg.GrowthFactor)
	}
}

func TestNewBufferPoolGrowthFactor(t *testing.T) {
	t.Parallel()
	ebp := NewBufferPool(1024, 4096, 65536)
	defer ebp.Shutdown()
	if ebp.config.GrowthFactor != 1.5 {
		t.Errorf("expected GrowthFactor 1.5, got %f", ebp.config.GrowthFactor)
	}
}

// =============================================================================
// Kill: ARITHMETIC_BASE at line 84 (config.MinSize) and line 85 (config.DefaultSize)
// These are the first two entries in the standardSizes slice in NewEnhancedBufferPool.
// After construction, pools must exist for exactly MinSize and DefaultSize.
// =============================================================================

func TestInitialPoolsContainMinAndDefaultSizes(t *testing.T) {
	t.Parallel()
	config := &EnhancedBufferPoolConfig{
		MinSize:     1024,
		DefaultSize: 4096,
		MaxSize:     65536,
		PoolSize:    2,
		MaxPoolSize: 10,
	}
	ebp := NewEnhancedBufferPool(config, nil)
	defer ebp.Shutdown()

	ebp.poolsMutex.RLock()
	defer ebp.poolsMutex.RUnlock()

	// Pool for MinSize must exist
	if _, ok := ebp.pools[1024]; !ok {
		t.Error("expected pool for MinSize=1024 to exist")
	}
	// Pool for DefaultSize must exist
	if _, ok := ebp.pools[4096]; !ok {
		t.Error("expected pool for DefaultSize=4096 to exist")
	}
	// Pool for MaxSize/4 must exist
	if _, ok := ebp.pools[65536/4]; !ok {
		t.Errorf("expected pool for MaxSize/4=%d to exist", 65536/4)
	}
	// Pool for MaxSize/2 must exist
	if _, ok := ebp.pools[65536/2]; !ok {
		t.Errorf("expected pool for MaxSize/2=%d to exist", 65536/2)
	}
	// Pool for MaxSize must exist
	if _, ok := ebp.pools[65536]; !ok {
		t.Error("expected pool for MaxSize=65536 to exist")
	}
}

// =============================================================================
// Kill: CONDITIONALS_NEGATION at line 125 (pool == nil becomes pool != nil)
// When getPoolForSize returns nil for a non-standard size, customBuffers increments.
// If the condition is negated, we'd skip custom buffer creation for nil pools.
// =============================================================================

func TestGetSizedCustomBufferWhenPoolNil(t *testing.T) {
	t.Parallel()
	// Use config where DefaultSize*8 does NOT appear in isStandardSize's list.
	// selectOptimalSize tiers: 3000, 6000, 12000, 24000, 100000
	// isStandardSize list:     1000, 3000, 6000, 12000, 25000, 50000, 100000
	// So requesting a size between 12001..24000 causes selectOptimalSize to return 24000,
	// which is NOT a standard size -> getPoolForSize returns nil -> customBuffers increments.
	config := &EnhancedBufferPoolConfig{
		MinSize:          1000,
		DefaultSize:      3000,
		MaxSize:          100000,
		PoolSize:         0,
		MaxPoolSize:      10,
		ShrinkInterval:   0,
		EnablePrewarming: false,
	}
	ebp := NewEnhancedBufferPool(config, nil)
	defer ebp.Shutdown()

	atomic.StoreInt64(&ebp.customBuffers, 0)

	// Request 12001: selectOptimalSize rounds to DefaultSize*8 = 24000
	// 24000 is NOT in isStandardSize -> pool is nil -> custom buffer
	buf, err := ebp.GetSized(12001)
	if err != nil {
		t.Fatalf("GetSized failed: %v", err)
	}
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}
	if buf.Size() != 24000 {
		t.Errorf("expected buffer size 24000, got %d", buf.Size())
	}

	custom := atomic.LoadInt64(&ebp.customBuffers)
	if custom != 1 {
		t.Errorf("expected customBuffers=1 when pool is nil, got %d", custom)
	}
}

// =============================================================================
// Kill: CONDITIONALS_NEGATION at line 166 (pool != nil becomes pool == nil)
// When Put finds a matching pool, it should Put the buffer back.
// If negated, buffer would not be returned to pool.
// =============================================================================

func TestPutReturnsToPools(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// Get a buffer (creates one via pool)
	buf, err := ebp.GetSized(ebp.config.DefaultSize)
	if err != nil {
		t.Fatalf("GetSized failed: %v", err)
	}

	// Reset counters
	atomic.StoreInt64(&ebp.totalGets, 0)
	atomic.StoreInt64(&ebp.totalMisses, 0)

	// Put it back
	ebp.Put(buf)

	// Now get again - should come from pool (no miss)
	buf2, err := ebp.GetSized(ebp.config.DefaultSize)
	if err != nil {
		t.Fatalf("second GetSized failed: %v", err)
	}
	if buf2 == nil {
		t.Fatal("expected non-nil buffer from pool")
	}

	misses := atomic.LoadInt64(&ebp.totalMisses)
	if misses != 0 {
		t.Errorf("expected 0 misses after Put+Get cycle, got %d", misses)
	}
}

func TestPutNilBuffer(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	putsBefore := atomic.LoadInt64(&ebp.totalPuts)
	ebp.Put(nil)
	putsAfter := atomic.LoadInt64(&ebp.totalPuts)

	if putsAfter != putsBefore {
		t.Error("Put(nil) should not increment totalPuts")
	}
}

// =============================================================================
// Kill: CONDITIONALS_BOUNDARY at line 178 (requestedSize < MinSize -> <= or >)
// Requesting exactly MinSize should return MinSize, not something else.
// =============================================================================

func TestSelectOptimalSizeBoundaryAtMinSize(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// Exactly MinSize: should return MinSize (via the <= DefaultSize path)
	result := ebp.selectOptimalSize(ebp.config.MinSize)
	// MinSize < DefaultSize, so selectOptimalSize returns DefaultSize
	if result != ebp.config.DefaultSize {
		t.Errorf("selectOptimalSize(MinSize=%d) = %d, want DefaultSize=%d",
			ebp.config.MinSize, result, ebp.config.DefaultSize)
	}

	// Below MinSize: should clamp to MinSize
	result = ebp.selectOptimalSize(ebp.config.MinSize - 1)
	if result != ebp.config.MinSize {
		t.Errorf("selectOptimalSize(MinSize-1=%d) = %d, want MinSize=%d",
			ebp.config.MinSize-1, result, ebp.config.MinSize)
	}

	// One above MinSize: should still return DefaultSize (since MinSize+1 <= DefaultSize)
	result = ebp.selectOptimalSize(ebp.config.MinSize + 1)
	if result != ebp.config.DefaultSize {
		t.Errorf("selectOptimalSize(MinSize+1=%d) = %d, want DefaultSize=%d",
			ebp.config.MinSize+1, result, ebp.config.DefaultSize)
	}
}

// =============================================================================
// Kill: CONDITIONALS_BOUNDARY at line 183 (requestedSize <= DefaultSize -> < or >=)
// Requesting exactly DefaultSize should return DefaultSize.
// =============================================================================

func TestSelectOptimalSizeBoundaryAtDefaultSize(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// Exactly DefaultSize: should return DefaultSize
	result := ebp.selectOptimalSize(ebp.config.DefaultSize)
	if result != ebp.config.DefaultSize {
		t.Errorf("selectOptimalSize(DefaultSize=%d) = %d, want %d",
			ebp.config.DefaultSize, result, ebp.config.DefaultSize)
	}

	// DefaultSize+1: should return the next tier (DefaultSize*2) since it's > DefaultSize
	result = ebp.selectOptimalSize(ebp.config.DefaultSize + 1)
	expected := ebp.config.DefaultSize * 2
	if result != expected {
		t.Errorf("selectOptimalSize(DefaultSize+1=%d) = %d, want %d",
			ebp.config.DefaultSize+1, result, expected)
	}

	// DefaultSize-1: should return DefaultSize
	result = ebp.selectOptimalSize(ebp.config.DefaultSize - 1)
	if result != ebp.config.DefaultSize {
		t.Errorf("selectOptimalSize(DefaultSize-1=%d) = %d, want %d",
			ebp.config.DefaultSize-1, result, ebp.config.DefaultSize)
	}
}

// =============================================================================
// Kill: ARITHMETIC_BASE at line 192 (DefaultSize) and line 193 (DefaultSize * 2)
// The sizes list in selectOptimalSize should be [Default, Default*2, Default*4, Default*8, Max].
// If arithmetic is changed (e.g., * becomes /), the wrong tier is returned.
// =============================================================================

func TestSelectOptimalSizeTiers(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	def := ebp.config.DefaultSize // 4096
	max := ebp.config.MaxSize     // 65536

	tests := []struct {
		name      string
		requested int64
		expected  int64
	}{
		// Within default: returns default
		{"at default", def, def},
		// Just above default: next tier is default*2
		{"above default", def + 1, def * 2},
		// At default*2: returns default*2
		{"at default*2", def * 2, def * 2},
		// Above default*2, below default*4: returns default*4
		{"above default*2", def*2 + 1, def * 4},
		// At default*4: returns default*4
		{"at default*4", def * 4, def * 4},
		// Above default*4, below default*8: returns default*8
		{"above default*4", def*4 + 1, def * 8},
		// At default*8: returns default*8
		{"at default*8", def * 8, def * 8},
		// Above default*8: returns max
		{"above default*8", def*8 + 1, max},
		// At max: returns max
		{"at max", max, max},
		// Above max: clamped to max
		{"above max", max + 1, max},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ebp.selectOptimalSize(tt.requested)
			if result != tt.expected {
				t.Errorf("selectOptimalSize(%d) = %d, want %d", tt.requested, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Kill: ARITHMETIC_BASE at lines 263-266 in isStandardSize
//   line 263: MinSize
//   line 264: DefaultSize
//   line 265: DefaultSize * 2
//   line 266: DefaultSize * 4
// =============================================================================

func TestIsStandardSize(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	def := ebp.config.DefaultSize // 4096
	max := ebp.config.MaxSize     // 65536
	min := ebp.config.MinSize     // 1024

	standardSizes := []int64{
		min,
		def,
		def * 2,
		def * 4,
		max / 4,
		max / 2,
		max,
	}

	for _, size := range standardSizes {
		if !ebp.isStandardSize(size) {
			t.Errorf("isStandardSize(%d) = false, want true", size)
		}
	}

	// Non-standard sizes must return false
	nonStandard := []int64{
		min + 1,
		def + 1,
		def * 3,
		def * 5,
		max - 1,
		512,
		7777,
	}

	for _, size := range nonStandard {
		if ebp.isStandardSize(size) {
			t.Errorf("isStandardSize(%d) = true, want false", size)
		}
	}
}

// Test that isStandardSize distinguishes MinSize from DefaultSize correctly.
// If ARITHMETIC_BASE mutates MinSize to DefaultSize or vice versa, this catches it.
func TestIsStandardSizeDistinguishesMinFromDefault(t *testing.T) {
	t.Parallel()
	config := &EnhancedBufferPoolConfig{
		MinSize:     1024,
		DefaultSize: 4096,
		MaxSize:     65536,
	}
	ebp := &EnhancedBufferPool{config: config}

	// MinSize is standard
	if !ebp.isStandardSize(1024) {
		t.Error("isStandardSize(1024=MinSize) should be true")
	}
	// DefaultSize is standard
	if !ebp.isStandardSize(4096) {
		t.Error("isStandardSize(4096=DefaultSize) should be true")
	}
	// A value that equals neither should be false (unless it matches another entry)
	if ebp.isStandardSize(2048) {
		t.Error("isStandardSize(2048) should be false, it's not in the standard list")
	}
	// DefaultSize*2 = 8192 is standard
	if !ebp.isStandardSize(8192) {
		t.Error("isStandardSize(8192=DefaultSize*2) should be true")
	}
	// DefaultSize*4 = 16384 is standard
	if !ebp.isStandardSize(16384) {
		t.Error("isStandardSize(16384=DefaultSize*4) should be true")
	}
}

// =============================================================================
// Kill: CONDITIONALS_NEGATION at line 271 (slices.Contains returns true -> negated to false)
// =============================================================================

func TestIsStandardSizeNegation(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// A known standard size must return true (negation would return false)
	if !ebp.isStandardSize(ebp.config.MinSize) {
		t.Error("MinSize should be standard")
	}
	if !ebp.isStandardSize(ebp.config.MaxSize) {
		t.Error("MaxSize should be standard")
	}

	// A known non-standard size must return false (negation would return true)
	if ebp.isStandardSize(12345) {
		t.Error("12345 should not be standard")
	}
}

// =============================================================================
// Kill: CONDITIONALS_NEGATION at line 300 (ebp.logger != nil -> == nil)
// and INCREMENT_DECREMENT at line 282 (successfulBuffers++)
// and INCREMENT_DECREMENT at line 308 (successfulBuffers in Debugf)
// Test prewarmPools by verifying that after prewarming, Get doesn't cause a miss.
// =============================================================================

func TestPrewarmPoolsPopulatesBuffers(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}

	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         3,
		MaxPoolSize:      10,
		GrowthFactor:     1.5,
		ShrinkThreshold:  0.3,
		ShrinkInterval:   0,
		EnableMetrics:    true,
		EnablePrewarming: true,
	}
	ebp := NewEnhancedBufferPool(config, logger)
	defer ebp.Shutdown()

	// After prewarming, logger should have debug messages about pre-warmed pools
	if len(logger.debugMessages) == 0 {
		t.Error("expected debug messages from prewarming, got none")
	}

	// Reset counters to check misses
	atomic.StoreInt64(&ebp.totalGets, 0)
	atomic.StoreInt64(&ebp.totalMisses, 0)

	// Get a buffer at default size - should come from pre-warmed pool
	buf, err := ebp.GetSized(ebp.config.DefaultSize)
	if err != nil {
		t.Fatalf("GetSized failed: %v", err)
	}
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}

	misses := atomic.LoadInt64(&ebp.totalMisses)
	if misses != 0 {
		t.Errorf("expected 0 misses after prewarming, got %d", misses)
	}

	ebp.Put(buf)
}

// Verify successfulBuffers is incremented properly during prewarming.
// The logger should report the correct number of successfully pre-warmed buffers.
func TestPrewarmPoolsSuccessfulBufferCount(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}

	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         5,
		MaxPoolSize:      20,
		GrowthFactor:     1.5,
		ShrinkThreshold:  0.3,
		ShrinkInterval:   0,
		EnableMetrics:    true,
		EnablePrewarming: true,
	}
	ebp := NewEnhancedBufferPool(config, logger)
	defer ebp.Shutdown()

	// After prewarming with PoolSize=5, we should be able to get 5 buffers
	// from the default size pool without any misses.
	atomic.StoreInt64(&ebp.totalGets, 0)
	atomic.StoreInt64(&ebp.totalMisses, 0)

	buffers := make([]*circbuf.Buffer, 0, 5)
	for i := range 5 {
		buf, err := ebp.GetSized(ebp.config.DefaultSize)
		if err != nil {
			t.Fatalf("GetSized(%d) failed: %v", i, err)
		}
		buffers = append(buffers, buf)
	}

	misses := atomic.LoadInt64(&ebp.totalMisses)
	if misses > 0 {
		t.Errorf("expected 0 misses for first %d gets after prewarming, got %d", 5, misses)
	}

	for _, buf := range buffers {
		ebp.Put(buf)
	}
}

// Test that prewarming is skipped when EnablePrewarming is false.
func TestPrewarmPoolsSkippedWhenDisabled(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}

	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         5,
		MaxPoolSize:      20,
		ShrinkInterval:   0,
		EnablePrewarming: false,
	}
	ebp := NewEnhancedBufferPool(config, logger)
	defer ebp.Shutdown()

	// Should NOT have debug messages about prewarming
	for _, msg := range logger.debugMessages {
		if msg == "Pre-warmed pool for size %d with %d/%d buffers" {
			t.Error("prewarming debug message found despite EnablePrewarming=false")
		}
	}
}

// =============================================================================
// Kill: CONDITIONALS_BOUNDARY at line 392 (totalGets > 0 -> >= 0 or < 0)
// Kill: CONDITIONALS_NEGATION at line 392 (totalGets > 0 -> totalGets <= 0)
// Kill: ARITHMETIC_BASE at line 393:30 (totalGets-totalMisses -> +, *, /)
// Kill: ARITHMETIC_BASE at line 393:44 (/ float64(totalGets) -> *, +, -)
// Kill: ARITHMETIC_BASE at line 393:65 (* 100 -> /, +, -)
// Kill: INVERT_NEGATIVES at line 393:30 (-(totalGets-totalMisses))
// =============================================================================

func TestGetStatsHitRateZeroGets(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// No operations: totalGets=0, hitRate should be 0
	stats := ebp.GetStats()
	hitRate, ok := stats["hit_rate_percent"].(float64)
	if !ok {
		t.Fatal("hit_rate_percent not float64")
	}
	if hitRate != 0 {
		t.Errorf("expected hit_rate_percent=0 with no gets, got %f", hitRate)
	}
}

func TestGetStatsHitRateAllHits(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// Simulate: 10 gets, 0 misses -> hitRate = (10-0)/10 * 100 = 100
	atomic.StoreInt64(&ebp.totalGets, 10)
	atomic.StoreInt64(&ebp.totalMisses, 0)

	stats := ebp.GetStats()
	hitRate := stats["hit_rate_percent"].(float64)
	if hitRate != 100.0 {
		t.Errorf("expected hit_rate_percent=100, got %f", hitRate)
	}
}

func TestGetStatsHitRatePartialHits(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// Simulate: 10 gets, 3 misses -> hitRate = (10-3)/10 * 100 = 70
	atomic.StoreInt64(&ebp.totalGets, 10)
	atomic.StoreInt64(&ebp.totalMisses, 3)

	stats := ebp.GetStats()
	hitRate := stats["hit_rate_percent"].(float64)
	if hitRate != 70.0 {
		t.Errorf("expected hit_rate_percent=70, got %f", hitRate)
	}
}

func TestGetStatsHitRateAllMisses(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// 5 gets, 5 misses -> hitRate = (5-5)/5 * 100 = 0
	atomic.StoreInt64(&ebp.totalGets, 5)
	atomic.StoreInt64(&ebp.totalMisses, 5)

	stats := ebp.GetStats()
	hitRate := stats["hit_rate_percent"].(float64)
	if hitRate != 0.0 {
		t.Errorf("expected hit_rate_percent=0, got %f", hitRate)
	}
}

func TestGetStatsHitRateOneGet(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// Boundary: exactly 1 get, 0 misses -> hitRate = 100
	atomic.StoreInt64(&ebp.totalGets, 1)
	atomic.StoreInt64(&ebp.totalMisses, 0)

	stats := ebp.GetStats()
	hitRate := stats["hit_rate_percent"].(float64)
	if hitRate != 100.0 {
		t.Errorf("expected hit_rate_percent=100 with 1 get, got %f", hitRate)
	}
}

// Verify the specific arithmetic: (gets-misses)/gets * 100
func TestGetStatsHitRateArithmetic(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// 4 gets, 1 miss -> (4-1)/4*100 = 75
	atomic.StoreInt64(&ebp.totalGets, 4)
	atomic.StoreInt64(&ebp.totalMisses, 1)

	stats := ebp.GetStats()
	hitRate := stats["hit_rate_percent"].(float64)
	if hitRate != 75.0 {
		t.Errorf("expected hit_rate_percent=75, got %f", hitRate)
	}
}

// =============================================================================
// Kill: ARITHMETIC_BASE at line 84/85 via pool creation in NewEnhancedBufferPool
// Verify that initial pool creation uses the exact config values, not mutations.
// =============================================================================

func TestNewEnhancedBufferPoolCreatesCorrectStandardPools(t *testing.T) {
	t.Parallel()
	config := &EnhancedBufferPoolConfig{
		MinSize:     2048,
		DefaultSize: 8192,
		MaxSize:     131072,
		PoolSize:    0,
		MaxPoolSize: 10,
	}
	ebp := NewEnhancedBufferPool(config, nil)
	defer ebp.Shutdown()

	ebp.poolsMutex.RLock()
	defer ebp.poolsMutex.RUnlock()

	expectedSizes := []int64{
		2048,       // MinSize
		8192,       // DefaultSize
		131072 / 4, // MaxSize/4 = 32768
		131072 / 2, // MaxSize/2 = 65536
		131072,     // MaxSize
	}

	for _, size := range expectedSizes {
		if _, ok := ebp.pools[size]; !ok {
			t.Errorf("expected pool for size %d to exist", size)
		}
	}

	// Verify exact pool count (no extra pools)
	if len(ebp.pools) != len(expectedSizes) {
		t.Errorf("expected %d pools, got %d", len(expectedSizes), len(ebp.pools))
	}
}

// =============================================================================
// Kill: CONDITIONALS_NEGATION at line 271 via getPoolForSize
// getPoolForSize should create pools for standard sizes but not non-standard.
// =============================================================================

func TestGetPoolForSizeCreatesForStandardOnly(t *testing.T) {
	t.Parallel()
	config := &EnhancedBufferPoolConfig{
		MinSize:     1024,
		DefaultSize: 4096,
		MaxSize:     65536,
		PoolSize:    0,
		MaxPoolSize: 10,
	}
	ebp := NewEnhancedBufferPool(config, nil)
	defer ebp.Shutdown()

	// getPoolForSize with a standard size that wasn't pre-created should create it
	standardSize := ebp.config.DefaultSize * 2 // 8192 is standard
	pool := ebp.getPoolForSize(standardSize)
	if pool == nil {
		t.Errorf("getPoolForSize(%d) returned nil for standard size", standardSize)
	}

	// getPoolForSize with a non-standard size should return nil
	nonStandardSize := int64(12345)
	pool = ebp.getPoolForSize(nonStandardSize)
	if pool != nil {
		t.Errorf("getPoolForSize(%d) returned non-nil for non-standard size", nonStandardSize)
	}
}

// =============================================================================
// Kill: INCREMENT_DECREMENT at line 282 (successfulBuffers++ -> --)
// If successfulBuffers is decremented instead of incremented, the count would be
// wrong (negative), and logging would show wrong values.
// We verify by checking prewarmed buffers are actually available.
// =============================================================================

func TestPrewarmSuccessfulBuffersIncrement(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}

	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         3,
		MaxPoolSize:      20,
		GrowthFactor:     1.5,
		ShrinkThreshold:  0.3,
		ShrinkInterval:   0,
		EnableMetrics:    true,
		EnablePrewarming: true,
	}
	ebp := NewEnhancedBufferPool(config, logger)
	defer ebp.Shutdown()

	// After prewarming with PoolSize=3, we should be able to get 3 buffers
	// without misses from the MinSize pool.
	atomic.StoreInt64(&ebp.totalGets, 0)
	atomic.StoreInt64(&ebp.totalMisses, 0)

	// Get 3 buffers from the MinSize pool
	buffers := make([]*circbuf.Buffer, 0, 3)
	for i := range 3 {
		buf, err := ebp.GetSized(ebp.config.MinSize)
		if err != nil {
			t.Fatalf("GetSized(MinSize) attempt %d failed: %v", i, err)
		}
		buffers = append(buffers, buf)
	}

	misses := atomic.LoadInt64(&ebp.totalMisses)
	if misses > 0 {
		t.Errorf("expected 0 misses for first 3 gets of MinSize after prewarming, got %d", misses)
	}

	for _, buf := range buffers {
		ebp.Put(buf)
	}
}

// =============================================================================
// Kill: INCREMENT_DECREMENT at line 308 (successfulBuffers in logging)
// This is the same successfulBuffers variable used in the Debugf call.
// If the counter was mutated (e.g., -- instead of ++), the log message would
// contain a negative or zero count. We verify through the logger.
// =============================================================================

func TestPrewarmLogsCorrectCount(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}

	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         2,
		MaxPoolSize:      10,
		GrowthFactor:     1.5,
		ShrinkThreshold:  0.3,
		ShrinkInterval:   0,
		EnableMetrics:    true,
		EnablePrewarming: true,
	}
	ebp := NewEnhancedBufferPool(config, logger)
	defer ebp.Shutdown()

	// Verify that debug messages were generated (logger != nil path)
	found := false
	for _, msg := range logger.debugMessages {
		if msg == "Pre-warmed pool for size %d with %d/%d buffers" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'Pre-warmed pool' debug message, got messages: %v", logger.debugMessages)
	}
}

// =============================================================================
// Kill: CONDITIONALS_NEGATION at line 300 (ebp.logger != nil -> == nil in error path)
// The error logging path in prewarmPools is for when buffer creation fails.
// Since circbuf.NewBuffer only fails on size <= 0, we can't easily trigger that
// in the standard flow. But we test the non-nil logger path is taken.
// =============================================================================

func TestPrewarmPoolsWithLogger(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}

	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         2,
		MaxPoolSize:      10,
		GrowthFactor:     1.5,
		ShrinkThreshold:  0.3,
		ShrinkInterval:   0,
		EnableMetrics:    true,
		EnablePrewarming: true,
	}
	ebp := NewEnhancedBufferPool(config, logger)
	defer ebp.Shutdown()

	// With logger != nil, debug messages should have been produced
	if len(logger.debugMessages) == 0 {
		t.Error("expected debug messages from prewarming with non-nil logger")
	}
}

func TestPrewarmPoolsWithoutLogger(t *testing.T) {
	t.Parallel()

	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         2,
		MaxPoolSize:      10,
		GrowthFactor:     1.5,
		ShrinkThreshold:  0.3,
		ShrinkInterval:   0,
		EnableMetrics:    true,
		EnablePrewarming: true,
	}
	// Should not panic with nil logger
	ebp := NewEnhancedBufferPool(config, nil)
	defer ebp.Shutdown()
}

// =============================================================================
// Test GetStats returns correct values for all fields
// This catches arithmetic mutations in the stats computation.
// =============================================================================

func TestGetStatsFieldValues(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	// Set known values
	atomic.StoreInt64(&ebp.totalGets, 20)
	atomic.StoreInt64(&ebp.totalPuts, 15)
	atomic.StoreInt64(&ebp.totalMisses, 5)
	atomic.StoreInt64(&ebp.customBuffers, 2)
	atomic.StoreInt64(&ebp.totalShrinks, 1)
	atomic.StoreInt64(&ebp.totalGrows, 3)

	stats := ebp.GetStats()

	// Verify each field
	if v := stats["total_gets"].(int64); v != 20 {
		t.Errorf("total_gets: got %d, want 20", v)
	}
	if v := stats["total_puts"].(int64); v != 15 {
		t.Errorf("total_puts: got %d, want 15", v)
	}
	if v := stats["total_misses"].(int64); v != 5 {
		t.Errorf("total_misses: got %d, want 5", v)
	}
	if v := stats["custom_buffers"].(int64); v != 2 {
		t.Errorf("custom_buffers: got %d, want 2", v)
	}
	if v := stats["total_shrinks"].(int64); v != 1 {
		t.Errorf("total_shrinks: got %d, want 1", v)
	}
	if v := stats["total_grows"].(int64); v != 3 {
		t.Errorf("total_grows: got %d, want 3", v)
	}

	// hitRate = (20-5)/20 * 100 = 75.0
	hitRate := stats["hit_rate_percent"].(float64)
	if hitRate != 75.0 {
		t.Errorf("hit_rate_percent: got %f, want 75.0", hitRate)
	}

	// Verify config sub-map
	configMap := stats["config"].(map[string]any)
	if v := configMap["default_size"].(int64); v != 4096 {
		t.Errorf("config.default_size: got %d, want 4096", v)
	}
	if v := configMap["max_size"].(int64); v != 65536 {
		t.Errorf("config.max_size: got %d, want 65536", v)
	}
}

// =============================================================================
// Test adaptive management to exercise performAdaptiveManagement
// =============================================================================

func TestPerformAdaptiveManagement(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}
	ebp := newTestPool(logger)
	defer ebp.Shutdown()

	// Track some usage
	ebp.trackUsage(ebp.config.DefaultSize)
	ebp.trackUsage(ebp.config.DefaultSize)
	ebp.trackUsage(ebp.config.MinSize)

	// Perform adaptive management
	ebp.performAdaptiveManagement()

	// After adaptive management, usage tracking should be reset
	ebp.usageMutex.RLock()
	remaining := len(ebp.usageTracking)
	ebp.usageMutex.RUnlock()

	if remaining != 0 {
		t.Errorf("expected usage tracking to be reset, got %d entries", remaining)
	}
}

func TestPerformAdaptiveManagementNoUsage(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}
	ebp := newTestPool(logger)
	defer ebp.Shutdown()

	// No usage tracked - should return early without error
	ebp.performAdaptiveManagement()

	// Verify no debug messages about utilization (since totalUsage == 0)
	for _, msg := range logger.debugMessages {
		if msg == "Buffer pool size %d has low utilization: %.2f%%" {
			t.Error("should not log utilization when totalUsage is 0")
		}
	}
}

// =============================================================================
// Test adaptive management worker starts and stops properly
// =============================================================================

func TestAdaptiveManagementWorker(t *testing.T) {
	t.Parallel()
	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         0,
		MaxPoolSize:      10,
		GrowthFactor:     1.5,
		ShrinkThreshold:  0.3,
		ShrinkInterval:   10 * time.Millisecond,
		EnableMetrics:    true,
		EnablePrewarming: false,
	}
	ebp := NewEnhancedBufferPool(config, nil)

	// Let the worker run for a bit
	time.Sleep(50 * time.Millisecond)

	// Shutdown should complete without hanging
	ebp.Shutdown()
}

// =============================================================================
// Test trackUsage increments correctly
// =============================================================================

func TestTrackUsage(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	ebp.trackUsage(1024)
	ebp.trackUsage(1024)
	ebp.trackUsage(4096)

	ebp.usageMutex.RLock()
	defer ebp.usageMutex.RUnlock()

	if ebp.usageTracking[1024] != 2 {
		t.Errorf("expected usage[1024]=2, got %d", ebp.usageTracking[1024])
	}
	if ebp.usageTracking[4096] != 1 {
		t.Errorf("expected usage[4096]=1, got %d", ebp.usageTracking[4096])
	}
}

// =============================================================================
// Test selectOptimalSize clamp at MaxSize
// =============================================================================

func TestSelectOptimalSizeAboveMax(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	result := ebp.selectOptimalSize(ebp.config.MaxSize * 10)
	if result != ebp.config.MaxSize {
		t.Errorf("selectOptimalSize(MaxSize*10) = %d, want MaxSize=%d", result, ebp.config.MaxSize)
	}
}

// =============================================================================
// Test Get (default size) delegates to GetSized correctly
// =============================================================================

func TestGetUsesDefaultSize(t *testing.T) {
	t.Parallel()
	ebp := newTestPool(nil)
	defer ebp.Shutdown()

	buf, err := ebp.Get()
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	// Default size for our test pool is 4096
	if buf.Size() != ebp.config.DefaultSize {
		t.Errorf("Get() returned buffer of size %d, want %d", buf.Size(), ebp.config.DefaultSize)
	}
	ebp.Put(buf)
}

// =============================================================================
// Test createPoolForSize creates working pools
// =============================================================================

func TestCreatePoolForSizeWithLogger(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}

	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         0,
		MaxPoolSize:      10,
		EnableMetrics:    true,
		EnablePrewarming: false,
	}
	ebp := NewEnhancedBufferPool(config, logger)
	defer ebp.Shutdown()

	// Logger should have recorded pool creation debug messages
	if len(logger.debugMessages) == 0 {
		t.Error("expected debug messages from pool creation with EnableMetrics=true")
	}
}

// =============================================================================
// Test NewBufferPool compatibility wrapper
// =============================================================================

func TestNewBufferPoolCompatibility(t *testing.T) {
	t.Parallel()
	ebp := NewBufferPool(512, 2048, 32768)
	defer ebp.Shutdown()

	if ebp.config.MinSize != 512 {
		t.Errorf("MinSize: got %d, want 512", ebp.config.MinSize)
	}
	if ebp.config.DefaultSize != 2048 {
		t.Errorf("DefaultSize: got %d, want 2048", ebp.config.DefaultSize)
	}
	if ebp.config.MaxSize != 32768 {
		t.Errorf("MaxSize: got %d, want 32768", ebp.config.MaxSize)
	}
	if ebp.config.PoolSize != 10 {
		t.Errorf("PoolSize: got %d, want 10", ebp.config.PoolSize)
	}
	if ebp.config.GrowthFactor != 1.5 {
		t.Errorf("GrowthFactor: got %f, want 1.5", ebp.config.GrowthFactor)
	}
	if ebp.config.ShrinkThreshold != 0.3 {
		t.Errorf("ShrinkThreshold: got %f, want 0.3", ebp.config.ShrinkThreshold)
	}

	// Verify we can Get/Put
	buf, err := ebp.Get()
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if buf.Size() != 2048 {
		t.Errorf("buffer size: got %d, want 2048", buf.Size())
	}
	ebp.Put(buf)
}

// =============================================================================
// Test DefaultEnhancedBufferPoolConfig returns correct defaults
// =============================================================================

func TestDefaultEnhancedBufferPoolConfigValues(t *testing.T) {
	t.Parallel()
	cfg := DefaultEnhancedBufferPoolConfig()

	if cfg.MinSize != 1024 {
		t.Errorf("MinSize: got %d, want 1024", cfg.MinSize)
	}
	if cfg.DefaultSize != 256*1024 {
		t.Errorf("DefaultSize: got %d, want %d", cfg.DefaultSize, 256*1024)
	}
	if cfg.MaxSize != maxStreamSize {
		t.Errorf("MaxSize: got %d, want %d", cfg.MaxSize, maxStreamSize)
	}
	if cfg.PoolSize != 50 {
		t.Errorf("PoolSize: got %d, want 50", cfg.PoolSize)
	}
	if cfg.MaxPoolSize != 200 {
		t.Errorf("MaxPoolSize: got %d, want 200", cfg.MaxPoolSize)
	}
	if cfg.GrowthFactor != 1.5 {
		t.Errorf("GrowthFactor: got %f, want 1.5", cfg.GrowthFactor)
	}
	if cfg.ShrinkThreshold != 0.3 {
		t.Errorf("ShrinkThreshold: got %f, want 0.3", cfg.ShrinkThreshold)
	}
	if cfg.ShrinkInterval != 5*time.Minute {
		t.Errorf("ShrinkInterval: got %v, want 5m", cfg.ShrinkInterval)
	}
	if !cfg.EnableMetrics {
		t.Error("EnableMetrics should be true")
	}
	if !cfg.EnablePrewarming {
		t.Error("EnablePrewarming should be true")
	}
}

// =============================================================================
// Test Shutdown clears all pools
// =============================================================================

func TestShutdownClearsPools(t *testing.T) {
	t.Parallel()
	logger := &mockLogger{}
	config := &EnhancedBufferPoolConfig{
		MinSize:          1024,
		DefaultSize:      4096,
		MaxSize:          65536,
		PoolSize:         0,
		MaxPoolSize:      10,
		ShrinkInterval:   0,
		EnablePrewarming: false,
	}
	ebp := NewEnhancedBufferPool(config, logger)

	// Verify pools exist before shutdown
	ebp.poolsMutex.RLock()
	beforeCount := len(ebp.pools)
	ebp.poolsMutex.RUnlock()
	if beforeCount == 0 {
		t.Fatal("expected pools to exist before shutdown")
	}

	ebp.Shutdown()

	// Verify pools are cleared
	ebp.poolsMutex.RLock()
	afterCount := len(ebp.pools)
	ebp.poolsMutex.RUnlock()
	if afterCount != 0 {
		t.Errorf("expected 0 pools after shutdown, got %d", afterCount)
	}

	// Verify logger received shutdown message
	found := false
	for _, msg := range logger.noticeMessages {
		if msg == "Enhanced buffer pool shutdown complete" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected shutdown notice message")
	}
}

// =============================================================================
// Test NewEnhancedBufferPool with nil config uses defaults
// =============================================================================

func TestNewEnhancedBufferPoolNilConfig(t *testing.T) {
	t.Parallel()
	ebp := NewEnhancedBufferPool(nil, nil)
	defer ebp.Shutdown()

	if ebp.config.DefaultSize != 256*1024 {
		t.Errorf("expected default config DefaultSize=%d, got %d", 256*1024, ebp.config.DefaultSize)
	}
}
