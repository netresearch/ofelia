// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

// authCookie returns a session cookie for srv's configured user.
func authCookie(t *testing.T, srv *Server) *http.Cookie {
	t.Helper()
	token, err := srv.tokenManager.GenerateToken("admin")
	require.NoError(t, err)
	return &http.Cookie{Name: "auth_token", Value: token}
}

// postJob sends a job-create request through srv's full handler chain.
func postJob(srv *Server, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)
	return w
}

func jobBody(name string) string {
	return fmt.Sprintf(`{"name":%q,"type":"local","schedule":"@every 1h","command":"echo hi"}`, name)
}

// TestCrossOriginWritesAreRejected pins the cross-origin check on
// state-changing requests. The session cookie is SameSite=Strict, but another
// port on the same host counts as the same site, so the cookie alone does
// not stop a page served there from creating a job.
func TestCrossOriginWritesAreRejected(t *testing.T) {
	t.Parallel()

	srv, _ := newAuthTestServer(t)
	cookie := authCookie(t, srv)

	tests := []struct {
		name    string
		headers map[string]string
		want403 bool
	}{
		{"another site", map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		{"same site, other origin", map[string]string{"Sec-Fetch-Site": "same-site"}, true},
		{"origin mismatch without Sec-Fetch-Site", map[string]string{"Origin": "http://example.com:8080"}, true},
		{"same origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, false},
		{"non-browser client", nil, false},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := postJob(srv, jobBody(fmt.Sprintf("cross-origin-%d", i)), cookie, tc.headers)
			if tc.want403 {
				assert.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
			} else {
				assert.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
			}
		})
	}
}

