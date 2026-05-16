// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

// Package docker provides an adapter for the official Docker SDK.
package docker

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"github.com/docker/go-connections/tlsconfig"

	"github.com/netresearch/ofelia/core/ports"
)

// defaultNegotiateTimeout bounds the initial Docker API version negotiation.
// Used both as the DefaultConfig value and as the fallback when callers pass
// a non-positive NegotiateTimeout.
const defaultNegotiateTimeout = 30 * time.Second

// Docker host scheme constants. These names are RFC 3986 URL schemes; we
// compare them case-insensitively elsewhere by normalizing to lowercase first.
// Keep this the SOLE source of truth for scheme spelling — referencing it
// from the handler map, the TrimPrefix in the unix dialer, the error
// formatter, and tests.
//
// Tracking refactor: https://github.com/netresearch/ofelia/issues/617
const (
	schemeUnix   = "unix"
	schemeTCP    = "tcp"
	schemeHTTP   = "http"
	schemeHTTPS  = "https"
	schemeNpipe  = "npipe"
	schemeTCPTLS = "tcp+tls"
)

// ErrUnsupportedDockerHostScheme is returned when a DOCKER_HOST value uses
// a URL scheme that this adapter does not support. The error message lists
// the supported schemes so operators get an actionable failure at startup
// instead of an opaque dial error later.
//
// Supported schemes (case-insensitive): unix, tcp, tcp+tls, http, https, npipe.
// Notably unsupported: ssh, fd. See docs/TROUBLESHOOTING.md.
//
// Tracking: https://github.com/netresearch/ofelia/issues/609
var ErrUnsupportedDockerHostScheme = errors.New("unsupported DOCKER_HOST scheme")

// ErrMissingDockerHostScheme is returned when DOCKER_HOST is non-empty but
// has no "://" separator (e.g. "127.0.0.1:2375"). Distinct from
// ErrUnsupportedDockerHostScheme so operators can tell "I forgot the scheme"
// from "I used a scheme that isn't supported."
var ErrMissingDockerHostScheme = errors.New("missing DOCKER_HOST scheme")

// getenv is a package-level seam over os.Getenv so tests can count or stub
// environment reads. Production code MUST go through this rather than
// os.Getenv directly so we can prove the "DOCKER_HOST read at most once per
// NewClientWithConfig" contract from issue #617 with a test.
//
// MUST NOT be reassigned outside _test.go files. Tests that swap it MUST
// run serially (no t.Parallel()) and restore the original via t.Cleanup.
//
//nolint:gochecknoglobals // intentional test seam, see TestResolveDockerHost_ReadsEnvOnce
var getenv = os.Getenv

// schemeHandler describes how the HTTP transport is configured for a given
// Docker host scheme. The handler map (see schemeHandlers) is the SINGLE
// source of truth: keys are the schemes we recognize at dispatch time, and
// `allowed` records whether the public NewClientWithConfig surface accepts
// the scheme.
//
// The split (recognized vs allowed) keeps schemes dispatchable inside
// createHTTPClient — which is invoked directly by some tests — while still
// letting NewClientWithConfig reject anything not on the public allow-list
// at the front door (the security / silent-downgrade contract from PR #612).
//
// `tlsCapable` marks schemes whose apply function wires real TLS material so
// the transport can negotiate ALPN h2. createHTTPClient inverts this flag to
// drive disableHTTP2AutoConfig — non-TLS schemes MUST suppress Go std lib's
// lazy HTTP/2 auto-config or the SDK's hijack-path dialer misreads the
// auto-allocated TLSClientConfig as operator-configured TLS and crashes on
// plain Docker daemons (https://github.com/netresearch/ofelia/issues/668).
// Driving suppression from the flag — instead of from a call inside each
// apply function — means a future non-TLS scheme can't silently regress #668
// by forgetting the call (see #683).
type schemeHandler struct {
	allowed    bool
	tlsCapable bool
	apply      func(transport *http.Transport, cfg *ClientConfig, host string)
}

