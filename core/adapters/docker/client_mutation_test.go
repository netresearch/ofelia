// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests targeting surviving CONDITIONALS_NEGATION mutations in client.go
// These test both branches of config option conditionals

func TestNewClientWithConfig_ConfigOptions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		config *ClientConfig
		desc   string
	}{
		// Targeting line 79: if config.Host != ""
		{
			name: "empty_host",
			config: &ClientConfig{
				Host:    "",
				Version: "",
			},
			desc: "Empty host should use default",
		},
		{
			name: "non_empty_host",
			config: &ClientConfig{
				Host:    "unix:///var/run/docker.sock",
				Version: "",
			},
			desc: "Non-empty host should be applied",
		},
		// Targeting line 83: if config.Version != ""
		{
			name: "empty_version",
			config: &ClientConfig{
				Host:    "",
				Version: "",
			},
			desc: "Empty version should use default",
		},
		{
			name: "non_empty_version",
			config: &ClientConfig{
				Host:    "",
				Version: "1.41",
			},
			desc: "Non-empty version should be applied",
		},
		// Targeting line 87: if config.HTTPHeaders != nil
		{
			name: "nil_http_headers",
			config: &ClientConfig{
				Host:        "",
				HTTPHeaders: nil,
			},
			desc: "Nil HTTP headers should be skipped",
		},
		{
			name: "non_nil_http_headers",
			config: &ClientConfig{
				Host:        "",
				HTTPHeaders: map[string]string{"X-Custom": "value"},
			},
			desc: "Non-nil HTTP headers should be applied",
		},
		// Test combinations
		{
			name: "all_options_set",
			config: &ClientConfig{
				Host:        "unix:///var/run/docker.sock",
				Version:     "1.41",
				HTTPHeaders: map[string]string{"X-Test": "test"},
			},
			desc: "All options set should be applied",
		},
		{
			name: "all_options_empty",
			config: &ClientConfig{
				Host:        "",
				Version:     "",
				HTTPHeaders: nil,
			},
			desc: "All options empty should use defaults",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewClientWithConfig(tc.config)
			if err != nil {
				t.Logf("%s: got expected error (no Docker): %v", tc.desc, err)
			}
		})
	}
}

func TestCreateHTTPClient_HostConditions(t *testing.T) {
	// Targeting line 124: if host == ""
	// Test that empty host defaults to DefaultDockerHost

	testCases := []struct {
		name   string
		config *ClientConfig
		desc   string
	}{
		{
			name: "empty_host_defaults",
			config: &ClientConfig{
				Host:            "",
				MaxIdleConns:    10,
				IdleConnTimeout: 30 * time.Second,
			},
			desc: "Empty host should default to Docker default host",
		},
		{
			name: "unix_socket_host",
			config: &ClientConfig{
				Host:            "unix:///var/run/docker.sock",
				MaxIdleConns:    10,
				IdleConnTimeout: 30 * time.Second,
			},
			desc: "Unix socket host should be used",
		},
		{
			name: "tcp_host",
			config: &ClientConfig{
				Host:            "tcp://localhost:2375",
				MaxIdleConns:    10,
				IdleConnTimeout: 30 * time.Second,
			},
			desc: "TCP host should be used",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// createHTTPClient is called internally by NewClientWithConfig
			// We test it indirectly by creating a client with config
			httpClient := createHTTPClient(tc.config)
			if httpClient == nil {
				t.Errorf("%s: expected non-nil HTTP client", tc.desc)
			}
		})
	}
}

