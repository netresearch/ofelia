package middlewares

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateWebhookURL_RequiresHTTP(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("ftp://example.com/file")
	require.Error(t, err)
	assert.Regexp(t, ".*http or https.*", err.Error())
}

func TestValidateWebhookURL_RequiresHost(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("http:///path")
	require.Error(t, err)
	assert.Regexp(t, ".*host.*", err.Error())
}

func TestValidateWebhookURL_AllowsHTTP(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("http://example.com/webhook")
	require.NoError(t, err)
}

func TestValidateWebhookURL_AllowsHTTPS(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("https://example.com/webhook")
	require.NoError(t, err)
}

func TestValidateWebhookURL_AllowsLocalhost(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("http://localhost/webhook")
	require.NoError(t, err)
}

func TestValidateWebhookURL_Allows127001(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("http://127.0.0.1/webhook")
	require.NoError(t, err)
}

func TestValidateWebhookURL_AllowsPrivateClassA(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("http://10.0.0.1/webhook")
	require.NoError(t, err)
}

func TestValidateWebhookURL_AllowsPrivateClassB(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("http://172.16.0.1/webhook")
	require.NoError(t, err)
}

func TestValidateWebhookURL_AllowsPrivateClassC(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("http://192.168.1.1/webhook")
	require.NoError(t, err)
}

func TestValidateWebhookURL_AllowsInternalHostname(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("http://ntfy.local/webhook")
	require.NoError(t, err)
}

func TestValidateWebhookURL_AllowsPublicHTTPS(t *testing.T) {
	t.Parallel()

	err := ValidateWebhookURLImpl("https://hooks.slack.com/services/T123/B456/secret")
	require.NoError(t, err)
}

func TestSecurityValidator_DefaultConfig(t *testing.T) {
	t.Parallel()

	validator := NewWebhookSecurityValidator(nil)
	assert.NotNil(t, validator)
	assert.Len(t, validator.config.AllowedHosts, 1)
	assert.Equal(t, "*", validator.config.AllowedHosts[0])
}

func TestSecurityValidator_DefaultAllowsAll(t *testing.T) {
	t.Parallel()

	validator := NewWebhookSecurityValidator(nil)

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
		require.NoError(t, err, "Expected URL %s to be allowed", url)
	}
}

func TestSecurityValidator_WhitelistMode(t *testing.T) {
	t.Parallel()

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"hooks.slack.com", "ntfy.local"},
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("https://hooks.slack.com/webhook")
	require.NoError(t, err)

	err = validator.Validate("http://ntfy.local/webhook")
	require.NoError(t, err)

	err = validator.Validate("http://192.168.1.1/webhook")
	require.Error(t, err)
	assert.Regexp(t, ".*not in allowed hosts.*", err.Error())
}

func TestSecurityValidator_WhitelistWildcard(t *testing.T) {
	t.Parallel()

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"*.slack.com", "*.internal.example.com"},
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("https://hooks.slack.com/webhook")
	require.NoError(t, err)

	err = validator.Validate("http://ntfy.internal.example.com/webhook")
	require.NoError(t, err)

	err = validator.Validate("https://discord.com/webhook")
	require.Error(t, err)
}

func TestSecurityValidator_ExplicitAllowAll(t *testing.T) {
	t.Parallel()

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"*"},
	}
	validator := NewWebhookSecurityValidator(config)

	testURLs := []string{
		"http://localhost/webhook",
		"http://192.168.1.1/webhook",
		"https://hooks.slack.com/webhook",
	}

	for _, url := range testURLs {
		err := validator.Validate(url)
		require.NoError(t, err, "Expected URL %s to be allowed with explicit *", url)
	}
}

func TestSecurityValidator_MixedWithWildcard(t *testing.T) {
	t.Parallel()

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"hooks.slack.com", "*"},
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("http://any-host.example.com/webhook")
	require.NoError(t, err)
}

func TestSecurityConfig_Default(t *testing.T) {
	t.Parallel()

	config := DefaultWebhookSecurityConfig()

	assert.Len(t, config.AllowedHosts, 1)
	assert.Equal(t, "*", config.AllowedHosts[0])
}

func TestURLValidation_AllowsAllHostsByDefault(t *testing.T) {
	t.Parallel()

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
		require.NoError(t, err, "Expected URL %s to be allowed", url)
	}
}

func TestURLValidation_BlocksInvalidSchemes(t *testing.T) {
	t.Parallel()

	invalidURLs := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://example.com/",
		"javascript:alert(1)",
	}

	for _, url := range invalidURLs {
		err := ValidateWebhookURLImpl(url)
		require.Error(t, err, "Expected URL %s to be blocked (invalid scheme)", url)
	}
}

func TestURLValidation_RequiresHostname(t *testing.T) {
	t.Parallel()

	invalidURLs := []string{
		"http:///path",
		"https://",
	}

	for _, url := range invalidURLs {
		err := ValidateWebhookURLImpl(url)
		require.Error(t, err, "Expected URL %s to be blocked (no hostname)", url)
	}
}

func TestSecurityConfigFromGlobal_NilConfig(t *testing.T) {
	t.Parallel()

	config := SecurityConfigFromGlobal(nil)

	require.NotNil(t, config)
	assert.Len(t, config.AllowedHosts, 1)
	assert.Equal(t, "*", config.AllowedHosts[0])
}

func TestSecurityConfigFromGlobal_EmptyAllowedHosts(t *testing.T) {
	t.Parallel()

	global := &WebhookGlobalConfig{
		AllowedHosts: "",
	}

	config := SecurityConfigFromGlobal(global)

	assert.Len(t, config.AllowedHosts, 1)
	assert.Equal(t, "*", config.AllowedHosts[0])
}

