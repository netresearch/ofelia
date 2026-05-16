// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
)

// TestPlainHijack_ContainerExecAttachWorks pins the hijack-path fixes for
// both #668 (tcp://) and #682 (http://). Replaces the t.Skip placeholder
// TestPlainHTTPHijack_ContainerExecAttach_FollowUp introduced in PR #681.
//
// Failure modes pinned:
//   - tcp://: pre-#681, the first plain-HTTP request mutated
//     baseTransport.TLSClientConfig (Go's lazy HTTP/2 auto-config), and the
//     SDK's hijack dialer then mis-routed to tls.Dial — surfacing as
//     "tls: first record does not look like a TLS handshake".
//   - http://: pre-#682, the SDK's hijack dialer fell to
//     net.Dial("http", addr) which Go rejects with "unknown network http".
//
// Subtests share the same plain-HTTP TCP listener and a tight helper, so
// only the host string differs. The path-component and IPv6 cases pin the
// url.Parse behavior in applyHTTPTransport against a future regression to
// strings.TrimPrefix (which would feed "host:port/v1.43" to net.Dial).
func TestPlainHijack_ContainerExecAttachWorks(t *testing.T) {
	srv, addr := newFakePlainDockerProxy(t)
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	// IPv6 sibling addr — same listener bound to ::1 so the dialer
	// resolution path is exercised. Skipped on hosts without IPv6 loopback.
	v6Addr := newFakePlainDockerProxyV6(t)

	cases := []struct {
		name       string
		host       string
		skipIfNoV6 bool
	}{
		{"tcp", "tcp://" + addr, false},
		{"http", "http://" + addr, false},
		// Path-bearing host: the SDK's ParseHostURL splits the path off
		// for tcp:// (via url.Parse), so http:// should behave the same.
		// Pre-url.Parse our TrimPrefix would have fed "addr/v1.43" to
		// net.Dial and failed.
		{"http+pathcomponent", "http://" + addr + "/v1.43", false},
		// IPv6 literal — square brackets must round-trip through url.Parse.
		{"http+ipv6", "http://" + v6Addr, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipIfNoV6 && v6Addr == "" {
				t.Skip("no IPv6 loopback listener available")
			}
			assertHijackOK(t, tc.host)
		})
	}
}

// assertHijackOK builds a Docker client against host, primes the regular
// HTTP path with a Ping (the request that previously mutated
// TLSClientConfig — see #668), then drives ContainerExecAttach down the
// hijack path. Verified bidirectionally for both #668 and #682.
func assertHijackOK(t *testing.T, host string) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Host = host
	cfg.NegotiateTimeout = 500 * time.Millisecond

	c, err := NewClientWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewClientWithConfig(%q): %v", host, err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, pingErr := c.SDK().Ping(ctx); pingErr != nil {
		t.Fatalf("Ping(%q): %v", host, pingErr)
	}

	resp, err := c.SDK().ContainerExecAttach(ctx, "deadbeef", containertypes.ExecAttachOptions{})
	if err != nil {
		t.Fatalf("ContainerExecAttach(%q): %v", host, err)
	}
	defer resp.Close()
	_, _ = io.Copy(io.Discard, resp.Reader)
}

// TestDisableHTTP2AutoConfig_KeepsTLSClientConfigNil is a focused unit test
// on the helper. It documents the underlying stdlib contract directly: a
// transport with TLSNextProto set to a non-nil (empty) map must NOT have
// TLSClientConfig auto-mutated on the first request.
//
// Without this, a future stdlib refactor that changes the auto-config
// trigger could silently re-enable the #668 bug while the end-to-end
// regression test still passes (because the SDK might pick a different
// dial path). This test fails fast and on the right layer.
func TestDisableHTTP2AutoConfig_KeepsTLSClientConfigNil(t *testing.T) {
	ts := httpTestServer(t)
	t.Cleanup(func() { _ = ts.Shutdown(context.Background()) })

	transport := &http.Transport{}
	disableHTTP2AutoConfig(transport)
	httpClient := &http.Client{Transport: transport, Timeout: 2 * time.Second}

	// Trigger Go's lazy onceSetNextProtoDefaults via a real request.
	resp, err := httpClient.Get("http://" + ts.Addr)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = resp.Body.Close()

	if transport.TLSClientConfig != nil {
		t.Fatalf("disableHTTP2AutoConfig did not suppress lazy http2 setup: TLSClientConfig = %+v (expected nil)", transport.TLSClientConfig)
	}
}