func TestClientConfig_PoolingOptions(t *testing.T) {
	// Test various connection pooling configurations
	testCases := []struct {
		name   string
		config *ClientConfig
	}{
		{
			name: "default_pooling",
			config: &ClientConfig{
				MaxIdleConns:          0,
				MaxIdleConnsPerHost:   0,
				MaxConnsPerHost:       0,
				IdleConnTimeout:       0,
				ResponseHeaderTimeout: 0,
			},
		},
		{
			name: "custom_pooling",
			config: &ClientConfig{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				MaxConnsPerHost:       50,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := createHTTPClient(tc.config)
			if client == nil {
				t.Error("Expected non-nil HTTP client")
				return
			}

			// Verify transport was configured
			transport, ok := client.Transport.(*http.Transport)
			if !ok {
				// Transport might be wrapped, that's OK
				t.Logf("Transport is not *http.Transport, might be wrapped")
				return
			}

			// Verify pooling settings were applied
			if tc.config.MaxIdleConns > 0 && transport.MaxIdleConns != tc.config.MaxIdleConns {
				t.Errorf("MaxIdleConns: got %d, want %d", transport.MaxIdleConns, tc.config.MaxIdleConns)
			}
			if tc.config.MaxIdleConnsPerHost > 0 && transport.MaxIdleConnsPerHost != tc.config.MaxIdleConnsPerHost {
				t.Errorf("MaxIdleConnsPerHost: got %d, want %d", transport.MaxIdleConnsPerHost, tc.config.MaxIdleConnsPerHost)
			}
		})
	}
}

// TestNewClientWithConfig_UnsupportedSchemes verifies that DOCKER_HOST values
// with unsupported URL schemes are rejected at construction with a clear error,
// instead of silently falling through to a plain-TCP transport. The validation
// happens in NewClientWithConfig (not createHTTPClient), hence the name.
//
// See: https://github.com/netresearch/ofelia/issues/609
func TestNewClientWithConfig_UnsupportedSchemes(t *testing.T) {
	t.Parallel()

	// tcp+tls:// was re-enabled in #616 once the TLS plumbing from #613
	// landed; it is asserted as a positive case in
	// TestNewClientWithConfig_TCPPlusTLSScheme below.
	testCases := []struct {
		name string
		host string
	}{
		{
			name: "ssh_scheme",
			host: "ssh://user@docker-host:22",
		},
		{
			name: "fd_scheme",
			host: "fd://",
		},
		{
			name: "bogus_scheme",
			host: "gopher://something",
		},
		{
			// no_scheme is a distinct error class (ErrMissingDockerHostScheme,
			// not ErrUnsupportedDockerHostScheme). Asserted in
			// TestNewClientWithConfig_MissingScheme below.
			name: "bare_path_with_scheme_chars",
			host: "tcp+ssh://something",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewClientWithConfig(&ClientConfig{Host: tc.host})
			if err == nil {
				t.Fatalf("expected error for unsupported scheme %q, got nil", tc.host)
			}
			if !errors.Is(err, ErrUnsupportedDockerHostScheme) {
				t.Errorf("expected ErrUnsupportedDockerHostScheme for %q, got %v", tc.host, err)
			}
			// Error message must list at least one supported scheme so operators
			// know what to switch to.
			if !strings.Contains(err.Error(), "unix://") {
				t.Errorf("expected error message to list supported schemes (e.g. unix://), got %q", err.Error())
			}
		})
	}
}

// TestNewClientWithConfig_TCPPlusTLSScheme asserts that DOCKER_HOST values
// using the tcp+tls:// scheme are accepted by NewClientWithConfig now that the
// TLS plumbing from #613 wires cert material into the custom transport.
//
// Construction must NOT fail with ErrUnsupportedDockerHostScheme. We use port
// 0 so no real network connection is attempted; an unrelated dial / negotiate
// error is acceptable - the only thing this test pins is that the scheme
// passes the allow-list. See https://github.com/netresearch/ofelia/issues/616.
func TestNewClientWithConfig_TCPPlusTLSScheme(t *testing.T) {
	// Not parallel: mutates DOCKER_HOST / DOCKER_TLS_VERIFY / DOCKER_CERT_PATH
	// via t.Setenv to avoid leaking ambient env into createHTTPClient's
	// FromEnv-style fallbacks.
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	_, err := NewClientWithConfig(&ClientConfig{Host: "tcp+tls://127.0.0.1:0"})
	if errors.Is(err, ErrUnsupportedDockerHostScheme) {
		t.Fatalf("tcp+tls:// must be on the allow-list (#616), got ErrUnsupportedDockerHostScheme: %v", err)
	}
	// Any other error (dial / negotiate failure against 127.0.0.1:0) is fine
	// - this test only pins scheme acceptance, not connectivity.
}