// schemeHandlers maps a lowercase URL scheme to its transport configuration.
// Adding a new scheme means adding ONE entry here and nothing else: the
// allow-list (validateAndNormalizeHost), the dispatch (createHTTPClient),
// and the error message (formatSupportedSchemes) all derive from this map.
//
// Documented choices:
//
//   - unix:    Unix domain socket dialer; HTTP/1.1 only.
//   - tcp:     Plain TCP; auto-upgrades to https:// (TLS) when
//     DOCKER_CERT_PATH (or ClientConfig.TLSCertPath) is set, mirroring the
//     docker CLI. The rewrite happens in NewClientWithConfig via
//     upgradeTCPToHTTPSIfTLSMaterial so the SDK and HTTP transport
//     agree on the URL scheme — without it, http.Transport never
//     triggers TLS for tcp:// even when cert material is configured
//     (#634, follow-up to #613). The handler below remains in place
//     for direct callers of createHTTPClient (tests).
//   - tcp+tls: Explicit TLS over TCP. Requires TLS material via
//     DOCKER_CERT_PATH / DOCKER_TLS_VERIFY (or ClientConfig.TLSCertPath /
//     TLSVerify). Re-enabled on the public allow-list in #616 now that the
//     TLS plumbing from #613 wires the cert material into the custom
//     transport via applyDockerTLS.
//   - http:    Plain HTTP/1.1; no TLS handling. Installs an explicit TCP
//     DialContext so the SDK's hijack-path dialer routes ContainerExecAttach
//     etc. via dialerFromTransport rather than falling to the broken
//     net.Dial("http", addr) default (#682).
//   - https:   HTTPS with HTTP/2 via ALPN; TLS material is wired in.
//   - npipe:   Windows named pipe; the actual dialer lives in the SDK and
//     is build-tagged. The handler is a no-op so non-Windows builds still
//     compile and the scheme is on the public allow-list.
var schemeHandlers = map[string]schemeHandler{
	schemeUnix:   {allowed: true, tlsCapable: false, apply: applyUnixTransport},
	schemeTCP:    {allowed: true, tlsCapable: false, apply: applyTCPTransport},
	schemeHTTP:   {allowed: true, tlsCapable: false, apply: applyHTTPTransport},
	schemeHTTPS:  {allowed: true, tlsCapable: true, apply: applyTLSTransport},
	schemeNpipe:  {allowed: true, tlsCapable: false, apply: applyPlainTransport},
	schemeTCPTLS: {allowed: true, tlsCapable: true, apply: applyTLSTransport},
}

// supportedSchemesMsg is the cached, comma-separated list of allowed schemes
// (with "://" suffix) used in error messages. Computed once at package init
// from schemeHandlers so future scheme additions stay DRY.
var supportedSchemesMsg = formatSupportedSchemes()

