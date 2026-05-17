// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// TestGlobalLabelAllowListBlocksRunJobHostMount pins the security policy
// extension from https://github.com/netresearch/ofelia/issues/462: when
// AllowHostJobsFromLabels=false, a job-run that mounts a host path via
// labels (e.g. volume=/:/host:rw) must be dropped, the same way
// job-local and job-compose already are.
//
// Pre-fix, the policy only covered job-local and job-compose, leaving
// job-run with `volume=/:/host:rw` as an open container-to-host
// privilege-escalation vector. The new filter drops the job entirely
// (consistent with the localJobs/composeJobs pattern in the same
// branch) and logs a SECURITY POLICY VIOLATION with the offending
// volume specs.
func TestGlobalLabelAllowListBlocksRunJobHostMount(t *testing.T) {
	t.Parallel()
	logger, handler := test.NewTestLoggerWithHandler()
	c := NewConfig(logger)
	c.Global.AllowHostJobsFromLabels = false

	containers := []DockerContainerInfo{
		{
			Name:  "attacker-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                   "true",
				"ofelia.service":                   "true",
				"ofelia.job-run.host-pwn.schedule": "@daily",
				"ofelia.job-run.host-pwn.image":    "alpine",
				"ofelia.job-run.host-pwn.command":  "sh -c 'cat /host/etc/shadow'",
				"ofelia.job-run.host-pwn.volume":   "/:/host:rw",
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	assert.Empty(t, c.RunJobs,
		"job-run with host volume mount must be blocked when AllowHostJobsFromLabels=false (#462)")
	assert.True(t, handler.HasError("SECURITY POLICY VIOLATION"),
		"should log a SECURITY POLICY VIOLATION error for the dropped job")
	assert.True(t, handler.HasError("host-pwn"),
		"error message should name the dropped job for operator triage")
	assert.True(t, handler.HasError("/:/host:rw"),
		"error message should include the offending volume spec for operator triage")
}

// TestGlobalLabelAllowListKeepsRunJobWithoutHostMount confirms the
// inverse contract: a job-run that does NOT mount any host paths must
// survive the filter when AllowHostJobsFromLabels=false. Otherwise the
// policy would over-block, breaking legitimate use cases like named
// volumes and anonymous data volumes.
func TestGlobalLabelAllowListKeepsRunJobWithoutHostMount(t *testing.T) {
	t.Parallel()
	logger, _ := test.NewTestLoggerWithHandler()
	c := NewConfig(logger)
	c.Global.AllowHostJobsFromLabels = false

	containers := []DockerContainerInfo{
		{
			Name:  "legit-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                    "true",
				"ofelia.service":                    "true",
				"ofelia.job-run.named-vol.schedule": "@daily",
				"ofelia.job-run.named-vol.image":    "alpine",
				"ofelia.job-run.named-vol.command":  "echo ok",
				"ofelia.job-run.named-vol.volume":   "my-named-volume:/data",
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	assert.Contains(t, c.RunJobs, "named-vol",
		"job-run with named volume (not host path) must NOT be blocked — only host mounts trigger the policy")
}

// TestGlobalLabelAllowListBlocksRunJobHostMountJSONArray confirms the
// filter also catches the multi-volume JSON-array form
// (ofelia.job-run.foo.volume=["...","..."]). A single bad mount in a
// JSON array still drops the entire job, mirroring the localJobs /
// composeJobs "drop wholesale" pattern.
func TestGlobalLabelAllowListBlocksRunJobHostMountJSONArray(t *testing.T) {
	t.Parallel()
	logger, handler := test.NewTestLoggerWithHandler()
	c := NewConfig(logger)
	c.Global.AllowHostJobsFromLabels = false

	containers := []DockerContainerInfo{
		{
			Name:  "attacker-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                "true",
				"ofelia.service":                "true",
				"ofelia.job-run.mixed.schedule": "@daily",
				"ofelia.job-run.mixed.image":    "alpine",
				"ofelia.job-run.mixed.command":  "echo ok",
				"ofelia.job-run.mixed.volume":   `["my-named:/data","/etc:/host-etc:ro"]`,
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	assert.Empty(t, c.RunJobs,
		"a single host mount in a multi-volume job-run must drop the entire job")
	assert.True(t, handler.HasError("/etc:/host-etc:ro"),
		"the error message should name only the offending mount, not the legitimate named volume")
}

// TestIsHostVolumeMount exercises the spec parser on every shape it
// needs to handle. Locks the security predicate's behavior in isolation
// so the integration tests above don't have to enumerate every variant.
func TestIsHostVolumeMount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		spec     string
		wantHost bool
		why      string
	}{
		{"/host:/container", true, "absolute host path"},
		{"/host:/container:ro", true, "absolute host path with options"},
		{"/:/host:rw", true, "root mount — the #462 vector"},
		{"./relative:/container", true, "relative host path (./)"},
		{"~/home:/container", true, "home-relative host path (~/)"},
		{"named-vol:/container", false, "named volume — Docker auto-creates if missing"},
		{"my_volume:/data:ro", false, "named volume with options"},
		{"/container", false, "anonymous volume — target-only, no source"},
		{"", false, "empty spec — malformed but not a host mount"},
		{":/container", false, "empty source — malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			got := isHostVolumeMount(tc.spec)
			assert.Equal(t, tc.wantHost, got,
				"isHostVolumeMount(%q) = %v, want %v (%s)", tc.spec, got, tc.wantHost, tc.why)
		})
	}
}

// TestGlobalLabelAllowListBlocksRunJobAcceptsRunJobsWhenAllowed verifies
// the inverse: when AllowHostJobsFromLabels=true (operator opt-in for
// trusted single-tenant environments), a job-run with a host mount
// passes through unchanged.
func TestGlobalLabelAllowListAcceptsRunJobHostMountWhenAllowed(t *testing.T) {
	t.Parallel()
	logger, _ := test.NewTestLoggerWithHandler()
	c := NewConfig(logger)
	c.Global.AllowHostJobsFromLabels = true

	containers := []DockerContainerInfo{
		{
			Name:  "trusted-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                   "true",
				"ofelia.service":                   "true",
				"ofelia.job-run.host-job.schedule": "@daily",
				"ofelia.job-run.host-job.image":    "alpine",
				"ofelia.job-run.host-job.command":  "echo ok",
				"ofelia.job-run.host-job.volume":   "/host:/container",
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	assert.Contains(t, c.RunJobs, "host-job",
		"AllowHostJobsFromLabels=true must permit host-mounting job-runs from labels (operator opt-in)")
}
