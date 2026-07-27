// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"testing"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/swarm"
	"github.com/stretchr/testify/assert"

	"github.com/netresearch/ofelia/core/domain"
)

// ---------------------------------------------------------------------------
// convertFromSDKEvent (0% → 100%)
// ---------------------------------------------------------------------------

func TestConvertFromSDKEvent(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name     string
		input    *events.Message
		expected domain.Event
	}{
		{
			name: "container start event",
			input: &events.Message{
				Type:   events.ContainerEventType,
				Action: events.ActionStart,
				Actor: events.Actor{
					ID: "abc123",
					Attributes: map[string]string{
						"name":  "my-container",
						"image": "alpine:latest",
					},
				},
				Scope:    "local",
				Time:     now.Unix(),
				TimeNano: now.UnixNano(),
			},
			expected: domain.Event{
				Type:   "container",
				Action: "start",
				Actor: domain.EventActor{
					ID: "abc123",
					Attributes: map[string]string{
						"name":  "my-container",
						"image": "alpine:latest",
					},
				},
				Scope:    "local",
				Time:     time.Unix(now.Unix(), now.UnixNano()),
				TimeNano: now.UnixNano(),
			},
		},
		{
			name: "empty event",
			input: &events.Message{
				Actor: events.Actor{},
			},
			expected: domain.Event{
				Actor: domain.EventActor{},
				Time:  time.Unix(0, 0),
			},
		},
		{
			name: "network event",
			input: &events.Message{
				Type:   events.NetworkEventType,
				Action: events.ActionConnect,
				Actor: events.Actor{
					ID: "net-456",
				},
				Scope: "swarm",
			},
			expected: domain.Event{
				Type:   "network",
				Action: "connect",
				Actor: domain.EventActor{
					ID: "net-456",
				},
				Scope: "swarm",
				Time:  time.Unix(0, 0),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := convertFromSDKEvent(tc.input)
			assert.Equal(t, tc.expected.Type, result.Type)
			assert.Equal(t, tc.expected.Action, result.Action)
			assert.Equal(t, tc.expected.Actor.ID, result.Actor.ID)
			assert.Equal(t, tc.expected.Scope, result.Scope)
			assert.Equal(t, tc.expected.TimeNano, result.TimeNano)
		})
	}
}

// ---------------------------------------------------------------------------
// logWarning (0% → 100%)
// ---------------------------------------------------------------------------

func TestConfigAuthProvider_LogWarning_NilLogger(t *testing.T) {
	t.Parallel()
	p := &ConfigAuthProvider{} // nil logger

	// Should not panic
	p.logWarning("test warning %s", "message")
}

func TestConfigAuthProvider_LogWarning_WithLogger(t *testing.T) {
	t.Parallel()

	// Use a minimal logger
	p := NewConfigAuthProviderWithOptions("", nil)

	// Should not panic with nil logger
	p.logWarning("test warning %s", "message")
}

// ---------------------------------------------------------------------------
// Service conversion functions - convertToSwarmSpec edge cases
// ---------------------------------------------------------------------------

func TestConvertToSwarmSpec_WithResources(t *testing.T) {
	t.Parallel()

	cpuLimit := int64(1000000000) // 1 CPU
	memLimit := int64(536870912)  // 512MB

	spec := &domain.ServiceSpec{
		Name:   "test-service",
		Labels: map[string]string{"app": "test"},
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image:   "nginx:latest",
				Command: []string{"nginx", "-g", "daemon off;"},
				Env:     []string{"FOO=bar"},
				User:    "nobody",
				TTY:     true,
				Mounts: []domain.ServiceMount{
					{Type: "bind", Source: "/host", Target: "/container"},
				},
			},
			RestartPolicy: &domain.ServiceRestartPolicy{
				Condition: domain.RestartConditionNone,
			},
			Resources: &domain.ResourceRequirements{
				Limits: &domain.Resources{
					NanoCPUs:    cpuLimit,
					MemoryBytes: memLimit,
				},
				Reservations: &domain.Resources{
					NanoCPUs:    cpuLimit / 2,
					MemoryBytes: memLimit / 2,
				},
			},
		},
		Networks: []domain.NetworkAttachment{
			{Target: "my-network", Aliases: []string{"app"}},
		},
		Mode: domain.ServiceMode{
			Replicated: &domain.ReplicatedService{
				Replicas: func() *uint64 { v := uint64(3); return &v }(),
			},
		},
	}

	result := convertToSwarmSpec(spec)
	assert.Equal(t, "test-service", result.Annotations.Name)
	assert.NotNil(t, result.TaskTemplate.Resources)
	assert.NotNil(t, result.TaskTemplate.Resources.Limits)
	assert.NotNil(t, result.TaskTemplate.Resources.Reservations)
	assert.Equal(t, cpuLimit, result.TaskTemplate.Resources.Limits.NanoCPUs)
	assert.Len(t, result.TaskTemplate.Networks, 1)
	assert.NotNil(t, result.Mode.Replicated)
}

func TestConvertToSwarmSpec_GlobalMode(t *testing.T) {
	t.Parallel()

	spec := &domain.ServiceSpec{
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
			},
		},
		Mode: domain.ServiceMode{
			Global: &domain.GlobalService{},
		},
	}

	result := convertToSwarmSpec(spec)
	assert.NotNil(t, result.Mode.Global)
	assert.Nil(t, result.Mode.Replicated)
}

// ---------------------------------------------------------------------------
// convertFromSwarmTask with nil ContainerStatus
// ---------------------------------------------------------------------------

func TestConvertFromSwarmTask_NilContainerStatus(t *testing.T) {
	t.Parallel()

	task := swarm.Task{
		ID:        "task-nil-cs",
		ServiceID: "svc-1",
		NodeID:    "node-1",
		Status: swarm.TaskStatus{
			State:   swarm.TaskStateComplete,
			Message: "completed",
			// ContainerStatus is nil
		},
	}

	result := convertFromSwarmTask(&task)
	assert.Equal(t, "task-nil-cs", result.ID)
	assert.Nil(t, result.Status.ContainerStatus)
}

func TestConvertFromSwarmTask_WithContainerStatus(t *testing.T) {
	t.Parallel()

	task := swarm.Task{
		ID:        "task-with-cs",
		ServiceID: "svc-1",
		NodeID:    "node-1",
		Status: swarm.TaskStatus{
			State: swarm.TaskStateComplete,
			ContainerStatus: &swarm.ContainerStatus{
				ContainerID: "container-abc",
				PID:         1234,
				ExitCode:    0,
			},
		},
	}

	result := convertFromSwarmTask(&task)
	assert.Equal(t, "task-with-cs", result.ID)
	assert.NotNil(t, result.Status.ContainerStatus)
	assert.Equal(t, "container-abc", result.Status.ContainerStatus.ContainerID)
	assert.Equal(t, 0, result.Status.ContainerStatus.ExitCode)
}

// ---------------------------------------------------------------------------
// convertFromSwarmService with nil ContainerSpec
// ---------------------------------------------------------------------------

func TestConvertFromSwarmService_NilContainerSpec(t *testing.T) {
	t.Parallel()

	svc := swarm.Service{
		ID: "svc-nil-cs",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: "my-service",
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: nil, // nil ContainerSpec
			},
		},
	}

	result := convertFromSwarmService(&svc)
	assert.Equal(t, "svc-nil-cs", result.ID)
	assert.Equal(t, "my-service", result.Spec.Name)
}