// allowedSchemes returns the sorted list of scheme names that
// NewClientWithConfig accepts on input. Derived from schemeHandlers so the
// allow-list cannot drift from the dispatch table.
func allowedSchemes() []string {
	out := make([]string, 0, len(schemeHandlers))
	for name, h := range schemeHandlers {
		if h.allowed {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// formatSupportedSchemes returns a comma-separated, suffixed list of the
// publicly allowed Docker host schemes for inclusion in error messages.
// Called once at init; see supportedSchemesMsg.
func formatSupportedSchemes() string {
	allowed := allowedSchemes()
	parts := make([]string, len(allowed))
	for i, s := range allowed {
		parts[i] = s + "://"
	}
	return strings.Join(parts, ", ")
}

// Client implements ports.DockerClient using the official Docker SDK.
type Client struct {
	sdk *client.Client

	containers *ContainerServiceAdapter
	exec       *ExecServiceAdapter
	images     *ImageServiceAdapter
	events     *EventServiceAdapter
	services   *SwarmServiceAdapter
	networks   *NetworkServiceAdapter
	system     *SystemServiceAdapter
}

// ClientConfig contains configuration for the Docker client.
type ClientConfig struct {
	// Host is the Docker host address (e.g., "unix:///var/run/docker.sock")
	Host string

	// Version is the API version (empty for auto-negotiation)
	Version string

	// HTTPClient is a custom HTTP client (optional)
	HTTPClient *http.Client

	// HTTPHeaders are custom HTTP headers (optional)
	HTTPHeaders map[string]string

	// Connection pool settings
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int
	IdleConnTimeout     time.Duration

	// Timeout settings
	DialTimeout           time.Duration
	ResponseHeaderTimeout time.Duration

	// NegotiateTimeout bounds the initial Docker API version negotiation that
	// runs once at client creation. Without a bound, NegotiateAPIVersion uses
	// context.Background() and can block forever when the Docker daemon is
	// reachable but unresponsive (e.g. a socket proxy whose upstream is wedged),
	// hanging Ofelia at startup with no diagnostic output.
	//
	// The Docker SDK swallows ping errors silently, so this timeout does not
	// change correctness on successful paths - it only bounds the failure case.
	//
	// Set to 0 to inherit the default (see DefaultConfig). Use a small value
	// to fail fast in tests; production typically wants 10-30s.
	NegotiateTimeout time.Duration

	// TLSCertPath is the directory containing TLS material (ca.pem, cert.pem,
	// key.pem) for HTTPS Docker hosts. When empty, the DOCKER_CERT_PATH
	// environment variable is consulted instead. If both are empty, no TLS
	// configuration is applied (mirroring docker/docker's
	// WithTLSClientConfigFromEnv).
	TLSCertPath string

	// TLSVerify controls whether the server certificate is verified for HTTPS
	// Docker hosts. When nil, the DOCKER_TLS_VERIFY environment variable is
	// consulted: any non-empty value enables verification (mirroring
	// docker/docker's WithTLSClientConfigFromEnv).
	TLSVerify *bool
}

// DefaultConfig returns a default configuration.
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       90 * time.Second,
		DialTimeout:           30 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		NegotiateTimeout:      defaultNegotiateTimeout,
	}
}

// NewClient creates a new Docker client from environment variables.
func NewClient() (*Client, error) {
	return NewClientWithConfig(DefaultConfig())
}

// NewClientWithConfig creates a new Docker client with custom configuration.
//
// The effective Docker host is resolved in this order:
//  1. config.Host (if non-empty)
//  2. DOCKER_HOST environment variable
//  3. client.DefaultDockerHost
//
// The host's URL scheme is validated against the allow-list (see
// schemeHandlers) and normalized to lowercase. Unsupported schemes
// (ssh://, fd://, etc.) return ErrUnsupportedDockerHostScheme.
//
// The DOCKER_HOST environment variable is consulted at most once per call.
// This is the contract from issue #617: a single resolveDockerHost seam
// avoids the SDK-vs-transport disagreement that caused #605 / #606 / #607 /
// #609 when two readers raced on env.
func NewClientWithConfig(config *ClientConfig) (*Client, error) {
	// Tolerate a nil config the same way DefaultConfig() would: callers
	// constructing through the public surface should not be able to panic
	// the daemon at startup with a nil-pointer deref.
	if config == nil {
		config = DefaultConfig()
	}

	// Resolve the effective host and validate/normalize its scheme up front.
	// resolveDockerHost is the single seam for DOCKER_HOST resolution: it
	// reads the env var at most once per call and returns a non-empty,
	// lowercase-scheme URL ready for both the SDK option and the HTTP
	// transport, so neither downstream caller has to re-derive it.
	normalizedHost, scheme, err := resolveDockerHost(config)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	// Fail-closed for tcp+tls:// without USABLE TLS material. tcp+tls:// is
	// an EXPLICIT TLS opt-in. Invoke resolveTLSConfig so a typo or missing
	// cert files at the path also fails closed at startup. See #627 / #646.
	if scheme == schemeTCPTLS {
		if !hasTLSMaterial(config) {
			return nil, fmt.Errorf(
				"%w: set DOCKER_CERT_PATH (or ClientConfig.TLSCertPath); see docs/TROUBLESHOOTING.md",
				ErrTCPTLSRequiresCertMaterial,
			)
		}
		if _, tlsErr := resolveTLSConfig(config); tlsErr != nil {
			return nil, fmt.Errorf(
				"%w: cert material at the configured path is unreadable or invalid; see docs/TROUBLESHOOTING.md: %w",
				ErrTCPTLSRequiresCertMaterial, tlsErr,
			)
		}
	}

	// Fail-closed for https:// when TLS material IS configured but unloadable.
	// Asymmetry vs tcp+tls://: https:// without ANY material is fine (operator
	// uses system CA), so we only gate when hasTLSMaterial reports true.
	// Without this gate, applyDockerTLS warns-and-continues with TLSClientConfig
	// nil and the SDK dials with Go default TLS — system CA, no client cert —
	// silently downgrading declared mTLS to unauthenticated TLS. See #653.
	if scheme == schemeHTTPS && hasTLSMaterial(config) {
		if _, tlsErr := resolveTLSConfig(config); tlsErr != nil {
			return nil, fmt.Errorf(
				"%w: cert material at the configured path is unreadable or invalid; see docs/TROUBLESHOOTING.md: %w",
				ErrHTTPSRequiresUsableCertMaterial, tlsErr,
			)
		}
	}

	// Mirror the docker CLI: when the host scheme is plain tcp:// AND TLS
	// material is configured, rewrite tcp:// to https:// so the SDK and
	// HTTP transport agree. Without this, Go's http.Transport only triggers
	// TLS for https:// URLs — the cert material wired in by applyDockerTLS
	// (PR #613) was silently unused for tcp://. See #634. The
	// applyTCPTransport hasTLSMaterial branch remains in place to support
	// direct callers of createHTTPClient; in production this rewrite means
	// dispatch goes through applyTLSTransport.
	normalizedHost = upgradeTCPToHTTPSIfTLSMaterial(normalizedHost, config)

	// Build a local copy of config with the normalized host for createHTTPClient
	// so the dispatch sees the lowercase scheme without a second env read. We
	// deliberately avoid mutating the caller's struct - reusing a *ClientConfig
	// across constructions is a reasonable pattern and silent mutation is
	// surprising (TestNewClientWithConfig_DoesNotMutateConfigHost).
	cfgForTransport := *config
	cfgForTransport.Host = normalizedHost

	// Drop client.FromEnv: it would re-read DOCKER_HOST inside the SDK,
	// recreating the dual-reader bug from #605 / #617. We mirror its
	// host + TLS handling explicitly via WithHost / createHTTPClient and
	// preserve the DOCKER_API_VERSION knob via WithVersionFromEnv.
	opts := []client.Opt{
		client.WithVersionFromEnv(),
		client.WithAPIVersionNegotiation(),
		client.WithHost(normalizedHost),
	}

	if config.Version != "" {
		opts = append(opts, client.WithVersion(config.Version))
	}

	if config.HTTPHeaders != nil {
		opts = append(opts, client.WithHTTPHeaders(config.HTTPHeaders))
	}

	// Create custom HTTP client with connection pooling. The host is already
	// resolved on cfgForTransport, so createHTTPClient does not re-read env.
	httpClient := createHTTPClient(&cfgForTransport)
	if httpClient != nil {
		opts = append(opts, client.WithHTTPClient(httpClient))
	}

	sdk, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	// Force early API version negotiation to prevent race conditions.
	// Without this, concurrent goroutines (e.g., Events and ContainerList) making their
	// first API calls simultaneously will race on the lazy version negotiation.
	//
	// Bound the call so a reachable-but-wedged daemon (e.g. socket proxy with a
	// hung upstream) cannot hang Ofelia at startup. NegotiateAPIVersion swallows
	// ping errors silently, so a timeout only bounds the failure case; the
	// successful path is unaffected. See https://github.com/netresearch/ofelia/issues/608.
	negotiateTimeout := config.NegotiateTimeout
	if negotiateTimeout <= 0 {
		negotiateTimeout = defaultNegotiateTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), negotiateTimeout)
	defer cancel()
	sdk.NegotiateAPIVersion(ctx)
	// NegotiateAPIVersion swallows ping errors silently. Surface a deadline
	// hit so operators can correlate startup slowness with daemon health
	// rather than chasing phantom bugs - context cancellation is the only
	// observable signal the SDK leaves us.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		slog.Default().Warn(
			"Docker API version negotiation timed out; continuing with default API version",
			"timeout", negotiateTimeout,
			"hint", "check Docker daemon health and DOCKER_HOST reachability",
		)
	}

	return newClientFromSDK(sdk), nil
}

