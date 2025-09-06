package web

import (
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// TokenEntry represents a token with expiration for efficient cleanup
type TokenEntry struct {
	Token     string
	Username  string
	ExpiresAt time.Time
	Index     int // For heap implementation
}

// TokenExpiryHeap implements a min-heap of tokens ordered by expiration time
type TokenExpiryHeap []*TokenEntry

func (h TokenExpiryHeap) Len() int           { return len(h) }
func (h TokenExpiryHeap) Less(i, j int) bool { return h[i].ExpiresAt.Before(h[j].ExpiresAt) }
func (h TokenExpiryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].Index = i
	h[j].Index = j
}

func (h *TokenExpiryHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*TokenEntry)
	item.Index = n
	*h = append(*h, item)
}

func (h *TokenExpiryHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.Index = -1 // for safety
	*h = old[0 : n-1]
	return item
}

// OptimizedTokenManagerConfig holds configuration for the optimized token manager
type OptimizedTokenManagerConfig struct {
	SecretKey           string        `json:"secretKey"`
	TokenExpiry         time.Duration `json:"tokenExpiry"`
	CleanupInterval     time.Duration `json:"cleanupInterval"`
	MaxTokens           int           `json:"maxTokens"`        // LRU eviction threshold
	CleanupBatchSize    int           `json:"cleanupBatchSize"` // Process tokens in batches
	EnableMetrics       bool          `json:"enableMetrics"`
	MaxConcurrentCleans int           `json:"maxConcurrentCleans"` // Prevent cleanup storms
}

// DefaultOptimizedTokenManagerConfig returns sensible defaults
func DefaultOptimizedTokenManagerConfig() *OptimizedTokenManagerConfig {
	return &OptimizedTokenManagerConfig{
		TokenExpiry:         24 * time.Hour,
		CleanupInterval:     5 * time.Minute, // Less frequent cleanup
		MaxTokens:           10000,           // Support large number of concurrent users
		CleanupBatchSize:    100,             // Process 100 expired tokens per batch
		EnableMetrics:       true,
		MaxConcurrentCleans: 1, // Only one cleanup routine running at a time
	}
}

// OptimizedTokenManager provides high-performance token management with single background worker
type OptimizedTokenManager struct {
	config        *OptimizedTokenManagerConfig
	tokens        map[string]*TokenEntry // Fast token lookup
	expiryHeap    *TokenExpiryHeap       // Efficient expiry tracking
	mutex         sync.RWMutex           // Protect concurrent access
	ctx           context.Context        // For graceful shutdown //nolint:containedctx // Valid pattern for goroutine lifecycle management
	cancel        context.CancelFunc     // Cancel background worker
	cleanupActive int32                  // Atomic flag to prevent concurrent cleanups

	// Metrics
	totalTokens       int64
	expiredTokens     int64
	cleanupOperations int64

	logger interface {
		Debugf(format string, args ...interface{})
		Warningf(format string, args ...interface{})
		Noticef(format string, args ...interface{})
	}
}

// NewOptimizedTokenManager creates a new optimized token manager
func NewOptimizedTokenManager(
	config *OptimizedTokenManagerConfig,
	logger interface {
		Debugf(format string, args ...interface{})
		Warningf(format string, args ...interface{})
		Noticef(format string, args ...interface{})
	},
) *OptimizedTokenManager {
	if config == nil {
		config = DefaultOptimizedTokenManagerConfig()
	}

	// Generate secure secret key if not provided
	if config.SecretKey == "" {
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		config.SecretKey = base64.StdEncoding.EncodeToString(key)
	}

	ctx, cancel := context.WithCancel(context.Background())

	heapInstance := &TokenExpiryHeap{}
	heap.Init(heapInstance)

	tm := &OptimizedTokenManager{
		config:     config,
		tokens:     make(map[string]*TokenEntry),
		expiryHeap: heapInstance,
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger,
	}

	// Start single background cleanup worker
	go tm.backgroundCleanupWorker()

	return tm
}

// GenerateToken creates a new authentication token efficiently
func (tm *OptimizedTokenManager) GenerateToken(username string) (string, error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// Check if we need to evict old tokens (LRU-like behavior)
	if len(tm.tokens) >= tm.config.MaxTokens {
		tm.evictOldestTokensUnsafe(tm.config.MaxTokens / 10) // Evict 10% when full
	}

	// Generate cryptographically secure token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}

	token := base64.URLEncoding.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(tm.config.TokenExpiry)

	// Create token entry
	entry := &TokenEntry{
		Token:     token,
		Username:  username,
		ExpiresAt: expiresAt,
	}

	// Store in both map and heap
	tm.tokens[token] = entry
	heap.Push(tm.expiryHeap, entry)

	// Update metrics
	tm.totalTokens++

	if tm.config.EnableMetrics && tm.logger != nil {
		tm.logger.Debugf("Generated token for user %s, total active tokens: %d",
			username, len(tm.tokens))
	}

	return token, nil
}

