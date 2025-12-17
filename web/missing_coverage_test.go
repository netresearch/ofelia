package web

import (
	"testing"
)

const (
	testSecretKey = "test-secret-for-testing"
)

func TestNewRateLimiter(t *testing.T) {
	t.Parallel()

	// Test creating a new rate limiter
	rateLimiter := NewRateLimiter(10, 5) // 10 requests per minute, burst of 5

	if rateLimiter == nil {
		t.Fatal("NewRateLimiter returned nil")
	}

	if rateLimiter.rate != 10 {
		t.Errorf("Expected rate 10, got %d", rateLimiter.rate)
	}

	if rateLimiter.burst != 5 {
		t.Errorf("Expected burst 5, got %d", rateLimiter.burst)
	}

	if rateLimiter.limiters == nil {
		t.Error("limiters map should be initialized")
	}

	if len(rateLimiter.limiters) != 0 {
		t.Error("limiters map should be empty initially")
	}
}

// TestRateLimiterGetLimiter tests the GetLimiter method that currently has 0% coverage
func TestRateLimiterGetLimiter(t *testing.T) {
	t.Parallel()

	rateLimiter := NewRateLimiter(10, 5)

	// Test getting a limiter for a new key
	limiter1 := rateLimiter.GetLimiter("test-key-1")
	if limiter1 == nil {
		t.Fatal("GetLimiter returned nil for new key")
	}

	// Test getting the same limiter again
	limiter2 := rateLimiter.GetLimiter("test-key-1")
	if limiter2 != limiter1 {
		t.Error("GetLimiter should return the same limiter instance for same key")
	}

	// Test getting a limiter for a different key
	limiter3 := rateLimiter.GetLimiter("test-key-2")
	if limiter3 == limiter1 {
		t.Error("GetLimiter should return different limiter instances for different keys")
	}

	// Verify we have 2 limiters stored
	if len(rateLimiter.limiters) != 2 {
		t.Errorf("Expected 2 limiters, got %d", len(rateLimiter.limiters))
	}
}

// TestRateLimiterAllow tests the Allow method that currently has 0% coverage
func TestRateLimiterAllow(t *testing.T) {
	t.Parallel()

	// Create a rate limiter with very permissive settings
	rateLimiter := NewRateLimiter(60, 10) // 60 per minute, burst of 10

	// Test that initial requests are allowed
	if !rateLimiter.Allow("test-key") {
		t.Error("First request should be allowed")
	}

	if !rateLimiter.Allow("test-key") {
		t.Error("Second request should be allowed within burst limit")
	}

	// Test different keys are independent
	if !rateLimiter.Allow("different-key") {
		t.Error("Request with different key should be allowed")
	}
}

// TestRateLimiterCleanupOldLimiters tests the CleanupOldLimiters method that currently has 0% coverage
func TestRateLimiterCleanupOldLimiters(t *testing.T) {
	t.Parallel()

	rateLimiter := NewRateLimiter(10, 5)

	// Add some limiters
	rateLimiter.GetLimiter("key1")
	rateLimiter.GetLimiter("key2")
	rateLimiter.GetLimiter("key3")

	if len(rateLimiter.limiters) != 3 {
		t.Errorf("Expected 3 limiters before cleanup, got %d", len(rateLimiter.limiters))
	}

	// Call cleanup - this should execute without panic
	rateLimiter.CleanupOldLimiters()

	// The cleanup might or might not remove limiters depending on implementation
	// Main goal is to ensure it doesn't panic
	if rateLimiter.limiters == nil {
		t.Error("limiters map should not be nil after cleanup")
	}
}

// TestNewSecureTokenManager tests the NewSecureTokenManager function that currently has 0% coverage
func TestNewSecureTokenManager(t *testing.T) {
	t.Parallel()

	tokenExpiry := 24

	tokenManager, err := NewSecureTokenManager(testSecretKey, tokenExpiry)
	if err != nil {
		t.Fatalf("NewSecureTokenManager returned error: %v", err)
	}

	if tokenManager == nil {
		t.Fatal("NewSecureTokenManager returned nil")
	}

	// Check if the token manager was initialized properly
	// We can't directly check private fields, but we can test it doesn't panic
	// and basic functionality works
}

// TestSecureTokenManagerGenerateCSRFToken tests the GenerateCSRFToken method that currently has 0% coverage
func TestSecureTokenManagerGenerateCSRFToken(t *testing.T) {
	t.Parallel()

	tokenManager, err := NewSecureTokenManager(testSecretKey, 24)
	if err != nil {
		t.Fatalf("NewSecureTokenManager returned error: %v", err)
	}

	// Test generating CSRF token
	csrfToken, err := tokenManager.GenerateCSRFToken()
	if err != nil {
		t.Errorf("GenerateCSRFToken returned error: %v", err)
	}

	if csrfToken == "" {
		t.Error("GenerateCSRFToken returned empty result")
	}

	// Test that multiple calls generate different tokens
	csrfToken2, err := tokenManager.GenerateCSRFToken()
	if err != nil {
		t.Errorf("Second GenerateCSRFToken returned error: %v", err)
	}

	if csrfToken == csrfToken2 {
		t.Error("GenerateCSRFToken should generate different results on each call")
	}
}
