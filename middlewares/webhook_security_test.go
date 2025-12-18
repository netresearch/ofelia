package middlewares

import (
	"testing"

	. "gopkg.in/check.v1"
)

type SuiteWebhookSecurity struct {
	BaseSuite
}

var _ = Suite(&SuiteWebhookSecurity{})

// URL Scheme Validation Tests

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

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsHTTP(c *C) {
	err := ValidateWebhookURLImpl("http://example.com/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsHTTPS(c *C) {
	err := ValidateWebhookURLImpl("https://example.com/webhook")
	c.Assert(err, IsNil)
}

// Default Allow-All Behavior Tests (Trust-the-Config Model)
// Since users can run arbitrary local commands, they can send webhooks anywhere

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsLocalhost(c *C) {
	// Default behavior: allow all hosts (consistent with local command trust model)
	err := ValidateWebhookURLImpl("http://localhost/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_Allows127001(c *C) {
	err := ValidateWebhookURLImpl("http://127.0.0.1/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsPrivateClassA(c *C) {
	err := ValidateWebhookURLImpl("http://10.0.0.1/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsPrivateClassB(c *C) {
	err := ValidateWebhookURLImpl("http://172.16.0.1/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsPrivateClassC(c *C) {
	err := ValidateWebhookURLImpl("http://192.168.1.1/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsInternalHostname(c *C) {
	err := ValidateWebhookURLImpl("http://ntfy.local/webhook")
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookSecurity) TestValidateWebhookURL_AllowsPublicHTTPS(c *C) {
	err := ValidateWebhookURLImpl("https://hooks.slack.com/services/T123/B456/secret")
	c.Assert(err, IsNil)
}

// Security Config Default Tests

func (s *SuiteWebhookSecurity) TestSecurityValidator_DefaultConfig(c *C) {
	validator := NewWebhookSecurityValidator(nil)
	c.Assert(validator, NotNil)
	// Default: AllowedHosts = ["*"] (allow all)
	c.Assert(len(validator.config.AllowedHosts), Equals, 1)
	c.Assert(validator.config.AllowedHosts[0], Equals, "*")
}

func (s *SuiteWebhookSecurity) TestSecurityValidator_DefaultAllowsAll(c *C) {
	validator := NewWebhookSecurityValidator(nil)

	// All hosts should be allowed by default
	testURLs := []string{
		"http://localhost/webhook",
		"http://127.0.0.1/webhook",
		"http://10.0.0.1/webhook",
		"http://192.168.1.1/webhook",
		"http://ntfy.internal/webhook",
		"https://hooks.slack.com/webhook",
	}

	for _, url := range testURLs {
		err := validator.Validate(url)
		c.Assert(err, IsNil, Commentf("Expected URL %s to be allowed", url))
	}
}

// Whitelist Mode Tests

func (s *SuiteWebhookSecurity) TestSecurityValidator_WhitelistMode(c *C) {
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"hooks.slack.com", "ntfy.local"},
	}
	validator := NewWebhookSecurityValidator(config)

	// Allowed: exact match
	err := validator.Validate("https://hooks.slack.com/webhook")
	c.Assert(err, IsNil)

	err = validator.Validate("http://ntfy.local/webhook")
	c.Assert(err, IsNil)

	// Blocked: not in whitelist
	err = validator.Validate("http://192.168.1.1/webhook")
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*not in allowed hosts.*")
}

func (s *SuiteWebhookSecurity) TestSecurityValidator_WhitelistWildcard(c *C) {
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"*.slack.com", "*.internal.example.com"},
	}
	validator := NewWebhookSecurityValidator(config)

	// Allowed: wildcard match
	err := validator.Validate("https://hooks.slack.com/webhook")
	c.Assert(err, IsNil)

	err = validator.Validate("http://ntfy.internal.example.com/webhook")
	c.Assert(err, IsNil)

	// Blocked: doesn't match wildcard
	err = validator.Validate("https://discord.com/webhook")
	c.Assert(err, NotNil)
}

func (s *SuiteWebhookSecurity) TestSecurityValidator_ExplicitAllowAll(c *C) {
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"*"},
	}
	validator := NewWebhookSecurityValidator(config)

	// All hosts should be allowed
	testURLs := []string{
		"http://localhost/webhook",
		"http://192.168.1.1/webhook",
		"https://hooks.slack.com/webhook",
	}

	for _, url := range testURLs {
		err := validator.Validate(url)
		c.Assert(err, IsNil, Commentf("Expected URL %s to be allowed with explicit *", url))
	}
}

