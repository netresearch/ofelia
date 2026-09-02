// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
)

// TestRateLimiterScope pins what the per-IP rate limiter covers: every
// request — API, rendered page, static assets — counts against the
// budget. Static assets are not free work: each response is compressed
// per request (embedded files carry no ModTime, so conditional requests
// never short-circuit into a 304), which is exactly what an
// unauthenticated flood would target. Only /live and /ready are exempt
// — a probe answered 429 reads as "unhealthy" and gets the daemon
// restarted. /health and /healthz are token-free but not exempt:
// GetHealth calls runtime.ReadMemStats per request (stop-the-world) and
// reports version and goroutine count, so an exemption there would be
// an unauthenticated, unthrottled GC pause.
func TestRateLimiterScope(t *testing.T) {
	t.Parallel()

	sched := &core.Scheduler{Jobs: []core.Job{}, Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)
	// The probe routes exist only after health registration.
	srv.RegisterHealthEndpoints(webpkg.NewHealthChecker(nil, nil, "test"))
	handler := srv.HTTPServer().Handler

	probe := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.0.2.77:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	// Static assets consume the budget and eventually 429.
	assetLimited := false
	for range 150 {
		if probe("/styles.css") == http.StatusTooManyRequests {
			assetLimited = true
			break
		}
	}
	if !assetLimited {
		t.Fatalf("static assets are not rate-limited")
	}

	// The same exhausted IP is limited on the page and the API too.
	if probe("/") != http.StatusTooManyRequests {
		t.Fatalf("the rendered page is not rate-limited")
	}
	if probe("/api/jobs") != http.StatusTooManyRequests {
		t.Fatalf("/api/ requests are no longer rate-limited")
	}

	// The orchestrator probes must never see 429, even from an
	// exhausted IP.
	for _, path := range []string{"/ready", "/live"} {
		if code := probe(path); code == http.StatusTooManyRequests {
			t.Fatalf("orchestrator probe %s rate-limited", path)
		}
	}

	// The health endpoints are expensive (ReadMemStats stops the world)
	// and must stay inside the budget.
	for _, path := range []string{"/health", "/healthz"} {
		if code := probe(path); code != http.StatusTooManyRequests {
			t.Fatalf("%s is exempt from rate limiting, got %d", path, code)
		}
	}
}