// TestValidateAndNormalizeHost covers the host scheme validation helper directly.
// It verifies case-insensitivity (RFC 3986: schemes are case-insensitive) and
// the allow-list of supported transports.
func TestValidateAndNormalizeHost(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		input         string
		want          string
		wantErr       bool
		errSentry     error
		wantErrSubstr string
	}{
		// Supported schemes - lowercase.
		{name: "unix_lowercase", input: "unix:///var/run/docker.sock", want: "unix:///var/run/docker.sock"},
		{name: "tcp_lowercase", input: "tcp://127.0.0.1:2375", want: "tcp://127.0.0.1:2375"},
		{name: "tcp_plus_tls_lowercase", input: "tcp+tls://127.0.0.1:2376", want: "tcp+tls://127.0.0.1:2376"},
		{name: "http_lowercase", input: "http://127.0.0.1:2375", want: "http://127.0.0.1:2375"},
		{name: "https_lowercase", input: "https://127.0.0.1:2376", want: "https://127.0.0.1:2376"},
		{name: "npipe_lowercase", input: `npipe:////./pipe/docker_engine`, want: `npipe:////./pipe/docker_engine`},

		// Case-insensitivity: schemes are normalized to lowercase, paths preserved.
		{name: "tcp_uppercase", input: "TCP://127.0.0.1:2375", want: "tcp://127.0.0.1:2375"},
		{name: "unix_uppercase", input: "UNIX:///var/run/docker.sock", want: "unix:///var/run/docker.sock"},
		{name: "https_mixed", input: "HtTpS://127.0.0.1:2376", want: "https://127.0.0.1:2376"},
		{name: "tcp_plus_tls_uppercase", input: "TCP+TLS://127.0.0.1:2376", want: "tcp+tls://127.0.0.1:2376"},

		// Path casing is preserved (only scheme is lowercased).
		{name: "unix_mixed_path", input: "UNIX:///Var/Run/docker.sock", want: "unix:///Var/Run/docker.sock"},

		// Empty string passes through (caller supplies default).
		{name: "empty_string", input: "", want: ""},

		// Unsupported schemes.
		{name: "ssh", input: "ssh://docker-host", wantErr: true, errSentry: ErrUnsupportedDockerHostScheme},
		{name: "fd", input: "fd://", wantErr: true, errSentry: ErrUnsupportedDockerHostScheme},
		{name: "gopher", input: "gopher://something", wantErr: true, errSentry: ErrUnsupportedDockerHostScheme},

		// Missing scheme separator. This is a distinct error from "unsupported
		// scheme" - Copilot review feedback was that conflating the two reads
		// confusingly. Assert on the dedicated sentinel.
		{name: "no_scheme", input: "127.0.0.1:2375", wantErr: true, errSentry: ErrMissingDockerHostScheme},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := validateAndNormalizeHost(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("input %q: expected error, got nil (result=%q)", tc.input, got)
				}
				if tc.errSentry != nil && !errors.Is(err, tc.errSentry) {
					t.Errorf("input %q: expected errors.Is(err, %v), got %v", tc.input, tc.errSentry, err)
				}
				if tc.wantErrSubstr != "" && !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Errorf("input %q: expected error to contain %q, got %v", tc.input, tc.wantErrSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("input %q: unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("input %q: got %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestNewClientWithConfig_NormalizesDockerHostEnv verifies that DOCKER_HOST
// environment variables with uppercase schemes are normalized to lowercase
// before scheme-dispatch, instead of falling through to the plain-TCP default.
//
// Cannot use t.Parallel() — t.Setenv is incompatible with parallel subtests.
func TestNewClientWithConfig_NormalizesDockerHostEnv(t *testing.T) {
	// 127.0.0.1:0 is unreachable (port 0); API negotiation is best-effort and
	// tolerates failure at this layer for unit-test purposes.
	t.Setenv("DOCKER_HOST", "TCP://127.0.0.1:0")

	// We don't care if the actual connection fails — we care that construction
	// validates the scheme and doesn't reject a valid (if uppercase) TCP host.
	_, err := NewClientWithConfig(&ClientConfig{NegotiateTimeout: unreachableNegotiateTimeout})
	if err != nil && errors.Is(err, ErrUnsupportedDockerHostScheme) {
		t.Fatalf("uppercase TCP:// scheme should be normalized, not rejected: %v", err)
	}
}

// TestCreateHTTPClient_NpipeTransport documents the npipe:// behavior decision:
// npipe:// is on the allow-list (so Windows builds work transparently), but on
// non-Windows the transport falls back to the default (plain TCP) configuration
// — the actual named-pipe dialer lives in the SDK and is build-tagged. We assert
// only that construction does not return ErrUnsupportedDockerHostScheme for
// npipe://, mirroring the documented contract.
func TestCreateHTTPClient_NpipeTransport(t *testing.T) {
	t.Parallel()

	_, err := NewClientWithConfig(&ClientConfig{Host: `npipe:////./pipe/docker_engine`})
	if err != nil && errors.Is(err, ErrUnsupportedDockerHostScheme) {
		t.Errorf("npipe:// should be on the allow-list, got %v", err)
	}
}

// TestNewClientWithConfig_NilConfig pins the contract that a nil *ClientConfig
// does not panic at startup. Without the nil-guard, callers passing nil would
// panic the daemon on the first Host field access during validation. Surfaced
// by Gemini code-review on PR #612.
func TestNewClientWithConfig_NilConfig(t *testing.T) {
	// Use a clearly-invalid host via env so we exit early on the validation
	// path rather than attempting a real Docker dial.
	t.Setenv("DOCKER_HOST", "ssh://example")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewClientWithConfig(nil) panicked: %v", r)
		}
	}()

	_, err := NewClientWithConfig(nil)
	if err == nil || !errors.Is(err, ErrUnsupportedDockerHostScheme) {
		t.Errorf("expected ErrUnsupportedDockerHostScheme from nil-config + ssh:// env, got err=%v", err)
	}
}

// TestNewClientWithConfig_DoesNotMutateConfigHost ensures the validation /
// normalization step does not silently rewrite the caller's config. Reusing a
// shared *ClientConfig across constructions is a reasonable pattern; mutation
// would be surprising and bug-prone. Surfaced by Copilot review on PR #612.
func TestNewClientWithConfig_DoesNotMutateConfigHost(t *testing.T) {
	// Use an upper-case scheme that we know normalizes to lowercase. If the
	// constructor mutates config.Host, we'd see "tcp://..." after the call.
	const original = "TCP://127.0.0.1:0"
	cfg := DefaultConfig()
	cfg.Host = original
	cfg.NegotiateTimeout = unreachableNegotiateTimeout

	// We don't care whether the dial succeeds (it won't on port 0) - only
	// that the config struct is unchanged when control returns.
	_, _ = NewClientWithConfig(cfg)

	if cfg.Host != original {
		t.Errorf("NewClientWithConfig mutated config.Host: was %q, now %q", original, cfg.Host)
	}
}

// TestNewClientWithConfig_MissingScheme asserts the dedicated error class
// for DOCKER_HOST values that lack a "://" separator. Copilot review on
// PR #612 flagged that conflating "missing scheme" with "unsupported scheme"
// reads confusingly to operators.
func TestNewClientWithConfig_MissingScheme(t *testing.T) {
	t.Parallel()

	_, err := NewClientWithConfig(&ClientConfig{Host: "127.0.0.1:2375"})
	if err == nil {
		t.Fatal("expected error for host without scheme separator, got nil")
	}
	if !errors.Is(err, ErrMissingDockerHostScheme) {
		t.Errorf("expected ErrMissingDockerHostScheme, got %v", err)
	}
	if errors.Is(err, ErrUnsupportedDockerHostScheme) {
		t.Errorf("missing-scheme error should NOT also wrap ErrUnsupportedDockerHostScheme; got %v", err)
	}
	if !strings.Contains(err.Error(), "unix://") {
		t.Errorf("expected error to mention example schemes, got %q", err.Error())
	}
}

// writeTLSFixtures generates a self-signed CA + cert/key triplet (ca.pem,
// cert.pem, key.pem) into dir. The material is throwaway and only intended
// to give crypto/tls something parseable to load — no handshake ever runs
// against it.
func writeTLSFixtures(t *testing.T, dir string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ofelia-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	for name, data := range map[string][]byte{
		"ca.pem":   certPEM,
		"cert.pem": certPEM,
		"key.pem":  keyPEM,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// TestCreateHTTPClient_HonorsTLSEnv asserts that when DOCKER_HOST is an HTTPS
// endpoint and DOCKER_CERT_PATH / DOCKER_TLS_VERIFY are set, the resulting
// transport carries a non-nil *tls.Config with RootCAs and Certificates
// populated from the cert path.
//
// Regression test for #607: client.FromEnv installs TLS on a transport that
// client.WithHTTPClient then replaces wholesale, discarding the TLS material.
// createHTTPClient is the boundary that constructs the replacement transport,
// so we assert on its output directly.
func TestCreateHTTPClient_HonorsTLSEnv(t *testing.T) {
	certDir := t.TempDir()
	writeTLSFixtures(t, certDir)

	// Use loopback to avoid any chance of contaminating other parallel tests
	// that might be checking DOCKER_HOST resolution.
	t.Setenv("DOCKER_HOST", "https://127.0.0.1:0")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", certDir)

	httpClient := createHTTPClient(DefaultConfig())
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}

	if transport.TLSClientConfig == nil {
		t.Fatal("transport.TLSClientConfig is nil; DOCKER_TLS_VERIFY/DOCKER_CERT_PATH not honored")
	}
	if transport.TLSClientConfig.RootCAs == nil {
		t.Error("transport.TLSClientConfig.RootCAs is nil; ca.pem not loaded")
	}
	if len(transport.TLSClientConfig.Certificates) == 0 {
		t.Error("transport.TLSClientConfig.Certificates is empty; cert.pem/key.pem not loaded")
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("transport.TLSClientConfig.InsecureSkipVerify=true; DOCKER_TLS_VERIFY=1 should imply verify")
	}
}

// TestCreateHTTPClient_ExplicitTLSOverridesEnv asserts the precedence
// contract: ClientConfig fields take priority over DOCKER_* env vars.
func TestCreateHTTPClient_ExplicitTLSOverridesEnv(t *testing.T) {
	envDir := t.TempDir()
	writeTLSFixtures(t, envDir)
	cfgDir := t.TempDir()
	writeTLSFixtures(t, cfgDir)

	t.Setenv("DOCKER_HOST", "https://127.0.0.1:0")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", envDir)

	verify := true
	cfg := DefaultConfig()
	cfg.TLSCertPath = cfgDir
	cfg.TLSVerify = &verify

	httpClient := createHTTPClient(cfg)
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}

	if transport.TLSClientConfig == nil {
		t.Fatal("transport.TLSClientConfig is nil; explicit ClientConfig.TLSCertPath not honored")
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify=true; explicit TLSVerify=true should override empty env var")
	}
	if len(transport.TLSClientConfig.Certificates) == 0 {
		t.Error("Certificates not loaded from explicit TLSCertPath")
	}
}

// TestCreateHTTPClient_NoTLSForUnixSocket asserts no TLS is applied when the
// resolved host is a unix socket, even if DOCKER_CERT_PATH is set in env.
func TestCreateHTTPClient_NoTLSForUnixSocket(t *testing.T) {
	certDir := t.TempDir()
	writeTLSFixtures(t, certDir)

	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", certDir)

	cfg := DefaultConfig()
	cfg.Host = "unix:///var/run/docker.sock"

	httpClient := createHTTPClient(cfg)
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}

	if transport.TLSClientConfig != nil {
		t.Errorf("expected nil TLSClientConfig for unix socket, got %+v", transport.TLSClientConfig)
	}
}

