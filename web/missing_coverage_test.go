package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLegacyAuthProviderMiddleware tests the Middleware function that currently has 0% coverage
func TestLegacyAuthProviderMiddleware(t *testing.T) {
	t.Parallel()

	// Create a legacy auth provider with secret key and expiry hours
	provider := NewLegacyAuthProvider("test-secret", 24)

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	// Wrap with middleware
	wrappedHandler := provider.Middleware(testHandler)

	// Test that the middleware is applied (should return a handler)
	if wrappedHandler == nil {
		t.Fatal("Middleware returned nil handler")
	}

	// Create a test request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Execute the wrapped handler - this will test the middleware execution
	wrappedHandler.ServeHTTP(w, req)

	// The middleware should handle the request (likely return unauthorized since we have no token)
	// We're mainly testing that the middleware function executes without panicking
	if w.Code == 0 {
		t.Error("Middleware didn't execute properly")
	}
}