// ValidateToken checks if a token is valid with high performance
func (tm *OptimizedTokenManager) ValidateToken(token string) (*TokenData, bool) {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	entry, exists := tm.tokens[token]
	if !exists {
		return nil, false
	}

	// Check expiration
	if time.Now().After(entry.ExpiresAt) {
		// Don't remove here - let background cleanup handle it
		// This avoids write locks in the hot path
		return nil, false
	}

	// Return compatible TokenData structure
	return &TokenData{
		Username:  entry.Username,
		ExpiresAt: entry.ExpiresAt,
	}, true
}

// RevokeToken immediately invalidates a token
func (tm *OptimizedTokenManager) RevokeToken(token string) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	if entry, exists := tm.tokens[token]; exists {
		delete(tm.tokens, token)
		// Mark as expired in heap (will be cleaned up by background worker)
		entry.ExpiresAt = time.Now().Add(-time.Hour)
	}
}

// backgroundCleanupWorker runs a single background goroutine for token cleanup
func (tm *OptimizedTokenManager) backgroundCleanupWorker() {
	ticker := time.NewTicker(tm.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			tm.logger.Debugf("Token manager cleanup worker shutting down")
			return

		case <-ticker.C:
			tm.performCleanup()
		}
	}
}

// performCleanup efficiently removes expired tokens in batches
func (tm *OptimizedTokenManager) performCleanup() {
	// Prevent concurrent cleanups
	if !atomic.CompareAndSwapInt32(&tm.cleanupActive, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&tm.cleanupActive, 0)

	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	now := time.Now()
	cleaned := 0
	batchSize := tm.config.CleanupBatchSize

	// Clean expired tokens from heap in batches
	for tm.expiryHeap.Len() > 0 && cleaned < batchSize {
		// Peek at the earliest expiring token
		earliest := (*tm.expiryHeap)[0]

		if earliest.ExpiresAt.After(now) {
			// No more expired tokens
			break
		}

		// Remove from heap
		heap.Pop(tm.expiryHeap)

		// Remove from map if still present
		if _, exists := tm.tokens[earliest.Token]; exists {
			delete(tm.tokens, earliest.Token)
			tm.expiredTokens++
			cleaned++
		}
	}

	tm.cleanupOperations++

	if tm.config.EnableMetrics && cleaned > 0 && tm.logger != nil {
		tm.logger.Debugf("Cleaned up %d expired tokens, %d active tokens remaining",
			cleaned, len(tm.tokens))
	}
}

// evictOldestTokensUnsafe removes the oldest tokens when capacity is exceeded
// Must be called with mutex held
func (tm *OptimizedTokenManager) evictOldestTokensUnsafe(count int) {
	evicted := 0

	// Remove oldest tokens from heap
	for tm.expiryHeap.Len() > 0 && evicted < count {
		oldest := heap.Pop(tm.expiryHeap).(*TokenEntry)

		if _, exists := tm.tokens[oldest.Token]; exists {
			delete(tm.tokens, oldest.Token)
			evicted++
		}
	}

	if tm.config.EnableMetrics && evicted > 0 && tm.logger != nil {
		tm.logger.Debugf("Evicted %d oldest tokens due to capacity limit", evicted)
	}
}

// GetStats returns performance statistics
func (tm *OptimizedTokenManager) GetStats() map[string]interface{} {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	return map[string]interface{}{
		"active_tokens":      len(tm.tokens),
		"total_generated":    tm.totalTokens,
		"total_expired":      tm.expiredTokens,
		"cleanup_operations": tm.cleanupOperations,
		"heap_size":          tm.expiryHeap.Len(),
		"cleanup_active":     atomic.LoadInt32(&tm.cleanupActive) == 1,
		"config": map[string]interface{}{
			"max_tokens":       tm.config.MaxTokens,
			"cleanup_interval": tm.config.CleanupInterval,
			"token_expiry":     tm.config.TokenExpiry,
			"batch_size":       tm.config.CleanupBatchSize,
		},
	}
}

// Shutdown gracefully stops the token manager
func (tm *OptimizedTokenManager) Shutdown(ctx context.Context) error {
	tm.logger.Noticef("Shutting down optimized token manager")

	// Cancel background worker
	tm.cancel()

	// Perform final cleanup
	tm.performCleanup()

	// Clear all tokens
	tm.mutex.Lock()
	tm.tokens = make(map[string]*TokenEntry)
	tm.expiryHeap = &TokenExpiryHeap{}
	tm.mutex.Unlock()

	return nil
}

// GetActiveTokenCount returns the number of currently active tokens
func (tm *OptimizedTokenManager) GetActiveTokenCount() int {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	return len(tm.tokens)
}

// ForceCleanup triggers an immediate cleanup operation
func (tm *OptimizedTokenManager) ForceCleanup() {
	go tm.performCleanup()
}
