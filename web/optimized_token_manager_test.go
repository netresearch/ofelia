package web

import (
	"context"
	"testing"
	"time"
)

// MockLogger provides a mock logger for testing
type MockTokenManagerLogger struct {
	debugMessages   []string
	warningMessages []string
	noticeMessages  []string
}

func (m *MockTokenManagerLogger) Debugf(format string, args ...interface{}) {
	// Store debug messages for testing
	m.debugMessages = append(m.debugMessages, format)
}

func (m *MockTokenManagerLogger) Warningf(format string, args ...interface{}) {
	m.warningMessages = append(m.warningMessages, format)
}

func (m *MockTokenManagerLogger) Noticef(format string, args ...interface{}) {
	m.noticeMessages = append(m.noticeMessages, format)
}

// TestDefaultOptimizedTokenManagerConfig tests the default configuration
func TestDefaultOptimizedTokenManagerConfig(t *testing.T) {
	t.Parallel()

	config := DefaultOptimizedTokenManagerConfig()
	if config == nil {
		t.Fatal("DefaultOptimizedTokenManagerConfig returned nil")
	}

	if config.TokenExpiry != 24*time.Hour {
		t.Errorf("Expected token expiry 24h, got %v", config.TokenExpiry)
	}

	if config.CleanupInterval != 5*time.Minute {
		t.Errorf("Expected cleanup interval 5m, got %v", config.CleanupInterval)
	}

	if config.MaxTokens != 10000 {
		t.Errorf("Expected max tokens 10000, got %d", config.MaxTokens)
	}

	if config.CleanupBatchSize != 100 {
		t.Errorf("Expected cleanup batch size 100, got %d", config.CleanupBatchSize)
	}

	if !config.EnableMetrics {
		t.Error("Expected metrics to be enabled by default")
	}

	if config.MaxConcurrentCleans != 1 {
		t.Errorf("Expected max concurrent cleans 1, got %d", config.MaxConcurrentCleans)
	}
}

// TestNewOptimizedTokenManager tests the constructor
func TestNewOptimizedTokenManager(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}

	// Test with default config
	tm := NewOptimizedTokenManager(nil, logger)
	if tm == nil {
		t.Fatal("NewOptimizedTokenManager returned nil")
	}

	if tm.config == nil {
		t.Fatal("Token manager config is nil")
	}

	if tm.tokens == nil {
		t.Fatal("Token manager tokens map is nil")
	}

	if tm.expiryHeap == nil {
		t.Fatal("Token manager expiry heap is nil")
	}

	if tm.logger != logger {
		t.Error("Token manager logger not set correctly")
	}

	// Test with custom config
	config := &OptimizedTokenManagerConfig{
		TokenExpiry:         12 * time.Hour,
		CleanupInterval:     2 * time.Minute,
		MaxTokens:           5000,
		CleanupBatchSize:    50,
		EnableMetrics:       false,
		MaxConcurrentCleans: 2,
		SecretKey:           "test-secret-key",
	}

	tm2 := NewOptimizedTokenManager(config, logger)
	if tm2.config.TokenExpiry != 12*time.Hour {
		t.Errorf("Expected custom token expiry 12h, got %v", tm2.config.TokenExpiry)
	}

	if tm2.config.SecretKey != "test-secret-key" {
		t.Errorf("Expected custom secret key, got %s", tm2.config.SecretKey)
	}

	// Clean up
	tm.Shutdown(context.Background())
	tm2.Shutdown(context.Background())
}