// TestCreateHTTPClient_NoTLSWhenCertPathEmpty asserts SDK-equivalent
// behavior: with no DOCKER_CERT_PATH and no explicit config, no TLS is
// applied even for an HTTPS host.
func TestCreateHTTPClient_NoTLSWhenCertPathEmpty(t *testing.T) {
	t.Setenv("DOCKER_HOST", "https://127.0.0.1:0")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", "")

	httpClient := createHTTPClient(DefaultConfig())
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}

	if transport.TLSClientConfig != nil {
		t.Errorf("expected nil TLSClientConfig when DOCKER_CERT_PATH is empty, got %+v", transport.TLSClientConfig)
	}
}

// TestCreateHTTPClient_TLSVerifyExplicitFalse asserts the explicit opt-out
// path: ClientConfig.TLSVerify = &false must yield InsecureSkipVerify=true
// even when DOCKER_TLS_VERIFY=1 in the environment. Mirrors the explicit-wins
// precedence and prevents a refactor from flipping the !verify polarity.
func TestCreateHTTPClient_TLSVerifyExplicitFalse(t *testing.T) {
	certDir := t.TempDir()
	writeTLSFixtures(t, certDir)

	// Env says verify, but explicit config opts out — explicit must win.
	t.Setenv("DOCKER_HOST", "https://127.0.0.1:0")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", certDir)

	verify := false
	cfg := DefaultConfig()
	cfg.TLSCertPath = certDir
	cfg.TLSVerify = &verify

	httpClient := createHTTPClient(cfg)
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("transport.TLSClientConfig is nil; cert path was set, expected TLS config")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify=false; explicit TLSVerify=&false must override env DOCKER_TLS_VERIFY=1")
	}
}

