package middlewares

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// SSRF protection: block requests to internal networks and sensitive hosts

// blockedHosts contains hostnames that should never be accessed
var blockedHosts = map[string]bool{
	"localhost":                true,
	"127.0.0.1":                true,
	"::1":                      true,
	"0.0.0.0":                  true,
	"metadata.google":          true, // GCP metadata
	"metadata":                 true, // Generic cloud metadata
	"169.254.169.254":          true, // AWS/Azure/GCP metadata endpoint
	"metadata.google.internal": true,
}

// blockedPrefixes contains hostname prefixes that indicate internal services
var blockedPrefixes = []string{
	"10.",                                      // Private class A
	"192.168.",                                 // Private class C
	"172.16.", "172.17.", "172.18.", "172.19.", // Private class B (172.16-31)
	"172.20.", "172.21.", "172.22.", "172.23.",
	"172.24.", "172.25.", "172.26.", "172.27.",
	"172.28.", "172.29.", "172.30.", "172.31.",
	"fd",      // IPv6 private
	"fe80:",   // IPv6 link-local
	"::ffff:", // IPv4-mapped IPv6
}

// blockedSuffixes contains hostname suffixes that indicate internal services
var blockedSuffixes = []string{
	".local",
	".internal",
	".localhost",
	".localdomain",
	".corp",
	".home",
	".lan",
}

// ValidateWebhookURLImpl validates a URL for SSRF protection
func ValidateWebhookURLImpl(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow HTTP and HTTPS
	if u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS {
		return fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}

	// Must have a host
	if u.Host == "" {
		return fmt.Errorf("URL must have a host")
	}

	// Extract hostname (without port)
	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Check blocked hosts
	if blockedHosts[strings.ToLower(hostname)] {
		return fmt.Errorf("access to %q is not allowed (blocked host)", hostname)
	}

	// Check blocked prefixes
	lowerHost := strings.ToLower(hostname)
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(lowerHost, prefix) {
			return fmt.Errorf("access to %q is not allowed (private network)", hostname)
		}
	}

	// Check blocked suffixes
	for _, suffix := range blockedSuffixes {
		if strings.HasSuffix(lowerHost, suffix) {
			return fmt.Errorf("access to %q is not allowed (internal hostname)", hostname)
		}
	}

	// If it looks like an IP address, validate it
	if ip := net.ParseIP(hostname); ip != nil {
		if err := validateIP(ip); err != nil {
			return fmt.Errorf("access to %q is not allowed: %w", hostname, err)
		}
	}

	// Check for URL-encoded localhost bypasses
	if containsLocalhost(rawURL) {
		return fmt.Errorf("URL contains localhost bypass attempt")
	}

	return nil
}

// validateIP checks if an IP address is safe to access
func validateIP(ip net.IP) error {
	// Block loopback
	if ip.IsLoopback() {
		return fmt.Errorf("loopback address")
	}

	// Block private addresses
	if ip.IsPrivate() {
		return fmt.Errorf("private address")
	}

	// Block link-local addresses
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local address")
	}

	// Block unspecified addresses (0.0.0.0, ::)
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified address")
	}

	// Block multicast addresses
	if ip.IsMulticast() {
		return fmt.Errorf("multicast address")
	}

	// Block interface-local multicast
	if ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("interface-local multicast address")
	}

	return nil
}

// containsLocalhost checks for various localhost bypass attempts
func containsLocalhost(rawURL string) bool {
	// Common bypass patterns
	bypasses := []string{
		"%6c%6f%63%61%6c%68%6f%73%74", // localhost URL-encoded
		"%31%32%37%2e%30%2e%30%2e%31", // 127.0.0.1 URL-encoded
		"0x7f.0x0.0x0.0x1",            // Hex IP
		"0177.0.0.01",                 // Octal IP
		"2130706433",                  // Decimal IP for 127.0.0.1
		"@localhost",                  // Credential bypass
		"@127.0.0.1",                  // Credential bypass
		"#localhost",                  // Fragment bypass
		"#127.0.0.1",                  // Fragment bypass
	}

	lowerURL := strings.ToLower(rawURL)
	for _, bypass := range bypasses {
		if strings.Contains(lowerURL, bypass) {
			return true
		}
	}

	return false
}

// WebhookSecurityConfig holds security configuration for webhooks
type WebhookSecurityConfig struct {
	// AllowPrivateNetworks allows access to private IP ranges (dangerous!)
	AllowPrivateNetworks bool

	// AllowLocalhost allows access to localhost (dangerous!)
	AllowLocalhost bool

	// AllowedHosts whitelist of allowed hosts (if set, only these hosts are allowed)
	AllowedHosts []string

	// BlockedHosts additional hosts to block
	BlockedHosts []string
}

