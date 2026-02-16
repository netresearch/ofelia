package docker

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRegistry(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		expected string
	}{
		// Docker Hub defaults
		{"simple image", "alpine", "docker.io"},
		{"image with tag", "nginx:latest", "docker.io"},
		{"library image", "library/ubuntu", "docker.io"},
		{"user/repo", "myuser/myimage", "docker.io"},
		{"user/repo with tag", "myuser/myimage:v1.0", "docker.io"},

		// Private registries
		{"gcr.io", "gcr.io/project/image:tag", "gcr.io"},
		{"quay.io", "quay.io/org/image", "quay.io"},
		{"ghcr.io", "ghcr.io/owner/image:latest", "ghcr.io"},
		{"ecr", "123456789.dkr.ecr.us-east-1.amazonaws.com/myimage", "123456789.dkr.ecr.us-east-1.amazonaws.com"},

		// Registry with port
		{"localhost with port", "localhost:5000/myimage", "localhost:5000"},
		{"IP with port", "192.168.1.1:5000/image", "192.168.1.1:5000"},
		{"custom registry with port", "registry.example.com:8080/org/image:tag", "registry.example.com:8080"},

		// Custom domain registries
		{"custom domain", "registry.example.com/org/image", "registry.example.com"},
		{"subdomain registry", "docker.my-company.com/project/image:v2", "docker.my-company.com"},

		// Digests - note: reference.ParseNormalizedNamed may not handle
		// all digest formats, especially with incomplete sha256
		{"image with digest", "alpine@sha256:abc123def456", "docker.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractRegistry(tt.image)
			if got != tt.expected {
				t.Errorf("ExtractRegistry(%q) = %q, want %q", tt.image, got, tt.expected)
			}
		})
	}
}

