// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"testing"
	"time"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
)

// roundTripServiceSpec converts domain->swarm->domain and returns the result.
func roundTripServiceSpec(spec *domain.ServiceSpec) *domain.Service {
	swarmSpec := convertToSwarmSpec(spec)
	svc := &swarm.Service{
		ID:   "roundtrip-test",
		Spec: swarmSpec,
	}

	return convertFromSwarmService(svc)
}

func TestServiceSpec_RoundTrip_ContainerSpec(t *testing.T) {
	t.Parallel()

	original := &domain.ServiceSpec{
		Name:   "container-rt",
		Labels: map[string]string{"app": "test"},
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image:     "alpine:3.20",
				Labels:    map[string]string{"tier": "backend"},
				Command:   []string{"/bin/sh", "-c"},
				Args:      []string{"echo hello"},
				Hostname:  "worker-01",
				Env:       []string{"FOO=bar", "BAZ=qux"},
				Dir:       "/opt/app",
				User:      "appuser",
				TTY:       true,
				OpenStdin: true,
				Mounts: []domain.ServiceMount{
					{Type: domain.MountTypeBind, Source: "/host/data", Target: "/data", ReadOnly: true},
					{Type: domain.MountTypeVolume, Source: "vol1", Target: "/vol1", ReadOnly: false},
				},
			},
		},
	}

	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	cs := result.Spec.TaskTemplate.ContainerSpec
	assert.Equal(t, "alpine:3.20", cs.Image, "Image should survive round-trip")
	assert.Equal(t, map[string]string{"tier": "backend"}, cs.Labels, "ContainerSpec Labels should survive round-trip")
	assert.Equal(t, []string{"/bin/sh", "-c"}, cs.Command, "Command should survive round-trip")
	assert.Equal(t, []string{"echo hello"}, cs.Args, "Args should survive round-trip")
	assert.Equal(t, "worker-01", cs.Hostname, "Hostname should survive round-trip")
	assert.Equal(t, []string{"FOO=bar", "BAZ=qux"}, cs.Env, "Env should survive round-trip")
	assert.Equal(t, "/opt/app", cs.Dir, "Dir should survive round-trip")
	assert.Equal(t, "appuser", cs.User, "User should survive round-trip")
	assert.True(t, cs.TTY, "TTY should survive round-trip")
	assert.True(t, cs.OpenStdin, "OpenStdin should survive round-trip")

	require.Len(t, cs.Mounts, 2, "Mounts should survive round-trip")
	assert.Equal(t, domain.MountTypeBind, cs.Mounts[0].Type)
	assert.Equal(t, "/host/data", cs.Mounts[0].Source)
	assert.Equal(t, "/data", cs.Mounts[0].Target)
	assert.True(t, cs.Mounts[0].ReadOnly)
	assert.Equal(t, domain.MountTypeVolume, cs.Mounts[1].Type)
	assert.Equal(t, "vol1", cs.Mounts[1].Source)
	assert.Equal(t, "/vol1", cs.Mounts[1].Target)
	assert.False(t, cs.Mounts[1].ReadOnly)

	// Service-level fields
	assert.Equal(t, "container-rt", result.Spec.Name, "Service name should survive round-trip")
	assert.Equal(t, map[string]string{"app": "test"}, result.Spec.Labels, "Service labels should survive round-trip")
}

func TestServiceSpec_RoundTrip_TaskTemplateNetworks(t *testing.T) {
	t.Parallel()

	// This mimics how buildService() populates networks: on TaskTemplate.Networks.
	// convertToSwarmSpec reads from both spec.Networks and spec.TaskTemplate.Networks.
	original := &domain.ServiceSpec{
		Name: "task-net-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
			},
			Networks: []domain.NetworkAttachment{
				{Target: "frontend-net", Aliases: []string{"web", "proxy"}},
				{Target: "backend-net", Aliases: []string{"api"}},
			},
		},
	}

	// First, verify the toSwarm direction: TaskTemplate.Networks should appear
	// in the swarm spec's TaskTemplate.Networks.
	swarmSpec := convertToSwarmSpec(original)
	require.Len(t, swarmSpec.TaskTemplate.Networks, 2,
		"convertToSwarmSpec should include TaskTemplate.Networks")
	assert.Equal(t, "frontend-net", swarmSpec.TaskTemplate.Networks[0].Target)
	assert.Equal(t, []string{"web", "proxy"}, swarmSpec.TaskTemplate.Networks[0].Aliases)
	assert.Equal(t, "backend-net", swarmSpec.TaskTemplate.Networks[1].Target)
	assert.Equal(t, []string{"api"}, swarmSpec.TaskTemplate.Networks[1].Aliases)

	// Full round-trip
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	// Check that networks survived the full round-trip (either in Networks or TaskTemplate.Networks)
	allNetworks := append(result.Spec.Networks, result.Spec.TaskTemplate.Networks...)
	require.Len(t, allNetworks, 2,
		"Networks set on TaskTemplate should survive the full round-trip")
}

