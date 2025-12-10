package docker

import (
	"fmt"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"

	"github.com/netresearch/ofelia/core/domain"
)

// ConfigAuthProvider implements ports.AuthProvider using Docker's config.json.
// It reads credentials fresh on each call to support dynamic credential updates
// (e.g., short-lived tokens from AWS ECR, GCR).
type ConfigAuthProvider struct {
	// configDir overrides the default Docker config directory (for testing)
	configDir string
	// logger for debug/warning messages (optional)
	logger Logger
}

// Logger interface for auth provider logging
type Logger interface {
	Debugf(format string, args ...interface{})
	Warningf(format string, args ...interface{})
}

// NewConfigAuthProvider creates a new auth provider.
func NewConfigAuthProvider() *ConfigAuthProvider {
	return &ConfigAuthProvider{}
}

// NewConfigAuthProviderWithOptions creates an auth provider with options.
func NewConfigAuthProviderWithOptions(configDir string, logger Logger) *ConfigAuthProvider {
	return &ConfigAuthProvider{
		configDir: configDir,
		logger:    logger,
	}
}

// GetAuthConfig returns auth configuration for a registry.
func (p *ConfigAuthProvider) GetAuthConfig(registry string) (domain.AuthConfig, error) {
	// Load config fresh each time (no caching) to support credential rotation
	cfg, err := p.loadConfig()
	if err != nil {
		p.logWarning("Failed to load Docker config: %v", err)
		return domain.AuthConfig{}, nil // Graceful fallback for public images
	}

	// Normalize registry address
	registry = normalizeRegistry(registry)

	// Get auth from config (supports credential helpers)
	authConfig, err := cfg.GetAuthConfig(registry)
	if err != nil {
		p.logWarning("Failed to get auth for registry %q: %v", registry, err)
		return domain.AuthConfig{}, nil // Graceful fallback
	}

	// Log if we found credentials
	if authConfig.Username != "" || authConfig.IdentityToken != "" {
		p.logDebug("Found credentials for registry %q", registry)
	}

	return convertAuthConfig(authConfig), nil
}

// GetEncodedAuth returns base64-encoded auth for a registry.
func (p *ConfigAuthProvider) GetEncodedAuth(registry string) (string, error) {
	auth, err := p.GetAuthConfig(registry)
	if err != nil {
		return "", err
	}

	// Empty auth is valid (public registry)
	if auth.Username == "" && auth.Password == "" && auth.IdentityToken == "" && auth.Auth == "" {
		return "", nil
	}

	return EncodeAuthConfig(auth)
}

func (p *ConfigAuthProvider) loadConfig() (*configfile.ConfigFile, error) {
	var cfg *configfile.ConfigFile
	var err error
	if p.configDir != "" {
		cfg, err = config.Load(p.configDir)
	} else {
		cfg, err = config.Load(config.Dir())
	}
	if err != nil {
		return nil, fmt.Errorf("loading docker config: %w", err)
	}
	return cfg, nil
}

func (p *ConfigAuthProvider) logDebug(format string, args ...interface{}) {
	if p.logger != nil {
		p.logger.Debugf(format, args...)
	}
}

func (p *ConfigAuthProvider) logWarning(format string, args ...interface{}) {
	if p.logger != nil {
		p.logger.Warningf(format, args...)
	}
}

// normalizeRegistry normalizes a registry address for credential lookup.
func normalizeRegistry(registry string) string {
	// Docker Hub special cases
	if registry == "" || registry == "docker.io" || registry == "index.docker.io" {
		return "https://index.docker.io/v1/"
	}
	return registry
}

// convertAuthConfig converts Docker CLI types to domain types.
func convertAuthConfig(src types.AuthConfig) domain.AuthConfig {
	return domain.AuthConfig{
		Username:      src.Username,
		Password:      src.Password,
		Auth:          src.Auth,
		ServerAddress: src.ServerAddress,
		IdentityToken: src.IdentityToken,
		RegistryToken: src.RegistryToken,
	}
}

// ExtractRegistry extracts the registry hostname from an image reference.
// Uses the canonical docker/distribution/reference parser for robustness.
func ExtractRegistry(image string) string {
	// Use canonical reference parsing
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		// Fallback to Docker Hub for unparseable images
		return "docker.io"
	}

	// reference.Domain returns the registry domain
	return reference.Domain(named)
}
