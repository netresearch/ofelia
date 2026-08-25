// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"testing"
	"time"

	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/swarm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
)

// --- convertToSwarmSpec ---

func TestConvertToSwarmSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    *domain.ServiceSpec
		validate func(t *testing.T, result swarm.ServiceSpec)
	}{
		{
			name: "basic spec with name and labels",
			input: &domain.ServiceSpec{
				Name:   "web-service",
				Labels: map[string]string{"app": "web"},
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{
						Image:   "nginx:latest",
						Command: []string{"nginx"},
						Args:    []string{"-g", "daemon off;"},
						Env:     []string{"ENV=prod"},
					},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				assert.Equal(t, "web-service", result.Annotations.Name)
				assert.Equal(t, map[string]string{"app": "web"}, result.Annotations.Labels)
				require.NotNil(t, result.TaskTemplate.ContainerSpec)
				assert.Equal(t, "nginx:latest", result.TaskTemplate.ContainerSpec.Image)
				assert.Equal(t, []string{"nginx"}, result.TaskTemplate.ContainerSpec.Command)
				assert.Equal(t, []string{"-g", "daemon off;"}, result.TaskTemplate.ContainerSpec.Args)
				assert.Equal(t, []string{"ENV=prod"}, result.TaskTemplate.ContainerSpec.Env)
			},
		},
		{
			name: "container spec all fields",
			input: &domain.ServiceSpec{
				Name: "full-spec",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{
						Image:     "alpine:3.18",
						Labels:    map[string]string{"tier": "backend"},
						Hostname:  "worker",
						Dir:       "/app",
						User:      "app",
						TTY:       true,
						OpenStdin: true,
					},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				cs := result.TaskTemplate.ContainerSpec
				require.NotNil(t, cs)
				assert.Equal(t, "alpine:3.18", cs.Image)
				assert.Equal(t, map[string]string{"tier": "backend"}, cs.Labels)
				assert.Equal(t, "worker", cs.Hostname)
				assert.Equal(t, "/app", cs.Dir)
				assert.Equal(t, "app", cs.User)
				assert.True(t, cs.TTY)
				assert.True(t, cs.OpenStdin)
			},
		},
		{
			name: "with mounts",
			input: &domain.ServiceSpec{
				Name: "mounted-service",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{
						Image: "nginx",
						Mounts: []domain.ServiceMount{
							{Type: domain.MountTypeBind, Source: "/host", Target: "/container", ReadOnly: true},
							{Type: domain.MountTypeVolume, Source: "data", Target: "/data"},
						},
					},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				mounts := result.TaskTemplate.ContainerSpec.Mounts
				require.Len(t, mounts, 2)
				assert.Equal(t, mount.TypeBind, mounts[0].Type)
				assert.Equal(t, "/host", mounts[0].Source)
				assert.Equal(t, "/container", mounts[0].Target)
				assert.True(t, mounts[0].ReadOnly)
				assert.Equal(t, mount.TypeVolume, mounts[1].Type)
				assert.Equal(t, "data", mounts[1].Source)
			},
		},
		{
			name: "with restart policy",
			input: &domain.ServiceSpec{
				Name: "restart-service",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{Image: "nginx"},
					RestartPolicy: &domain.ServiceRestartPolicy{
						Condition:   domain.RestartConditionOnFailure,
						Delay:       new(5 * time.Second),
						MaxAttempts: new(uint64(3)),
						Window:      new(2 * time.Minute),
					},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				rp := result.TaskTemplate.RestartPolicy
				require.NotNil(t, rp)
				assert.Equal(t, swarm.RestartPolicyCondition("on-failure"), rp.Condition)
				require.NotNil(t, rp.Delay)
				assert.Equal(t, 5*time.Second, *rp.Delay)
				require.NotNil(t, rp.MaxAttempts)
				assert.Equal(t, uint64(3), *rp.MaxAttempts)
				require.NotNil(t, rp.Window)
				assert.Equal(t, 2*time.Minute, *rp.Window)
			},
		},
		{
			name: "without restart policy",
			input: &domain.ServiceSpec{
				Name: "no-restart",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{Image: "nginx"},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				assert.Nil(t, result.TaskTemplate.RestartPolicy)
			},
		},
		{
			name: "with resources limits and reservations",
			input: &domain.ServiceSpec{
				Name: "resourced-service",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{Image: "nginx"},
					Resources: &domain.ResourceRequirements{
						Limits:       &domain.Resources{NanoCPUs: 1000000000, MemoryBytes: 536870912},
						Reservations: &domain.Resources{NanoCPUs: 500000000, MemoryBytes: 268435456},
					},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				res := result.TaskTemplate.Resources
				require.NotNil(t, res)
				require.NotNil(t, res.Limits)
				assert.Equal(t, int64(1000000000), res.Limits.NanoCPUs)
				assert.Equal(t, int64(536870912), res.Limits.MemoryBytes)
				require.NotNil(t, res.Reservations)
				assert.Equal(t, int64(500000000), res.Reservations.NanoCPUs)
				assert.Equal(t, int64(268435456), res.Reservations.MemoryBytes)
			},
		},
		{
			name: "with resources limits only",
			input: &domain.ServiceSpec{
				Name: "limits-only",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{Image: "nginx"},
					Resources:     &domain.ResourceRequirements{Limits: &domain.Resources{NanoCPUs: 1000000000}},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				require.NotNil(t, result.TaskTemplate.Resources)
				require.NotNil(t, result.TaskTemplate.Resources.Limits)
				assert.Nil(t, result.TaskTemplate.Resources.Reservations)
			},
		},
		{
			name: "without resources",
			input: &domain.ServiceSpec{
				Name: "no-resources",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{Image: "nginx"},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				assert.Nil(t, result.TaskTemplate.Resources)
			},
		},
		{
			name: "with networks",
			input: &domain.ServiceSpec{
				Name: "networked-service",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{Image: "nginx"},
				},
				Networks: []domain.NetworkAttachment{
					{Target: "frontend", Aliases: []string{"web"}},
					{Target: "backend", Aliases: []string{"api", "app"}},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				nets := result.TaskTemplate.Networks
				require.Len(t, nets, 2)
				assert.Equal(t, "frontend", nets[0].Target)
				assert.Equal(t, []string{"web"}, nets[0].Aliases)
				assert.Equal(t, "backend", nets[1].Target)
				assert.Equal(t, []string{"api", "app"}, nets[1].Aliases)
			},
		},
		{
			name: "replicated mode",
			input: &domain.ServiceSpec{
				Name: "replicated",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{Image: "nginx"},
				},
				Mode: domain.ServiceMode{
					Replicated: &domain.ReplicatedService{Replicas: new(uint64(3))},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				require.NotNil(t, result.Mode.Replicated)
				require.NotNil(t, result.Mode.Replicated.Replicas)
				assert.Equal(t, uint64(3), *result.Mode.Replicated.Replicas)
				assert.Nil(t, result.Mode.Global)
			},
		},
		{
			name: "global mode",
			input: &domain.ServiceSpec{
				Name: "global",
				TaskTemplate: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{Image: "nginx"},
				},
				Mode: domain.ServiceMode{
					Global: &domain.GlobalService{},
				},
			},
			validate: func(t *testing.T, result swarm.ServiceSpec) {
				require.NotNil(t, result.Mode.Global)
				assert.Nil(t, result.Mode.Replicated)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertToSwarmSpec(tt.input)
			tt.validate(t, result)
		})
	}
}

