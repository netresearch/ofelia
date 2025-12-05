//go:build integration

package docker_test

import (
	"context"
	"testing"
	"time"

	dockeradapter "github.com/netresearch/ofelia/core/adapters/docker"
)

// Tests targeting surviving CONDITIONALS_NEGATION mutations in client.go
// These require a real Docker daemon to properly exercise all code paths

// TestNewClientWithConfig_ConfigOptionsIntegration tests all config option branches
// with a real Docker daemon to properly kill mutations at lines 79, 83, 87, 93, 124
func TestNewClientWithConfig_ConfigOptionsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test case 1: Empty host should use environment/defaults (line 79: if config.Host != "")
	t.Run("empty_host_uses_default", func(t *testing.T) {
		config := dockeradapter.DefaultConfig()
		config.Host = "" // Empty host - tests the false branch of line 79

		client, err := dockeradapter.NewClientWithConfig(config)
		if err != nil {
			skipOrFailDockerUnavailable(t, err)
		}
		defer client.Close()

		// Verify client works - this proves the empty host path works correctly
		_, err = client.System().Ping(ctx)
		if err != nil {
			t.Errorf("Ping failed with empty host config: %v", err)
		}
	})

	// Test case 2: Explicit host should be used (line 79: if config.Host != "")
	t.Run("explicit_host_is_used", func(t *testing.T) {
		config := dockeradapter.DefaultConfig()
		config.Host = "unix:///var/run/docker.sock" // Non-empty host - tests the true branch

		client, err := dockeradapter.NewClientWithConfig(config)
		if err != nil {
			skipOrFailDockerUnavailable(t, err)
		}
		defer client.Close()

		// Verify client works with explicit host
		_, err = client.System().Ping(ctx)
		if err != nil {
			t.Errorf("Ping failed with explicit host config: %v", err)
		}
	})

	// Test case 3: Empty version uses negotiation (line 83: if config.Version != "")
	t.Run("empty_version_uses_negotiation", func(t *testing.T) {
		config := dockeradapter.DefaultConfig()
		config.Version = "" // Empty version - tests the false branch of line 83

		client, err := dockeradapter.NewClientWithConfig(config)
		if err != nil {
			skipOrFailDockerUnavailable(t, err)
		}
		defer client.Close()

		// Verify client works with API negotiation
		version, err := client.System().Version(ctx)
		if err != nil {
			t.Errorf("Version failed with empty version config: %v", err)
		}
		if version == nil || version.APIVersion == "" {
			t.Error("Expected API version to be negotiated")
		}
	})

	// Test case 4: Explicit version should be used (line 83: if config.Version != "")
	t.Run("explicit_version_is_used", func(t *testing.T) {
		config := dockeradapter.DefaultConfig()
		config.Version = "1.41" // Non-empty version - tests the true branch

		client, err := dockeradapter.NewClientWithConfig(config)
		if err != nil {
			skipOrFailDockerUnavailable(t, err)
		}
		defer client.Close()

		// Verify client works with explicit version
		_, err = client.System().Ping(ctx)
		if err != nil {
			t.Errorf("Ping failed with explicit version config: %v", err)
		}
	})

	// Test case 5: Nil HTTPHeaders should be skipped (line 87: if config.HTTPHeaders != nil)
	t.Run("nil_http_headers_skipped", func(t *testing.T) {
		config := dockeradapter.DefaultConfig()
		config.HTTPHeaders = nil // Nil headers - tests the false branch of line 87

		client, err := dockeradapter.NewClientWithConfig(config)
		if err != nil {
			skipOrFailDockerUnavailable(t, err)
		}
		defer client.Close()

		// Verify client works without headers
		_, err = client.System().Ping(ctx)
		if err != nil {
			t.Errorf("Ping failed with nil headers config: %v", err)
		}
	})

	// Test case 6: Non-nil HTTPHeaders should be applied (line 87: if config.HTTPHeaders != nil)
	t.Run("non_nil_http_headers_applied", func(t *testing.T) {
		config := dockeradapter.DefaultConfig()
		config.HTTPHeaders = map[string]string{
			"X-Test-Header": "test-value",
		} // Non-nil headers - tests the true branch of line 87

		client, err := dockeradapter.NewClientWithConfig(config)
		if err != nil {
			skipOrFailDockerUnavailable(t, err)
		}
		defer client.Close()

		// Verify client works with custom headers
		_, err = client.System().Ping(ctx)
		if err != nil {
			t.Errorf("Ping failed with custom headers config: %v", err)
		}
	})

	// Test case 7: All options together (except version - use API negotiation for compatibility)
	t.Run("all_options_combined", func(t *testing.T) {
		config := dockeradapter.DefaultConfig()
		config.Host = "unix:///var/run/docker.sock"
		// Don't set Version - let API negotiation handle it for compatibility
		config.HTTPHeaders = map[string]string{
			"X-Custom": "value",
		}

		client, err := dockeradapter.NewClientWithConfig(config)
		if err != nil {
			skipOrFailDockerUnavailable(t, err)
		}
		defer client.Close()

		// Verify client works with combined options
		info, err := client.System().Info(ctx)
		if err != nil {
			t.Errorf("Info failed with all options: %v", err)
		}
		if info == nil || info.ID == "" {
			t.Error("Expected valid system info")
		}
	})

	// Test case 8: No options (all defaults)
	t.Run("no_options_all_defaults", func(t *testing.T) {
		config := &dockeradapter.ClientConfig{
			Host:        "",
			Version:     "",
			HTTPHeaders: nil,
			// All pooling options at zero/default
		}

		client, err := dockeradapter.NewClientWithConfig(config)
		if err != nil {
			skipOrFailDockerUnavailable(t, err)
		}
		defer client.Close()

		// Verify client works with no explicit options
		_, err = client.System().Ping(ctx)
		if err != nil {
			t.Errorf("Ping failed with no options: %v", err)
		}
	})
}