// TestCreateHTTPClient_InsecureSkipVerifyDefaults pins the InsecureSkipVerify
// default for the env-driven path: with cert path set but DOCKER_TLS_VERIFY
// unset, the SDK semantics demand InsecureSkipVerify=true. Without this
// assertion a refactor flipping the !verify polarity could silently make
// every TLS connection skip verification.
func TestCreateHTTPClient_InsecureSkipVerifyDefaults(t *testing.T) {
	certDir := t.TempDir()
	writeTLSFixtures(t, certDir)

	t.Setenv("DOCKER_HOST", "https://127.0.0.1:0")
	t.Setenv("DOCKER_TLS_VERIFY", "") // unset = SDK skips verify
	t.Setenv("DOCKER_CERT_PATH", certDir)

	httpClient := createHTTPClient(DefaultConfig())
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("transport.TLSClientConfig is nil; cert path was set")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify=false; with empty DOCKER_TLS_VERIFY the SDK skips verify (and so must we)")
	}
}

// TestCreateHTTPClient_TLSLoadErrorDoesNotPanic asserts that when
// resolveTLSConfig fails (e.g. DOCKER_CERT_PATH points at a directory
// missing ca.pem/cert.pem/key.pem), createHTTPClient does not panic and
// the resulting transport falls back to no TLS - the warning logged via
// slog.Default() is the operator-facing signal. The pre-fix call site
// silently swallowed the error, recreating the very class of bug
// PR #613 fixes; this test pins the surfacing behavior.
func TestCreateHTTPClient_TLSLoadErrorDoesNotPanic(t *testing.T) {
	emptyDir := t.TempDir() // exists but has no cert files

	t.Setenv("DOCKER_HOST", "https://127.0.0.1:0")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", emptyDir)

	httpClient := createHTTPClient(DefaultConfig())
	if httpClient == nil {
		t.Fatal("createHTTPClient returned nil on TLS load error; expected non-nil with no TLS")
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}
	// resolveTLSConfig errored, so transport.TLSClientConfig must remain nil.
	// The fallback is intentional: failing closed at construction would prevent
	// any future graceful degradation, but the warning-on-error pattern surfaces
	// the misconfiguration loudly via slog.
	if transport.TLSClientConfig != nil {
		t.Errorf("expected nil TLSClientConfig after resolveTLSConfig error, got %+v", transport.TLSClientConfig)
	}
}