// newClientFromSDK wraps an existing SDK client.
func newClientFromSDK(sdk *client.Client) *Client {
	c := &Client{sdk: sdk}
	c.containers = &ContainerServiceAdapter{client: sdk}
	c.exec = &ExecServiceAdapter{client: sdk}
	c.images = &ImageServiceAdapter{client: sdk}
	c.events = &EventServiceAdapter{client: sdk}
	c.services = &SwarmServiceAdapter{client: sdk}
	c.networks = &NetworkServiceAdapter{client: sdk}
	c.system = &SystemServiceAdapter{client: sdk}
	return c
}

// resolveDockerHost is the single source of truth for Docker host
// resolution. It applies the config > DOCKER_HOST env > client.DefaultDockerHost
// precedence, normalizes the scheme to lowercase, and validates the scheme
// against the public allow-list.
//
// Returns:
//   - host:   the fully resolved + normalized URL (always non-empty on success)
//   - scheme: the lowercase scheme portion (e.g. "tcp", "unix")
//   - err:    ErrMissingDockerHostScheme or ErrUnsupportedDockerHostScheme
//
// The DOCKER_HOST env var is read at most once per call (and only when
// cfg.Host is empty). This is the contract verified by
// TestResolveDockerHost_ReadsEnvOnce.
func resolveDockerHost(cfg *ClientConfig) (host, scheme string, err error) {
	host = ""
	if cfg != nil {
		host = cfg.Host
	}
	if host == "" {
		host = getenv(client.EnvOverrideHost)
	}
	if host == "" {
		host = client.DefaultDockerHost
	}

	rawScheme, rest, hasScheme := strings.Cut(host, "://")
	if !hasScheme {
		return "", "", fmt.Errorf("%w: %q (e.g. %q, %q); supported schemes: %s",
			ErrMissingDockerHostScheme, host,
			schemeUnix+"://", schemeTCP+"://",
			supportedSchemesMsg)
	}

	scheme = strings.ToLower(rawScheme)
	handler, known := schemeHandlers[scheme]
	if !known || !handler.allowed {
		return "", "", fmt.Errorf("%w: %q; supported schemes: %s",
			ErrUnsupportedDockerHostScheme, scheme+"://", supportedSchemesMsg)
	}

	return scheme + "://" + rest, scheme, nil
}

