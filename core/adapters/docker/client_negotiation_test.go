// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"net"
	"net/http"
	"net/http/httptest"
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
