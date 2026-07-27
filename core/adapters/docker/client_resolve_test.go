// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"net"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/moby/moby/client"
)

// historicalAllowedSchemes is the SORTED list of schemes the public
// NewClientWithConfig surface accepts. It exists so a future scheme addition
// that forgets to register a handler (or, conversely, a handler addition that
// forgets to flip allowed=true) fails this test loudly. Update intentionally
// in lockstep with schemeHandlers when the public allow-list legitimately
// changes (tcp+tls was added in #616 once the TLS plumbing from #613 landed).
var historicalAllowedSchemes = []string{"http", "https", "npipe", "tcp", "tcp+tls", "unix"}

// TestSchemeHandlers_AllowListParity is the parity test mandated by issue
// #617: the keys of schemeHandlers with allowed=true must EXACTLY equal
// the historical allow-list. Catches drift in both directions:
//
//   - A new entry with allowed=true that's not in the historical list
//     means the public surface grew without an explicit decision.
//   - A historical scheme missing or flipped to allowed=false means the
//     public surface shrank without an explicit decision.
//
// Adding tcp+tls to the dispatch map without flipping allowed=true is the
// pattern this test endorses; it MUST stay allowed=false until #616.
func TestSchemeHandlers_AllowListParity(t *testing.T) {
	t.Parallel()

	got := allowedSchemes() // sorted
	want := append([]string(nil), historicalAllowedSchemes...)
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("allowed schemes drifted from historical list:\n  got:  %v\n  want: %v\n"+
			"If this is intentional, update historicalAllowedSchemes in this test.",
			got, want)
	}

	// Also assert every dispatch entry has a non-nil apply func, since a nil
	// would panic at first use — caught here cheaply at test time.
	for name, h := range schemeHandlers {
		if h.apply == nil {
			t.Errorf("schemeHandlers[%q].apply is nil", name)
		}
	}
}

// TestSchemeHandlers_DispatchHasUnixForDefault pins that the SDK's default
// host (client.DefaultDockerHost, e.g. "unix:///var/run/docker.sock") has a
// matching dispatch entry. Without this, the empty-config / empty-env path
// would silently fall through to the plain-HTTP1.1 branch.
func TestSchemeHandlers_DispatchHasUnixForDefault(t *testing.T) {
	t.Parallel()

	defaultScheme := "" // derived below
	for s := range schemeHandlers {
		if rest, found := stripScheme(client.DefaultDockerHost, s); found && rest != "" {
			defaultScheme = s
			break
		}
	}
	if defaultScheme == "" {
		t.Fatalf("no scheme handler matches client.DefaultDockerHost=%q", client.DefaultDockerHost)
	}
	if _, ok := schemeHandlers[defaultScheme]; !ok {
		t.Errorf("scheme %q (from DefaultDockerHost) missing from schemeHandlers", defaultScheme)
	}
}

func stripScheme(host, scheme string) (string, bool) {
	prefix := scheme + "://"
	if len(host) > len(prefix) && host[:len(prefix)] == prefix {
		return host[len(prefix):], true
	}
	return "", false
}

// withCountingGetenv swaps the package-level getenv with a counting wrapper
// that increments cnt only for reads of key. The original is restored via
// t.Cleanup so the seam is safe under -parallel (well-behaved tests that
// don't also stub getenv themselves).
func withCountingGetenv(t *testing.T, key string, cnt *atomic.Int64) {
	t.Helper()
	orig := getenv
	getenv = func(k string) string {
		if k == key {
			cnt.Add(1)
		}
		return orig(k)
	}
	t.Cleanup(func() { getenv = orig })
}

