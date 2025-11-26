// Package ports defines the port interfaces for Docker operations.
// These interfaces abstract the Docker client implementation, enabling
// easy testing with mocks and future SDK migrations.
package ports

// DockerClient is the main interface for Docker operations.
// It provides access to specialized service interfaces for different
// Docker resource types.
type DockerClient interface {
	// Containers returns the container service interface.
	Containers() ContainerService

	// Exec returns the exec service interface.
	Exec() ExecService

	// Images returns the image service interface.
	Images() ImageService

	// Events returns the event service interface.
	Events() EventService

	// Services returns the Swarm service interface.
	Services() SwarmService

	// Networks returns the network service interface.
	Networks() NetworkService

	// System returns the system service interface.
	System() SystemService

	// Close closes the client and releases resources.
	Close() error
}

// ClientFactory creates DockerClient instances.
type ClientFactory interface {
	// NewClient creates a new DockerClient from environment variables.
	NewClient() (DockerClient, error)

	// NewClientWithOptions creates a new DockerClient with custom options.
	NewClientWithOptions(opts ...ClientOption) (DockerClient, error)
}

// ClientOption is a function that configures a DockerClient.
type ClientOption func(*ClientOptions)

// ClientOptions contains options for creating a DockerClient.
type ClientOptions struct {
	// Host is the Docker host address.
	Host string

	// Version is the API version to use.
	Version string

	// TLSConfig contains TLS configuration.
	TLSConfig *TLSConfig

	// HTTPHeaders are custom HTTP headers to send.
	HTTPHeaders map[string]string
}

// TLSConfig contains TLS configuration options.
type TLSConfig struct {
	CAFile   string
	CertFile string
	KeyFile  string
	Insecure bool
}

// WithHost sets the Docker host address.
func WithHost(host string) ClientOption {
	return func(o *ClientOptions) {
		o.Host = host
	}
}

// WithVersion sets the API version.
func WithVersion(version string) ClientOption {
	return func(o *ClientOptions) {
		o.Version = version
	}
}

// WithTLSConfig sets TLS configuration.
func WithTLSConfig(config *TLSConfig) ClientOption {
	return func(o *ClientOptions) {
		o.TLSConfig = config
	}
}

// WithHTTPHeaders sets custom HTTP headers.
func WithHTTPHeaders(headers map[string]string) ClientOption {
	return func(o *ClientOptions) {
		o.HTTPHeaders = headers
	}
}