func TestServiceSpec_RoundTrip_ServiceSpecNetworks(t *testing.T) {
	t.Parallel()

	// Networks set on ServiceSpec.Networks -- this is the location convertToSwarmSpec reads from.
	original := &domain.ServiceSpec{
		Name: "spec-net-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
			},
		},
		Networks: []domain.NetworkAttachment{
			{Target: "overlay-1", Aliases: []string{"svc-a"}},
			{Target: "overlay-2", Aliases: []string{"svc-b", "svc-c"}},
		},
	}

	// toSwarm direction should work: spec.Networks -> swarmSpec.TaskTemplate.Networks
	swarmSpec := convertToSwarmSpec(original)
	require.Len(t, swarmSpec.TaskTemplate.Networks, 2, "toSwarm should convert spec.Networks")
	assert.Equal(t, "overlay-1", swarmSpec.TaskTemplate.Networks[0].Target)
	assert.Equal(t, "overlay-2", swarmSpec.TaskTemplate.Networks[1].Target)

	// Full round-trip: fromSwarm should convert networks back
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	allNetworks := append(result.Spec.Networks, result.Spec.TaskTemplate.Networks...)
	require.Len(t, allNetworks, 2, "Networks should survive round-trip")
	assert.Equal(t, "overlay-1", allNetworks[0].Target)
	assert.Equal(t, []string{"svc-a"}, allNetworks[0].Aliases)
	assert.Equal(t, "overlay-2", allNetworks[1].Target)
	assert.Equal(t, []string{"svc-b", "svc-c"}, allNetworks[1].Aliases)
}

func TestServiceSpec_RoundTrip_RestartPolicy(t *testing.T) {
	t.Parallel()

	original := &domain.ServiceSpec{
		Name: "restart-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "alpine:latest",
			},
			RestartPolicy: &domain.ServiceRestartPolicy{
				Condition:   domain.RestartConditionOnFailure,
				Delay:       new(10 * time.Second),
				MaxAttempts: new(uint64(5)),
				Window:      new(3 * time.Minute),
			},
		},
	}

	// toSwarm direction should work (already tested in unit tests)
	swarmSpec := convertToSwarmSpec(original)
	require.NotNil(t, swarmSpec.TaskTemplate.RestartPolicy, "toSwarm should convert restart policy")
	assert.Equal(t, swarm.RestartPolicyCondition("on-failure"), swarmSpec.TaskTemplate.RestartPolicy.Condition)

	// Full round-trip
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	rp := result.Spec.TaskTemplate.RestartPolicy
	require.NotNil(t, rp, "RestartPolicy should survive round-trip")
	assert.Equal(t, domain.RestartConditionOnFailure, rp.Condition)
	require.NotNil(t, rp.Delay)
	assert.Equal(t, 10*time.Second, *rp.Delay)
	require.NotNil(t, rp.MaxAttempts)
	assert.Equal(t, uint64(5), *rp.MaxAttempts)
	require.NotNil(t, rp.Window)
	assert.Equal(t, 3*time.Minute, *rp.Window)
}

func TestServiceSpec_RoundTrip_Resources(t *testing.T) {
	t.Parallel()

	original := &domain.ServiceSpec{
		Name: "resources-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
			},
			Resources: &domain.ResourceRequirements{
				Limits: &domain.Resources{
					NanoCPUs:    2000000000, // 2 CPUs
					MemoryBytes: 1073741824, // 1 GiB
				},
				Reservations: &domain.Resources{
					NanoCPUs:    500000000, // 0.5 CPU
					MemoryBytes: 268435456, // 256 MiB
				},
			},
		},
	}

	// toSwarm direction should work
	swarmSpec := convertToSwarmSpec(original)
	require.NotNil(t, swarmSpec.TaskTemplate.Resources)
	require.NotNil(t, swarmSpec.TaskTemplate.Resources.Limits)
	assert.Equal(t, int64(2000000000), swarmSpec.TaskTemplate.Resources.Limits.NanoCPUs)
	assert.Equal(t, int64(1073741824), swarmSpec.TaskTemplate.Resources.Limits.MemoryBytes)
	require.NotNil(t, swarmSpec.TaskTemplate.Resources.Reservations)
	assert.Equal(t, int64(500000000), swarmSpec.TaskTemplate.Resources.Reservations.NanoCPUs)
	assert.Equal(t, int64(268435456), swarmSpec.TaskTemplate.Resources.Reservations.MemoryBytes)

	// Full round-trip
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	res := result.Spec.TaskTemplate.Resources
	require.NotNil(t, res, "Resources should survive round-trip")
	require.NotNil(t, res.Limits, "Resource Limits should survive round-trip")
	assert.Equal(t, int64(2000000000), res.Limits.NanoCPUs)
	assert.Equal(t, int64(1073741824), res.Limits.MemoryBytes)
	require.NotNil(t, res.Reservations, "Resource Reservations should survive round-trip")
	assert.Equal(t, int64(500000000), res.Reservations.NanoCPUs)
	assert.Equal(t, int64(268435456), res.Reservations.MemoryBytes)
}

