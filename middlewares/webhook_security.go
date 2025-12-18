package middlewares

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// ValidateWebhookURLImpl validates basic URL requirements.
// This is the default validator that allows all hosts (consistent with local command trust model).
// For whitelist mode, use WebhookSecurityValidator with specific AllowedHosts.
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

	return nil
}

// WebhookSecurityConfig holds security configuration for webhooks
type WebhookSecurityConfig struct {
	// AllowedHosts controls which hosts webhooks can target.
	// "*" = allow all hosts (default, consistent with local command trust model)
	// Specific list = whitelist mode, only those hosts allowed
	// Supports wildcards: "*.example.com"
	AllowedHosts []string
}

// DefaultWebhookSecurityConfig returns the default security configuration
// Default: AllowedHosts=["*"] for consistency with local command execution trust model
func DefaultWebhookSecurityConfig() *WebhookSecurityConfig {
	return &WebhookSecurityConfig{
		AllowedHosts: []string{"*"}, // Allow all by default
	}
}

// WebhookSecurityValidator validates URLs with configurable security rules
type WebhookSecurityValidator struct {
	config *WebhookSecurityConfig
}

// NewWebhookSecurityValidator creates a new security validator
func NewWebhookSecurityValidator(config *WebhookSecurityConfig) *WebhookSecurityValidator {
	if config == nil || len(config.AllowedHosts) == 0 {
		config = DefaultWebhookSecurityConfig()
	}
	return &WebhookSecurityValidator{config: config}
}

// Validate checks if a URL is safe to access based on the allowed hosts configuration.
// If AllowedHosts contains "*", all hosts are allowed (default behavior).
// Otherwise, only hosts in the whitelist are allowed.
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

	// Check if all hosts are allowed (default behavior)
	if v.isAllowAll() {
		return nil
	}

	// Whitelist mode: only allow hosts in the list
	if !v.isAllowedHost(hostname) {
		return fmt.Errorf("host %q is not in allowed hosts list", hostname)
	}

	return nil
}

// isAllowAll checks if the configuration allows all hosts
func (v *WebhookSecurityValidator) isAllowAll() bool {
	for _, h := range v.config.AllowedHosts {
		if h == "*" {
			return true
		}
	}
	return false
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

// SetGlobalSecurityConfig sets the global security configuration for webhooks
// This should be called during initialization with the parsed configuration
func SetGlobalSecurityConfig(config *WebhookSecurityConfig) {
	// Update the global validator to use the new config
	if config != nil {
		validator := NewWebhookSecurityValidator(config)
		ValidateWebhookURL = validator.Validate
		TransportFactory = func() *http.Transport {
			return NewConfigurableTransport(config)
		}
	} else {
		ValidateWebhookURL = ValidateWebhookURLImpl
		TransportFactory = NewSafeTransport
	}
}

// SecurityConfigFromGlobal creates a WebhookSecurityConfig from WebhookGlobalConfig
func SecurityConfigFromGlobal(global *WebhookGlobalConfig) *WebhookSecurityConfig {
	if global == nil {
		return DefaultWebhookSecurityConfig()
	}

	config := &WebhookSecurityConfig{}

	// Parse allowed hosts from comma-separated string
	// Default is "*" (allow all) if not specified
	allowedHosts := global.AllowedHosts
	if allowedHosts == "" {
		allowedHosts = "*"
	}

	hosts := strings.Split(allowedHosts, ",")
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h != "" {
			config.AllowedHosts = append(config.AllowedHosts, h)
		}
	}

	return config
}

func init() {
	// Set the global validator function
	ValidateWebhookURL = ValidateWebhookURLImpl
}

// TransportFactory creates HTTP transports for webhook requests.
// This can be overridden in tests to bypass DNS rebinding protection.
var TransportFactory = NewSafeTransport

// NewSafeTransport creates a standard HTTP transport.
// URL validation is handled by the security validator before requests are made.
func NewSafeTransport() *http.Transport {
	return NewConfigurableTransport(DefaultWebhookSecurityConfig())
}

// NewConfigurableTransport creates a standard HTTP transport.
// Security validation is handled by WebhookSecurityValidator before requests are made.
// The transport itself doesn't need additional restrictions since we follow
// the "trust the config" model - if users can run arbitrary commands, they can
// send webhooks to any configured destination.
func NewConfigurableTransport(config *WebhookSecurityConfig) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