// TestCrossOriginWritesAreRejectedWithoutAuth covers the default
// configuration: with auth disabled there is no cookie to withhold, so the
// cross-origin check is the only thing between a web page and job creation.
func TestCrossOriginWritesAreRejectedWithoutAuth(t *testing.T) {
	t.Parallel()

	srv := NewServerWithAuth("", core.NewScheduler(newDiscardLogger()), nil, nil, nil)
	require.NotNil(t, srv)
	t.Cleanup(srv.rl.close)

	w := postJob(srv, jobBody("no-auth-cross-site"), nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	assert.Equal(t, http.StatusForbidden, w.Code)

	w = postJob(srv, jobBody("no-auth-same-origin"), nil, map[string]string{"Sec-Fetch-Site": "same-origin"})
	assert.Equal(t, http.StatusCreated, w.Code)
}

// TestCrossOriginReadsStayAllowed pins that the check leaves safe methods
// alone, so the dashboard keeps loading behind proxies that set odd Origins.
func TestCrossOriginReadsStayAllowed(t *testing.T) {
	t.Parallel()

	srv, _ := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(authCookie(t, srv))
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequestBodyIsCapped pins the server-wide body limit: a job definition
// past maxRequestBodyBytes is refused and nothing is created, while the same
// definition at normal size goes through.
func TestRequestBodyIsCapped(t *testing.T) {
	t.Parallel()

	srv, _ := newAuthTestServer(t)
	cookie := authCookie(t, srv)

	w := postJob(srv, jobBody("small-enough"), cookie, nil)
	require.Equal(t, http.StatusCreated, w.Code, "control: a normal body must be accepted")

	padding := strings.Repeat("a", maxRequestBodyBytes)
	big := fmt.Sprintf(`{"name":"too-big","type":"local","schedule":"@every 1h","command":"echo hi","pad":%q}`, padding)
	w = postJob(srv, big, cookie, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	for _, j := range srv.scheduler.GetActiveJobs() {
		assert.NotEqual(t, "too-big", j.GetName(), "an oversized body must not create a job")
	}
}

// TestLoginBodyIsCapped pins the much tighter limit on the unauthenticated
// login endpoint.
func TestLoginBodyIsCapped(t *testing.T) {
	t.Parallel()

	srv, _ := newAuthTestServer(t)

	login := func(password string) *httptest.ResponseRecorder {
		csrf, err := srv.tokenManager.GenerateCSRFToken()
		require.NoError(t, err)
		body, err := json.Marshal(map[string]string{"username": "admin", "password": password})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, pathAPILogin, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(w, req)
		return w
	}

	assert.Equal(t, http.StatusUnauthorized, login("wrong").Code,
		"control: a normal-size body reaches the credential check")
	assert.Equal(t, http.StatusRequestEntityTooLarge, login(strings.Repeat("a", maxLoginBodyBytes)).Code)
}

// TestTokenResponsesAreNotCached pins Cache-Control: no-store on the two
// responses that carry a secret.
func TestTokenResponsesAreNotCached(t *testing.T) {
	t.Parallel()

	srv, _ := newAuthTestServer(t)
	hash, err := HashPassword("correct-password")
	require.NoError(t, err)
	srv.authConfig.PasswordHash = hash

	req := httptest.NewRequest(http.MethodGet, pathAPICSRFToken, nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "csrf-token response")

	var tok map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))

	req = httptest.NewRequest(http.MethodPost, pathAPILogin,
		strings.NewReader(`{"username":"admin","password":"correct-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", tok["csrf_token"])
	w = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "login response")
}

// TestCSRFTokenStoreIsBounded pins the cap on outstanding CSRF tokens:
// expired tokens are swept first, then the one closest to expiry goes, and a
// freshly issued token is always kept.
func TestCSRFTokenStoreIsBounded(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("", 1)
	require.NoError(t, err)
	t.Cleanup(tm.Close)

	now := time.Now()
	tm.csrfMu.Lock()
	for i := range maxCSRFTokens {
		tm.csrfTokens[fmt.Sprintf("filler-%d", i)] = now.Add(time.Duration(i+1) * time.Second)
	}
	tm.csrfMu.Unlock()

	fresh, err := tm.GenerateCSRFToken()
	require.NoError(t, err)

	tm.csrfMu.RLock()
	size := len(tm.csrfTokens)
	_, oldestKept := tm.csrfTokens["filler-0"]
	_, nextKept := tm.csrfTokens["filler-1"]
	tm.csrfMu.RUnlock()

	assert.Equal(t, maxCSRFTokens, size, "the store must not grow past the cap")
	assert.False(t, oldestKept, "the token closest to expiry is evicted")
	assert.True(t, nextKept, "only one token is evicted per issue")
	assert.True(t, tm.ValidateCSRFToken(fresh), "the token just issued must be redeemable")
}

// TestCSRFTokenStoreSweepsExpiredBeforeEvicting pins that a full store frees
// expired tokens first, so no live token is evicted while dead ones remain.
func TestCSRFTokenStoreSweepsExpiredBeforeEvicting(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("", 1)
	require.NoError(t, err)
	t.Cleanup(tm.Close)

	now := time.Now()
	tm.csrfMu.Lock()
	for i := range maxCSRFTokens {
		expiry := now.Add(time.Hour)
		if i%2 == 0 {
			expiry = now.Add(-time.Second)
		}
		tm.csrfTokens[fmt.Sprintf("filler-%d", i)] = expiry
	}
	tm.csrfMu.Unlock()

	_, err = tm.GenerateCSRFToken()
	require.NoError(t, err)

	tm.csrfMu.RLock()
	defer tm.csrfMu.RUnlock()
	assert.Len(t, tm.csrfTokens, maxCSRFTokens/2+1, "every expired token is swept, no live one evicted")
}

// TestCSRFTokenLifetime pins the shortened validity window.
func TestCSRFTokenLifetime(t *testing.T) {
	t.Parallel()

	tm, err := NewSecureTokenManager("", 1)
	require.NoError(t, err)
	t.Cleanup(tm.Close)

	before := time.Now()
	token, err := tm.GenerateCSRFToken()
	require.NoError(t, err)

	tm.csrfMu.RLock()
	expiry := tm.csrfTokens[token]
	tm.csrfMu.RUnlock()
	assert.WithinDuration(t, before.Add(csrfTokenTTL), expiry, time.Second)
}

// TestLoginLimiterCleanupEvictsIdleBuckets pins that the eviction loop runs:
// without it, one bucket per client address stays for the daemon's lifetime.
func TestLoginLimiterCleanupEvictsIdleBuckets(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(5, 5)
	rl.GetLimiter("198.51.100.7")
	rl.mu.Lock()
	rl.lastAccess["198.51.100.7"] = time.Now().Add(-time.Hour)
	rl.mu.Unlock()

	stop := rl.StartCleanup(10*time.Millisecond, time.Minute)
	t.Cleanup(stop)

	assert.Eventually(t, func() bool {
		rl.mu.RLock()
		defer rl.mu.RUnlock()
		return len(rl.limiters) == 0
	}, 2*time.Second, 10*time.Millisecond)

	stop()
	stop() // a second call must not panic
}

// TestServerEvictsIdleLoginBuckets pins that NewServerWithAuth starts the
// eviction loop on its own login limiter, not merely that a stop function
// exists. Not parallel: it shortens loginLimiterCleanupInterval, which every
// server built meanwhile would read.
func TestServerEvictsIdleLoginBuckets(t *testing.T) {
	saved := loginLimiterCleanupInterval
	loginLimiterCleanupInterval = 10 * time.Millisecond
	t.Cleanup(func() { loginLimiterCleanupInterval = saved })

	srv, _ := newAuthTestServer(t)
	srv.loginLimiter.GetLimiter("198.51.100.7")
	srv.loginLimiter.mu.Lock()
	srv.loginLimiter.lastAccess["198.51.100.7"] = time.Now().Add(-time.Hour)
	srv.loginLimiter.mu.Unlock()

	assert.Eventually(t, func() bool {
		srv.loginLimiter.mu.RLock()
		defer srv.loginLimiter.mu.RUnlock()
		return len(srv.loginLimiter.limiters) == 0
	}, 2*time.Second, 10*time.Millisecond, "an idle login bucket must be evicted")
}

// TestServerWiresHardening pins that NewServerWithAuth actually installs the
// eviction loop and the read timeout, so the pieces above are not dead code.
func TestServerWiresHardening(t *testing.T) {
	t.Parallel()

	srv, _ := newAuthTestServer(t)
	assert.NotNil(t, srv.stopLoginCleanup, "login limiter eviction must be started")
	assert.Equal(t, readTimeout, srv.srv.ReadTimeout)
}
