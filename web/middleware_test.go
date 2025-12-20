package web

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRateLimiter verifies rate limiting actually works
func TestRateLimiter(t *testing.T) {
	// Create rate limiter: 10 requests per second
	rl := newRateLimiter(10, time.Second)

	// Create a test handler that counts successful requests
	var successCount int32
	handler := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&successCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send 20 requests rapidly (should hit rate limit)
	var wg sync.WaitGroup
	var rateLimitedCount int32

	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "127.0.0.1:1234" // Same IP for all requests
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code == http.StatusTooManyRequests {
				atomic.AddInt32(&rateLimitedCount, 1)
			}
		}()
	}

	wg.Wait()

	// Check results
	success := atomic.LoadInt32(&successCount)
	limited := atomic.LoadInt32(&rateLimitedCount)

	t.Logf("Rate Limiting Test Results:")
	t.Logf("  Requests sent:        20")
	t.Logf("  Requests succeeded:   %d", success)
	t.Logf("  Requests rate limited: %d", limited)

	// Should have exactly 10 successful (the limit) and 10 rate limited
	if success != 10 {
		t.Errorf("Expected 10 successful requests, got %d", success)
	}
	if limited != 10 {
		t.Errorf("Expected 10 rate-limited requests, got %d", limited)
	}
}

// TestRateLimiterWindow verifies the time window works
func TestRateLimiterWindow(t *testing.T) {
	// Create rate limiter: 5 requests per 100ms
	rl := newRateLimiter(5, 100*time.Millisecond)

	var successCount int32
	handler := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&successCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// First batch: send 5 requests (should all succeed)
	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d in first batch failed: %d", i+1, w.Code)
		}
	}

	// 6th request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th request should be rate limited, got: %d", w.Code)
	}

	// Wait for window to reset
	time.Sleep(150 * time.Millisecond)

	// Should be able to send requests again
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Request after window reset failed: %d", w.Code)
	}

	t.Logf("Rate limiter window test passed: limits reset after time window")
}

// TestRateLimiterPerIP verifies rate limiting is per-IP
func TestRateLimiterPerIP(t *testing.T) {
	rl := newRateLimiter(3, time.Second)

	var successCount int32
	handler := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&successCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send 3 requests from IP1 and 3 from IP2
	ips := []string{"192.168.1.1:1234", "192.168.1.2:1234"}

	for _, ip := range ips {
		for i := range 3 {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = ip
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Request %d from %s failed: %d", i+1, ip, w.Code)
			}
		}

		// 4th request from same IP should be rate limited
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusTooManyRequests {
			t.Errorf("4th request from %s should be rate limited, got: %d", ip, w.Code)
		}
	}

	success := atomic.LoadInt32(&successCount)
	if success != 6 {
		t.Errorf("Expected 6 successful requests (3 per IP), got %d", success)
	}

	t.Logf("Per-IP rate limiting test passed: each IP has independent limits")
}

// TestSecurityHeaders verifies security headers are added
func TestSecurityHeaders(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test HTTP request (no TLS)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Check security headers
	headers := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"X-XSS-Protection":        "1; mode=block",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'",
	}

	for header, expected := range headers {
		actual := w.Header().Get(header)
		if actual != expected {
			t.Errorf("Header %s: expected %q, got %q", header, expected, actual)
		}
	}

	// HSTS should NOT be set for non-TLS
	if hsts := w.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("HSTS should not be set for non-TLS request, got: %q", hsts)
	}

	// Test HTTPS request (with TLS)
	req = httptest.NewRequest(http.MethodGet, "https://example.com/test", nil)
	req.TLS = &tls.ConnectionState{} // Simulate TLS
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// HSTS should be set for TLS
	hsts := w.Header().Get("Strict-Transport-Security")
	expectedHSTS := "max-age=31536000; includeSubDomains"
	if hsts != expectedHSTS {
		t.Errorf("HSTS for TLS: expected %q, got %q", expectedHSTS, hsts)
	}

	t.Logf("Security headers test passed: all headers correctly applied")
}

// Benchmark rate limiter performance
func BenchmarkRateLimiter(b *testing.B) {
	rl := newRateLimiter(1000, time.Second)

	handler := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})
}