// TestApplyTLSTransport_DoesNotDisableHTTP2AutoConfig is the negative
// control for the #668 fix: TLS apply paths (https://, tcp+tls://) MUST
// leave TLSNextProto nil so ALPN h2 negotiation still works. If a refactor
// accidentally adds disableHTTP2AutoConfig to the TLS path, HTTPS Docker
// hosts silently lose HTTP/2.
func TestApplyTLSTransport_DoesNotDisableHTTP2AutoConfig(t *testing.T) {
	transport := &http.Transport{}
	applyTLSTransport(transport, DefaultConfig(), "https://example.invalid:2376")

	if transport.TLSNextProto != nil {
		t.Fatalf("applyTLSTransport set TLSNextProto (got %v); TLS paths must keep it nil so ALPN h2 negotiation works", transport.TLSNextProto)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatalf("applyTLSTransport must set ForceAttemptHTTP2=true (got false)")
	}
}

// TestSchemeHandler_NonTLSCapableSuppressesHTTP2 asserts the structural
// invariant from #683: every scheme registered with tlsCapable=false in
// schemeHandlers MUST end up with TLSNextProto suppressed after going
// through createHTTPClient. Adding a new non-TLS scheme handler now only
// requires registering it with tlsCapable=false — the suppression follows
// from the dispatch, not from a manually-duplicated call inside each apply
// function. Replaces the per-apply-function check that lived here before
// #683's data-driven refactor.
func TestSchemeHandler_NonTLSCapableSuppressesHTTP2(t *testing.T) {
	// Static host samples for each scheme. Values don't matter beyond
	// parsing — createHTTPClient never dials.
	hostFor := map[string]string{
		schemeUnix:   "unix:///var/run/docker.sock",
		schemeTCP:    "tcp://127.0.0.1:2375",
		schemeHTTP:   "http://127.0.0.1:2375",
		schemeHTTPS:  "https://127.0.0.1:2376",
		schemeNpipe:  "npipe://./pipe/docker_engine",
		schemeTCPTLS: "tcp+tls://127.0.0.1:2376",
	}
	for scheme, handler := range schemeHandlers {
		if handler.tlsCapable {
			continue
		}
		t.Run(scheme, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Host = hostFor[scheme]
			client := createHTTPClient(cfg)
			transport, ok := client.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("createHTTPClient returned non-*http.Transport: %T", client.Transport)
			}
			if transport.TLSNextProto == nil {
				t.Fatalf("createHTTPClient(%s) left TLSNextProto nil — non-TLS scheme must suppress HTTP/2 auto-config (#668, structural fix in #683)", scheme)
			}
		})
	}
}

// TestSchemeHandler_TLSCapableMeansHTTP2Allowed locks the inverse half of
// #683's structural invariant: every scheme registered with tlsCapable=true
// MUST keep TLSNextProto nil so ALPN h2 negotiation works. If a refactor
// accidentally flips a TLS-capable scheme into the suppression path, HTTPS
// Docker hosts silently lose HTTP/2.
func TestSchemeHandler_TLSCapableMeansHTTP2Allowed(t *testing.T) {
	hostFor := map[string]string{
		schemeHTTPS:  "https://127.0.0.1:2376",
		schemeTCPTLS: "tcp+tls://127.0.0.1:2376",
	}
	for scheme, handler := range schemeHandlers {
		if !handler.tlsCapable {
			continue
		}
		t.Run(scheme, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Host = hostFor[scheme]
			client := createHTTPClient(cfg)
			transport, ok := client.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("createHTTPClient returned non-*http.Transport: %T", client.Transport)
			}
			if transport.TLSNextProto != nil {
				t.Fatalf("createHTTPClient(%s) set TLSNextProto on a tlsCapable scheme — ALPN h2 negotiation now broken (#683)", scheme)
			}
			if !transport.ForceAttemptHTTP2 {
				t.Fatalf("createHTTPClient(%s) did not set ForceAttemptHTTP2=true on a tlsCapable scheme", scheme)
			}
		})
	}
}

