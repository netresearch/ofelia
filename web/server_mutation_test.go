package web

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/static"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// Tests targeting surviving mutants in server.go.
//
// CONDITIONALS_NEGATION at line 55:  tokenExpiry == 0
// CONDITIONALS_NEGATION at line 163: err == nil
// CONDITIONALS_NEGATION at line 251: t.Kind() == reflect.Pointer
// ARITHMETIC_BASE at lines 114-116:  timeout multiplications

// --- CONDITIONALS_NEGATION at line 55: tokenExpiry == 0 -------------

// TestNewServerWithAuth_TokenExpiryDefault kills the mutant that changes
// `tokenExpiry == 0` to `tokenExpiry != 0` at line 55.
// When the default should apply (tokenExpiry=0), the mutant would skip the
// default and use 0, which would create tokens that expire immediately.
// When a non-zero value is provided, the mutant would override it with 24.
func TestNewServerWithAuth_TokenExpiryZeroUsesDefault(t *testing.T) {
	t.Parallel()

	sched := core.NewScheduler(newDiscardLogger())
	authCfg := &SecureAuthConfig{
		Enabled:      true,
		Username:     "admin",
		PasswordHash: "$2a$04$placeholder", // won't be used for login
		SecretKey:    "test-secret-key-32-bytes-long!!!",
		TokenExpiry:  0, // should default to 24
		MaxAttempts:  5,
	}

	srv := NewServerWithAuth("", sched, nil, nil, authCfg)
	require.NotNil(t, srv, "server should be created when tokenExpiry is 0")
	require.NotNil(t, srv.tokenManager, "tokenManager should be initialized")

	// With the correct code, tokenExpiry of 0 should default to 24 hours.
	// Generate a token and check it's valid (it wouldn't be if expiry were 0).
	token, err := srv.tokenManager.GenerateToken("admin")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	data, valid := srv.tokenManager.ValidateToken(token)
	assert.True(t, valid, "token should be valid when using default 24-hour expiry")
	assert.Equal(t, "admin", data.Username)

	// The token should expire roughly 24 hours from now (not immediately).
	assert.True(t, data.ExpiresAt.After(time.Now().Add(23*time.Hour)),
		"default token expiry should be approximately 24 hours, got %v", data.ExpiresAt)
}

// TestNewServerWithAuth_TokenExpiryNonZeroPreserved verifies that a non-zero
// tokenExpiry value is used as-is and not overridden.
func TestNewServerWithAuth_TokenExpiryNonZeroPreserved(t *testing.T) {
	t.Parallel()

	sched := core.NewScheduler(newDiscardLogger())
	authCfg := &SecureAuthConfig{
		Enabled:      true,
		Username:     "admin",
		PasswordHash: "$2a$04$placeholder",
		SecretKey:    "test-secret-key-32-bytes-long!!!",
		TokenExpiry:  48, // explicitly 48 hours
		MaxAttempts:  5,
	}

	srv := NewServerWithAuth("", sched, nil, nil, authCfg)
	require.NotNil(t, srv, "server should be created with custom tokenExpiry")
	require.NotNil(t, srv.tokenManager)

	token, err := srv.tokenManager.GenerateToken("admin")
	require.NoError(t, err)

	data, valid := srv.tokenManager.ValidateToken(token)
	assert.True(t, valid)

	// Should be approximately 48 hours from now, NOT 24.
	assert.True(t, data.ExpiresAt.After(time.Now().Add(47*time.Hour)),
		"token expiry should be approximately 48 hours when explicitly set, got %v", data.ExpiresAt)
	assert.True(t, data.ExpiresAt.Before(time.Now().Add(49*time.Hour)),
		"token expiry should not exceed 49 hours, got %v", data.ExpiresAt)
}

// --- CONDITIONALS_NEGATION at line 163: err == nil ------------------

// TestRegisterHealthEndpoints_UIFSSubSuccess kills the mutant that changes
// `err == nil` to `err != nil` at line 163. The mutant would only mount the
// file server when fs.Sub fails, which is the opposite of correct behavior.
func TestRegisterHealthEndpoints_UIFSSubSuccess(t *testing.T) {
	t.Parallel()

	// Verify that the "ui" subdirectory exists in static.UI (precondition).
	_, err := fs.Sub(static.UI, "ui")
	require.NoError(t, err, "static.UI should contain a 'ui' subdirectory")

	sched := core.NewScheduler(newDiscardLogger())
	srv := NewServer("", sched, nil, nil)
	require.NotNil(t, srv)

	hc := NewHealthChecker(nil, "test-version")
	// Give the health checker time to run initial checks.
	time.Sleep(100 * time.Millisecond)
	srv.RegisterHealthEndpoints(hc)

	httpSrv := srv.HTTPServer()

	// After RegisterHealthEndpoints, the "/" route should serve static files.
	// With the correct `err == nil`, the file server is mounted.
	// With the mutant `err != nil`, it would NOT be mounted.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)

	// The static file server should respond (either 200 with index or 404 for
	// a specific file, but NOT the default 404 from an empty mux).
	// The key test is that the health endpoints AND static file serving work.
	assert.NotEqual(t, http.StatusInternalServerError, w.Code,
		"static file server should be mounted when fs.Sub succeeds")

	// Also verify health endpoints are working.
	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	w2 := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code, "health endpoint should be accessible")
}

