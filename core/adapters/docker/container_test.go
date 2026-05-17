// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// TestContainerServiceAdapter_Inspect_EmptyID pins the contract that
// Inspect("") returns a non-nil error and does NOT panic.
//
// The Docker SDK validates empty IDs locally (client.emptyIDError) before
// issuing any HTTP request, so this test does not require a running
// Docker daemon. Pure coverage — no production change required.
func TestContainerServiceAdapter_Inspect_EmptyID(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "Inspect with empty ID")()

	// Loopback SDK client — the SDK rejects the empty ID before
	// attempting to connect, so the host is never dialed.
	adapter := &ContainerServiceAdapter{client: newLoopbackSDKClient(t)}

	got, err := adapter.Inspect(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty container ID, got nil")
	}
	if got != nil {
		t.Errorf("expected nil container on error, got %+v", got)
	}
	// The SDK error message contains "empty" — keep the assertion loose
	// to avoid coupling to upstream wording, but verify the spirit.
	if !strings.Contains(strings.ToLower(err.Error()), "empty") &&
		!strings.Contains(strings.ToLower(err.Error()), "invalid") {
		t.Errorf("expected error mentioning empty/invalid ID, got: %v", err)
	}
}

// newStopRequestCapturer builds a ContainerServiceAdapter pointed at
// an httptest server that captures the query string of any POST to
// /containers/{id}/stop. The returned pointer is populated on each
// matched request; callers inspect it after invoking adapter.Stop.
// Extracted from the two #234 SDK-boundary tests below to keep
// duplication under SonarCloud's 3% budget on new code.
func newStopRequestCapturer(t *testing.T) (*ContainerServiceAdapter, *url.Values) {
	t.Helper()
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/stop") && r.Method == http.MethodPost {
			captured = r.URL.Query()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return &ContainerServiceAdapter{client: newSDKClientForStubServer(t, srv)}, &captured
}

// TestContainerServiceAdapter_Stop_PropagatesSignal pins the SDK
// boundary for the #234 stop-signal feature: domain.StopOptions.Signal
// must arrive at the Docker daemon as the documented `signal` query
// parameter on POST /containers/{id}/stop. Without this test, a
// refactor that drops `Signal: opts.Signal` from the
// containertypes.StopOptions struct literal at container.go:84 would
// silently break #234 and all core-level tests still pass (they only
// exercise the mock provider).
func TestContainerServiceAdapter_Stop_PropagatesSignal(t *testing.T) {
	t.Parallel()

	adapter, captured := newStopRequestCapturer(t)

	timeout := 15 * time.Second
	err := adapter.Stop(context.Background(), "test-container",
		domain.StopOptions{Timeout: &timeout, Signal: "SIGINT"})
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := captured.Get("signal"); got != "SIGINT" {
		t.Errorf("daemon `signal` query param = %q, want SIGINT (#234)", got)
	}
	if got := captured.Get("t"); got != "15" {
		t.Errorf("daemon `t` (timeout seconds) query param = %q, want 15", got)
	}
}

// TestContainerServiceAdapter_Stop_OmitsSignalWhenEmpty pins the
// inverse: an empty Signal (the default for legacy RunJobs without
// stop-signal configured) must NOT add a `signal=` query param so the
// daemon honors the image's STOPSIGNAL (which itself falls back to
// SIGTERM). The SDK uses omitempty semantics on the query string.
func TestContainerServiceAdapter_Stop_OmitsSignalWhenEmpty(t *testing.T) {
	t.Parallel()

	adapter, captured := newStopRequestCapturer(t)

	err := adapter.Stop(context.Background(), "test-container", domain.StopOptions{})
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := captured.Get("signal"); got != "" {
		t.Errorf("empty Signal must NOT be sent as a `signal=` query param (got %q); Docker would otherwise interpret a literal empty value differently than 'unset'", got)
	}
}