// addrServer wraps an *http.Server with its listen address. Lets cleanup
// route through Shutdown so the serve goroutine exits before t.Logf can
// race with test completion.
type addrServer struct {
	*http.Server
	Addr string
	done <-chan struct{}
}

func (a *addrServer) Shutdown(ctx context.Context) error {
	err := a.Server.Shutdown(ctx)
	// Wait for the serve goroutine to exit so no late t.Log races test end.
	if a.done != nil {
		select {
		case <-a.done:
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	return err
}

// newFakePlainDockerProxy stands up a minimal plain-HTTP TCP listener that
// mimics tecnativa/docker-socket-proxy enough to drive ContainerExecAttach
// down the SDK's hijack path.
func newFakePlainDockerProxy(t *testing.T) (*addrServer, string) {
	t.Helper()
	return startTestServer(t, fakePlainDockerHandler())
}

// newFakePlainDockerProxyV6 stands up the same handler on the IPv6
// loopback. Returns the bracketed address (e.g. "[::1]:NNNN") suitable
// for DOCKER_HOST. Returns "" if [::1] isn't listenable on this host
// (CI environments without IPv6 loopback enabled).
func newFakePlainDockerProxyV6(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		// IPv6 loopback may be disabled (some CI / container envs); skip.
		return ""
	}
	done := make(chan struct{})
	var once sync.Once
	srv := &addrServer{
		Server: &http.Server{
			Handler:           fakePlainDockerHandler(),
			ReadHeaderTimeout: 2 * time.Second,
		},
		Addr: ln.Addr().String(),
		done: done,
	}
	go func() {
		defer once.Do(func() { close(done) })
		if serveErr := srv.Server.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Logf("fake proxy v6 serve: %v", serveErr)
		}
	}()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	return srv.Addr
}

// httpTestServer is a generic plain-HTTP listener used by the helper-only
// unit test that just needs to fire one round-trip.
func httpTestServer(t *testing.T) *addrServer {
	t.Helper()
	srv, _ := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	return srv
}

// startTestServer binds 127.0.0.1:0, serves handler in a goroutine, and
// returns an addrServer whose Shutdown synchronizes with the serve loop
// to avoid t.Logf races at test exit.
func startTestServer(t *testing.T, handler http.Handler) (*addrServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	var once sync.Once
	srv := &addrServer{
		Server: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 2 * time.Second,
		},
		Addr: ln.Addr().String(),
		done: done,
	}
	go func() {
		defer once.Do(func() { close(done) })
		if serveErr := srv.Server.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			// Best-effort log; the t.Helper + done sync above gates this
			// before test cleanup completes.
			t.Logf("fake proxy serve: %v", serveErr)
		}
	}()
	return srv, srv.Addr
}

// fakePlainDockerHandler answers the minimum routes the Docker SDK touches
// during client setup + ContainerExecAttach: _ping, exec create, exec start
// (hijack).
func fakePlainDockerHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/_ping"):
			w.Header().Set("API-Version", "1.43")
			w.Header().Set("OSType", "linux")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		case strings.HasSuffix(path, "/exec") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"deadbeef"}`))
		case strings.Contains(path, "/exec/") && strings.HasSuffix(path, "/start") && r.Method == http.MethodPost:
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijacker", http.StatusInternalServerError)
				return
			}
			conn, _, hjErr := hj.Hijack()
			if hjErr != nil {
				return
			}
			defer conn.Close()
			_, _ = conn.Write([]byte("HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	})
}

// Keep tls package imported (used by helper signature). Empty struct used
// rather than _ = tls.Config{} so the import stays without a dead value.
var _ tls.Conn