// validateAndNormalizeHost is a thin wrapper preserved for the existing
// internal test surface (TestValidateAndNormalizeHost). It defers to
// resolveDockerHost with an empty config so callers can validate a
// caller-supplied host string directly. An empty input passes through
// (returns "", nil) so the historical "empty means caller will default"
// contract from PR #612 still holds.
func validateAndNormalizeHost(host string) (string, error) {
	if host == "" {
		return "", nil
	}
	resolved, _, err := resolveDockerHost(&ClientConfig{Host: host})
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// createHTTPClient creates an HTTP client with connection pooling.
//
// When called from NewClientWithConfig, config.Host is already the resolved,
// normalized URL and the env-fallback inside resolveHostForDispatch is a
// no-op (no second env read). When called directly by tests with an empty
// config.Host, the same precedence (env > default) applies.
//
// Dispatch uses the schemeHandlers map directly (NOT the public allow-list),
// so tcp+tls — which is publicly rejected by NewClientWithConfig — still
// dispatches correctly when a test invokes createHTTPClient with
// DOCKER_HOST=tcp+tls:// (TestCreateHTTPClient_TCPPlusTLSEnablesTLS).
//
// On unknown schemes the function falls back to a plain HTTP/1.1 transport
// rather than returning an error; NewClientWithConfig has already validated
// the scheme by the time it gets here in production paths.
func createHTTPClient(config *ClientConfig) *http.Client {
	host, scheme := resolveHostForDispatch(config)

	transport := &http.Transport{
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.MaxConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
	}

	if handler, ok := schemeHandlers[scheme]; ok {
		handler.apply(transport, config, host)
		// Non-TLS schemes MUST suppress Go std lib's lazy HTTP/2 auto-config
		// (see schemeHandler.tlsCapable doc and #668). Driving this from the
		// flag — instead of the apply function — guarantees by construction
		// that a future non-TLS scheme can't regress the invariant (#683).
		if !handler.tlsCapable {
			disableHTTP2AutoConfig(transport)
		}
	} else {
		// Unknown / unrecognized scheme: plain HTTP/1.1. This branch is
		// unreachable when NewClientWithConfig validated the host, but
		// defensive against future direct callers passing exotic schemes.
		// Same hijack risk applies (#668) — suppress HTTP/2 auto-config too.
		transport.ForceAttemptHTTP2 = false
		disableHTTP2AutoConfig(transport)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   0, // No overall timeout; individual operations have timeouts
	}
}

// resolveHostForDispatch returns the (host, scheme) pair used by
// createHTTPClient's dispatch. Unlike resolveDockerHost it does NOT enforce
// the public allow-list — schemes like tcp+tls that are recognized by the
// dispatch table but not exposed via NewClientWithConfig still resolve
// here so direct callers (tests) get a TLS-equipped transport.
//
// Like resolveDockerHost, it consults the env at most once per call (and
// only when cfg.Host is empty), preserving the issue #617 contract for the
// NewClientWithConfig pre-resolved path.
func resolveHostForDispatch(cfg *ClientConfig) (host, scheme string) {
	if cfg != nil {
		host = cfg.Host
	}
	if host == "" {
		host = getenv(client.EnvOverrideHost)
	}
	if host == "" {
		host = client.DefaultDockerHost
	}

	rawScheme, _, ok := strings.Cut(host, "://")
	if !ok {
		return host, ""
	}
	return host, strings.ToLower(rawScheme)
}

// disableHTTP2AutoConfig suppresses Go std lib's lazy HTTP/2 auto-config on
// the given transport. Setting TLSNextProto to a non-nil (empty) map is the
// documented stdlib contract for "don't auto-enable HTTP/2".
//
// Why we need this on the non-TLS apply paths (unix/tcp/plain): the SDK's
// hijack-path dialer (used by ContainerExecAttach -> ofelia's run_exec, plus
// ContainerAttach / ContainerLogs follow) reads baseTransport.TLSClientConfig
// as its "TLS is required" signal. The first ordinary HTTP request through
// the transport triggers http2configureTransports, which lazily ALLOCATES
// TLSClientConfig on the transport in place (to seed NextProtos=[h2 http/1.1]
// for ALPN). After that mutation, the SDK misreads the auto-installed config
// as operator-configured TLS and calls tls.Dial against the plain Docker
// daemon -- producing "tls: first record does not look like a TLS handshake"
// on tcp:// (handshake hits a plain HTTP response) or "dial http: unknown
// network http" on http:// (tls.Dial passes the URL scheme to net.Dial).
//
// TLS Docker hosts (https://, tcp+tls://) leave TLSNextProto nil so ALPN h2
// negotiation still works there.
//
// See https://github.com/netresearch/ofelia/issues/668 and the upstream
// dialer at moby/moby's client/client.go:dialerFromTransport.
func disableHTTP2AutoConfig(transport *http.Transport) {
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
}

// applyUnixTransport configures the transport to dial a Unix domain socket
// at the path encoded in host. HTTP/2 is disabled (Docker over a Unix
// socket is HTTP/1.1). HTTP/2 auto-config suppression is handled centrally
// by createHTTPClient via the tlsCapable: false flag in schemeHandlers.
func applyUnixTransport(transport *http.Transport, cfg *ClientConfig, host string) {
	socketPath := strings.TrimPrefix(host, schemeUnix+"://")
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: cfg.DialTimeout}
		return dialer.DialContext(ctx, schemeUnix, socketPath)
	}
	transport.ForceAttemptHTTP2 = false
}

