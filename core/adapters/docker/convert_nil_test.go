// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"errors"
	"testing"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	networktypes "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/stretchr/testify/assert"

	"github.com/netresearch/ofelia/core/domain"
)

// Regression tests for #622 / #632. PR #626 nil-guarded the *To*Swarm/*Mount
// converters; the file also covers the symmetric *From* helpers and the
// ContainerServiceAdapter.Create entry path that still dereferenced their
// pointer arguments unconditionally.
//
// The helpers under test are package-private. Production callers always
// pass non-nil pointers today, but the helper signatures invite unsafe
// direct calls from tests/refactors and the public Create() path passes
// the config straight through. Same bug class as #619 (panics in Exec).

// TestConvertFromSwarmService_NilInput pins the contract that
// convertFromSwarmService returns nil (no panic) when called with a nil
// *swarm.Service. Latent today (only test-callable), guarded for symmetry
// with convertToSwarmSpec.
func TestConvertFromSwarmService_NilInput(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertFromSwarmService(nil)")()

	if got := convertFromSwarmService(nil); got != nil {
		t.Errorf("convertFromSwarmService(nil) = %+v, want nil", got)
	}
}

// TestConvertTaskTemplateFromSwarm_NilInputs pins the contract that
// convertTaskTemplateFromSwarm is a no-op (no panic) when either src or
// dst is nil. Mirrors the convertTaskTemplateToSwarm guard from #626.
func TestConvertTaskTemplateFromSwarm_NilInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  *swarm.TaskSpec
		dst  *domain.TaskSpec
	}{
		{name: "nil src", src: nil, dst: &domain.TaskSpec{}},
		{name: "nil dst", src: &swarm.TaskSpec{}, dst: nil},
		{name: "both nil", src: nil, dst: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer failOnPanic(t, "convertTaskTemplateFromSwarm "+tt.name)()
			convertTaskTemplateFromSwarm(tt.src, tt.dst)
		})
	}
}

// TestConvertFromSwarmTask_NilInput pins the contract that
// convertFromSwarmTask returns the zero domain.Task (no panic) when
// called with nil. Reachable through SwarmServiceAdapter.WaitForServiceTasks
// if the SDK ever yields a nil entry — defense-in-depth.
func TestConvertFromSwarmTask_NilInput(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertFromSwarmTask(nil)")()

	got := convertFromSwarmTask(nil)
	if got.ID != "" || got.ServiceID != "" || got.NodeID != "" {
		t.Errorf("convertFromSwarmTask(nil) = %+v, want zero domain.Task", got)
	}
	if got.Status.ContainerStatus != nil {
		t.Errorf("convertFromSwarmTask(nil).Status.ContainerStatus = %+v, want nil",
			got.Status.ContainerStatus)
	}
	if !got.CreatedAt.IsZero() || !got.UpdatedAt.IsZero() {
		t.Errorf("convertFromSwarmTask(nil) timestamps = %v / %v, want zero",
			got.CreatedAt, got.UpdatedAt)
	}
}

// TestContainerServiceAdapter_Create_NilConfig pins the contract that
// Create returns ErrNilContainerConfig (and does NOT panic) when called
// with a nil *domain.ContainerConfig.
//
// Before the fix this panicked on `config.HostConfig` / `config.NetworkConfig`
// at container.go:47-48 because both sub-config converters were called
// unconditionally on the embedded pointer fields.
//
// Uses a loopback SDK client so the new input-validation guard fires
// before the daemon dial would fail (and before the existing
// ErrNilDockerClient guard).
func TestContainerServiceAdapter_Create_NilConfig(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "ContainerServiceAdapter.Create with nil config")()

	adapter := &ContainerServiceAdapter{client: newLoopbackSDKClient(t)}

	id, err := adapter.Create(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
	if id != "" {
		t.Errorf("expected empty container ID on error, got %q", id)
	}
	if !errors.Is(err, ErrNilContainerConfig) {
		t.Errorf("expected errors.Is(err, ErrNilContainerConfig), got: %v", err)
	}
}

// TestConvertToSwarmSpec_Nil pins the contract that convertToSwarmSpec
// returns a zero-value swarm.ServiceSpec (and does NOT panic) when called
// with a nil *domain.ServiceSpec.
//
// Before the fix this panicked on `spec.Name` at service.go:182.
func TestConvertToSwarmSpec_Nil(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertToSwarmSpec(nil)")()

	result := convertToSwarmSpec(nil)
	assert.Equal(t, swarm.ServiceSpec{}, result)
}

// TestConvertTaskTemplateToSwarm_NilSrc pins the contract that
// convertTaskTemplateToSwarm leaves dst untouched (and does NOT panic) when
// called with a nil src.
//
// Before the fix this panicked on `src.ContainerSpec.Image` at service.go:232.
func TestConvertTaskTemplateToSwarm_NilSrc(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertTaskTemplateToSwarm(nil, &dst)")()

	dst := swarm.TaskSpec{}
	convertTaskTemplateToSwarm(nil, &dst)
	assert.Equal(t, swarm.TaskSpec{}, dst, "dst should remain zero when src is nil")
}