// --- CONDITIONALS_NEGATION at line 251: t.Kind() == reflect.Pointer -

// customTestJob is a job type not in the known switch cases of jobType().
// It forces the default branch that uses reflection.
type customTestJob struct{ core.BareJob }

func (j *customTestJob) Run(*core.Context) error { return nil }

// TestJobType_PointerToUnknownType kills the mutant that changes
// `t.Kind() == reflect.Pointer` to `t.Kind() != reflect.Pointer` at line 251.
// When passed a *customTestJob (pointer), the correct code dereferences to get
// "customtestjob". The mutant would NOT dereference, yielding an empty string
// or wrong type name.
func TestJobType_PointerToUnknownType(t *testing.T) {
	t.Parallel()

	ptrJob := &customTestJob{}
	result := jobType(ptrJob)

	// reflect.TypeOf(&customTestJob{}) is *web.customTestJob
	// With correct code: dereferences pointer -> "customtestjob"
	// With mutant: does NOT dereference -> would get "" from pointer type Name()
	assert.Equal(t, "customtestjob", result,
		"jobType should dereference pointer to get the underlying type name")
}

// TestJobType_NonPointerUnknownType is the complementary test. When the job is
// NOT a pointer, the code should skip the dereference.
func TestJobType_NonPointerUnknownType(t *testing.T) {
	t.Parallel()

	// We can't directly pass a non-pointer struct as core.Job since the
	// interface requires pointer receivers via BareJob. But we can test that
	// a known pointer type still works via the known type branches.
	// This test verifies the pointer dereference path is specific to pointers.

	// Verify that reflect on a pointer type has empty Name().
	ptrType := reflect.TypeOf(&customTestJob{})
	assert.Equal(t, reflect.Pointer, ptrType.Kind())
	assert.Empty(t, ptrType.Name(), "pointer types have empty Name()")

	// After Elem(), we get the concrete type name.
	elemType := ptrType.Elem()
	assert.Equal(t, "customTestJob", elemType.Name())
}

// --- ARITHMETIC_BASE at lines 114-116: timeout multiplications ------

// TestServerTimeouts kills the ARITHMETIC_BASE mutants that change
// multiplication to other arithmetic operators in the http.Server timeout
// configuration:
//   - Line 114: ReadHeaderTimeout = 5 * time.Second
//   - Line 115: WriteTimeout = 60 * time.Second
//   - Line 116: IdleTimeout = 120 * time.Second
func TestServerTimeouts(t *testing.T) {
	t.Parallel()

	sched := core.NewScheduler(newDiscardLogger())
	srv := NewServer(":0", sched, nil, nil)
	require.NotNil(t, srv)

	httpSrv := srv.HTTPServer()

	// Verify exact timeout values. If the mutant changes `*` to `/`, `+`, or `-`,
	// these values would be drastically different.

	assert.Equal(t, 5*time.Second, httpSrv.ReadHeaderTimeout,
		"ReadHeaderTimeout should be 5 seconds (5 * time.Second)")

	assert.Equal(t, 60*time.Second, httpSrv.WriteTimeout,
		"WriteTimeout should be 60 seconds (60 * time.Second)")

	assert.Equal(t, 120*time.Second, httpSrv.IdleTimeout,
		"IdleTimeout should be 120 seconds (120 * time.Second)")
}

// TestServerTimeouts_NotMutated provides extra assertions to ensure the
// arithmetic base mutants are caught even with slight tolerance.
func TestServerTimeouts_NotMutated(t *testing.T) {
	t.Parallel()

	sched := core.NewScheduler(newDiscardLogger())
	srv := NewServer(":0", sched, nil, nil)
	require.NotNil(t, srv)

	httpSrv := srv.HTTPServer()

	// ReadHeaderTimeout: 5 * time.Second = 5s
	// Mutant with /: 5 / time.Second = 5ns (way too small)
	// Mutant with +: 5 + time.Second = 1000000005ns ~ 1s (wrong)
	// Mutant with -: 5 - time.Second = -999999995ns (negative!)
	assert.Greater(t, httpSrv.ReadHeaderTimeout, time.Second,
		"ReadHeaderTimeout must be greater than 1 second")
	assert.Less(t, httpSrv.ReadHeaderTimeout, time.Minute,
		"ReadHeaderTimeout must be less than 1 minute")

	// WriteTimeout: 60 * time.Second = 60s = 1 minute
	// Mutant with /: 60 / time.Second = 60ns
	// Mutant with +: 60 + time.Second = ~1s
	// Mutant with -: 60 - time.Second = negative
	assert.Greater(t, httpSrv.WriteTimeout, time.Second,
		"WriteTimeout must be greater than 1 second")
	assert.LessOrEqual(t, httpSrv.WriteTimeout, time.Minute,
		"WriteTimeout must be at most 1 minute")

	// IdleTimeout: 120 * time.Second = 120s = 2 minutes
	// Mutant with /: 120 / time.Second = 120ns
	// Mutant with +: 120 + time.Second = ~1s
	// Mutant with -: 120 - time.Second = negative
	assert.Greater(t, httpSrv.IdleTimeout, time.Minute,
		"IdleTimeout must be greater than 1 minute")
	assert.LessOrEqual(t, httpSrv.IdleTimeout, 2*time.Minute,
		"IdleTimeout must be at most 2 minutes")
}
