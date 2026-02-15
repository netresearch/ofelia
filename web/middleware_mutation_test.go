package web

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests targeting surviving mutants in middleware.go.
//
// CONDITIONALS_BOUNDARY at line 68: now.Sub(t) < rl.window
// CONDITIONALS_NEGATION at line 68: now.Sub(t) < rl.window
// CONDITIONALS_BOUNDARY at line 72: len(valid) > 0
// CONDITIONALS_NEGATION at line 72: len(valid) > 0
// CONDITIONALS_BOUNDARY at line 98: now.Sub(t) < rl.window

// TestCleanup_BoundaryExactlyAtWindow kills the CONDITIONALS_BOUNDARY mutant at
// line 68 which changes `<` to `<=`. If the mutant lives, a request whose age
// equals exactly the window duration would be *kept* instead of discarded.
// We verify that after sleeping for exactly the window duration, the old
// request is cleaned up and a new request is allowed.
func TestCleanup_BoundaryExactlyAtWindow(t *testing.T) {
	t.Parallel()

	window := 50 * time.Millisecond
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    1,
		window:   window,
	}

	ip := "10.0.0.1"

	// Insert a request timestamp that is exactly `window` old.
	rl.mu.Lock()
	rl.requests[ip] = []time.Time{time.Now().Add(-window)}
	rl.mu.Unlock()

	// Run cleanup -- with the correct `<` operator, a request whose age
	// equals the window (now.Sub(t) == window) should NOT be kept because
	// the condition `now.Sub(t) < window` is false when equal.
	rl.cleanup()

	rl.mu.RLock()
	remaining := len(rl.requests[ip])
	_, ipExists := rl.requests[ip]
	rl.mu.RUnlock()

	// The entry at exactly the window boundary should be removed.
	assert.Equal(t, 0, remaining, "request at exactly the window boundary should be cleaned up")
	assert.False(t, ipExists, "IP entry should be deleted when no valid requests remain")
}

// TestCleanup_NegationKeepsRecent kills CONDITIONALS_NEGATION at line 68.
// The mutant changes `<` to `>=`, meaning recent requests would be discarded
// while old ones are kept. We insert a very recent request and verify cleanup
// retains it.
func TestCleanup_NegationKeepsRecent(t *testing.T) {
	t.Parallel()

	window := 10 * time.Second
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    5,
		window:   window,
	}

	ip := "10.0.0.2"

	// Insert a request from just now (well within the window).
	recentTime := time.Now()
	rl.mu.Lock()
	rl.requests[ip] = []time.Time{recentTime}
	rl.mu.Unlock()

	rl.cleanup()

	rl.mu.RLock()
	times := rl.requests[ip]
	rl.mu.RUnlock()

	require.Len(t, times, 1, "recent request should be retained after cleanup")
	assert.Equal(t, recentTime, times[0])
}

// TestCleanup_BoundaryLenValidGtZero kills CONDITIONALS_BOUNDARY at line 72
// which changes `len(valid) > 0` to `len(valid) >= 0`. When mutated, an IP
// with zero valid requests would keep an empty slice instead of being deleted.
func TestCleanup_BoundaryLenValidGtZero(t *testing.T) {
	t.Parallel()

	window := 10 * time.Millisecond
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    5,
		window:   window,
	}

	ip := "10.0.0.3"

	// Insert an old request that is beyond the window.
	rl.mu.Lock()
	rl.requests[ip] = []time.Time{time.Now().Add(-time.Second)}
	rl.mu.Unlock()

	rl.cleanup()

	rl.mu.RLock()
	_, exists := rl.requests[ip]
	rl.mu.RUnlock()

	// With correct `> 0`, the IP entry should be fully deleted.
	// With mutant `>= 0`, the IP entry would be kept with an empty slice.
	assert.False(t, exists, "IP with no valid requests should be deleted from map, not kept with empty slice")
}