// TestNewClientWithConfig_ReadsDOCKERHOSTOnce is the contract test mandated
// by issue #617: a single call to NewClientWithConfig must read
// DOCKER_HOST AT MOST ONCE. Two readers can disagree under env mutation,
// which is the bug class fixed by #606 / #607 / #609.
//
// We exercise three paths:
//   - cfg.Host non-empty (env should NOT be read)
//   - cfg.Host empty, env set (env read exactly once)
//   - both empty (env read exactly once, then default applied)
//
// Cannot use t.Parallel() — t.Setenv is incompatible.
func TestNewClientWithConfig_ReadsDOCKERHOSTOnce(t *testing.T) {
	cases := []struct {
		name    string
		cfgHost string
		envHost string
		wantMax int64 // upper bound on getenv("DOCKER_HOST") calls
		// wantExact, when true, requires exactly wantMax reads (catches a
		// regression that drops the env read entirely, which the upper-bound
		// check would silently accept). See issue #633.
		wantExact bool
		wantErr   bool
		errIsArg  error
	}{
		{
			name:    "explicit_config_host",
			cfgHost: "tcp://127.0.0.1:0",
			envHost: "tcp://127.0.0.1:0", // present but should NOT be consulted
			wantMax: 0,                   // cfg.Host wins; env not read
		},
		{
			name:      "env_only",
			cfgHost:   "",
			envHost:   "tcp://127.0.0.1:0",
			wantMax:   1,
			wantExact: true, // #633: exact-1 catches a regression that reads zero times
		},
		{
			name:    "both_empty_falls_back_to_default",
			cfgHost: "",
			envHost: "",
			wantMax: 1, // read once, returns "", then DefaultDockerHost applies
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Listener so the SDK has something to dial against; we never
			// expect a real connection — NegotiateAPIVersion is bounded.
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			t.Cleanup(func() { _ = ln.Close() })

			t.Setenv("DOCKER_HOST", tc.envHost)
			t.Setenv("DOCKER_CERT_PATH", "")
			t.Setenv("DOCKER_TLS_VERIFY", "")

			var count atomic.Int64
			withCountingGetenv(t, client.EnvOverrideHost, &count)

			cfg := &ClientConfig{
				Host:             tc.cfgHost,
				NegotiateTimeout: 50 * 1000 * 1000, // 50ms; we don't care about negotiation
			}
			c, err := NewClientWithConfig(cfg)
			if c != nil {
				_ = c.Close()
			}
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				// Ignore network/dial errors that propagate from NewClientWithOpts;
				// what we care about here is the env read count.
				t.Logf("non-fatal: NewClientWithConfig returned err=%v (expected for stub host)", err)
			}

			got := count.Load()
			if tc.wantExact {
				if got != tc.wantMax {
					t.Errorf("DOCKER_HOST read %d times during NewClientWithConfig, want exactly %d "+
						"(issue #633: exact-count catches a regression that drops the env read entirely)",
						got, tc.wantMax)
				}
			} else if got > tc.wantMax {
				t.Errorf("DOCKER_HOST read %d times during NewClientWithConfig, want <= %d "+
					"(issue #617: a single resolveDockerHost seam must read DOCKER_HOST at most once)",
					got, tc.wantMax)
			}
		})
	}
}

// TestResolveDockerHost_ReadsEnvOnce exercises resolveDockerHost in
// isolation, asserting the env is read exactly once when cfg.Host is empty
// and zero times when cfg.Host is set.
func TestResolveDockerHost_ReadsEnvOnce(t *testing.T) {
	t.Run("cfg_host_set_skips_env", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "tcp://1.2.3.4:2375")

		var cnt atomic.Int64
		withCountingGetenv(t, client.EnvOverrideHost, &cnt)

		host, scheme, err := resolveDockerHost(&ClientConfig{Host: "unix:///var/run/docker.sock"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if host != "unix:///var/run/docker.sock" {
			t.Errorf("host=%q, want unix:///var/run/docker.sock", host)
		}
		if scheme != "unix" {
			t.Errorf("scheme=%q, want unix", scheme)
		}
		if got := cnt.Load(); got != 0 {
			t.Errorf("env read %d times despite cfg.Host being set; want 0", got)
		}
	})

	t.Run("cfg_host_empty_reads_env_once", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "tcp://1.2.3.4:2375")

		var cnt atomic.Int64
		withCountingGetenv(t, client.EnvOverrideHost, &cnt)

		host, scheme, err := resolveDockerHost(&ClientConfig{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if host != "tcp://1.2.3.4:2375" {
			t.Errorf("host=%q, want tcp://1.2.3.4:2375", host)
		}
		if scheme != "tcp" {
			t.Errorf("scheme=%q, want tcp", scheme)
		}
		if got := cnt.Load(); got != 1 {
			t.Errorf("env read %d times; want exactly 1", got)
		}
	})
}

// TestSupportedSchemesMsg_Cached pins that the package-level
// supportedSchemesMsg is initialized at package load and equals the
// freshly-computed value. Avoids the "called per error" smell from #617.
func TestSupportedSchemesMsg_Cached(t *testing.T) {
	t.Parallel()

	if supportedSchemesMsg == "" {
		t.Fatal("supportedSchemesMsg is empty; expected lazy init at package load")
	}
	// Recomputing should produce an identical string (idempotency check).
	if got := formatSupportedSchemes(); got != supportedSchemesMsg {
		t.Errorf("formatSupportedSchemes() drift:\n  cached: %q\n  fresh:  %q",
			supportedSchemesMsg, got)
	}
	// Spot-check it actually mentions "unix://" so error messages stay useful.
	if !contains(supportedSchemesMsg, "unix://") {
		t.Errorf("supportedSchemesMsg=%q missing unix://", supportedSchemesMsg)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
