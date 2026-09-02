// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// securityHeaders adds essential security headers to all responses
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Basic CSP - can be adjusted based on needs
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline';"+
				" style-src 'self' 'unsafe-inline'; img-src 'self' data:")

		// HSTS - only in production with HTTPS
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}

// rateLimiter provides basic rate limiting per IP
type rateLimiter struct {
	requests       map[string][]time.Time
	mu             sync.RWMutex
	limit          int
	window         time.Duration
	trustedProxies []*net.IPNet
	done           chan struct{}
	closeOnce      sync.Once
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
		done:     make(chan struct{}),
	}

	// Clean up old entries periodically
	go func() {
		ticker := time.NewTicker(window)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.cleanup()
			case <-rl.done:
				return
			}
		}
	}()

	return rl
}

func (rl *rateLimiter) close() { rl.closeOnce.Do(func() { close(rl.done) }) }

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, times := range rl.requests {
		// Keep only requests within the window
		var valid []time.Time
		for _, t := range times {
			if now.Sub(t) < rl.window {
				valid = append(valid, t)
			}
		}
		if len(valid) > 0 {
			rl.requests[ip] = valid
		} else {
			delete(rl.requests, ip)
		}
	}
}

// extractRemoteIP strips the port from a RemoteAddr string, handling both
// IPv4 ("1.2.3.4:port") and IPv6 ("[::1]:port") formats.
func extractRemoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// No port or unparseable — return as-is
		return remoteAddr
	}
	return host
}

// isLoopback returns true if the given IP string is a loopback address
// (127.0.0.0/8 for IPv4, ::1 for IPv6).
func isLoopback(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

// isTrustedProxy returns true if the given IP is in any of the trusted CIDR
// ranges. When trustedProxies is nil or empty, only loopback is trusted.
func isTrustedProxy(ip string, trustedProxies []*net.IPNet) bool {
	if isLoopback(ip) {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, cidr := range trustedProxies {
		if cidr.Contains(parsed) {
			return true
		}
	}
	return false
}

// ErrInvalidTrustedProxy indicates a trusted proxy value could not be parsed.
var ErrInvalidTrustedProxy = errors.New("invalid trusted proxy")

// ParseTrustedProxies parses a slice of CIDR strings (e.g. "172.17.0.0/16",
// "10.0.0.1/32") into []*net.IPNet. Plain IPs without a mask are treated as
// /32 (IPv4) or /128 (IPv6).
func ParseTrustedProxies(cidrs []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, s := range cidrs {
		if !strings.Contains(s, "/") {
			// Plain IP — add /32 or /128
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("%w: %q is not a valid IP", ErrInvalidTrustedProxy, s)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			s = ip.String() + "/" + fmt.Sprint(bits)
		}
		_, network, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", s, err)
		}
		nets = append(nets, network)
	}
	return nets, nil
}

// isOrchestratorProbePath reports whether path is one of the two probe
// endpoints an orchestrator polls: /live and /ready. They bypass rate
// limiting — a probe answered 429 reads as "unhealthy" and gets the
// daemon restarted — and both are cheap: a constant string, and a copy
// of the check map the background loop maintains. Keeping /ready cheap
// is what makes the exemption safe: it used to call
// runtime.ReadMemStats through GetHealth, so an unauthenticated caller
// could force one stop-the-world pause per request on an endpoint with
// no rate limit at all. GetHealth now reports the snapshot the periodic
// system check takes (web/health.go).
//
// /health and /healthz are deliberately NOT exempt. They are token-free
// like the probes, but they answer with the full report — every check,
// the version and the goroutine count — which is both more work per
// request and more than a probe needs to know. They stay inside the
// per-IP budget; the UI's single footer-version fetch fits in it easily.
func isOrchestratorProbePath(path string) bool {
	return path == "/live" || path == "/ready"
}

// middleware wraps an http.Handler with per-IP rate limiting. The client IP
// is determined from X-Forwarded-For or X-Real-IP headers only when the direct
// connection comes from a trusted proxy (loopback or configured CIDRs).
// For untrusted connections, forwarded headers are ignored to prevent IP spoofing.
func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The limiter counts every request — API, rendered page, and
		// static assets alike. Static assets are not free: each response
		// is compressed per request (embedded files carry no ModTime, so
		// conditional requests never 304), which is exactly the work an
		// unauthenticated flood would target. Only the orchestrator
		// probes (/live, /ready) are exempt: an orchestrator must never
		// see 429 on its probes. /health stays counted — see
		// isOrchestratorProbePath.
		if isOrchestratorProbePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		ip := extractRemoteIP(r.RemoteAddr)
		// Only trust forwarded headers from trusted proxies (loopback or configured CIDRs)
		if isTrustedProxy(ip, rl.trustedProxies) {
			if xForwarded := r.Header.Get("X-Forwarded-For"); xForwarded != "" {
				ip = strings.TrimSpace(strings.Split(xForwarded, ",")[0])
			} else if xRealIP := r.Header.Get("X-Real-IP"); xRealIP != "" {
				ip = xRealIP
			}
		}

		rl.mu.Lock()
		now := time.Now()

		// Get or create request history for this IP
		if rl.requests[ip] == nil {
			rl.requests[ip] = []time.Time{}
		}

		// Filter out old requests
		var valid []time.Time
		for _, t := range rl.requests[ip] {
			if now.Sub(t) < rl.window {
				valid = append(valid, t)
			}
		}

		// Check if limit exceeded
		if len(valid) >= rl.limit {
			rl.mu.Unlock()
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Add current request
		rl.requests[ip] = append(valid, now)
		rl.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}