// DefaultWebhookSecurityConfig returns the default security configuration
func DefaultWebhookSecurityConfig() *WebhookSecurityConfig {
	return &WebhookSecurityConfig{
		AllowPrivateNetworks: false,
		AllowLocalhost:       false,
		AllowedHosts:         nil,
		BlockedHosts:         nil,
	}
}

// WebhookSecurityValidator validates URLs with configurable security rules
type WebhookSecurityValidator struct {
	config *WebhookSecurityConfig
}

// NewWebhookSecurityValidator creates a new security validator
func NewWebhookSecurityValidator(config *WebhookSecurityConfig) *WebhookSecurityValidator {
	if config == nil {
		config = DefaultWebhookSecurityConfig()
	}
	return &WebhookSecurityValidator{config: config}
}

// Validate checks if a URL is safe to access.
// The complexity is intentional for comprehensive security validation.
//
//nolint:gocyclo // security validation requires multiple checks in sequence
func (v *WebhookSecurityValidator) Validate(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow HTTP and HTTPS
	if u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS {
		return fmt.Errorf("URL scheme must be http or https")
	}

	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Check whitelist first (if configured)
	if len(v.config.AllowedHosts) > 0 {
		if !v.isAllowedHost(hostname) {
			return fmt.Errorf("host %q is not in allowed hosts list", hostname)
		}
		// If in whitelist, allow without further checks
		return nil
	}

	// Check additional blocked hosts
	for _, blocked := range v.config.BlockedHosts {
		if strings.EqualFold(hostname, blocked) {
			return fmt.Errorf("host %q is blocked", hostname)
		}
	}

	// Check localhost (unless allowed)
	if !v.config.AllowLocalhost {
		if isLocalhost(hostname) {
			return fmt.Errorf("localhost access is not allowed")
		}
	}

	// Check private networks (unless allowed)
	if !v.config.AllowPrivateNetworks {
		if ip := net.ParseIP(hostname); ip != nil {
			if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				return fmt.Errorf("private network access is not allowed")
			}
		}

		// Check hostname patterns
		lowerHost := strings.ToLower(hostname)
		for _, suffix := range blockedSuffixes {
			if strings.HasSuffix(lowerHost, suffix) {
				return fmt.Errorf("internal hostname %q is not allowed", hostname)
			}
		}
	}

	// Check bypass attempts
	if containsLocalhost(rawURL) {
		return fmt.Errorf("URL contains suspicious patterns")
	}

	return nil
}

// isAllowedHost checks if a hostname matches the allowed hosts list
func (v *WebhookSecurityValidator) isAllowedHost(hostname string) bool {
	lowerHost := strings.ToLower(hostname)
	for _, allowed := range v.config.AllowedHosts {
		lowerAllowed := strings.ToLower(allowed)

		// Exact match
		if lowerHost == lowerAllowed {
			return true
		}

		// Wildcard match (e.g., "*.example.com")
		if strings.HasPrefix(lowerAllowed, "*.") {
			suffix := lowerAllowed[1:] // Keep the dot
			if strings.HasSuffix(lowerHost, suffix) {
				return true
			}
		}
	}
	return false
}

// isLocalhost checks if a hostname is localhost
func isLocalhost(hostname string) bool {
	lowerHost := strings.ToLower(hostname)

	if lowerHost == "localhost" || lowerHost == "127.0.0.1" || lowerHost == "::1" {
		return true
	}

	if strings.HasSuffix(lowerHost, ".localhost") {
		return true
	}

	// Check if it resolves to localhost
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}

	return false
}

func init() {
	// Set the global validator function
	ValidateWebhookURL = ValidateWebhookURLImpl
}

// TransportFactory creates HTTP transports for webhook requests.
// This can be overridden in tests to bypass DNS rebinding protection.
var TransportFactory = NewSafeTransport

// NewSafeTransport creates an HTTP transport with DNS rebinding protection.
// It validates that resolved IP addresses are not private or loopback addresses,
// preventing DNS rebinding attacks where a domain initially resolves to a public IP
// but later resolves to a private/internal IP.
func NewSafeTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Extract host from address
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q: %w", addr, err)
			}

			// Resolve the hostname to IP addresses
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("DNS lookup failed for %q: %w", host, err)
			}

			// Validate all resolved IPs
			for _, ip := range ips {
				if err := validateIP(ip); err != nil {
					return nil, fmt.Errorf("DNS rebinding protection: %q resolved to blocked IP %s: %w", host, ip, err)
				}
			}

			// Connect to the first valid IP
			if len(ips) > 0 {
				addr = net.JoinHostPort(ips[0].String(), port)
			}

			return dialer.DialContext(ctx, network, addr)
		},
	}
}