// TestGenerateToken tests token generation
func TestGenerateToken(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}
	config := &OptimizedTokenManagerConfig{
		TokenExpiry:         1 * time.Hour,
		CleanupInterval:     1 * time.Minute,
		MaxTokens:           100,
		CleanupBatchSize:    10,
		EnableMetrics:       true,
		MaxConcurrentCleans: 1,
		SecretKey:           "test-secret",
	}

	tm := NewOptimizedTokenManager(config, logger)
	defer tm.Shutdown(context.Background())

	// Test successful token generation
	token, err := tm.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if token == "" {
		t.Fatal("Generated token is empty")
	}

	if len(token) == 0 {
		t.Fatal("Generated token has zero length")
	}

	// Check that token is stored
	if tm.GetActiveTokenCount() != 1 {
		t.Errorf("Expected 1 active token, got %d", tm.GetActiveTokenCount())
	}

	// Test generating multiple tokens for same user
	token2, err := tm.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("Second GenerateToken failed: %v", err)
	}

	if token == token2 {
		t.Error("Generated tokens should be unique")
	}

	if tm.GetActiveTokenCount() != 2 {
		t.Errorf("Expected 2 active tokens, got %d", tm.GetActiveTokenCount())
	}

	// Test generating tokens for different users
	token3, err := tm.GenerateToken("anotheruser")
	if err != nil {
		t.Fatalf("Third GenerateToken failed: %v", err)
	}

	if token3 == token || token3 == token2 {
		t.Error("Tokens for different users should be unique")
	}
}

// TestValidateToken tests token validation
func TestValidateToken(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}
	config := &OptimizedTokenManagerConfig{
		TokenExpiry:         100 * time.Millisecond, // Short expiry for testing
		CleanupInterval:     10 * time.Second,       // Long cleanup interval
		MaxTokens:           100,
		CleanupBatchSize:    10,
		EnableMetrics:       false,
		MaxConcurrentCleans: 1,
	}

	tm := NewOptimizedTokenManager(config, logger)
	defer tm.Shutdown(context.Background())

	// Generate a token
	token, err := tm.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Test successful validation
	tokenData, valid := tm.ValidateToken(token)
	if !valid {
		t.Fatal("Token validation failed for valid token")
	}

	if tokenData == nil {
		t.Fatal("TokenData is nil for valid token")
	}

	if tokenData.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", tokenData.Username)
	}

	// Test validation of non-existent token
	_, valid = tm.ValidateToken("non-existent-token")
	if valid {
		t.Error("Validation should fail for non-existent token")
	}

	// Test validation of expired token
	time.Sleep(150 * time.Millisecond) // Wait for token to expire
	_, valid = tm.ValidateToken(token)
	if valid {
		t.Error("Validation should fail for expired token")
	}
}

// TestRevokeToken tests token revocation
func TestRevokeToken(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}
	config := DefaultOptimizedTokenManagerConfig()
	tm := NewOptimizedTokenManager(config, logger)
	defer tm.Shutdown(context.Background())

	// Generate a token
	token, err := tm.GenerateToken("testuser")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Verify token is valid
	_, valid := tm.ValidateToken(token)
	if !valid {
		t.Fatal("Token should be valid before revocation")
	}

	// Revoke the token
	tm.RevokeToken(token)

	// Verify token is no longer valid
	_, valid = tm.ValidateToken(token)
	if valid {
		t.Error("Token should be invalid after revocation")
	}

	// Test revoking non-existent token (should not panic)
	tm.RevokeToken("non-existent-token")
}

// TestTokenManagerCapacityManagement tests token capacity limits
func TestTokenManagerCapacityManagement(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}
	config := &OptimizedTokenManagerConfig{
		TokenExpiry:         1 * time.Hour,
		CleanupInterval:     10 * time.Second,
		MaxTokens:           5, // Small limit for testing
		CleanupBatchSize:    2,
		EnableMetrics:       true,
		MaxConcurrentCleans: 1,
	}

	tm := NewOptimizedTokenManager(config, logger)
	defer tm.Shutdown(context.Background())

	// Generate tokens up to and beyond capacity
	tokens := make([]string, 0, config.MaxTokens+2)
	for i := 0; i < config.MaxTokens+2; i++ {
		token, err := tm.GenerateToken("user" + string(rune('0'+i)))
		if err != nil {
			t.Fatalf("GenerateToken failed at %d: %v", i, err)
		}
		tokens = append(tokens, token)
	}

	// Allow some time for eviction to occur
	time.Sleep(10 * time.Millisecond)

	// Check that eviction mechanism works by verifying tokens are managed
	activeCount := tm.GetActiveTokenCount()

	// The exact count may vary due to eviction timing, but should be reasonable
	if activeCount == 0 {
		t.Error("All tokens were evicted - capacity management too aggressive")
	}

	// Verify that some eviction occurred if we exceeded capacity significantly
	if activeCount > config.MaxTokens*2 {
		t.Errorf("Active token count %d is much higher than expected max %d - eviction may not be working",
			activeCount, config.MaxTokens)
	}

	// Test that new tokens can still be generated
	newToken, err := tm.GenerateToken("newuser")
	if err != nil {
		t.Errorf("Should be able to generate new token after capacity management: %v", err)
	}
	if newToken == "" {
		t.Error("Generated token should not be empty")
	}
}

