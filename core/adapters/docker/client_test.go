package docker_test

import (
	"os"
	"testing"
	"time"

	dockeradapter "github.com/netresearch/ofelia/core/adapters/docker"
	"github.com/netresearch/ofelia/core/ports"
)

// isCI returns true if running in a CI environment.
// In CI, tests must not skip - they must pass or fail.
func isCI() bool {
	// GitHub Actions sets CI=true and GITHUB_ACTIONS=true
	// Most CI systems set CI=true
	return os.Getenv("CI") == "true" || os.Getenv("GITHUB_ACTIONS") == "true"
}

// skipOrFailDockerUnavailable either skips (locally) or fails (CI) when Docker is unavailable.
func skipOrFailDockerUnavailable(t *testing.T, err error) {
	t.Helper()
	if isCI() {
		t.Fatalf("Docker must be available in CI - test cannot run: %v", err)
	}
	t.Skipf("Skipping test - Docker not available (run in CI to ensure this test runs): %v", err)
}

// TestClientImplementsInterface verifies the Docker client implements the interface.
func TestClientImplementsInterface(t *testing.T) {
	// This is a compile-time check
	var _ ports.DockerClient = (*dockeradapter.Client)(nil)
}

func TestDefaultConfig(t *testing.T) {
	config := dockeradapter.DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	// Verify default values
	if config.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns = %d, want 100", config.MaxIdleConns)
	}
	if config.MaxIdleConnsPerHost != 50 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 50", config.MaxIdleConnsPerHost)
	}
	if config.MaxConnsPerHost != 100 {
		t.Errorf("MaxConnsPerHost = %d, want 100", config.MaxConnsPerHost)
	}
	if config.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s", config.IdleConnTimeout)
	}
	if config.DialTimeout != 30*time.Second {
		t.Errorf("DialTimeout = %v, want 30s", config.DialTimeout)
	}
	if config.ResponseHeaderTimeout != 120*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 120s", config.ResponseHeaderTimeout)
	}
}

func TestClientConfigCustomValues(t *testing.T) {
	config := &dockeradapter.ClientConfig{
		Host:                  "unix:///custom/docker.sock",
		Version:               "1.43",
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   25,
		MaxConnsPerHost:       50,
		IdleConnTimeout:       60 * time.Second,
		DialTimeout:           15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}

	if config.Host != "unix:///custom/docker.sock" {
		t.Errorf("Host = %v, want unix:///custom/docker.sock", config.Host)
	}
	if config.Version != "1.43" {
		t.Errorf("Version = %v, want 1.43", config.Version)
	}
	if config.MaxIdleConns != 50 {
		t.Errorf("MaxIdleConns = %d, want 50", config.MaxIdleConns)
	}
}

// TestNewClientWithConfig_UnixSocket tests client creation with Unix socket config.
// Note: This test will fail if Docker is not running, which is expected in CI without Docker.
func TestNewClientWithConfig_UnixSocket(t *testing.T) {
	config := dockeradapter.DefaultConfig()
	config.Host = "unix:///var/run/docker.sock"

	// Try to create client - may fail without Docker
	client, err := dockeradapter.NewClientWithConfig(config)
	if err != nil {
		skipOrFailDockerUnavailable(t, err)
	}
	defer client.Close()

	// Verify all services are available
	if client.Containers() == nil {
		t.Error("Containers() returned nil")
	}
	if client.Exec() == nil {
		t.Error("Exec() returned nil")
	}
	if client.Images() == nil {
		t.Error("Images() returned nil")
	}
	if client.Events() == nil {
		t.Error("Events() returned nil")
	}
	if client.Services() == nil {
		t.Error("Services() returned nil")
	}
	if client.Networks() == nil {
		t.Error("Networks() returned nil")
	}
	if client.System() == nil {
		t.Error("System() returned nil")
	}
}

// TestNewClient tests the default client creation.
func TestNewClient(t *testing.T) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		skipOrFailDockerUnavailable(t, err)
	}
	defer client.Close()

	// Verify SDK client is accessible
	if client.SDK() == nil {
		t.Error("SDK() returned nil")
	}
}

// TestClientClose tests the close functionality.
func TestClientClose(t *testing.T) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		skipOrFailDockerUnavailable(t, err)
	}

	err = client.Close()
	if err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}
}

// TestClientWithHTTPSHost tests HTTPS host configuration.
func TestClientConfigHTTPSHost(t *testing.T) {
	config := dockeradapter.DefaultConfig()
	config.Host = "https://docker.example.com:2376"

	// We can't actually connect, but we can verify configuration is accepted
	if config.Host != "https://docker.example.com:2376" {
		t.Errorf("Host = %v, want https://docker.example.com:2376", config.Host)
	}
}

// TestClientConfigTCPHost tests TCP host configuration.
func TestClientConfigTCPHost(t *testing.T) {
	config := dockeradapter.DefaultConfig()
	config.Host = "tcp://localhost:2375"

	if config.Host != "tcp://localhost:2375" {
		t.Errorf("Host = %v, want tcp://localhost:2375", config.Host)
	}
}

// TestClientConfigHTTPHeaders tests custom HTTP headers.
func TestClientConfigHTTPHeaders(t *testing.T) {
	config := dockeradapter.DefaultConfig()
	config.HTTPHeaders = map[string]string{
		"X-Custom-Header": "custom-value",
		"Authorization":   "Bearer token",
	}

	if config.HTTPHeaders["X-Custom-Header"] != "custom-value" {
		t.Errorf("HTTPHeaders[X-Custom-Header] = %v, want custom-value", config.HTTPHeaders["X-Custom-Header"])
	}
	if config.HTTPHeaders["Authorization"] != "Bearer token" {
		t.Errorf("HTTPHeaders[Authorization] = %v, want Bearer token", config.HTTPHeaders["Authorization"])
	}
}
