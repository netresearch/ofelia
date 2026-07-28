// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

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
	Image     string
	Labels    map[string]string
	Command   []string
	Args      []string
	Hostname  string
	Env       []string
	Dir       string
	User      string
	Mounts    []ServiceMount
	TTY       bool
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

// Conditions under which Swarm restarts a task. The values are the
// restart-condition strings the Docker API expects.
const (
	RestartConditionNone      RestartCondition = "none"       // never restart
	RestartConditionOnFailure RestartCondition = "on-failure" // restart only after a non-zero exit
	RestartConditionAny       RestartCondition = "any"        // restart regardless of exit status
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

// How clients resolve a service name to its tasks. The values are the
// resolution-mode strings the Docker API expects.
const (
	ResolutionModeVIP   ResolutionMode = "vip"   // one virtual IP, load-balanced across the tasks
	ResolutionModeDNSRR ResolutionMode = "dnsrr" // DNS round-robin over the task IPs, no virtual IP
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

// Transport protocols a published port can use. The values are the protocol
// strings the Docker API expects.
const (
	PortProtocolTCP  PortProtocol = "tcp"
	PortProtocolUDP  PortProtocol = "udp"
	PortProtocolSCTP PortProtocol = "sctp"
)

// PortPublishMode represents how a port is published.
type PortPublishMode string

// Where a published service port is reachable. The values are the
// publish-mode strings the Docker API expects.
const (
	PortPublishModeIngress PortPublishMode = "ingress" // reachable on every swarm node via the routing mesh
	PortPublishModeHost    PortPublishMode = "host"    // reachable only on nodes actually running a task
)

// ServiceEndpoint represents the endpoint info for a service.
type ServiceEndpoint struct {
	Spec  *EndpointSpec
	Ports []PortConfig
}

// Task represents a Swarm task.
type Task struct {
	ID           string
	ServiceID    string
	NodeID       string
	Status       TaskStatus
	DesiredState TaskState
	Spec         TaskSpec
	CreatedAt    time.Time
	UpdatedAt    time.Time
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

// The lifecycle states a Swarm task moves through, listed in the order the
// orchestrator normally reaches them. The values are the task-state strings
// the Docker API reports. Complete, Shutdown, Failed, Rejected and Orphaned
// are final — use TaskState.IsTerminalState rather than comparing against
// them by hand.
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
	case TaskStateNew, TaskStatePending, TaskStateAssigned, TaskStateAccepted,
		TaskStatePreparing, TaskStateReady, TaskStateStarting, TaskStateRunning, TaskStateRemove:
		return false
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