func TestServiceSpec_RoundTrip_ModeReplicated(t *testing.T) {
	t.Parallel()

	original := &domain.ServiceSpec{
		Name: "replicated-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
			},
		},
		Mode: domain.ServiceMode{
			Replicated: &domain.ReplicatedService{
				Replicas: new(uint64(3)),
			},
		},
	}

	// toSwarm direction should work
	swarmSpec := convertToSwarmSpec(original)
	require.NotNil(t, swarmSpec.Mode.Replicated)
	require.NotNil(t, swarmSpec.Mode.Replicated.Replicas)
	assert.Equal(t, uint64(3), *swarmSpec.Mode.Replicated.Replicas)

	// Full round-trip
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	require.NotNil(t, result.Spec.Mode.Replicated, "Mode.Replicated should survive round-trip")
	require.NotNil(t, result.Spec.Mode.Replicated.Replicas)
	assert.Equal(t, uint64(3), *result.Spec.Mode.Replicated.Replicas)
	assert.Nil(t, result.Spec.Mode.Global, "Global should be nil for replicated mode")
}

func TestServiceSpec_RoundTrip_ModeGlobal(t *testing.T) {
	t.Parallel()

	original := &domain.ServiceSpec{
		Name: "global-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
			},
		},
		Mode: domain.ServiceMode{
			Global: &domain.GlobalService{},
		},
	}

	// toSwarm direction should work
	swarmSpec := convertToSwarmSpec(original)
	require.NotNil(t, swarmSpec.Mode.Global)
	assert.Nil(t, swarmSpec.Mode.Replicated)

	// Full round-trip
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	require.NotNil(t, result.Spec.Mode.Global, "Mode.Global should survive round-trip")
	assert.Nil(t, result.Spec.Mode.Replicated, "Replicated should be nil for global mode")
}

func TestServiceSpec_RoundTrip_Mounts(t *testing.T) {
	t.Parallel()

	original := &domain.ServiceSpec{
		Name: "mounts-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
				Mounts: []domain.ServiceMount{
					{
						Type:     domain.MountTypeBind,
						Source:   "/etc/config",
						Target:   "/app/config",
						ReadOnly: true,
					},
					{
						Type:     domain.MountTypeVolume,
						Source:   "app-data",
						Target:   "/app/data",
						ReadOnly: false,
					},
				},
			},
		},
	}

	// toSwarm direction should work
	swarmSpec := convertToSwarmSpec(original)
	require.Len(t, swarmSpec.TaskTemplate.ContainerSpec.Mounts, 2)
	assert.Equal(t, "/etc/config", swarmSpec.TaskTemplate.ContainerSpec.Mounts[0].Source)
	assert.Equal(t, "/app/config", swarmSpec.TaskTemplate.ContainerSpec.Mounts[0].Target)
	assert.True(t, swarmSpec.TaskTemplate.ContainerSpec.Mounts[0].ReadOnly)
	assert.Equal(t, "app-data", swarmSpec.TaskTemplate.ContainerSpec.Mounts[1].Source)
	assert.Equal(t, "/app/data", swarmSpec.TaskTemplate.ContainerSpec.Mounts[1].Target)
	assert.False(t, swarmSpec.TaskTemplate.ContainerSpec.Mounts[1].ReadOnly)

	// Full round-trip
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	mounts := result.Spec.TaskTemplate.ContainerSpec.Mounts
	require.Len(t, mounts, 2, "Mounts should survive round-trip")
	assert.Equal(t, domain.MountTypeBind, mounts[0].Type)
	assert.Equal(t, "/etc/config", mounts[0].Source)
	assert.Equal(t, "/app/config", mounts[0].Target)
	assert.True(t, mounts[0].ReadOnly)
	assert.Equal(t, domain.MountTypeVolume, mounts[1].Type)
	assert.Equal(t, "app-data", mounts[1].Source)
	assert.Equal(t, "/app/data", mounts[1].Target)
	assert.False(t, mounts[1].ReadOnly)
}

