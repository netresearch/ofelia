// Package domain contains SDK-agnostic domain models for Docker operations.
// These types are designed to be independent of any specific Docker client implementation.
package domain

import (
	"io"
	"time"
)

// Container represents a Docker container.
type Container struct {
	ID      string
	Name    string
	Image   string
	State   ContainerState
	Created time.Time
	Labels  map[string]string
	Mounts  []Mount
	Config  *ContainerConfig
}

// ContainerState represents the state of a container.
type ContainerState struct {
	Running    bool
	Paused     bool
	Restarting bool
	OOMKilled  bool
	Dead       bool
	Pid        int
	ExitCode   int
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
	Health     *Health
}

// Health represents container health check status.
type Health struct {
	Status        string // "healthy", "unhealthy", "starting", "none"
	FailingStreak int
	Log           []HealthCheckResult
}

// HealthCheckResult represents a single health check result.
type HealthCheckResult struct {
	Start    time.Time
	End      time.Time
	ExitCode int
	Output   string
}

// ContainerConfig represents the configuration for creating a container.
type ContainerConfig struct {
	// Basic configuration
	Image        string
	Cmd          []string
	Entrypoint   []string
	Env          []string
	WorkingDir   string
	User         string
	Labels       map[string]string
	Hostname     string
	AttachStdin  bool
	AttachStdout bool
	AttachStderr bool
	Tty          bool
	OpenStdin    bool
	StdinOnce    bool

	// Host configuration
	HostConfig *HostConfig

	// Networking configuration
	NetworkConfig *NetworkConfig

	// Container name (optional)
	Name string
}

// HostConfig contains the host-specific configuration for a container.
type HostConfig struct {
	// Resource limits
	Memory     int64 // Memory limit in bytes
	MemorySwap int64 // Total memory limit (memory + swap)
	CPUShares  int64 // CPU shares (relative weight)
	CPUPeriod  int64 // CPU CFS period
	CPUQuota   int64 // CPU CFS quota
	NanoCPUs   int64 // CPU limit in units of 10^-9 CPUs

	// Binds and mounts
	Binds  []string // Volume bindings in format "host:container[:options]"
	Mounts []Mount  // Mount configurations

	// Networking
	NetworkMode  string   // Network mode (bridge, host, none, container:<name|id>)
	PortBindings PortMap  // Port mappings
	DNS          []string // DNS servers
	DNSSearch    []string // DNS search domains
	ExtraHosts   []string // Extra hosts in format "hostname:IP"

	// Security
	Privileged     bool     // Run in privileged mode
	CapAdd         []string // Capabilities to add
	CapDrop        []string // Capabilities to drop
	SecurityOpt    []string // Security options
	ReadonlyRootfs bool     // Mount root filesystem as read-only

	// Runtime
	AutoRemove  bool          // Automatically remove container when it exits
	RestartPolicy RestartPolicy // Restart policy

	// Logging
	LogConfig LogConfig // Logging configuration

	// Other
	PidMode    string // PID namespace mode
	UsernsMode string // User namespace mode
	ShmSize    int64  // Size of /dev/shm in bytes
	Tmpfs      map[string]string // Tmpfs mounts
	Ulimits    []Ulimit // Ulimit settings
}

// RestartPolicy represents the restart policy for a container.
type RestartPolicy struct {
	Name              string // "no", "always", "on-failure", "unless-stopped"
	MaximumRetryCount int
}

// LogConfig represents logging configuration for a container.
type LogConfig struct {
	Type   string            // Logging driver type (json-file, syslog, etc.)
	Config map[string]string // Driver-specific options
}

// Ulimit represents a ulimit setting.
type Ulimit struct {
	Name string
	Soft int64
	Hard int64
}

// Mount represents a mount configuration.
type Mount struct {
	Type        MountType
	Source      string
	Target      string
	ReadOnly    bool
	Consistency string
	BindOptions *BindOptions
	VolumeOptions *VolumeOptions
	TmpfsOptions *TmpfsOptions
}

// MountType represents the type of mount.
type MountType string

const (
	MountTypeBind   MountType = "bind"
	MountTypeVolume MountType = "volume"
	MountTypeTmpfs  MountType = "tmpfs"
	MountTypeNpipe  MountType = "npipe"
)

// BindOptions represents options for bind mounts.
type BindOptions struct {
	Propagation string // "private", "rprivate", "shared", "rshared", "slave", "rslave"
}

// VolumeOptions represents options for volume mounts.
type VolumeOptions struct {
	NoCopy  bool
	Labels  map[string]string
	DriverConfig *Driver
}

// TmpfsOptions represents options for tmpfs mounts.
type TmpfsOptions struct {
	SizeBytes int64
	Mode      uint32
}

// Driver represents a volume driver configuration.
type Driver struct {
	Name    string
	Options map[string]string
}

// NetworkConfig contains networking configuration for a container.
type NetworkConfig struct {
	EndpointsConfig map[string]*EndpointSettings
}

// EndpointSettings represents the settings for a network endpoint.
type EndpointSettings struct {
	IPAMConfig          *EndpointIPAMConfig
	Links               []string
	Aliases             []string
	NetworkID           string
	EndpointID          string
	Gateway             string
	IPAddress           string
	IPPrefixLen         int
	IPv6Gateway         string
	GlobalIPv6Address   string
	GlobalIPv6PrefixLen int
	MacAddress          string
	DriverOpts          map[string]string
}

// EndpointIPAMConfig represents IPAM settings for an endpoint.
type EndpointIPAMConfig struct {
	IPv4Address  string
	IPv6Address  string
	LinkLocalIPs []string
}

// PortMap is a map of ports to their bindings.
type PortMap map[Port][]PortBinding

// Port represents a container port.
type Port string

// PortBinding represents a port binding.
type PortBinding struct {
	HostIP   string
	HostPort string
}

// ListOptions represents options for listing containers.
type ListOptions struct {
	All     bool              // Show all containers (default shows just running)
	Size    bool              // Show size
	Limit   int               // Max number of containers to return
	Filters map[string][]string // Filters to apply
}

// RemoveOptions represents options for removing a container.
type RemoveOptions struct {
	RemoveVolumes bool // Remove associated volumes
	RemoveLinks   bool // Remove associated links
	Force         bool // Force removal of running container
}

// WaitResponse contains the response from waiting for a container.
type WaitResponse struct {
	StatusCode int64
	Error      *WaitError
}

// WaitError represents an error from the container wait operation.
type WaitError struct {
	Message string
}

// LogOptions represents options for retrieving container logs.
type LogOptions struct {
	ShowStdout bool
	ShowStderr bool
	Since      string // Show logs since timestamp or relative time
	Until      string // Show logs until timestamp or relative time
	Timestamps bool   // Add timestamps to output
	Follow     bool   // Follow log output
	Tail       string // Number of lines to show from the end
	Details    bool   // Show extra details
}

// LogsReader provides methods to read container logs.
type LogsReader interface {
	io.ReadCloser
}