// applyTLSTransport wires the transport for HTTPS / tcp+tls hosts: HTTP/2
// via ALPN and TLS material from ClientConfig / DOCKER_CERT_PATH env.
func applyTLSTransport(transport *http.Transport, cfg *ClientConfig, _ string) {
	transport.ForceAttemptHTTP2 = true
	applyDockerTLS(transport, cfg)
}

// applyTCPTransport handles plain tcp:// hosts as a no-frills HTTP/1.1
// transport. We deliberately do NOT auto-upgrade to TLS when
// DOCKER_TLS_VERIFY / DOCKER_CERT_PATH are set, even though the docker CLI
// does: Go's http.Transport only performs TLS on https:// URLs, so wiring
// TLSClientConfig into a transport whose URL stays tcp:// would silently
// load the cert material without ever putting it on the wire — worse than
// failing loud, because operators believe they have mTLS when they do not.
//
// Operators who want TLS over a TCP daemon must use tcp+tls:// (#616) or
// https:// directly. See #628 (this reconciliation) and #634 (the deeper
// SDK URL-rewrite half of the docker-CLI parity story).
//
// HTTP/2 auto-config suppression is handled centrally by createHTTPClient
// via the tlsCapable: false flag in schemeHandlers — do not re-add a
// disableHTTP2AutoConfig call here.
func applyTCPTransport(transport *http.Transport, _ *ClientConfig, _ string) {
	transport.ForceAttemptHTTP2 = false
}