func TestServiceSpec_RoundTrip_NilFields(t *testing.T) {
	t.Parallel()

	// Minimal spec — only required Image. Everything else nil/zero.
	// Verifies nil stays nil after round-trip (not converted to empty slice).
	original := &domain.ServiceSpec{
		Name: "minimal-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "alpine:latest",
			},
		},
	}

	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	assert.Equal(t, "minimal-rt", result.Spec.Name)
	assert.Equal(t, "alpine:latest", result.Spec.TaskTemplate.ContainerSpec.Image)

	// These should all remain nil/empty — not be converted to empty slices or zero structs
	assert.Nil(t, result.Spec.TaskTemplate.RestartPolicy, "nil RestartPolicy should stay nil")
	assert.Nil(t, result.Spec.TaskTemplate.Resources, "nil Resources should stay nil")
	assert.Empty(t, result.Spec.TaskTemplate.Networks, "nil Networks should stay empty")
	assert.Empty(t, result.Spec.Networks, "nil ServiceSpec.Networks should stay empty")
	assert.Empty(t, result.Spec.TaskTemplate.ContainerSpec.Mounts, "nil Mounts should stay empty")
	assert.Nil(t, result.Spec.Mode.Replicated, "nil Replicated should stay nil")
	assert.Nil(t, result.Spec.Mode.Global, "nil Global should stay nil")

	// Zero-value strings/bools should stay zero
	assert.Empty(t, result.Spec.TaskTemplate.ContainerSpec.Hostname)
	assert.Empty(t, result.Spec.TaskTemplate.ContainerSpec.Dir)
	assert.Empty(t, result.Spec.TaskTemplate.ContainerSpec.User)
	assert.Empty(t, result.Spec.TaskTemplate.ContainerSpec.Env)
	assert.Empty(t, result.Spec.TaskTemplate.ContainerSpec.Command)
	assert.False(t, result.Spec.TaskTemplate.ContainerSpec.TTY)
	assert.False(t, result.Spec.TaskTemplate.ContainerSpec.OpenStdin)
}

func TestServiceSpec_RoundTrip_RestartConditionNone(t *testing.T) {
	t.Parallel()

	// buildService() uses RestartConditionNone with MaxAttempts=1 — test the exact
	// values used in production.
	maxAttempts := uint64(1)
	original := &domain.ServiceSpec{
		Name: "condnone-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "alpine:latest",
			},
			RestartPolicy: &domain.ServiceRestartPolicy{
				Condition:   domain.RestartConditionNone,
				MaxAttempts: &maxAttempts,
			},
		},
	}

	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	rp := result.Spec.TaskTemplate.RestartPolicy
	require.NotNil(t, rp, "RestartPolicy should survive round-trip")
	assert.Equal(t, domain.RestartConditionNone, rp.Condition)
	require.NotNil(t, rp.MaxAttempts)
	assert.Equal(t, uint64(1), *rp.MaxAttempts)
	// Delay and Window were not set — should be nil
	assert.Nil(t, rp.Delay, "unset Delay should stay nil")
	assert.Nil(t, rp.Window, "unset Window should stay nil")
}

func TestServiceSpec_RoundTrip_Placement(t *testing.T) {
	t.Parallel()

	original := &domain.ServiceSpec{
		Name: "placement-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
			},
			Placement: &domain.Placement{
				Constraints: []string{"node.role==worker", "node.labels.zone==us-east"},
				Preferences: []domain.PlacementPreference{
					{Spread: &domain.SpreadOver{SpreadDescriptor: "node.labels.zone"}},
				},
			},
		},
	}

	// toSwarm direction
	swarmSpec := convertToSwarmSpec(original)
	require.NotNil(t, swarmSpec.TaskTemplate.Placement, "toSwarm should convert Placement")
	assert.Equal(t, []string{"node.role==worker", "node.labels.zone==us-east"}, swarmSpec.TaskTemplate.Placement.Constraints)
	require.Len(t, swarmSpec.TaskTemplate.Placement.Preferences, 1)
	require.NotNil(t, swarmSpec.TaskTemplate.Placement.Preferences[0].Spread)
	assert.Equal(t, "node.labels.zone", swarmSpec.TaskTemplate.Placement.Preferences[0].Spread.SpreadDescriptor)

	// Full round-trip
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	p := result.Spec.TaskTemplate.Placement
	require.NotNil(t, p, "Placement should survive round-trip")
	assert.Equal(t, []string{"node.role==worker", "node.labels.zone==us-east"}, p.Constraints)
	require.Len(t, p.Preferences, 1)
	require.NotNil(t, p.Preferences[0].Spread)
	assert.Equal(t, "node.labels.zone", p.Preferences[0].Spread.SpreadDescriptor)
}