// --- convertFromSwarmService ---

func TestConvertFromSwarmService(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name     string
		input    *swarm.Service
		validate func(t *testing.T, result *domain.Service)
	}{
		{
			name: "basic service with container spec",
			input: &swarm.Service{
				ID: "svc-123",
				Meta: swarm.Meta{
					Version:   swarm.Version{Index: 42},
					CreatedAt: now,
					UpdatedAt: now.Add(time.Hour),
				},
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{
						Name:   "my-service",
						Labels: map[string]string{"env": "prod"},
					},
					TaskTemplate: swarm.TaskSpec{
						ContainerSpec: &swarm.ContainerSpec{
							Image:     "nginx:latest",
							Labels:    map[string]string{"role": "web"},
							Command:   []string{"nginx"},
							Args:      []string{"-g", "daemon off;"},
							Hostname:  "web-host",
							Env:       []string{"PORT=80"},
							Dir:       "/usr/share/nginx",
							User:      "nginx",
							TTY:       true,
							OpenStdin: true,
						},
					},
				},
			},
			validate: func(t *testing.T, result *domain.Service) {
				require.NotNil(t, result)
				assert.Equal(t, "svc-123", result.ID)
				assert.Equal(t, uint64(42), result.Meta.Version.Index)
				assert.Equal(t, now, result.Meta.CreatedAt)
				assert.Equal(t, "my-service", result.Spec.Name)
				assert.Equal(t, map[string]string{"env": "prod"}, result.Spec.Labels)

				cs := result.Spec.TaskTemplate.ContainerSpec
				assert.Equal(t, "nginx:latest", cs.Image)
				assert.Equal(t, map[string]string{"role": "web"}, cs.Labels)
				assert.Equal(t, []string{"nginx"}, cs.Command)
				assert.Equal(t, []string{"-g", "daemon off;"}, cs.Args)
				assert.Equal(t, "web-host", cs.Hostname)
				assert.Equal(t, []string{"PORT=80"}, cs.Env)
				assert.Equal(t, "/usr/share/nginx", cs.Dir)
				assert.Equal(t, "nginx", cs.User)
				assert.True(t, cs.TTY)
				assert.True(t, cs.OpenStdin)
			},
		},
		{
			name: "service without container spec",
			input: &swarm.Service{
				ID: "svc-456",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{Name: "empty-service"},
					TaskTemplate: swarm.TaskSpec{
						ContainerSpec: nil,
					},
				},
			},
			validate: func(t *testing.T, result *domain.Service) {
				require.NotNil(t, result)
				assert.Equal(t, "svc-456", result.ID)
				assert.Equal(t, "empty-service", result.Spec.Name)
				assert.Empty(t, result.Spec.TaskTemplate.ContainerSpec.Image)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertFromSwarmService(tt.input)
			tt.validate(t, result)
		})
	}
}