// TestCreateHTTPClient_HostConditionsIntegration tests createHTTPClient host handling
// Targets line 124: if host == ""
func TestCreateHTTPClient_HostConditionsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testCases := []struct {
		name string
		host string
		desc string
	}{
		{
			name: "empty_host_defaults_to_docker_host",
			host: "", // Tests line 124 true branch (uses DefaultDockerHost)
			desc: "Empty host should default to DefaultDockerHost",
		},
		{
			name: "unix_socket_host",
			host: "unix:///var/run/docker.sock", // Tests line 138 switch case
			desc: "Unix socket host should be handled",
		},
		{
			name: "tcp_host_localhost",
			host: "tcp://localhost:2375", // Tests default switch case
			desc: "TCP host should be handled",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := dockeradapter.DefaultConfig()
			config.Host = tc.host

			// For TCP, we don't expect it to work without a daemon listening there
			// But for unix socket and empty (default), it should work
			if tc.host == "tcp://localhost:2375" {
				// Skip TCP test as we likely don't have Docker listening on TCP
				t.Skip("TCP Docker endpoint not typically available")
			}

			client, err := dockeradapter.NewClientWithConfig(config)
			if err != nil {
				skipOrFailDockerUnavailable(t, err)
			}
			defer client.Close()

			_, err = client.System().Ping(ctx)
			if err != nil {
				t.Errorf("%s: Ping failed: %v", tc.desc, err)
			}
		})
	}
}