func TestServiceSpec_RoundTrip_LogDriver(t *testing.T) {
	t.Parallel()

	original := &domain.ServiceSpec{
		Name: "logdriver-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
			},
			LogDriver: &domain.LogDriver{
				Name:    "json-file",
				Options: map[string]string{"max-size": "10m", "max-file": "3"},
			},
		},
	}

	// toSwarm direction
	swarmSpec := convertToSwarmSpec(original)
	require.NotNil(t, swarmSpec.TaskTemplate.LogDriver, "toSwarm should convert LogDriver")
	assert.Equal(t, "json-file", swarmSpec.TaskTemplate.LogDriver.Name)
	assert.Equal(t, map[string]string{"max-size": "10m", "max-file": "3"}, swarmSpec.TaskTemplate.LogDriver.Options)

	// Full round-trip
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	ld := result.Spec.TaskTemplate.LogDriver
	require.NotNil(t, ld, "LogDriver should survive round-trip")
	assert.Equal(t, "json-file", ld.Name)
	assert.Equal(t, map[string]string{"max-size": "10m", "max-file": "3"}, ld.Options)
}

func TestServiceSpec_RoundTrip_EndpointSpec(t *testing.T) {
	t.Parallel()

	original := &domain.ServiceSpec{
		Name: "endpoint-rt",
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: "nginx:latest",
			},
		},
		EndpointSpec: &domain.EndpointSpec{
			Mode: domain.ResolutionModeVIP,
			Ports: []domain.PortConfig{
				{
					Name:          "http",
					Protocol:      domain.PortProtocolTCP,
					TargetPort:    80,
					PublishedPort: 8080,
					PublishMode:   domain.PortPublishModeIngress,
				},
				{
					Name:          "https",
					Protocol:      domain.PortProtocolTCP,
					TargetPort:    443,
					PublishedPort: 8443,
					PublishMode:   domain.PortPublishModeHost,
				},
			},
		},
	}

	// toSwarm direction
	swarmSpec := convertToSwarmSpec(original)
	require.NotNil(t, swarmSpec.EndpointSpec, "toSwarm should convert EndpointSpec")
	assert.Equal(t, swarm.ResolutionModeVIP, swarmSpec.EndpointSpec.Mode)
	require.Len(t, swarmSpec.EndpointSpec.Ports, 2)
	assert.Equal(t, "http", swarmSpec.EndpointSpec.Ports[0].Name)
	assert.Equal(t, network.TCP, swarmSpec.EndpointSpec.Ports[0].Protocol)
	assert.Equal(t, uint32(80), swarmSpec.EndpointSpec.Ports[0].TargetPort)
	assert.Equal(t, uint32(8080), swarmSpec.EndpointSpec.Ports[0].PublishedPort)
	assert.Equal(t, swarm.PortConfigPublishModeIngress, swarmSpec.EndpointSpec.Ports[0].PublishMode)

	// Full round-trip
	result := roundTripServiceSpec(original)
	require.NotNil(t, result)

	ep := result.Spec.EndpointSpec
	require.NotNil(t, ep, "EndpointSpec should survive round-trip")
	assert.Equal(t, domain.ResolutionModeVIP, ep.Mode)
	require.Len(t, ep.Ports, 2)
	assert.Equal(t, "http", ep.Ports[0].Name)
	assert.Equal(t, domain.PortProtocolTCP, ep.Ports[0].Protocol)
	assert.Equal(t, uint32(80), ep.Ports[0].TargetPort)
	assert.Equal(t, uint32(8080), ep.Ports[0].PublishedPort)
	assert.Equal(t, domain.PortPublishModeIngress, ep.Ports[0].PublishMode)
	assert.Equal(t, "https", ep.Ports[1].Name)
	assert.Equal(t, uint32(443), ep.Ports[1].TargetPort)
	assert.Equal(t, uint32(8443), ep.Ports[1].PublishedPort)
	assert.Equal(t, domain.PortPublishModeHost, ep.Ports[1].PublishMode)
}