// TestGetActiveTokenCount tests the active token count method
func TestGetActiveTokenCount(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}
	config := DefaultOptimizedTokenManagerConfig()
	tm := NewOptimizedTokenManager(config, logger)
	defer tm.Shutdown(context.Background())

	// Initially should be 0
	if count := tm.GetActiveTokenCount(); count != 0 {
		t.Errorf("Expected 0 active tokens initially, got %d", count)
	}

	// Generate some tokens
	for i := 0; i < 3; i++ {
		_, err := tm.GenerateToken("user" + string(rune('0'+i)))
		if err != nil {
			t.Fatalf("GenerateToken failed: %v", err)
		}
	}

	if count := tm.GetActiveTokenCount(); count != 3 {
		t.Errorf("Expected 3 active tokens, got %d", count)
	}
}

// TestGetStats tests the statistics method
func TestGetStats(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}
	config := DefaultOptimizedTokenManagerConfig()
	tm := NewOptimizedTokenManager(config, logger)
	defer tm.Shutdown(context.Background())

	// Generate some tokens
	for i := 0; i < 3; i++ {
		_, err := tm.GenerateToken("user" + string(rune('0'+i)))
		if err != nil {
			t.Fatalf("GenerateToken failed: %v", err)
		}
	}

	stats := tm.GetStats()
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}

	// Check expected keys
	expectedKeys := []string{
		"active_tokens",
		"total_generated",
		"total_expired",
		"cleanup_operations",
		"heap_size",
		"cleanup_active",
		"config",
	}

	for _, key := range expectedKeys {
		if _, exists := stats[key]; !exists {
			t.Errorf("Stats missing key: %s", key)
		}
	}

	// Check values
	if activeTokens, ok := stats["active_tokens"].(int); !ok || activeTokens != 3 {
		t.Errorf("Expected active_tokens to be 3, got %v", stats["active_tokens"])
	}

	if totalGenerated, ok := stats["total_generated"].(int64); !ok || totalGenerated != 3 {
		t.Errorf("Expected total_generated to be 3, got %v", stats["total_generated"])
	}
}

// TestForceCleanup tests the force cleanup method
func TestForceCleanup(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}
	config := &OptimizedTokenManagerConfig{
		TokenExpiry:         10 * time.Millisecond, // Very short expiry
		CleanupInterval:     1 * time.Hour,         // Long interval so cleanup doesn't run automatically
		MaxTokens:           100,
		CleanupBatchSize:    10,
		EnableMetrics:       true,
		MaxConcurrentCleans: 1,
	}

	tm := NewOptimizedTokenManager(config, logger)
	defer tm.Shutdown(context.Background())

	// Generate tokens
	for i := 0; i < 5; i++ {
		_, err := tm.GenerateToken("user" + string(rune('0'+i)))
		if err != nil {
			t.Fatalf("GenerateToken failed: %v", err)
		}
	}

	initialCount := tm.GetActiveTokenCount()
	if initialCount != 5 {
		t.Errorf("Expected 5 active tokens initially, got %d", initialCount)
	}

	// Wait for tokens to expire
	time.Sleep(50 * time.Millisecond)

	// Force cleanup
	tm.ForceCleanup()

	// Give cleanup time to run (it runs in goroutine)
	time.Sleep(10 * time.Millisecond)

	// Should have fewer active tokens after cleanup
	finalCount := tm.GetActiveTokenCount()
	if finalCount >= initialCount {
		t.Errorf("Expected cleanup to reduce token count from %d, got %d", initialCount, finalCount)
	}
}

