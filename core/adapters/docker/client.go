// Package docker provides an adapter for the official Docker SDK.
package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/client"

	"github.com/netresearch/ofelia/core/ports"
)

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
	}
}

// NewClient creates a new Docker client from environment variables.
func NewClient() (*Client, error) {
	return NewClientWithConfig(DefaultConfig())
}

// NewClientWithConfig creates a new Docker client with custom configuration.
func NewClientWithConfig(config *ClientConfig) (*Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	if config.Host != "" {
		opts = append(opts, client.WithHost(config.Host))
	}

	if config.Version != "" {
		opts = append(opts, client.WithVersion(config.Version))
	}

	if config.HTTPHeaders != nil {
		opts = append(opts, client.WithHTTPHeaders(config.HTTPHeaders))
	}

	// Create custom HTTP client with connection pooling
	httpClient := createHTTPClient(config)
	if httpClient != nil {
		opts = append(opts, client.WithHTTPClient(httpClient))
	}

	sdk, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
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

// createHTTPClient creates an HTTP client with connection pooling.
func createHTTPClient(config *ClientConfig) *http.Client {
	// Determine if we should use HTTP/2
	// Docker daemon only supports HTTP/2 over TLS (ALPN negotiation)
	// For Unix sockets and plain TCP, we use HTTP/1.1
	host := config.Host
	if host == "" {
		host = client.DefaultDockerHost
	}

	transport := &http.Transport{
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.MaxConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
	}

	// Configure dialer based on host type
	switch {
	case strings.HasPrefix(host, "unix://"):
		socketPath := strings.TrimPrefix(host, "unix://")
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: config.DialTimeout}
			return dialer.DialContext(ctx, "unix", socketPath)
		}
		// HTTP/2 not supported on Unix sockets
		transport.ForceAttemptHTTP2 = false
	case strings.HasPrefix(host, "https://"):
		// HTTPS connections can use HTTP/2 via ALPN
		transport.ForceAttemptHTTP2 = true
	default:
		// TCP without TLS - HTTP/2 not supported (no h2c in Docker)
		transport.ForceAttemptHTTP2 = false
	}

	return &http.Client{
		Transport: transport,
		Timeout:   0, // No overall timeout; individual operations have timeouts
	}
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