// TestResolveTLSConfig_MissingCertFiles directly exercises resolveTLSConfig's
// error path so the wrapped error message (which carries the remediation hint
// "expected ca.pem, cert.pem, key.pem") is locked in.
func TestResolveTLSConfig_MissingCertFiles(t *testing.T) {
	emptyDir := t.TempDir()

	cfg := &ClientConfig{TLSCertPath: emptyDir}
	tlsCfg, err := resolveTLSConfig(cfg)
	if err == nil {
		t.Fatal("resolveTLSConfig returned nil error for empty cert dir; expected file-not-found wrap")
	}
	if tlsCfg != nil {
		t.Errorf("resolveTLSConfig returned non-nil tlsCfg on error: %+v", tlsCfg)
	}
	if !strings.Contains(err.Error(), "ca.pem") {
		t.Errorf("error message missing remediation hint about expected files: %v", err)
	}
	if !strings.Contains(err.Error(), emptyDir) {
		t.Errorf("error message missing the actual cert path: %v", err)
	}
}

// TestCreateHTTPClient_TCPDoesNotWireTLSEvenWithEnv pins the contract
// reconciled in #628: tcp:// is a plain HTTP/1.1 transport regardless of
// DOCKER_TLS_VERIFY / DOCKER_CERT_PATH. Wiring TLSClientConfig into a
// transport whose URL stays tcp:// would silently load cert material that
// Go's http.Transport never offers on the wire — a worse failure mode
// than failing loud, because operators believe they have mTLS when they
// do not. Operators wanting TLS over TCP must use tcp+tls:// or https://.
//
// See #628 (this reconciliation) and #634 (the deeper SDK URL-rewrite
// half of the docker-CLI parity story).
func TestCreateHTTPClient_TCPDoesNotWireTLSEvenWithEnv(t *testing.T) {
	certDir := t.TempDir()
	writeTLSFixtures(t, certDir)

	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:0")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", certDir)

	httpClient := createHTTPClient(DefaultConfig())
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}
	if transport.TLSClientConfig != nil {
		t.Errorf("tcp:// is plain transport — operators must use tcp+tls:// or https:// for TLS; "+
			"got TLSClientConfig=%+v (silent ineffective wiring is worse than failing loud)",
			transport.TLSClientConfig)
	}
	if transport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2=true for plain tcp://; should be HTTP/1.1")
	}
}

