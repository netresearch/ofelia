package domain

import "time"

// Event represents a Docker event.
type Event struct {
	// Type of event (container, image, volume, network, daemon, plugin, service, node, secret, config)
	Type string

	// Action that triggered the event (create, start, stop, die, kill, etc.)
	Action string

	// Actor that triggered the event
	Actor EventActor

	// Scope of the event (local, swarm)
	Scope string

	// Time when the event occurred
	Time time.Time

	// TimeNano is the time in nanoseconds
	TimeNano int64
}

// EventActor contains information about the object that triggered the event.
type EventActor struct {
	// ID of the object
	ID string

	// Attributes contain key-value pairs with additional info about the object
	Attributes map[string]string
}

// EventFilter represents filters for subscribing to events.
type EventFilter struct {
	// Since filters events after this time
	Since time.Time

	// Until filters events before this time
	Until time.Time

	// Filters is a map of filter types to filter values
	// Keys: container, event, image, label, network, type, volume, daemon, service, node, scope
	Filters map[string][]string
}

// Common event types.
const (
	EventTypeContainer = "container"
	EventTypeImage     = "image"
	EventTypeVolume    = "volume"
	EventTypeNetwork   = "network"
	EventTypeDaemon    = "daemon"
	EventTypePlugin    = "plugin"
	EventTypeService   = "service"
	EventTypeNode      = "node"
	EventTypeSecret    = "secret"
	EventTypeConfig    = "config"
)

// Common event actions.
const (
	EventActionCreate       = "create"
	EventActionStart        = "start"
	EventActionStop         = "stop"
	EventActionDie          = "die"
	EventActionKill         = "kill"
	EventActionPause        = "pause"
	EventActionUnpause      = "unpause"
	EventActionRestart      = "restart"
	EventActionOOM          = "oom"
	EventActionDestroy      = "destroy"
	EventActionRename       = "rename"
	EventActionUpdate       = "update"
	EventActionHealthStatus = "health_status"
	EventActionExecCreate   = "exec_create"
	EventActionExecStart    = "exec_start"
	EventActionExecDie      = "exec_die"
	EventActionAttach       = "attach"
	EventActionDetach       = "detach"
	EventActionCommit       = "commit"
	EventActionCopy         = "copy"
	EventActionArchivePath  = "archive-path"
	EventActionExtractToDir = "extract-to-dir"
	EventActionExport       = "export"
	EventActionTop          = "top"
	EventActionResize       = "resize"

	// Image events
	EventActionPull   = "pull"
	EventActionPush   = "push"
	EventActionTag    = "tag"
	EventActionUntag  = "untag"
	EventActionDelete = "delete"
	EventActionImport = "import"
	EventActionSave   = "save"
	EventActionLoad   = "load"

	// Volume events
	EventActionMount   = "mount"
	EventActionUnmount = "unmount"

	// Network events
	EventActionConnect    = "connect"
	EventActionDisconnect = "disconnect"
	EventActionRemove     = "remove"
)

// IsContainerStopEvent returns true if the event indicates a container has stopped.
func (e *Event) IsContainerStopEvent() bool {
	if e.Type != EventTypeContainer {
		return false
	}
	switch e.Action {
	case EventActionDie, EventActionKill, EventActionStop, EventActionOOM:
		return true
	}
	return false
}

// GetContainerID returns the container ID from the event, if applicable.
func (e *Event) GetContainerID() string {
	if e.Type == EventTypeContainer {
		return e.Actor.ID
	}
	return ""
}
