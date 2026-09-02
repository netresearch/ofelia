package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netresearch/ofelia/core"
)

// The rate limiter used to sit inside the auth middleware, so a request
// answered with 401 was never counted: /api/* token guessing was the one
// kind of traffic the limiter never saw, while the same address was being
// metered on every static asset it fetched. Measured before the fix, 400
// unauthenticated requests to /api/jobs produced map[401:400] — no 429 at
// any point (#804).
//
// These tests pin both halves of the move: that unauthenticated API traffic
// is now counted, and that pushing the limiter outward did not start
// metering the orchestrator probes, which must never see a 429.

func newAuthedTestServer(t *testing.T) http.Handler {
	t.Helper()

	authCfg := &SecureAuthConfig{
		Enabled:  true,
		Username: "operator",
		// bcrypt of an arbitrary password; these tests never log in.
		PasswordHash: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
		SecretKey:    "0123456789abcdef0123456789abcdef",
		TokenExpiry:  24,
		MaxAttempts:  5,
	}
	srv := NewServerWithAuth("", core.NewScheduler(newDiscardLogger()), nil, nil, authCfg)
	if srv == nil {
		t.Fatal("NewServerWithAuth returned nil")
	}
	// The probe routes exist only after health registration.
	srv.RegisterHealthEndpoints(NewHealthChecker(nil, nil, "test"))
	t.Cleanup(func() {
		srv.rl.close()
		srv.tokenManager.Close()
	})
	return srv.HTTPServer().Handler
}

// requestFrom sends one request from a fixed address and returns the status.
func requestFrom(h http.Handler, path, addr string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = addr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code
}

func TestRateLimit_CountsUnauthenticatedAPIRequests(t *testing.T) {
	t.Parallel()

	h := newAuthedTestServer(t)
	const addr = "192.0.2.11:33333"

	// Every one of these is rejected by auth. The question is only whether
	// the limiter got to see them on the way.
	var sawUnauthorized bool
	for range 400 {
		switch requestFrom(h, "/api/jobs", addr) {
		case http.StatusUnauthorized:
			sawUnauthorized = true
		case http.StatusTooManyRequests:
			if !sawUnauthorized {
				t.Fatal("limited before a single request was answered by auth")
			}
			return // counted, and the budget ran out: what this test exists for
		}
	}
	t.Fatal("400 unauthenticated /api/jobs requests from one address never hit " +
		"the rate limit — 401s are not being counted (#804)")
}

func TestRateLimit_ExemptsProbesFromTheOuterPosition(t *testing.T) {
	t.Parallel()

	h := newAuthedTestServer(t)
	const addr = "192.0.2.12:44444"

	// Exhaust the address's budget on API traffic first.
	for range 400 {
		if requestFrom(h, "/api/jobs", addr) == http.StatusTooManyRequests {
			break
		}
	}

	// An orchestrator must never see a 429 on its probes, whichever side of
	// the auth middleware the limiter sits on.
	for _, probe := range []string{"/live", "/ready"} {
		if got := requestFrom(h, probe, addr); got == http.StatusTooManyRequests {
			t.Errorf("%s answered 429 from an exhausted address; probes must stay exempt", probe)
		}
	}
}
