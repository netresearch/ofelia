// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// unreachableNegotiateTimeout bounds the eager API version negotiation in tests
// that construct a client against a deliberately unreachable address and assert
// something other than connectivity (scheme normalization, the TLS-material
// gate, config immutability).
//
// Those tests used to leave NegotiateTimeout at its 30s default and rely on the
// dial failing immediately, which holds only where the kernel answers a closed
// port with RST. On a host whose firewall DROPs instead of rejecting - the
// packets are silently discarded, so the connect runs to its own timeout -
// each such construction blocked for the full 30s and the package blew the
// 60s budget of the lefthook pre-push smoke hook. Bounding negotiation here
// keeps the assertions intact and makes their runtime independent of how the
// network answers an unreachable port.
const unreachableNegotiateTimeout = 50 * time.Millisecond

// TestNewClientWithConfig_NegotiateAPIVersionTimeout verifies that NewClientWithConfig
// returns within a bounded time when the Docker daemon is reachable but does not
// respond to API version negotiation (e.g. a wedged socket proxy whose upstream is hung).
//
// Regression for https://github.com/netresearch/ofelia/issues/608. Without
// NegotiateTimeout, sdk.NegotiateAPIVersion is invoked with context.Background() and
// can block forever, hanging Ofelia at startup with no diagnostic output.
func TestNewClientWithConfig_NegotiateAPIVersionTimeout(t *testing.T) {
	// Loopback listener that accepts connections but never reads/writes -
	// requests to /_ping will hang on the response. This mirrors a wedged
	// socket-proxy whose upstream is unresponsive.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}

	// Drain accepted connections in a goroutine but never serve them.
	// Hold accepted conns under a mutex so we can close them all on cleanup
	// without racing with the Accept goroutine. Closing ln makes Accept
	// return an error and the goroutine exits.
	var (
		connsMu sync.Mutex
		conns   []net.Conn
	)
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			connsMu.Lock()
			conns = append(conns, c)
			connsMu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-acceptDone
		connsMu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		connsMu.Unlock()
	})

	host := "tcp://" + ln.Addr().String()

	// Point DOCKER_HOST at our stalling listener. t.Setenv restores after the test.
	t.Setenv("DOCKER_HOST", host)
	// Make sure no TLS env vars from the developer environment leak in.
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	// 500ms gives slow CI runners (GitHub Actions under load can stall scheduling
	// for hundreds of ms) headroom before the configured timeout fires.
	const negotiateTimeout = 500 * time.Millisecond
	// Generous tolerance to avoid CI flakes: expect return within 10x the configured timeout.
	const returnWithin = 10 * negotiateTimeout
	// Hard safety net: if even the timeout path hangs, fail the test rather than the suite.
	const hardDeadline = 10 * time.Second

	cfg := DefaultConfig()
	cfg.Host = host
	cfg.NegotiateTimeout = negotiateTimeout

	type result struct {
		client *Client
		err    error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		c, e := NewClientWithConfig(cfg)
		done <- result{client: c, err: e}
	}()

	select {
	case r := <-done:
		elapsed := time.Since(start)
		if r.client != nil {
			_ = r.client.Close()
		}
		// We do not require an error - NegotiateAPIVersion swallows ping
		// failures silently. The point is that the call must return.
		if elapsed > returnWithin {
			t.Fatalf("NewClientWithConfig returned after %v, expected <= %v (configured timeout %v)",
				elapsed, returnWithin, negotiateTimeout)
		}
		// Sanity check: errors from client.NewClientWithOpts (e.g. bad host) are fine to surface,
		// but a network ping error should not be propagated by NegotiateAPIVersion.
		if r.err != nil && !errors.Is(r.err, context.DeadlineExceeded) {
			t.Logf("NewClientWithConfig returned err=%v (ignored - timeout path is what matters)", r.err)
		}
	case <-time.After(hardDeadline):
		t.Fatalf("NewClientWithConfig did not return within %v (configured NegotiateTimeout=%v); likely hanging on NegotiateAPIVersion",
			hardDeadline, negotiateTimeout)
	}
}

// TestDefaultConfig_NegotiateTimeout asserts the default configuration ships with a
// sane, non-zero NegotiateTimeout so that operators who use DefaultConfig() inherit
// the hang-prevention without explicit opt-in.
func TestDefaultConfig_NegotiateTimeout(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	if cfg.NegotiateTimeout != defaultNegotiateTimeout {
		t.Fatalf("DefaultConfig().NegotiateTimeout = %v, want %v", cfg.NegotiateTimeout, defaultNegotiateTimeout)
	}
}

// TestNewClientWithConfig_NegotiateTimeoutFallback ensures callers that pass a
// non-positive NegotiateTimeout (e.g. constructed *ClientConfig{} directly,
// without DefaultConfig()) still get the hang-prevention guarantee. Without
// this, a value of 0 would create a context with zero duration and effectively
// short-circuit negotiation; a negative value would do the same.
func TestNewClientWithConfig_NegotiateTimeoutFallback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
	}{
		{"zero", 0},
		{"negative", -1 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			t.Cleanup(func() { _ = ln.Close() })

			t.Setenv("DOCKER_HOST", "tcp://"+ln.Addr().String())
			t.Setenv("DOCKER_TLS_VERIFY", "")
			t.Setenv("DOCKER_CERT_PATH", "")

			cfg := &ClientConfig{
				Host:             "tcp://" + ln.Addr().String(),
				NegotiateTimeout: tc.timeout,
			}

			start := time.Now()
			done := make(chan struct{}, 1)
			go func() {
				c, _ := NewClientWithConfig(cfg)
				if c != nil {
					_ = c.Close()
				}
				done <- struct{}{}
			}()

			// Defaulting to 30s. We do not want to wait the full default in the test;
			// we only assert the constructor does NOT return effectively-instantly
			// (which is what would happen if zero/negative bypassed the fallback).
			select {
			case <-done:
				if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
					t.Fatalf("NewClientWithConfig returned in %v - expected fallback to defaultNegotiateTimeout, but the dial appears to have aborted instantly (zero/negative timeout leaked through)", elapsed)
				}
				// Note: with the listener never responding, the call will sit until
				// the 30s default fires. We do NOT wait for that here; below we cancel.
			case <-time.After(2 * time.Second):
				// Expected path: constructor is bounded by the (default) 30s timeout
				// and is still running 2s in. That proves the fallback engaged.
				_ = ln.Close() // unblock the constructor for cleanup
				<-done
			}
		})
	}
}