func TestSecurityConfigFromGlobal_ExplicitStar(t *testing.T) {
	t.Parallel()

	global := &WebhookGlobalConfig{
		AllowedHosts: "*",
	}

	config := SecurityConfigFromGlobal(global)

	assert.Len(t, config.AllowedHosts, 1)
	assert.Equal(t, "*", config.AllowedHosts[0])
}

func TestSecurityValidator_EmptyAllowedHostsDefaultsToAll(t *testing.T) {
	t.Parallel()

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{},
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("http://any-host.example.com/webhook")
	require.NoError(t, err)

	err = validator.Validate("http://192.168.1.1/webhook")
	require.NoError(t, err)
}

func TestSecurityConfigFromGlobal_SpecificHosts(t *testing.T) {
	t.Parallel()

	global := &WebhookGlobalConfig{
		AllowedHosts: "192.168.1.20, ntfy.local, *.internal.example.com",
	}

	config := SecurityConfigFromGlobal(global)

	assert.Len(t, config.AllowedHosts, 3)
	expectedHosts := []string{"192.168.1.20", "ntfy.local", "*.internal.example.com"}
	for i, expected := range expectedHosts {
		assert.Equal(t, expected, config.AllowedHosts[i])
	}
}

func TestSetGlobalSecurityConfig_SetsValidator(t *testing.T) {
	// Note: Not parallel - modifies global security config
	defer SetGlobalSecurityConfig(nil) // Reset to defaults

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"hooks.slack.com"},
	}

	SetGlobalSecurityConfig(config)

	err := ValidateWebhookURL("https://hooks.slack.com/webhook")
	require.NoError(t, err)

	err = ValidateWebhookURL("http://192.168.1.20/webhook")
	require.Error(t, err)
}

func TestSetGlobalSecurityConfig_NilResetsToDefault(t *testing.T) {
	// Note: Not parallel - modifies global security config
	defer SetGlobalSecurityConfig(nil) // Reset to defaults

	SetGlobalSecurityConfig(&WebhookSecurityConfig{AllowedHosts: []string{"hooks.slack.com"}})
	SetGlobalSecurityConfig(nil)

	err := ValidateWebhookURL("http://192.168.1.20/webhook")
	require.NoError(t, err)
}

func TestNewConfigurableTransport_Creation(t *testing.T) {
	t.Parallel()

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"*"},
	}

	transport := NewConfigurableTransport(config)
	assert.NotNil(t, transport)
}

func TestNewConfigurableTransport_NilConfigUsesDefaults(t *testing.T) {
	t.Parallel()

	transport := NewConfigurableTransport(nil)
	assert.NotNil(t, transport)
}

func TestSafeTransport_Creation(t *testing.T) {
	t.Parallel()

	transport := NewSafeTransport()
	assert.NotNil(t, transport)
}

func TestSecurityValidator_WhitelistCaseInsensitive(t *testing.T) {
	t.Parallel()

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"HOOKS.SLACK.COM"},
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("https://hooks.slack.com/webhook")
	require.NoError(t, err)
}

func TestSecurityValidator_WildcardMatchesSuffix(t *testing.T) {
	t.Parallel()

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"*.example.com"},
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("http://api.example.com/webhook")
	require.NoError(t, err)

	err = validator.Validate("http://deep.nested.example.com/webhook")
	require.NoError(t, err)

	err = validator.Validate("http://example.com/webhook")
	require.Error(t, err)
}

func TestSecurityValidator_IPAddressWhitelist(t *testing.T) {
	t.Parallel()

	config := &WebhookSecurityConfig{
		AllowedHosts: []string{"192.168.1.20", "10.0.0.1"},
	}
	validator := NewWebhookSecurityValidator(config)

	err := validator.Validate("http://192.168.1.20/webhook")
	require.NoError(t, err)

	err = validator.Validate("http://10.0.0.1/webhook")
	require.NoError(t, err)

	err = validator.Validate("http://192.168.1.21/webhook")
	require.Error(t, err)
}

// TestWebhookSecurityIPv6URLs verifies URL validation with IPv6 addresses.
// Go 1.26 is stricter about URL parsing — IPv6 must use bracket notation.
func TestWebhookSecurityIPv6URLs(t *testing.T) {
	t.Parallel()

	validIPv6URLs := []string{
		"http://[::1]:8080/webhook",
		"https://[::1]/webhook",
		"http://[2001:db8::1]:9090/hook",
		"https://[fe80::1%25eth0]:443/notify",
	}

	invalidIPv6URLs := []string{
		"http://::1/webhook",      // missing brackets — rejected by Go 1.26
		"http://::1:8080/webhook", // ambiguous colon — rejected by Go 1.26
	}

	// Valid IPv6 URLs should pass basic validation
	for _, u := range validIPv6URLs {
		err := ValidateWebhookURLImpl(u)
		require.NoError(t, err, "ValidateWebhookURLImpl(%q) should succeed", u)
	}

	// Invalid IPv6 URLs should fail
	for _, u := range invalidIPv6URLs {
		err := ValidateWebhookURLImpl(u)
		require.Error(t, err, "ValidateWebhookURLImpl(%q) should fail for malformed IPv6", u)
	}

	// IPv6 with whitelisted host — url.Hostname() strips brackets,
	// so AllowedHosts should contain bare addresses like "::1"
	validator := NewWebhookSecurityValidator(&WebhookSecurityConfig{
		AllowedHosts: []string{"::1", "2001:db8::1"},
	})

	err := validator.Validate("http://[::1]:8080/webhook")
	require.NoError(t, err, "Whitelisted IPv6 host should pass")

	err = validator.Validate("http://[2001:db8::99]:8080/webhook")
	assert.Error(t, err, "Non-whitelisted IPv6 host should fail")
}
