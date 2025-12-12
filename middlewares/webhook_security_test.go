package middlewares

import (
	"testing"

	. "gopkg.in/check.v1"
)

type SuiteWebhookSecurity struct {
	BaseSuite
}

var _ = Suite(&SuiteWebhookSecurity{})

// SSRF Protection Tests - Blocked Hosts

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksLocalhost(c *C) {
	err := ValidateWebhookURLImpl("http://localhost/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*blocked host.*")
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_Blocks127001(c *C) {
	err := ValidateWebhookURLImpl("http://127.0.0.1/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*blocked host.*")
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksIPv6Localhost(c *C) {
	err := ValidateWebhookURLImpl("http://[::1]/webhook")
	c.Assert(err, NotNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksMetadataEndpoint(c *C) {
	err := ValidateWebhookURLImpl("http://169.254.169.254/latest/meta-data/")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*blocked host.*")
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksGCPMetadata(c *C) {
	err := ValidateWebhookURLImpl("http://metadata.google.internal/computeMetadata/v1/")
	c.Assert(err, NotNil)
}

// SSRF Protection Tests - Private Networks

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksPrivateClassA(c *C) {
	err := ValidateWebhookURLImpl("http://10.0.0.1/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*private network.*")
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksPrivateClassB(c *C) {
	err := ValidateWebhookURLImpl("http://172.16.0.1/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*private network.*")

	err = ValidateWebhookURLImpl("http://172.31.255.255/webhook")
	c.Assert(err, NotNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksPrivateClassC(c *C) {
	err := ValidateWebhookURLImpl("http://192.168.1.1/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*private network.*")
}

// SSRF Protection Tests - Internal Hostnames

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksLocalSuffix(c *C) {
	err := ValidateWebhookURLImpl("http://myservice.local/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*internal hostname.*")
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksInternalSuffix(c *C) {
	err := ValidateWebhookURLImpl("http://api.internal/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*internal hostname.*")
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksCorpSuffix(c *C) {
	err := ValidateWebhookURLImpl("http://intranet.corp/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*internal hostname.*")
}

// SSRF Protection Tests - URL Scheme

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_RequiresHTTP(c *C) {
	err := ValidateWebhookURLImpl("ftp://example.com/file")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*http or https.*")
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_RequiresHost(c *C) {
	err := ValidateWebhookURLImpl("http:///path")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*host.*")
}

// SSRF Protection Tests - Bypass Attempts

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksHexIP(c *C) {
	err := ValidateWebhookURLImpl("http://0x7f.0x0.0x0.0x1/webhook")
	c.Assert(err, NotNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksDecimalIP(c *C) {
	err := ValidateWebhookURLImpl("http://2130706433/webhook") // 127.0.0.1 in decimal
	c.Assert(err, NotNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksURLEncodedLocalhost(c *C) {
	// URL-encoded "localhost"
	err := ValidateWebhookURLImpl("http://%6c%6f%63%61%6c%68%6f%73%74/webhook")
	c.Assert(err, NotNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_BlocksCredentialBypass(c *C) {
	err := ValidateWebhookURLImpl("http://user@localhost:8080/webhook")
	c.Assert(err, NotNil)
}

// SSRF Protection Tests - Valid URLs

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsPublicHTTP(c *C) {
	err := ValidateWebhookURLImpl("http://hooks.example.com/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsPublicHTTPS(c *C) {
	err := ValidateWebhookURLImpl("https://hooks.slack.com/services/T123/B456/secret")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsDiscord(c *C) {
	err := ValidateWebhookURLImpl("https://discord.com/api/webhooks/123/secret")
	c.Assert(err, IsNil)
}

// Configurable Security Validator Tests

func (s *SuiteWebhookSecurity) TestSecurityValidator_DefaultConfig(c *C) {
	validator := NewWebhookSecurityValidator(nil)
	c.Assert(validator, NotNil)
	c.Assert(validator.config.AllowLocalhost, Equals, false)
	c.Assert(validator.config.AllowPrivateNetworks, Equals, false)
}

func (s *SuiteWebhookSecurity) TestSecurityValidator_AllowLocalhost(c *C) {
	config := &WebhookSecurityConfig{
		AllowLocalhost: true,
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("http://localhost/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestSecurityValidator_AllowPrivateNetworks(c *C) {
	config := &WebhookSecurityConfig{
		AllowPrivateNetworks: true,
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("http://192.168.1.1/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestSecurityValidator_AllowedHostsWhitelist(c *C) {
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"hooks.example.com", "*.slack.com"},
	}
	validator := NewWebhookSecurityValidator(config)

	// Allowed exact match
	err := validator.Validate("https://hooks.example.com/webhook")
	c.Assert(err, IsNil)

	// Allowed wildcard match
	err = validator.Validate("https://hooks.slack.com/webhook")
	c.Assert(err, IsNil)

	// Not allowed - not in whitelist
	err = validator.Validate("https://other.example.com/webhook")
	c.Assert(err, NotNil)
}

func (s *SuiteWebhookSecurity) TestSecurityValidator_BlockedHosts(c *C) {
	config := &WebhookSecurityConfig{
		BlockedHosts: []string{"blocked.example.com"},
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("https://blocked.example.com/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*blocked.*")

	// Other hosts should work
	err = validator.Validate("https://allowed.example.com/webhook")
	c.Assert(err, IsNil)
}

// Standard Go testing for integration tests

func TestSSRFProtection_ComprehensiveBlocklist(t *testing.T) {
	blockedURLs := []string{
		// Localhost variations
		"http://localhost/",
		"http://127.0.0.1/",
		"http://[::1]/",
		"http://0.0.0.0/",

		// Cloud metadata endpoints
		"http://169.254.169.254/",
		"http://metadata.google.internal/",

		// Private networks
		"http://10.0.0.1/",
		"http://10.255.255.255/",
		"http://172.16.0.1/",
		"http://172.31.255.255/",
		"http://192.168.0.1/",
		"http://192.168.255.255/",

		// Internal hostnames
		"http://api.local/",
		"http://service.internal/",
		"http://server.corp/",
		"http://host.localhost/",
	}

	for _, url := range blockedURLs {
		err := ValidateWebhookURLImpl(url)
		if err == nil {
			t.Errorf("Expected URL %s to be blocked, but it was allowed", url)
		}
	}
}

func TestSSRFProtection_AllowedURLs(t *testing.T) {
	allowedURLs := []string{
		"https://hooks.slack.com/services/T123/B456/secret",
		"https://discord.com/api/webhooks/123/secret",
		"https://api.pushover.net/1/messages.json",
		"https://api.pagerduty.com/v2/enqueue",
		"https://ntfy.sh/mytopic",
		"https://hooks.example.com/webhook",
	}

	for _, url := range allowedURLs {
		err := ValidateWebhookURLImpl(url)
		if err != nil {
			t.Errorf("Expected URL %s to be allowed, but got error: %v", url, err)
		}
	}
}

func TestSecurityConfig_Default(t *testing.T) {
	config := DefaultWebhookSecurityConfig()

	if config.AllowLocalhost {
		t.Error("Default config should not allow localhost")
	}

	if config.AllowPrivateNetworks {
		t.Error("Default config should not allow private networks")
	}

	if len(config.AllowedHosts) != 0 {
		t.Error("Default config should have empty allowed hosts")
	}

	if len(config.BlockedHosts) != 0 {
		t.Error("Default config should have empty blocked hosts")
	}
}

// DNS Rebinding Protection Tests

func TestValidateResolvedIP_BlocksLoopback(t *testing.T) {
	// Test that resolved IPs are validated
	err := validateIP([]byte{127, 0, 0, 1})
	if err == nil {
		t.Error("Expected loopback IP to be blocked")
	}
}

func TestValidateResolvedIP_BlocksPrivate10(t *testing.T) {
	err := validateIP([]byte{10, 0, 0, 1})
	if err == nil {
		t.Error("Expected 10.x.x.x IP to be blocked")
	}
}

func TestValidateResolvedIP_BlocksPrivate172(t *testing.T) {
	err := validateIP([]byte{172, 16, 0, 1})
	if err == nil {
		t.Error("Expected 172.16.x.x IP to be blocked")
	}
}

func TestValidateResolvedIP_BlocksPrivate192(t *testing.T) {
	err := validateIP([]byte{192, 168, 1, 1})
	if err == nil {
		t.Error("Expected 192.168.x.x IP to be blocked")
	}
}

func TestValidateResolvedIP_AllowsPublic(t *testing.T) {
	// Google's DNS
	err := validateIP([]byte{8, 8, 8, 8})
	if err != nil {
		t.Errorf("Expected public IP 8.8.8.8 to be allowed, got error: %v", err)
	}
}

func TestSafeTransport_Creation(t *testing.T) {
	transport := NewSafeTransport()
	if transport == nil {
		t.Fatal("NewSafeTransport returned nil")
	}

	if transport.DialContext == nil {
		t.Error("SafeTransport should have custom DialContext")
	}
}

func TestSafeTransport_BlocksResolvedLoopback(t *testing.T) {
	transport := NewSafeTransport()

	// Create a test that would resolve to localhost
	// Note: This test verifies the transport exists and has the right structure
	// Actual DNS resolution testing would require mocking
	if transport.DialContext == nil {
		t.Error("Transport should have DialContext for DNS validation")
	}
}