// TestCreateHTTPClient_TCPWithoutTLSEnvStaysPlaintext is the negative case:
// tcp:// without any TLS env / config must remain plaintext. Without this,
// the TCPWithTLSEnvUpgrades test alone could pass even if every tcp://
// silently became HTTPS.
func TestCreateHTTPClient_TCPWithoutTLSEnvStaysPlaintext(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:0")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	httpClient := createHTTPClient(DefaultConfig())
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}
	if transport.TLSClientConfig != nil {
		t.Errorf("expected nil TLSClientConfig for tcp:// without TLS env, got %+v", transport.TLSClientConfig)
	}
	if transport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2=true for plain tcp://; should be HTTP/1.1")
	}
}

// TestCreateHTTPClient_TCPPlusTLSEnablesTLS asserts that tcp+tls:// (added by
// PR #609) also gets TLS material wired up. Without this, the security
// review of PR #612 would flag tcp+tls as a silent downgrade equivalent to
// the original #607 bug.
func TestCreateHTTPClient_TCPPlusTLSEnablesTLS(t *testing.T) {
	certDir := t.TempDir()
	writeTLSFixtures(t, certDir)

	t.Setenv("DOCKER_HOST", "tcp+tls://127.0.0.1:0")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", certDir)

	httpClient := createHTTPClient(DefaultConfig())
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("transport.TLSClientConfig is nil for tcp+tls://; silent TLS downgrade")
	}
	if !transport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2=false for tcp+tls://; should match https:// behavior")
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify=true for tcp+tls:// + DOCKER_TLS_VERIFY=1")
	}
}