// applyHTTPTransport handles plain http:// Docker hosts. Identical to
// applyTCPTransport in TLS / HTTP/2 posture, plus an explicit TCP
// DialContext.
//
// Why the explicit DialContext: the Docker SDK's hijack-path dialer
// (ContainerExecAttach / ContainerAttach / ContainerLogs follow) consults
// baseTransport.DialContext first; when it's nil the SDK falls to a
// switch keyed on cli.proto and ultimately net.Dial(cli.proto, addr).
// For proto == "http" that becomes net.Dial("http", addr), which Go's
// net package rejects with "unknown network http" — the bug tracked in
// https://github.com/netresearch/ofelia/issues/682. Installing a TCP
// dialer here lets the SDK pick up our DialContext via dialerFromTransport
// and never hits the broken fallback.
//
// url.Parse handles path-bearing hosts (http://host:port/v1.43) and IPv6
// literals (http://[::1]:2375) the same way the Docker SDK's ParseHostURL
// handles tcp://, so http:// and tcp:// dial to the same target for any
// given operator-supplied DOCKER_HOST value. A plain strings.TrimPrefix
// would have fed "host:port/v1.43" to net.Dial("tcp", …), failing.
//
// HTTP/2 auto-config suppression is handled centrally by createHTTPClient
// via the tlsCapable: false flag in schemeHandlers — do not re-add a
// disableHTTP2AutoConfig call here.
func applyHTTPTransport(transport *http.Transport, cfg *ClientConfig, host string) {
	addr := host
	if parsed, err := url.Parse(host); err == nil && parsed.Host != "" {
		addr = parsed.Host
	} else {
		addr = strings.TrimPrefix(host, schemeHTTP+"://")
	}
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: cfg.DialTimeout}
		return dialer.DialContext(ctx, schemeTCP, addr)
	}
	transport.ForceAttemptHTTP2 = false
}

// applyPlainTransport is the no-frills HTTP/1.1 path used for npipe://
// (the actual dialer lives in the SDK and is build-tagged; on non-Windows
// the connection will fail at dial time — see docs/TROUBLESHOOTING.md).
// http:// has its own handler (applyHTTPTransport, #682) so it can
// install a TCP DialContext that fixes hijack-path APIs.
//
// HTTP/2 auto-config suppression is handled centrally by createHTTPClient
// via the tlsCapable: false flag in schemeHandlers — do not re-add a
// disableHTTP2AutoConfig call here.
func applyPlainTransport(transport *http.Transport, _ *ClientConfig, _ string) {
	transport.ForceAttemptHTTP2 = false
}

// upgradeTCPToHTTPSIfTLSMaterial mirrors the docker CLI's silent scheme
// upgrade: when the resolved host uses the plain tcp:// scheme AND TLS
// material is configured (DOCKER_CERT_PATH env or the equivalent
// ClientConfig.TLSCertPath override), the scheme is rewritten to https://
// before being handed to client.WithHost. Without this rewrite, the SDK
// keeps a tcp:// URL while the custom transport carries TLS material that
// http.Transport never triggers (it only switches to TLS for https://
// URLs), so the cert material wired by applyDockerTLS is silently unused.
// Closes the docker-CLI parity gap from
// https://github.com/netresearch/ofelia/issues/634.
//
// All other schemes (tcp+tls://, https://, unix://, http://, npipe://) pass
// through unchanged: they either already TLS-correctly route through
// applyTLSTransport, are explicitly plaintext by the operator's choice, or
// have no TLS implication.
func upgradeTCPToHTTPSIfTLSMaterial(host string, cfg *ClientConfig) string {
	const tcpPrefix = schemeTCP + "://"
	if !strings.HasPrefix(host, tcpPrefix) {
		return host
	}
	if !hasTLSMaterial(cfg) {
		return host
	}
	return schemeHTTPS + "://" + strings.TrimPrefix(host, tcpPrefix)
}

