// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/moby/moby/client"
)

// Verifies the constructor negotiates the API version EAGERLY. The stub daemon
// reports an API version below the client's maximum, so a client that has
// negotiated reports the stub's version while one that has not still reports
// its own default. Without PingOptions.NegotiateAPIVersion the SDK defers
// negotiation to the first request (getAPIPath -> checkVersion), reinstating
// the startup race the warm-up exists to prevent.
func TestEagerNegotiationHappensAtConstruction(t *testing.T) {
	t.Parallel()

	const stubVersion = "1.44"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("API-Version", stubVersion)
		w.Header().Set("Ostype", "linux")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	host := "tcp://" + srv.Listener.Addr().(*net.TCPAddr).String()

	c, err := NewClientWithConfig(&ClientConfig{Host: host})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	got := c.SDK().ClientVersion()
	t.Logf("stub=%s clientVersion-after-construction=%s clientMax=%s", stubVersion, got, client.MaxAPIVersion)

	if got != stubVersion {
		t.Fatalf("client version after construction = %q, want %q: negotiation did not happen eagerly", got, stubVersion)
	}
}

// A pinned API version leaves nothing to negotiate, and the frozen SDK's
// NegotiateAPIVersion issued no request at all in that case (its body was
// wrapped in `if !cli.manualOverride`). Every branch of the split SDK's Ping
// performs a round-trip, so the warm-up has to be skipped explicitly —
// otherwise startup gains a blocking ping that a wedged daemon can stall for
// the full negotiate timeout, which is exactly the failure #608 addressed.
// Not parallel: the DOCKER_API_VERSION case uses t.Setenv.
func TestNoStartupPingWhenAPIVersionIsPinned(t *testing.T) {
	tests := []struct {
		name    string
		version string
		env     string
	}{
		{name: "pinned via config", version: "1.44"},
		{name: "pinned via DOCKER_API_VERSION", env: "1.44"},
		{name: "not pinned", version: "", env: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var paths []string

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
				w.Header().Set("API-Version", "1.44")
				w.Header().Set("Ostype", "linux")
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			if tt.env != "" {
				t.Setenv(client.EnvOverrideAPIVersion, tt.env)
			}

			host := "tcp://" + srv.Listener.Addr().(*net.TCPAddr).String()
			c, err := NewClientWithConfig(&ClientConfig{Host: host, Version: tt.version})
			if err != nil {
				t.Fatalf("NewClientWithConfig: %v", err)
			}
			t.Cleanup(func() { _ = c.Close() })

			mu.Lock()
			got := append([]string(nil), paths...)
			mu.Unlock()

			pinned := tt.version != "" || tt.env != ""
			if pinned && len(got) != 0 {
				t.Errorf("pinned version still issued %d startup request(s) %v, want none", len(got), got)
			}
			if !pinned && len(got) == 0 {
				t.Error("unpinned client issued no startup request, so it never negotiated eagerly")
			}
		})
	}
}