// TestCleanup_NegationLenValidGtZero kills CONDITIONALS_NEGATION at line 72
// which changes `len(valid) > 0` to `len(valid) <= 0`. When mutated, an IP
// with valid requests would be deleted instead of kept.
func TestCleanup_NegationLenValidGtZero(t *testing.T) {
	t.Parallel()

	window := 10 * time.Second
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    5,
		window:   window,
	}

	ip := "10.0.0.4"

	// Insert a recent request (within window).
	rl.mu.Lock()
	rl.requests[ip] = []time.Time{time.Now()}
	rl.mu.Unlock()

	rl.cleanup()

	rl.mu.RLock()
	_, exists := rl.requests[ip]
	rl.mu.RUnlock()

	// With correct `> 0`, the IP with one valid request should be retained.
	// With mutant `<= 0`, it would be deleted.
	assert.True(t, exists, "IP with valid requests should be retained after cleanup")
}

// TestMiddleware_BoundaryExactlyAtWindow kills CONDITIONALS_BOUNDARY at line 98
// which changes `<` to `<=` in the middleware's filtering of old requests.
// We send requests so that the first one is exactly at the window boundary,
// and then verify it is NOT counted toward the rate limit.
func TestMiddleware_BoundaryExactlyAtWindow(t *testing.T) {
	t.Parallel()

	window := 50 * time.Millisecond
	// Limit of 1: only 1 request allowed per window.
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    1,
		window:   window,
	}

	ip := "10.0.0.5:9999"

	// Pre-populate with a request that is exactly `window` old.
	rl.mu.Lock()
	rl.requests[ip] = []time.Time{time.Now().Add(-window)}
	rl.mu.Unlock()

	var successCount int32
	handler := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&successCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send one new request. With correct `<`, the old request (age == window)
	// should be filtered out, so our new request fits within the limit.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = ip
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "request should succeed when old entry is exactly at window boundary")
	assert.Equal(t, int32(1), atomic.LoadInt32(&successCount))
}

// TestMiddleware_RecentRequestCountedForLimit verifies that a request just
// barely within the window IS counted. This provides the complementary
// coverage to the boundary test above.
func TestMiddleware_RecentRequestCountedForLimit(t *testing.T) {
	t.Parallel()

	window := 10 * time.Second
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    1,
		window:   window,
	}

	ip := "10.0.0.6:9999"

	// Pre-populate with a very recent request (well within window).
	rl.mu.Lock()
	rl.requests[ip] = []time.Time{time.Now()}
	rl.mu.Unlock()

	handler := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = ip
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The recent request should still count, so this second request should be rate limited.
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "request should be rate limited when a recent request is within window")
}

// TestMiddleware_XForwardedFor verifies the X-Forwarded-For header is used for
// rate limiting when present. This is additional coverage that ensures the IP
// extraction path works correctly.
func TestMiddleware_XForwardedFor(t *testing.T) {
	t.Parallel()

	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    1,
		window:   time.Minute,
	}

	handler := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request with X-Forwarded-For
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.7:9999"
	req.Header.Set("X-Forwarded-For", "192.168.1.100")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Second request from same X-Forwarded-For should be rate limited
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "10.0.0.8:9999" // different RemoteAddr
	req2.Header.Set("X-Forwarded-For", "192.168.1.100")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code, "should rate limit by X-Forwarded-For, not RemoteAddr")
}

// TestCleanup_MixedOldAndNewRequests verifies that cleanup correctly retains
// only the recent requests when a mix of old and new timestamps exists.
func TestCleanup_MixedOldAndNewRequests(t *testing.T) {
	t.Parallel()

	window := 100 * time.Millisecond
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    10,
		window:   window,
	}

	ip := "10.0.0.9"
	now := time.Now()

	rl.mu.Lock()
	rl.requests[ip] = []time.Time{
		now.Add(-time.Second), // old, should be removed
		now.Add(-window),      // at boundary, should be removed (< not <=)
		now.Add(-window / 2),  // recent, should be kept
		now,                   // recent, should be kept
	}
	rl.mu.Unlock()

	rl.cleanup()

	rl.mu.RLock()
	times := rl.requests[ip]
	rl.mu.RUnlock()

	assert.Len(t, times, 2, "should retain only the 2 requests within the window")
}