// hasTLSMaterial reports whether the explicit ClientConfig fields or the
// DOCKER_CERT_PATH env var would cause resolveTLSConfig to produce a non-nil
// *tls.Config. Used by upgradeTCPToHTTPSIfTLSMaterial to decide whether to
// rewrite the tcp:// scheme to https:// (docker-CLI parity, #634), and
// consulted by NewClientWithConfig's tcp+tls:// fail-closed gate (#627) to
// short-circuit before resolveTLSConfig is even invoked.
func hasTLSMaterial(config *ClientConfig) bool {
	if config != nil && config.TLSCertPath != "" {
		return true
	}
	return getenv(client.EnvOverrideCertPath) != ""
}

// applyDockerTLS resolves TLS material and assigns it to the transport,
// surfacing misconfiguration via slog rather than silently downgrading.
// No-op when no TLS material is configured.
func applyDockerTLS(transport *http.Transport, config *ClientConfig) {
	tlsCfg, err := resolveTLSConfig(config)
	if err != nil {
		slog.Default().Warn(
			"Docker TLS config could not be loaded; falling back to default TLS without client cert / pinned CA",
			"error", err,
			"hint", "verify DOCKER_CERT_PATH points at a directory containing readable ca.pem, cert.pem, key.pem",
		)
		return
	}
	if tlsCfg != nil {
		transport.TLSClientConfig = tlsCfg
	}
}

// resolveTLSConfig builds a *tls.Config for an HTTPS Docker host using the
// ClientConfig fields with fallback to DOCKER_CERT_PATH / DOCKER_TLS_VERIFY
// environment variables. Returns a nil *tls.Config (with a nil error) when
// no cert path is configured — mirroring docker/docker's
// WithTLSClientConfigFromEnv: empty cert path means no TLS modification.
//
// Precedence:
//   - ClientConfig.TLSCertPath > DOCKER_CERT_PATH env > none
//   - ClientConfig.TLSVerify   > DOCKER_TLS_VERIFY env (any non-empty value
//     enables verification, matching the SDK's semantics)
func resolveTLSConfig(config *ClientConfig) (*tls.Config, error) {
	certPath := config.TLSCertPath
	if certPath == "" {
		certPath = getenv(client.EnvOverrideCertPath)
	}
	if certPath == "" {
		//nolint:nilnil // sentinel "no TLS configured" matches SDK semantics
		return nil, nil
	}

	var verify bool
	if config.TLSVerify != nil {
		verify = *config.TLSVerify
	} else {
		// Mirror docker/docker: InsecureSkipVerify = (DOCKER_TLS_VERIFY == "").
		// Any non-empty value (including "0") implies verify.
		verify = getenv(client.EnvTLSVerify) != ""
	}

	tlsCfg, err := tlsconfig.Client(tlsconfig.Options{
		CAFile:             filepath.Join(certPath, "ca.pem"),
		CertFile:           filepath.Join(certPath, "cert.pem"),
		KeyFile:            filepath.Join(certPath, "key.pem"),
		InsecureSkipVerify: !verify,
	})
	if err != nil {
		return nil, fmt.Errorf("loading docker TLS config from %s (expected ca.pem, cert.pem, key.pem): %w", certPath, err)
	}
	return tlsCfg, nil
}

// Containers returns the container service.
func (c *Client) Containers() ports.ContainerService {
	return c.containers
}

// Exec returns the exec service.
func (c *Client) Exec() ports.ExecService {
	return c.exec
}

// Images returns the image service.
func (c *Client) Images() ports.ImageService {
	return c.images
}

// Events returns the event service.
func (c *Client) Events() ports.EventService {
	return c.events
}

// Services returns the Swarm service.
func (c *Client) Services() ports.SwarmService {
	return c.services
}

// Networks returns the network service.
func (c *Client) Networks() ports.NetworkService {
	return c.networks
}

// System returns the system service.
func (c *Client) System() ports.SystemService {
	return c.system
}

// Close closes the client.
func (c *Client) Close() error {
	if err := c.sdk.Close(); err != nil {
		return fmt.Errorf("closing docker client: %w", err)
	}
	return nil
}

// SDK returns the underlying Docker SDK client.
// This should only be used for operations not covered by the ports interface.
func (c *Client) SDK() *client.Client {
	return c.sdk
}