// TestConvertTaskTemplateToSwarm_NilDst pins the contract that
// convertTaskTemplateToSwarm becomes a no-op (and does NOT panic) when
// called with a nil dst. The helper writes through dst, so a nil dst would
// otherwise nil-deref on the very first assignment.
func TestConvertTaskTemplateToSwarm_NilDst(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertTaskTemplateToSwarm(&src, nil)")()

	src := &domain.TaskSpec{
		ContainerSpec: domain.ContainerSpec{Image: "nginx"},
	}
	convertTaskTemplateToSwarm(src, nil)
}

// TestConvertToMount_Nil pins the contract that convertToMount returns a
// zero-value mount.Mount (and does NOT panic) when called with a nil
// *domain.Mount.
//
// Before the fix this panicked on `m.Type` at container.go:392.
func TestConvertToMount_Nil(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertToMount(nil)")()

	result := convertToMount(nil)
	assert.Equal(t, mount.Mount{}, result)
}

// Regression tests for #654. PR #648 nil-guarded the *From* swarm/event
// helpers; sibling-hunt found 4 more helpers in convert.go / container.go
// with the same defense-in-depth gap.

// TestConvertFromAPIContainer_NilInput pins the contract that
// convertFromAPIContainer returns the zero domain.Container (no panic)
// when called with a nil *containertypes.Summary. The previous code
// dereferenced c.Names / c.ID / c.Image / c.Created / c.Labels / c.State
// unconditionally.
func TestConvertFromAPIContainer_NilInput(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertFromAPIContainer(nil)")()

	got := convertFromAPIContainer(nil)
	if got.ID != "" || got.Name != "" || got.Image != "" {
		t.Errorf("convertFromAPIContainer(nil) = %+v, want zero domain.Container", got)
	}
	if got.Labels != nil {
		t.Errorf("convertFromAPIContainer(nil).Labels = %+v, want nil", got.Labels)
	}
	if got.State.Running {
		t.Errorf("convertFromAPIContainer(nil).State.Running = true, want false")
	}
	if !got.Created.IsZero() {
		t.Errorf("convertFromAPIContainer(nil).Created = %v, want zero", got.Created)
	}
}

// TestConvertFromAPIContainer_ValidInput sanity-checks that the new nil
// guard does not regress the happy path. Mirrors the live ContainerService
// list translation.
func TestConvertFromAPIContainer_ValidInput(t *testing.T) {
	t.Parallel()

	in := &containertypes.Summary{
		ID:      "abc123",
		Names:   []string{"/my-container"},
		Image:   "nginx:latest",
		Created: 1_700_000_000,
		Labels:  map[string]string{"env": "prod"},
		State:   containertypes.StateRunning,
	}

	got := convertFromAPIContainer(in)
	if got.ID != "abc123" {
		t.Errorf("ID = %q, want %q", got.ID, "abc123")
	}
	if got.Name != "my-container" {
		// Leading slash must be stripped (issue #422).
		t.Errorf("Name = %q, want %q", got.Name, "my-container")
	}
	if got.Image != "nginx:latest" {
		t.Errorf("Image = %q, want %q", got.Image, "nginx:latest")
	}
	if got.Labels["env"] != "prod" {
		t.Errorf("Labels[env] = %q, want %q", got.Labels["env"], "prod")
	}
	if !got.State.Running {
		t.Errorf("State.Running = false, want true")
	}
}

// TestConvertFromNetworkResource_NilInput pins the contract that
// convertFromNetworkResource returns the zero domain.Network (no panic)
// when called with a nil *networktypes.Summary. The previous code
// dereferenced n.Name / n.IPAM.Driver / n.Containers unconditionally.
func TestConvertFromNetworkResource_NilInput(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertFromNetworkResource(nil)")()

	got := convertFromNetworkResource(nil)
	if got.Name != "" || got.ID != "" || got.Driver != "" {
		t.Errorf("convertFromNetworkResource(nil) = %+v, want zero domain.Network", got)
	}
	if got.IPAM.Driver != "" || len(got.IPAM.Config) > 0 {
		t.Errorf("convertFromNetworkResource(nil).IPAM = %+v, want zero", got.IPAM)
	}
	if got.Containers != nil {
		t.Errorf("convertFromNetworkResource(nil).Containers = %+v, want nil", got.Containers)
	}
}