func (s *SuiteWebhookSecurity) TestSecurityValidator_MixedWithWildcard(c *C) {
	// If "*" is in the list, all hosts are allowed
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"hooks.slack.com", "*"},
	}
	validator := NewWebhookSecurityValidator(config)

	// All hosts should be allowed because * is in the list
	err := validator.Validate("http://any-host.example.com/webhook")
	c.Assert(err, IsNil)
}

// Standard Go testing for integration tests

func TestSecurityConfig_Default(t *testing.T) {
	config := DefaultWebhookSecurityConfig()

	// Default: AllowedHosts = ["*"] (allow all hosts)
	if len(config.AllowedHosts) != 1 {
		t.Errorf("Expected 1 entry in AllowedHosts, got %d", len(config.AllowedHosts))
	}

	if config.AllowedHosts[0] != "*" {
		t.Errorf("Expected AllowedHosts[0] to be '*', got %q", config.AllowedHosts[0])
	}
}

func TestURLValidation_AllowsAllHostsByDefault(t *testing.T) {
	// These URLs should all be allowed with default validation
	allowedURLs := []string{
		"https://hooks.slack.com/services/T123/B456/secret",
		"https://discord.com/api/webhooks/123/secret",
		"https://ntfy.sh/mytopic",
		"http://localhost/webhook",
		"http://127.0.0.1/webhook",
		"http://10.0.0.1/webhook",
		"http://192.168.1.20/webhook",
		"http://ntfy.internal/webhook",
		"http://metadata.google.internal/computeMetadata/v1/",
	}

	for _, url := range allowedURLs {
		err := ValidateWebhookURLImpl(url)
		if err != nil {
			t.Errorf("Expected URL %s to be allowed, but got error: %v", url, err)
		}
	}
}

func TestURLValidation_BlocksInvalidSchemes(t *testing.T) {
	invalidURLs := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://example.com/",
		"javascript:alert(1)",
	}

	for _, url := range invalidURLs {
		err := ValidateWebhookURLImpl(url)
		if err == nil {
			t.Errorf("Expected URL %s to be blocked (invalid scheme), but it was allowed", url)
		}
	}
}

func TestURLValidation_RequiresHostname(t *testing.T) {
	invalidURLs := []string{
		"http:///path",
		"https://",
	}

	for _, url := range invalidURLs {
		err := ValidateWebhookURLImpl(url)
		if err == nil {
			t.Errorf("Expected URL %s to be blocked (no hostname), but it was allowed", url)
		}
	}
}

// Tests for SecurityConfigFromGlobal

func TestSecurityConfigFromGlobal_NilConfig(t *testing.T) {
	config := SecurityConfigFromGlobal(nil)

	if config == nil {
		t.Fatal("SecurityConfigFromGlobal should return non-nil config for nil input")
	}

	// Default: AllowedHosts = ["*"]
	if len(config.AllowedHosts) != 1 || config.AllowedHosts[0] != "*" {
		t.Error("Default should have AllowedHosts = [\"*\"]")
	}
}

func TestSecurityConfigFromGlobal_EmptyAllowedHosts(t *testing.T) {
	global := &WebhookGlobalConfig{
		AllowedHosts: "",
	}

	config := SecurityConfigFromGlobal(global)

	// Empty string defaults to "*"
	if len(config.AllowedHosts) != 1 || config.AllowedHosts[0] != "*" {
		t.Errorf("Empty AllowedHosts should default to [\"*\"], got %v", config.AllowedHosts)
	}
}

func TestSecurityConfigFromGlobal_ExplicitStar(t *testing.T) {
	global := &WebhookGlobalConfig{
		AllowedHosts: "*",
	}

	config := SecurityConfigFromGlobal(global)

	if len(config.AllowedHosts) != 1 || config.AllowedHosts[0] != "*" {
		t.Errorf("Expected [\"*\"], got %v", config.AllowedHosts)
	}
}

func TestSecurityConfigFromGlobal_SpecificHosts(t *testing.T) {
	global := &WebhookGlobalConfig{
		AllowedHosts: "192.168.1.20, ntfy.local, *.internal.example.com",
	}

	config := SecurityConfigFromGlobal(global)

	if len(config.AllowedHosts) != 3 {
		t.Errorf("Expected 3 allowed hosts, got %d", len(config.AllowedHosts))
	}

	expectedHosts := []string{"192.168.1.20", "ntfy.local", "*.internal.example.com"}
	for i, expected := range expectedHosts {
		if i >= len(config.AllowedHosts) {
			t.Errorf("Missing expected host: %s", expected)
			continue
		}
		if config.AllowedHosts[i] != expected {
			t.Errorf("Expected host %q, got %q", expected, config.AllowedHosts[i])
		}
	}
}