// --- convertFromSwarmTask ---

func TestConvertFromSwarmTask(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name     string
		input    *swarm.Task
		validate func(t *testing.T, result domain.Task)
	}{
		{
			name: "running task with container status",
			input: &swarm.Task{
				ID:           "task-abc",
				ServiceID:    "svc-123",
				NodeID:       "node-xyz",
				DesiredState: swarm.TaskStateRunning,
				Meta: swarm.Meta{
					CreatedAt: now,
					UpdatedAt: now.Add(time.Minute),
				},
				Status: swarm.TaskStatus{
					Timestamp: now.Add(30 * time.Second),
					State:     swarm.TaskStateRunning,
					Message:   "started successfully",
					Err:       "",
					ContainerStatus: &swarm.ContainerStatus{
						ContainerID: "container-789",
						PID:         12345,
						ExitCode:    0,
					},
				},
			},
			validate: func(t *testing.T, result domain.Task) {
				assert.Equal(t, "task-abc", result.ID)
				assert.Equal(t, "svc-123", result.ServiceID)
				assert.Equal(t, "node-xyz", result.NodeID)
				assert.Equal(t, domain.TaskState("running"), result.DesiredState)
				assert.Equal(t, now, result.CreatedAt)
				assert.Equal(t, domain.TaskState("running"), result.Status.State)
				assert.Equal(t, "started successfully", result.Status.Message)
				assert.Empty(t, result.Status.Err)
				require.NotNil(t, result.Status.ContainerStatus)
				assert.Equal(t, "container-789", result.Status.ContainerStatus.ContainerID)
				assert.Equal(t, 12345, result.Status.ContainerStatus.PID)
				assert.Equal(t, 0, result.Status.ContainerStatus.ExitCode)
			},
		},
		{
			name: "failed task without container status",
			input: &swarm.Task{
				ID:           "task-def",
				ServiceID:    "svc-456",
				DesiredState: swarm.TaskStateShutdown,
				Status: swarm.TaskStatus{
					State:   swarm.TaskStateFailed,
					Message: "task failed",
					Err:     "exit code 1",
				},
			},
			validate: func(t *testing.T, result domain.Task) {
				assert.Equal(t, "task-def", result.ID)
				assert.Equal(t, domain.TaskState("shutdown"), result.DesiredState)
				assert.Equal(t, domain.TaskState("failed"), result.Status.State)
				assert.Equal(t, "task failed", result.Status.Message)
				assert.Equal(t, "exit code 1", result.Status.Err)
				assert.Nil(t, result.Status.ContainerStatus)
			},
		},
		{
			name: "completed task with exit code",
			input: &swarm.Task{
				ID:           "task-ghi",
				ServiceID:    "svc-789",
				DesiredState: swarm.TaskStateShutdown,
				Status: swarm.TaskStatus{
					State: swarm.TaskStateComplete,
					ContainerStatus: &swarm.ContainerStatus{
						ContainerID: "container-xxx",
						ExitCode:    0,
					},
				},
			},
			validate: func(t *testing.T, result domain.Task) {
				assert.Equal(t, domain.TaskState("complete"), result.Status.State)
				require.NotNil(t, result.Status.ContainerStatus)
				assert.Equal(t, 0, result.Status.ContainerStatus.ExitCode)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertFromSwarmTask(tt.input)
			tt.validate(t, result)
		})
	}
}

// --- Helper functions ---