// TestConvertFromNetworkResource_ValidInput sanity-checks that the new
// nil guard does not regress the happy path.
func TestConvertFromNetworkResource_ValidInput(t *testing.T) {
	t.Parallel()

	in := &networktypes.Summary{
		ID:     "net-1",
		Name:   "bridge",
		Driver: "bridge",
		Scope:  "local",
	}

	got := convertFromNetworkResource(in)
	if got.ID != "net-1" {
		t.Errorf("ID = %q, want %q", got.ID, "net-1")
	}
	if got.Name != "bridge" {
		t.Errorf("Name = %q, want %q", got.Name, "bridge")
	}
	if got.Driver != "bridge" {
		t.Errorf("Driver = %q, want %q", got.Driver, "bridge")
	}
	if got.Scope != "local" {
		t.Errorf("Scope = %q, want %q", got.Scope, "local")
	}
}

// TestConvertFromNetworkInspect_NilInput pins the contract that
// convertFromNetworkInspect returns nil (no panic) when called with a nil
// *networktypes.Inspect. The previous code dereferenced n.Name / n.IPAM /
// n.Containers unconditionally; the pointer-returning shape mirrors
// convertFromSwarmService(nil) -> nil from #648.
func TestConvertFromNetworkInspect_NilInput(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertFromNetworkInspect(nil)")()

	if got := convertFromNetworkInspect(nil); got != nil {
		t.Errorf("convertFromNetworkInspect(nil) = %+v, want nil", got)
	}
}

// TestConvertFromNetworkInspect_ValidInput sanity-checks that the new nil
// guard does not regress the happy path.
func TestConvertFromNetworkInspect_ValidInput(t *testing.T) {
	t.Parallel()

	in := &networktypes.Inspect{
		ID:     "net-2",
		Name:   "custom",
		Driver: "overlay",
		Scope:  "swarm",
	}

	got := convertFromNetworkInspect(in)
	if got == nil {
		t.Fatal("convertFromNetworkInspect(valid) = nil, want non-nil")
	}
	if got.ID != "net-2" {
		t.Errorf("ID = %q, want %q", got.ID, "net-2")
	}
	if got.Name != "custom" {
		t.Errorf("Name = %q, want %q", got.Name, "custom")
	}
	if got.Driver != "overlay" {
		t.Errorf("Driver = %q, want %q", got.Driver, "overlay")
	}
}

// (Note: convertToMount(nil) is exercised by TestConvertToMount_Nil above —
// PR #654's TestConvertToMount_NilInput would have been a literal duplicate
// after the rebase merged with #626's existing test, so it is dropped here.)

// TestConvertHelpers_ZeroValueArg verifies that the convert helpers also
// behave correctly with a non-nil but zero-value pointer argument — the
// other half of the table-driven nil/zero/valid contract demanded by
// #622's acceptance criteria.
func TestConvertHelpers_ZeroValueArg(t *testing.T) {
	t.Parallel()

	t.Run("convertToSwarmSpec zero", func(t *testing.T) {
		t.Parallel()
		defer failOnPanic(t, "convertToSwarmSpec(&zero)")()
		result := convertToSwarmSpec(&domain.ServiceSpec{})
		assert.Empty(t, result.Annotations.Name)
		assert.Nil(t, result.Mode.Replicated)
		assert.Nil(t, result.Mode.Global)
	})

	t.Run("convertTaskTemplateToSwarm zero src", func(t *testing.T) {
		t.Parallel()
		defer failOnPanic(t, "convertTaskTemplateToSwarm(&zero, &dst)")()
		dst := swarm.TaskSpec{}
		convertTaskTemplateToSwarm(&domain.TaskSpec{}, &dst)
		// Zero-value src still produces an empty ContainerSpec on dst (the
		// helper unconditionally writes one — that's the existing behavior
		// and not something this fix changes).
		assert.NotNil(t, dst.ContainerSpec)
		assert.Empty(t, dst.ContainerSpec.Image)
		assert.Nil(t, dst.RestartPolicy)
	})

	t.Run("convertToMount zero", func(t *testing.T) {
		t.Parallel()
		defer failOnPanic(t, "convertToMount(&zero)")()
		result := convertToMount(&domain.Mount{})
		assert.Equal(t, mount.Mount{}, result)
	})
}

// TestConvertToMount_ValidInput sanity-checks that the new nil guard
// does not regress the happy path. Complements TestConvertHelpers_ZeroValueArg
// by asserting the mapping logic actually copies fields end-to-end. See #654.
func TestConvertToMount_ValidInput(t *testing.T) {
	t.Parallel()

	in := &domain.Mount{
		Type:     domain.MountType("bind"),
		Source:   "/host/path",
		Target:   "/container/path",
		ReadOnly: true,
	}

	got := convertToMount(in)
	if got.Type != mount.TypeBind {
		t.Errorf("Type = %q, want %q", got.Type, mount.TypeBind)
	}
	if got.Source != "/host/path" {
		t.Errorf("Source = %q, want %q", got.Source, "/host/path")
	}
	if got.Target != "/container/path" {
		t.Errorf("Target = %q, want %q", got.Target, "/container/path")
	}
	if !got.ReadOnly {
		t.Errorf("ReadOnly = false, want true")
	}
}