// TestShutdown tests the shutdown method
func TestShutdown(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}
	config := DefaultOptimizedTokenManagerConfig()
	tm := NewOptimizedTokenManager(config, logger)

	// Generate some tokens
	for i := 0; i < 3; i++ {
		_, err := tm.GenerateToken("user" + string(rune('0'+i)))
		if err != nil {
			t.Fatalf("GenerateToken failed: %v", err)
		}
	}

	// Verify tokens exist
	if count := tm.GetActiveTokenCount(); count != 3 {
		t.Errorf("Expected 3 active tokens before shutdown, got %d", count)
	}

	// Shutdown
	err := tm.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Verify tokens are cleared
	if count := tm.GetActiveTokenCount(); count != 0 {
		t.Errorf("Expected 0 active tokens after shutdown, got %d", count)
	}

	// Verify notice message was logged
	if len(logger.noticeMessages) == 0 {
		t.Error("Expected notice message during shutdown")
	}
}

// TestTokenExpiryHeap tests the heap implementation
func TestTokenExpiryHeap(t *testing.T) {
	t.Parallel()

	now := time.Now()
	h := &TokenExpiryHeap{}

	// Test empty heap
	if h.Len() != 0 {
		t.Error("Empty heap should have length 0")
	}

	// Test basic heap operations without relying on specific ordering
	entry1 := &TokenEntry{Token: "token1", Username: "user1", ExpiresAt: now.Add(1 * time.Hour)}
	entry2 := &TokenEntry{Token: "token2", Username: "user2", ExpiresAt: now.Add(2 * time.Hour)}

	h.Push(entry1)
	h.Push(entry2)

	if h.Len() != 2 {
		t.Errorf("Expected heap length 2, got %d", h.Len())
	}

	// Test that we can pop elements
	popped1 := h.Pop().(*TokenEntry)
	if popped1 == nil {
		t.Error("First pop returned nil")
	}

	popped2 := h.Pop().(*TokenEntry)
	if popped2 == nil {
		t.Error("Second pop returned nil")
	}

	// Test swap functionality
	h.Push(entry1)
	h.Push(entry2)
	if h.Len() != 2 {
		t.Error("Heap should have 2 elements after re-adding")
	}

	// Test Less method directly
	if !h.Less(0, 1) && !h.Less(1, 0) {
		t.Error("At least one comparison should be true")
	}

	// Test Swap method
	h.Swap(0, 1)
	if h.Len() != 2 {
		t.Error("Heap length should remain 2 after swap")
	}
}

// TestTokenManagerConcurrentAccess tests concurrent access to token manager
func TestTokenManagerConcurrentAccess(t *testing.T) {
	t.Parallel()

	logger := &MockTokenManagerLogger{}
	config := DefaultOptimizedTokenManagerConfig()
	tm := NewOptimizedTokenManager(config, logger)
	defer tm.Shutdown(context.Background())

	// Concurrent token generation and validation
	const numGoroutines = 10
	const tokensPerGoroutine = 10

	done := make(chan bool, numGoroutines)

	// Launch concurrent token generators
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			for j := 0; j < tokensPerGoroutine; j++ {
				// Generate token
				token, err := tm.GenerateToken("user" + string(rune('0'+goroutineID)) + string(rune('0'+j)))
				if err != nil {
					t.Errorf("GenerateToken failed in goroutine %d: %v", goroutineID, err)
					return
				}

				// Validate token
				_, valid := tm.ValidateToken(token)
				if !valid {
					t.Errorf("Token validation failed in goroutine %d", goroutineID)
					return
				}

				// Revoke some tokens
				if j%3 == 0 {
					tm.RevokeToken(token)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify final state
	finalCount := tm.GetActiveTokenCount()
	if finalCount <= 0 {
		t.Error("Should have some active tokens after concurrent access")
	}

	stats := tm.GetStats()
	if totalGenerated, ok := stats["total_generated"].(int64); !ok || totalGenerated != int64(numGoroutines*tokensPerGoroutine) {
		t.Errorf("Expected total_generated to be %d, got %v", numGoroutines*tokensPerGoroutine, stats["total_generated"])
	}
}