func TestNormalizeRegistry(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "https://index.docker.io/v1/"},
		{"docker.io", "https://index.docker.io/v1/"},
		{"index.docker.io", "https://index.docker.io/v1/"},
		{"gcr.io", "gcr.io"},
		{"quay.io", "quay.io"},
		{"localhost:5000", "localhost:5000"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeRegistry(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeRegistry(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestConfigAuthProvider_GetAuthConfig_MissingConfig(t *testing.T) {
	// Create provider with non-existent config dir
	provider := NewConfigAuthProviderWithOptions("/nonexistent/path/12345", nil)

	// Should return empty auth (graceful fallback), not error
	auth, err := provider.GetAuthConfig("gcr.io")
	if err != nil {
		t.Errorf("GetAuthConfig() error = %v, want nil (graceful fallback)", err)
	}
	if auth.Username != "" || auth.Password != "" {
		t.Errorf("GetAuthConfig() = %+v, want empty auth", auth)
	}
}

func TestConfigAuthProvider_GetEncodedAuth_Empty(t *testing.T) {
	// Create provider with non-existent config dir
	provider := NewConfigAuthProviderWithOptions("/nonexistent/path/12345", nil)

	// Should return empty string (no auth needed)
	encoded, err := provider.GetEncodedAuth("docker.io")
	if err != nil {
		t.Errorf("GetEncodedAuth() error = %v, want nil", err)
	}
	if encoded != "" {
		t.Errorf("GetEncodedAuth() = %q, want empty string", encoded)
	}
}

func TestConfigAuthProvider_GetAuthConfig_ValidConfig(t *testing.T) {
	// Create temp dir with mock config.json
	tmpDir := t.TempDir()

	// Write mock config.json with basic auth
	configJSON := `{
		"auths": {
			"https://index.docker.io/v1/": {
				"auth": "dXNlcm5hbWU6cGFzc3dvcmQ="
			},
			"gcr.io": {
				"username": "oauth2accesstoken",
				"password": "ya29.token123"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatalf("Failed to write config.json: %v", err)
	}

	provider := NewConfigAuthProviderWithOptions(tmpDir, nil)

	// Test Docker Hub auth (base64 auth gets decoded to username/password by config library)
	auth, err := provider.GetAuthConfig("docker.io")
	if err != nil {
		t.Errorf("GetAuthConfig(docker.io) error = %v", err)
	}
	// The docker/cli config library decodes base64 auth into username/password
	if auth.Username != "username" {
		t.Errorf("GetAuthConfig(docker.io).Username = %q, want username", auth.Username)
	}
	if auth.Password != "password" {
		t.Errorf("GetAuthConfig(docker.io).Password = %q, want password", auth.Password)
	}

	// Test GCR auth
	auth, err = provider.GetAuthConfig("gcr.io")
	if err != nil {
		t.Errorf("GetAuthConfig(gcr.io) error = %v", err)
	}
	if auth.Username != "oauth2accesstoken" {
		t.Errorf("GetAuthConfig(gcr.io).Username = %q, want oauth2accesstoken", auth.Username)
	}
	if auth.Password != "ya29.token123" {
		t.Errorf("GetAuthConfig(gcr.io).Password = %q, want ya29.token123", auth.Password)
	}

	// Test unknown registry (should return empty)
	auth, err = provider.GetAuthConfig("unknown.registry.io")
	if err != nil {
		t.Errorf("GetAuthConfig(unknown) error = %v", err)
	}
	if auth.Username != "" || auth.Password != "" || auth.Auth != "" {
		t.Errorf("GetAuthConfig(unknown) = %+v, want empty", auth)
	}
}

func TestConfigAuthProvider_GetEncodedAuth_ValidConfig(t *testing.T) {
	// Create temp dir with mock config.json
	tmpDir := t.TempDir()

	// Write mock config.json
	configJSON := `{
		"auths": {
			"gcr.io": {
				"username": "testuser",
				"password": "testpass"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatalf("Failed to write config.json: %v", err)
	}

	provider := NewConfigAuthProviderWithOptions(tmpDir, nil)

	// Should return base64-encoded auth
	encoded, err := provider.GetEncodedAuth("gcr.io")
	if err != nil {
		t.Errorf("GetEncodedAuth() error = %v", err)
	}
	if encoded == "" {
		t.Error("GetEncodedAuth() returned empty, want encoded auth")
	}
}

// authTestHandler is a slog.Handler that captures log records for testing.
type authTestHandler struct {
	records []authTestRecord
}

type authTestRecord struct {
	level   slog.Level
	message string
}

func (h *authTestHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *authTestHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, authTestRecord{level: r.Level, message: r.Message})
	return nil
}

func (h *authTestHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *authTestHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *authTestHandler) hasDebugContaining(substr string) bool { //nolint:unused // kept for future test assertions
	for _, r := range h.records {
		if r.level == slog.LevelDebug && strings.Contains(r.message, substr) {
			return true
		}
	}
	return false
}

func TestConfigAuthProvider_Logging(t *testing.T) {
	handler := &authTestHandler{}
	logger := slog.New(handler)

	// Test with valid config dir - should log debug message when credentials found
	tmpDir := t.TempDir()

	// Write mock config.json with auth
	configJSON := `{
		"auths": {
			"gcr.io": {
				"username": "testuser",
				"password": "testpass"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatalf("Failed to write config.json: %v", err)
	}

	provider := NewConfigAuthProviderWithOptions(tmpDir, logger)
	_, _ = provider.GetAuthConfig("gcr.io")

	hasDebug := false
	for _, r := range handler.records {
		if r.level == slog.LevelDebug {
			hasDebug = true
			break
		}
	}
	if !hasDebug {
		t.Error("Expected debug message for found credentials, got none")
	}
}

func TestNewConfigAuthProvider(t *testing.T) {
	provider := NewConfigAuthProvider()
	if provider == nil {
		t.Error("NewConfigAuthProvider() returned nil")
	}
	if provider.configDir != "" {
		t.Errorf("NewConfigAuthProvider().configDir = %q, want empty", provider.configDir)
	}
}

func TestNewConfigAuthProviderWithOptions(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	provider := NewConfigAuthProviderWithOptions("/custom/path", logger)

	if provider.configDir != "/custom/path" {
		t.Errorf("configDir = %q, want /custom/path", provider.configDir)
	}
	if provider.logger != logger {
		t.Error("logger not set correctly")
	}
}
