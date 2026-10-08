// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupOldLimiters_Empty(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(10, 5)

	// Cleanup on empty map should not panic
	rl.CleanupOldLimiters(5 * time.Minute)
	assert.NotNil(t, rl.limiters)
	assert.Empty(t, rl.limiters)
}

func TestCleanupOldLimiters_WithEntries(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(10, 5)
	rl.GetLimiter("ip-1")
	rl.GetLimiter("ip-2")
	rl.GetLimiter("ip-3")

	assert.Len(t, rl.limiters, 3)

	// Cleanup should not panic and map should remain valid (all entries are recent)
	rl.CleanupOldLimiters(5 * time.Minute)
	assert.NotNil(t, rl.limiters)
}

func TestValidateCSRFToken_Valid(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("test-secret-key", 24)
	require.NoError(t, err)
	defer tm.Close()

	token, err := tm.GenerateCSRFToken()
	require.NoError(t, err)

	assert.True(t, tm.ValidateCSRFToken(token), "valid CSRF token should pass validation")
}

func TestValidateCSRFToken_OneTimeUse(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("test-secret-key", 24)
	require.NoError(t, err)
	defer tm.Close()

	token, err := tm.GenerateCSRFToken()
	require.NoError(t, err)

	assert.True(t, tm.ValidateCSRFToken(token))
	assert.False(t, tm.ValidateCSRFToken(token),
		"CSRF token should be invalidated after single use")
}

func TestValidateCSRFToken_Missing(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("test-secret-key", 24)
	require.NoError(t, err)
	defer tm.Close()

	assert.False(t, tm.ValidateCSRFToken("nonexistent-token"),
		"non-existent CSRF token should fail validation")
}

func TestValidateCSRFToken_Expired(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("test-secret-key", 24)
	require.NoError(t, err)
	defer tm.Close()

	token, err := tm.GenerateCSRFToken()
	require.NoError(t, err)

	tm.csrfMu.Lock()
	tm.csrfTokens[token] = time.Now().Add(-1 * time.Hour)
	tm.csrfMu.Unlock()

	assert.False(t, tm.ValidateCSRFToken(token),
		"expired CSRF token should fail validation")
}

func TestCleanupExpiredTokens(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("test-secret-key", 1)
	require.NoError(t, err)
	defer tm.Close()

	token, err := tm.GenerateToken("testuser")
	require.NoError(t, err)

	tm.mu.Lock()
	tm.tokens[token].ExpiresAt = time.Now().Add(-1 * time.Hour)
	tm.mu.Unlock()

	validToken, err := tm.GenerateToken("testuser2")
	require.NoError(t, err)

	tm.cleanupExpiredTokens()

	tm.mu.RLock()
	_, expiredExists := tm.tokens[token]
	_, validExists := tm.tokens[validToken]
	tm.mu.RUnlock()

	assert.False(t, expiredExists, "expired token should be cleaned up")
	assert.True(t, validExists, "valid token should survive cleanup")
}

func TestCleanupExpiredCSRFTokens(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("test-secret-key", 24)
	require.NoError(t, err)
	defer tm.Close()

	expiredToken, err := tm.GenerateCSRFToken()
	require.NoError(t, err)

	tm.csrfMu.Lock()
	tm.csrfTokens[expiredToken] = time.Now().Add(-1 * time.Hour)
	tm.csrfMu.Unlock()

	validToken, err := tm.GenerateCSRFToken()
	require.NoError(t, err)

	tm.cleanupExpiredCSRFTokens()

	tm.csrfMu.RLock()
	_, expiredExists := tm.csrfTokens[expiredToken]
	_, validExists := tm.csrfTokens[validToken]
	tm.csrfMu.RUnlock()

	assert.False(t, expiredExists, "expired CSRF token should be cleaned up")
	assert.True(t, validExists, "valid CSRF token should survive cleanup")
}

func TestHashPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
	}{
		{"simple password", "password123"},
		{"empty password", ""},
		{"long password", "this-is-a-very-long-password-that-should-still-work"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.password)
			require.NoError(t, err)
			assert.NotEmpty(t, hash)
			assert.Contains(t, hash, "$2a$12$", "hash should use cost 12")

			hash2, err := HashPassword(tt.password)
			require.NoError(t, err)
			assert.NotEqual(t, hash, hash2,
				"bcrypt should produce different hashes for same password")
		})
	}
}

func TestNewSecureTokenManager_EmptySecretKey(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("", 24)
	require.NoError(t, err)
	defer tm.Close()

	assert.NotEmpty(t, tm.secretKey, "empty secret key should result in generated key")

	token, err := tm.GenerateToken("admin")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	data, valid := tm.ValidateToken(token)
	assert.True(t, valid)
	assert.Equal(t, "admin", data.Username)
}

func TestSecureTokenManager_Close_Multiple(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("test-secret", 24)
	require.NoError(t, err)

	tm.Close()
	tm.Close()
	tm.Close()
}

