package domain

import "time"

// Service represents a Docker Swarm service.
type Service struct {
	ID   string
	Meta ServiceMeta
	Spec ServiceSpec
	// Endpoint contains the exposed ports
	Endpoint ServiceEndpoint
}

// ServiceMeta contains metadata about a service.
type ServiceMeta struct {
	Version   ServiceVersion
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ServiceVersion contains version information for a service.
type ServiceVersion struct {
	Index uint64
}

// ServiceSpec contains the specification for a service.
type ServiceSpec struct {
	Name         string
	Labels       map[string]string
	TaskTemplate TaskSpec
	Mode         ServiceMode
	Networks     []NetworkAttachment
	EndpointSpec *EndpointSpec
}

// TaskSpec represents the specification for a task.
type TaskSpec struct {
	ContainerSpec ContainerSpec
	Resources     *ResourceRequirements
	RestartPolicy *ServiceRestartPolicy
	Placement     *Placement
	Networks      []NetworkAttachment
	LogDriver     *LogDriver
}

// ContainerSpec represents the container specification for a service.
type ContainerSpec struct {
	Image    string
	Labels   map[string]string
	Command  []string
	Args     []string
	Hostname string
	Env      []string
	Dir      string
	User     string
	Mounts   []ServiceMount
	TTY      bool
	OpenStdin bool
}

// ServiceMount represents a mount for a service container.
type ServiceMount struct {
	Type     MountType
	Source   string
	Target   string
	ReadOnly bool
}

// ResourceRequirements represents resource constraints.
type ResourceRequirements struct {
	Limits       *Resources
	Reservations *Resources
}

// Resources represents resource limits/reservations.
type Resources struct {
	NanoCPUs    int64
	MemoryBytes int64
}

// ServiceRestartPolicy represents the restart policy for a service.
type ServiceRestartPolicy struct {
	Condition   RestartCondition
	Delay       *time.Duration
	MaxAttempts *uint64
	Window      *time.Duration
}

// RestartCondition represents when to restart a task.
type RestartCondition string

const (
	RestartConditionNone      RestartCondition = "none"
	RestartConditionOnFailure RestartCondition = "on-failure"
	RestartConditionAny       RestartCondition = "any"
)

// Placement represents placement constraints.
type Placement struct {
	Constraints []string
	Preferences []PlacementPreference
}

// PlacementPreference represents a placement preference.
type PlacementPreference struct {
	Spread *SpreadOver
}

// SpreadOver represents spread placement configuration.
type SpreadOver struct {
	SpreadDescriptor string
}

// LogDriver represents logging driver configuration.
type LogDriver struct {
	Name    string
	Options map[string]string
}

// ServiceMode represents how the service should be scheduled.
type ServiceMode struct {
	Replicated *ReplicatedService
	Global     *GlobalService
}

// ReplicatedService represents a replicated service mode.
type ReplicatedService struct {
	Replicas *uint64
}

// GlobalService represents a global service mode.
type GlobalService struct{}

// NetworkAttachment represents a network attachment for a service.
type NetworkAttachment struct {
	Target  string // Network ID or name
	Aliases []string
}

// EndpointSpec represents the endpoint specification for a service.
type EndpointSpec struct {
	Mode  ResolutionMode
	Ports []PortConfig
}

// ResolutionMode represents the endpoint resolution mode.
type ResolutionMode string

const (
	ResolutionModeVIP   ResolutionMode = "vip"
	ResolutionModeDNSRR ResolutionMode = "dnsrr"
)

// PortConfig represents a port configuration for a service.
type PortConfig struct {
	Name          string
	Protocol      PortProtocol
	TargetPort    uint32
	PublishedPort uint32
	PublishMode   PortPublishMode
}

// PortProtocol represents the protocol for a port.
type PortProtocol string

const (
	PortProtocolTCP  PortProtocol = "tcp"
	PortProtocolUDP  PortProtocol = "udp"
	PortProtocolSCTP PortProtocol = "sctp"
)

// PortPublishMode represents how a port is published.
type PortPublishMode string

const (
	PortPublishModeIngress PortPublishMode = "ingress"
	PortPublishModeHost    PortPublishMode = "host"
)

// ServiceEndpoint represents the endpoint info for a service.
type ServiceEndpoint struct {
	Spec  *EndpointSpec
	Ports []PortConfig
}

// Task represents a Swarm task.
type Task struct {
	ID          string
	ServiceID   string
	NodeID      string
	Status      TaskStatus
	DesiredState TaskState
	Spec        TaskSpec
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TaskStatus represents the status of a task.
type TaskStatus struct {
	Timestamp       time.Time
	State           TaskState
	Message         string
	Err             string
	ContainerStatus *ContainerStatus
}

// ContainerStatus represents the container status within a task.
type ContainerStatus struct {
	ContainerID string
	PID         int
	ExitCode    int
}

// TaskState represents the state of a task.
type TaskState string

const (
	TaskStateNew       TaskState = "new"
	TaskStatePending   TaskState = "pending"
	TaskStateAssigned  TaskState = "assigned"
	TaskStateAccepted  TaskState = "accepted"
	TaskStatePreparing TaskState = "preparing"
	TaskStateReady     TaskState = "ready"
	TaskStateStarting  TaskState = "starting"
	TaskStateRunning   TaskState = "running"
	TaskStateComplete  TaskState = "complete"
	TaskStateShutdown  TaskState = "shutdown"
	TaskStateFailed    TaskState = "failed"
	TaskStateRejected  TaskState = "rejected"
	TaskStateRemove    TaskState = "remove"
	TaskStateOrphaned  TaskState = "orphaned"
)

// IsTerminalState returns true if the task is in a terminal state.
func (s TaskState) IsTerminalState() bool {
	switch s {
	case TaskStateComplete, TaskStateFailed, TaskStateRejected, TaskStateShutdown, TaskStateOrphaned:
		return true
	}
	return false
}

// ServiceListOptions represents options for listing services.
type ServiceListOptions struct {
	Filters map[string][]string
}

// TaskListOptions represents options for listing tasks.
type TaskListOptions struct {
	Filters map[string][]string
}

// ServiceCreateOptions represents options for creating a service.
type ServiceCreateOptions struct {
	// EncodedRegistryAuth is the base64url encoded auth configuration
	EncodedRegistryAuth string
}

// ServiceRemoveOptions represents options for removing a service.
type ServiceRemoveOptions struct {
	// No options currently
}