// Tests for SetGlobalSecurityConfig

func TestSetGlobalSecurityConfig_SetsValidator(t *testing.T) {
	// Save original functions
	originalValidator := ValidateWebhookURL
	originalTransport := TransportFactory
	defer func() {
		ValidateWebhookURL = originalValidator
		TransportFactory = originalTransport
	}()

	// Set whitelist config
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"hooks.slack.com"},
	}

	SetGlobalSecurityConfig(config)

	// Test that only whitelisted host is allowed
	err := ValidateWebhookURL("https://hooks.slack.com/webhook")
	if err != nil {
		t.Errorf("Expected whitelisted host to be allowed, got error: %v", err)
	}

	// Test that other hosts are blocked
	err = ValidateWebhookURL("http://192.168.1.20/webhook")
	if err == nil {
		t.Error("Expected non-whitelisted host to be blocked in whitelist mode")
	}
}

func TestSetGlobalSecurityConfig_NilResetsToDefault(t *testing.T) {
	// Save original functions
	originalValidator := ValidateWebhookURL
	originalTransport := TransportFactory
	defer func() {
		ValidateWebhookURL = originalValidator
		TransportFactory = originalTransport
	}()

	// First set a restrictive config
	SetGlobalSecurityConfig(&WebhookSecurityConfig{AllowedHosts: []string{"hooks.slack.com"}})

	// Then reset to default
	SetGlobalSecurityConfig(nil)

	// Test that all hosts are now allowed
	err := ValidateWebhookURL("http://192.168.1.20/webhook")
	if err != nil {
		t.Errorf("Expected all hosts to be allowed after reset to default, got error: %v", err)
	}
}

// Tests for NewConfigurableTransport

func TestNewConfigurableTransport_Creation(t *testing.T) {
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"*"},
	}

	transport := NewConfigurableTransport(config)

	if transport == nil {
		t.Fatal("NewConfigurableTransport returned nil")
	}
}

func TestNewConfigurableTransport_NilConfigUsesDefaults(t *testing.T) {
	transport := NewConfigurableTransport(nil)

	if transport == nil {
		t.Fatal("NewConfigurableTransport should return non-nil transport for nil config")
	}
}

func TestSafeTransport_Creation(t *testing.T) {
	transport := NewSafeTransport()
	if transport == nil {
		t.Fatal("NewSafeTransport returned nil")
	}
}

// Whitelist Matching Tests

func TestSecurityValidator_WhitelistCaseInsensitive(t *testing.T) {
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"HOOKS.SLACK.COM"},
	}
	validator := NewWebhookSecurityValidator(config)

	// Should match case-insensitively
	err := validator.Validate("https://hooks.slack.com/webhook")
	if err != nil {
		t.Errorf("Expected case-insensitive match, got error: %v", err)
	}
}

func TestSecurityValidator_WildcardMatchesSuffix(t *testing.T) {
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"*.example.com"},
	}
	validator := NewWebhookSecurityValidator(config)

	// Should match subdomain
	err := validator.Validate("http://api.example.com/webhook")
	if err != nil {
		t.Errorf("Expected *.example.com to match api.example.com, got error: %v", err)
	}

	// Should match deeper subdomain
	err = validator.Validate("http://deep.nested.example.com/webhook")
	if err != nil {
		t.Errorf("Expected *.example.com to match deep.nested.example.com, got error: %v", err)
	}

	// Should NOT match just "example.com"
	err = validator.Validate("http://example.com/webhook")
	if err == nil {
		t.Error("Expected *.example.com to NOT match example.com (no subdomain)")
	}
}

func TestSecurityValidator_IPAddressWhitelist(t *testing.T) {
	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"192.168.1.20", "10.0.0.*"},
	}
	validator := NewWebhookSecurityValidator(config)

	// Exact IP match
	err := validator.Validate("http://192.168.1.20/webhook")
	if err != nil {
		t.Errorf("Expected exact IP match, got error: %v", err)
	}

	// Different IP blocked
	err = validator.Validate("http://192.168.1.21/webhook")
	if err == nil {
		t.Error("Expected non-whitelisted IP to be blocked")
	}
}