func TestSecureTokenManager_RevokeToken(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("test-secret", 24)
	require.NoError(t, err)
	defer tm.Close()

	token, err := tm.GenerateToken("user")
	require.NoError(t, err)

	_, valid := tm.ValidateToken(token)
	assert.True(t, valid)

	tm.RevokeToken(token)

	_, valid = tm.ValidateToken(token)
	assert.False(t, valid, "revoked token should not be valid")
}

func TestSecureTokenManager_ValidateToken_Expired(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("test-secret", 1)
	require.NoError(t, err)
	defer tm.Close()

	token, err := tm.GenerateToken("user")
	require.NoError(t, err)

	tm.mu.Lock()
	tm.tokens[token].ExpiresAt = time.Now().Add(-1 * time.Hour)
	tm.mu.Unlock()

	_, valid := tm.ValidateToken(token)
	assert.False(t, valid, "expired token should not be valid")
}

func TestGetClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		xff        string
		xri        string
		remoteAddr string
		expected   string
	}{
		{
			name:       "X-Forwarded-For single IP from loopback",
			xff:        "203.0.113.1",
			remoteAddr: "127.0.0.1:1234",
			expected:   "203.0.113.1",
		},
		{
			// The proxy on loopback appended 70.41.3.18, the address it
			// received the request from; 203.0.113.1 is what the client
			// sent itself and must not choose the rate-limit key.
			name:       "X-Forwarded-For multiple IPs from loopback",
			xff:        "203.0.113.1, 70.41.3.18",
			remoteAddr: "127.0.0.1:1234",
			expected:   "70.41.3.18",
		},
		{
			name:       "X-Forwarded-For whose every hop is loopback",
			xff:        "127.0.0.2, 127.0.0.3",
			remoteAddr: "127.0.0.1:1234",
			expected:   "127.0.0.2",
		},
		{
			name:       "X-Forwarded-For with empty entries",
			xff:        " , 70.41.3.18 ,",
			remoteAddr: "127.0.0.1:1234",
			expected:   "70.41.3.18",
		},
		{
			name:       "X-Real-IP header from loopback",
			xri:        "198.51.100.1",
			remoteAddr: "127.0.0.1:1234",
			expected:   "198.51.100.1",
		},
		{
			name:       "RemoteAddr with port",
			remoteAddr: "192.168.1.1:5678",
			expected:   "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}

			got := getClientIP(req)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Fix #6: CSRF bypass via X-Requested-With header
// ---------------------------------------------------------------------------

func TestCSRF_XRequestedWithBypassRemoved(t *testing.T) {
	t.Parallel()

	// Create a valid bcrypt hash for "testpassword"
	hash, err := HashPassword("testpassword")
	require.NoError(t, err)

	config := &SecureAuthConfig{
		Enabled:      true,
		Username:     "admin",
		PasswordHash: hash,
		SecretKey:    "test-secret-key-for-csrf",
		TokenExpiry:  24,
		MaxAttempts:  10,
	}

	tm, err := NewSecureTokenManager(config.SecretKey, config.TokenExpiry)
	require.NoError(t, err)
	defer tm.Close()

	rl := NewRateLimiter(config.MaxAttempts, config.MaxAttempts)
	handler := NewSecureLoginHandler(config, tm, rl)

	// Send a POST with X-Requested-With: XMLHttpRequest but NO CSRF token.
	// The bug allows this to bypass CSRF validation entirely.
	body := strings.NewReader(`{"username":"admin","password":"testpassword"}`)
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.RemoteAddr = "127.0.0.1:1234"

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// After the fix, this should return 403 Forbidden because no CSRF token
	// is provided, regardless of the X-Requested-With header.
	assert.Equal(t, http.StatusForbidden, w.Code,
		"requests with X-Requested-With but no CSRF token must be rejected")
	assert.Contains(t, w.Body.String(), "CSRF token required")
}

// ---------------------------------------------------------------------------
// Fix #7: Rate limiter cleanup (memory DoS)
// ---------------------------------------------------------------------------

func TestRateLimiterCleanupRemovesOldEntries(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(10, 5)

	// Access three IPs to create limiters
	rl.GetLimiter("192.168.1.1")
	rl.GetLimiter("192.168.1.2")
	rl.GetLimiter("192.168.1.3")

	require.Len(t, rl.limiters, 3)

	// Backdate lastAccess for two of the IPs to simulate stale entries
	rl.mu.Lock()
	staleTime := time.Now().Add(-10 * time.Minute)
	rl.lastAccess["192.168.1.1"] = staleTime
	rl.lastAccess["192.168.1.2"] = staleTime
	// Leave 192.168.1.3 with its recent access time
	rl.mu.Unlock()

	// Cleanup entries older than 5 minutes
	rl.CleanupOldLimiters(5 * time.Minute)

	rl.mu.RLock()
	defer rl.mu.RUnlock()

	assert.Len(t, rl.limiters, 1, "only the non-stale IP should remain")
	assert.Contains(t, rl.limiters, "192.168.1.3", "recent IP should survive cleanup")
	assert.NotContains(t, rl.limiters, "192.168.1.1", "stale IP should be removed")
	assert.NotContains(t, rl.limiters, "192.168.1.2", "stale IP should be removed")

	assert.Len(t, rl.lastAccess, 1, "lastAccess map should also be cleaned")
	assert.Contains(t, rl.lastAccess, "192.168.1.3")
}

func TestRateLimiterCleanupPreservesRecentEntries(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(10, 5)

	// Access several IPs -- all are recent
	rl.GetLimiter("10.0.0.1")
	rl.GetLimiter("10.0.0.2")
	rl.GetLimiter("10.0.0.3")

	require.Len(t, rl.limiters, 3)

	// Cleanup with a 5-minute maxAge -- all entries are fresh, none should be removed
	rl.CleanupOldLimiters(5 * time.Minute)

	rl.mu.RLock()
	defer rl.mu.RUnlock()

	assert.Len(t, rl.limiters, 3, "all recent entries should survive cleanup")
	assert.Len(t, rl.lastAccess, 3, "all recent lastAccess entries should survive cleanup")
}

// ---------------------------------------------------------------------------
// Fix #10: Trusted proxies for rate limiter IP extraction
// ---------------------------------------------------------------------------

func TestGetClientIP_IgnoresXFFFromNonLoopback(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	got := getClientIP(req)
	assert.Equal(t, "203.0.113.1", got,
		"X-Forwarded-For should be ignored when RemoteAddr is not loopback and no trusted proxies")
}

func TestGetClientIP_TrustsXFFFromConfiguredProxy(t *testing.T) {
	t.Parallel()

	// Configure 172.17.0.0/16 as trusted proxy CIDR (typical Docker bridge)
	trusted, err := ParseTrustedProxies([]string{"172.17.0.0/16"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.17.0.2:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	got := getClientIP(req, trusted...)
	assert.Equal(t, "203.0.113.50", got,
		"X-Forwarded-For should be trusted when RemoteAddr is in configured trusted proxy CIDR")
}

// TestGetClientIP_SkipsTrustedHopsFromTheRight pins the read direction behind
// a chain of proxies: every entry a trusted proxy appended is skipped, and the
// first untrusted one from the right is the client. Anything left of it was
// written by the client and cannot select the rate-limit key.
func TestGetClientIP_SkipsTrustedHopsFromTheRight(t *testing.T) {
	t.Parallel()

	trusted, err := ParseTrustedProxies([]string{"172.17.0.0/16"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.17.0.2:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 203.0.113.50, 172.17.0.3")

	assert.Equal(t, "203.0.113.50", getClientIP(req, trusted...))
}

// TestGetClientIP_ReadsAllXFFLines covers a proxy that adds its own
// X-Forwarded-For line instead of extending the existing one: the lines form
// one list in order, so the appended line still decides.
func TestGetClientIP_ReadsAllXFFLines(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Add("X-Forwarded-For", "198.51.100.7")
	req.Header.Add("X-Forwarded-For", "203.0.113.50")

	assert.Equal(t, "203.0.113.50", getClientIP(req))
}

// TestRateLimiterAndLoginKeyAgree pins that the request limiter and the login
// limiter key a request the same way, so one cannot be fooled where the other
// is not.
func TestRateLimiterAndLoginKeyAgree(t *testing.T) {
	t.Parallel()

	rl := newRateLimiter(1, time.Minute)
	t.Cleanup(rl.close)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 203.0.113.50")

	assert.Equal(t, "203.0.113.50", rl.clientIP(req))
	assert.Equal(t, rl.clientIP(req), getClientIP(req))
}

func TestGetClientIP_IgnoresXFFFromUntrustedProxy(t *testing.T) {
	t.Parallel()

	// Configure only 10.0.0.0/8 as trusted
	trusted, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.17.0.2:1234" // NOT in 10.0.0.0/8
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	got := getClientIP(req, trusted...)
	assert.Equal(t, "172.17.0.2", got,
		"X-Forwarded-For should be ignored when RemoteAddr is NOT in trusted proxy CIDR")
}

func TestGetClientIP_TrustsXFFFromLoopback(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")

	got := getClientIP(req)
	assert.Equal(t, "203.0.113.1", got,
		"X-Forwarded-For should be trusted when RemoteAddr is loopback")
}

func TestGetClientIP_IPv6Loopback(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[::1]:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	got := getClientIP(req)
	assert.Equal(t, "10.0.0.1", got,
		"X-Forwarded-For should be trusted when RemoteAddr is IPv6 loopback")
}

func TestParseTrustedProxies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []string
		wantErr bool
		wantLen int
	}{
		{"empty", nil, false, 0},
		{"single CIDR", []string{"172.17.0.0/16"}, false, 1},
		{"plain IP becomes /32", []string{"10.0.0.1"}, false, 1},
		{"multiple", []string{"10.0.0.0/8", "172.16.0.0/12"}, false, 2},
		{"invalid IP", []string{"not-an-ip"}, true, 0},
		{"invalid CIDR", []string{"10.0.0.0/99"}, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			nets, err := ParseTrustedProxies(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, nets, tt.wantLen)
			}
		})
	}
}
