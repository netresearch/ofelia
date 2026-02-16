package domain

import "time"

// SystemInfo represents Docker system information.
type SystemInfo struct {
	ID                 string
	Containers         int
	ContainersRunning  int
	ContainersPaused   int
	ContainersStopped  int
	Images             int
	Driver             string
	DriverStatus       [][2]string
	SystemStatus       [][2]string
	MemoryLimit        bool
	SwapLimit          bool
	CPUCfsPeriod       bool
	CPUCfsQuota        bool
	CPUShares          bool
	CPUSet             bool
	PidsLimit          bool
	IPv4Forwarding     bool
	Debug              bool
	NFd                int
	OomKillDisable     bool
	NGoroutines        int
	SystemTime         string
	LoggingDriver      string
	CgroupDriver       string
	CgroupVersion      string
	NEventsListener    int
	KernelVersion      string
	OperatingSystem    string
	OSVersion          string
	OSType             string
	Architecture       string
	IndexServerAddress string
	NCPU               int
	MemTotal           int64
	DockerRootDir      string
	HTTPProxy          string
	HTTPSProxy         string
	NoProxy            string
	Name               string
	Labels             []string
	ExperimentalBuild  bool
	ServerVersion      string
	Runtimes           map[string]Runtime
	DefaultRuntime     string
	Swarm              SwarmInfo
	LiveRestoreEnabled bool
	Isolation          string
	InitBinary         string
	SecurityOptions    []string
	Warnings           []string
}

// Runtime represents a container runtime.
type Runtime struct {
	Path string
	Args []string
}

// SwarmInfo represents Swarm-related information.
type SwarmInfo struct {
	NodeID           string
	NodeAddr         string
	LocalNodeState   LocalNodeState
	ControlAvailable bool
	Error            string
	RemoteManagers   []Peer
	Nodes            int
	Managers         int
	Cluster          *ClusterInfo
}

// LocalNodeState represents the state of the local Swarm node.
type LocalNodeState string

const (
	LocalNodeStateInactive LocalNodeState = "inactive"
	LocalNodeStatePending  LocalNodeState = "pending"
	LocalNodeStateActive   LocalNodeState = "active"
	LocalNodeStateError    LocalNodeState = "error"
	LocalNodeStateLocked   LocalNodeState = "locked"
)

// Peer represents a remote manager in the swarm.
type Peer struct {
	NodeID string
	Addr   string
}

// ClusterInfo represents information about the Swarm cluster.
type ClusterInfo struct {
	ID                     string
	Version                ServiceVersion
	CreatedAt              time.Time
	UpdatedAt              time.Time
	RootRotationInProgress bool
}

// Version represents Docker version information.
type Version struct {
	Platform      Platform
	Components    []ComponentVersion
	Version       string
	APIVersion    string
	MinAPIVersion string
	GitCommit     string
	GoVersion     string
	Os            string
	Arch          string
	KernelVersion string
	BuildTime     string
}

// Platform represents the platform information.
type Platform struct {
	Name string
}

// ComponentVersion represents version info for a component.
type ComponentVersion struct {
	Name    string
	Version string
	Details map[string]string
}

// PingResponse represents the response from a ping.
type PingResponse struct {
	APIVersion     string
	OSType         string
	Experimental   bool
	BuilderVersion string
}

// DiskUsage represents disk usage information.
type DiskUsage struct {
	LayersSize int64
	Images     []ImageSummary
	Containers []ContainerSummary
	Volumes    []VolumeSummary
}

// ContainerSummary represents a container summary for disk usage.
type ContainerSummary struct {
	ID         string
	Names      []string
	Image      string
	ImageID    string
	Command    string
	Created    int64
	State      string
	Status     string
	SizeRw     int64
	SizeRootFs int64
}

// VolumeSummary represents a volume summary for disk usage.
type VolumeSummary struct {
	Name       string
	Driver     string
	Mountpoint string
	CreatedAt  string
	Labels     map[string]string
	Scope      string
	Options    map[string]string
	UsageData  *VolumeUsageData
}

// VolumeUsageData represents volume usage data.
type VolumeUsageData struct {
	Size     int64
	RefCount int64
}