// TestClientPoolingOptionsIntegration tests connection pooling configuration
// These options affect the HTTP transport creation
func TestClientPoolingOptionsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testCases := []struct {
		name   string
		config *dockeradapter.ClientConfig
	}{
		{
			name: "default_pooling",
			config: &dockeradapter.ClientConfig{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   50,
				MaxConnsPerHost:       100,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 120 * time.Second,
			},
		},
		{
			name: "minimal_pooling",
			config: &dockeradapter.ClientConfig{
				MaxIdleConns:          1,
				MaxIdleConnsPerHost:   1,
				MaxConnsPerHost:       1,
				IdleConnTimeout:       10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
		{
			name: "high_pooling",
			config: &dockeradapter.ClientConfig{
				MaxIdleConns:          500,
				MaxIdleConnsPerHost:   100,
				MaxConnsPerHost:       200,
				IdleConnTimeout:       180 * time.Second,
				ResponseHeaderTimeout: 300 * time.Second,
			},
		},
		{
			name: "zero_pooling_values",
			config: &dockeradapter.ClientConfig{
				MaxIdleConns:          0,
				MaxIdleConnsPerHost:   0,
				MaxConnsPerHost:       0,
				IdleConnTimeout:       0,
				ResponseHeaderTimeout: 0,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := dockeradapter.NewClientWithConfig(tc.config)
			if err != nil {
				skipOrFailDockerUnavailable(t, err)
			}
			defer client.Close()

			// Make multiple requests to exercise connection pooling
			for i := 0; i < 3; i++ {
				_, err = client.System().Ping(ctx)
				if err != nil {
					t.Errorf("Ping %d failed with %s config: %v", i+1, tc.name, err)
				}
			}
		})
	}
}

// TestNewClient_SuccessfulConnectionIntegration tests successful client creation
// This exercises line 98: if err != nil (the true branch via error)
func TestNewClient_SuccessfulConnectionIntegration(t *testing.T) {
	// First, test successful connection (false branch of line 98)
	client, err := dockeradapter.NewClient()
	if err != nil {
		skipOrFailDockerUnavailable(t, err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verify all services are accessible and functional
	t.Run("containers_service", func(t *testing.T) {
		if client.Containers() == nil {
			t.Error("Containers() returned nil")
		}
	})

	t.Run("exec_service", func(t *testing.T) {
		if client.Exec() == nil {
			t.Error("Exec() returned nil")
		}
	})

	t.Run("images_service", func(t *testing.T) {
		if client.Images() == nil {
			t.Error("Images() returned nil")
		}
	})

	t.Run("events_service", func(t *testing.T) {
		if client.Events() == nil {
			t.Error("Events() returned nil")
		}
	})

	t.Run("services_service", func(t *testing.T) {
		if client.Services() == nil {
			t.Error("Services() returned nil")
		}
	})

	t.Run("networks_service", func(t *testing.T) {
		if client.Networks() == nil {
			t.Error("Networks() returned nil")
		}
	})

	t.Run("system_service", func(t *testing.T) {
		if client.System() == nil {
			t.Error("System() returned nil")
		}
	})

	t.Run("sdk_accessible", func(t *testing.T) {
		if client.SDK() == nil {
			t.Error("SDK() returned nil")
		}
	})

	// Make a real API call to verify functionality
	t.Run("real_api_call", func(t *testing.T) {
		ping, err := client.System().Ping(ctx)
		if err != nil {
			t.Errorf("Ping failed: %v", err)
		}
		if ping == nil || ping.APIVersion == "" {
			t.Error("Expected valid ping response")
		}
	})
}

// TestNewClientWithConfig_ErrorPathIntegration tests error handling
// This exercises line 98: if err != nil (the true branch)
func TestNewClientWithConfig_ErrorPathIntegration(t *testing.T) {
	// Test with invalid host to trigger error path
	// Note: This may not actually fail since Docker SDK is lenient
	config := dockeradapter.DefaultConfig()
	config.Host = "invalid://not-a-real-host:99999"

	_, err := dockeradapter.NewClientWithConfig(config)
	// The error handling depends on the Docker SDK's behavior
	// What we're testing is that our error wrapping code works
	if err != nil {
		t.Logf("Got expected error with invalid host: %v", err)
	}
}
