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
		{" /host:/container", true, "leading whitespace must NOT bypass — TrimSpace then check"},
		{"\t/host:/container", true, "leading tab must NOT bypass"},
		{"   :/host", false, "whitespace-only source — malformed, not a host mount"},
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

// TestGlobalLabelAllowListBlocksRunJobVolumesFrom pins the sibling-
// vector fix from PR #698 review: a job-run with `volumes-from=<donor>`
// must also be dropped when AllowHostJobsFromLabels=false. Pre-fix, the
// filter checked only `volume`, so an attacker could declare
// `volumes-from=ofelia` (or any donor with host binds) and inherit those
// bind mounts — including /var/run/docker.sock — bypassing the volume=
// filter entirely. The filter now treats any non-empty volumes-from as a
// violation: we cannot inspect the donor at filter time, so the
// conservative drop is the only safe call.
func TestGlobalLabelAllowListBlocksRunJobVolumesFrom(t *testing.T) {
	t.Parallel()
	logger, handler := test.NewTestLoggerWithHandler()
	c := NewConfig(logger)
	c.Global.AllowHostJobsFromLabels = false

	containers := []DockerContainerInfo{
		{
			Name:  "attacker-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                     "true",
				"ofelia.service":                     "true",
				"ofelia.job-run.vf-pwn.schedule":     "@daily",
				"ofelia.job-run.vf-pwn.image":        "alpine",
				"ofelia.job-run.vf-pwn.command":      "sh -c 'ls /var/run/docker.sock'",
				"ofelia.job-run.vf-pwn.volumes-from": "ofelia",
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	assert.Empty(t, c.RunJobs,
		"job-run with volumes-from must be blocked when AllowHostJobsFromLabels=false — inheriting a donor's bind mounts is an escape vector parallel to volume=")
	assert.True(t, handler.HasError("volumes-from"),
		"violation log must name the volumes-from vector class for operator triage")
	assert.True(t, handler.HasError("vf-pwn"),
		"violation log must name the dropped job")
}

// TestGlobalLabelAllowListAllowsRunJobWithoutEscalationVectors confirms
// the per-job filter does not over-block: a runJob with neither host
// mounts nor volumes-from passes through even when the policy is on.
func TestGlobalLabelAllowListAllowsRunJobWithoutEscalationVectors(t *testing.T) {
	t.Parallel()
	logger, _ := test.NewTestLoggerWithHandler()
	c := NewConfig(logger)
	c.Global.AllowHostJobsFromLabels = false

	containers := []DockerContainerInfo{
		{
			Name:  "legit-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":               "true",
				"ofelia.service":               "true",
				"ofelia.job-run.safe.schedule": "@daily",
				"ofelia.job-run.safe.image":    "alpine",
				"ofelia.job-run.safe.command":  "echo ok",
				// no volume, no volumes-from
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	assert.Contains(t, c.RunJobs, "safe",
		"job-run without any escalation vectors must NOT be blocked")
}

// TestExtractHostVolumeMounts_FailsClosedOnUnexpectedType pins the
// fail-closed contract for the type switch in extractHostVolumeMounts.
// A future change to setJobParam (or a new JSON shorthand) that
// delivers []any, map[string]any, etc. through the "volume" key MUST
// drop the job rather than silently bypass the security check. Same
// guarantee for extractVolumesFromSpecs.
func TestExtractHostVolumeMounts_FailsClosedOnUnexpectedType(t *testing.T) {
	t.Parallel()
	logger, _ := test.NewTestLoggerWithHandler()

	hostMounts, safe := extractHostVolumeMounts([]any{"/etc:/host:ro"}, logger, "j")
	assert.False(t, safe,
		"extractHostVolumeMounts must return safe=false for []any (fail closed); a future setJobParam change delivering []any would otherwise silently bypass the policy")
	assert.Empty(t, hostMounts,
		"the host-mount list is empty when the type is unknown — the caller drops the job based on safe=false alone")

	vf, vfSafe := extractVolumesFromSpecs(map[string]any{"donor": true}, logger, "j")
	assert.False(t, vfSafe,
		"extractVolumesFromSpecs must return safe=false for map[string]any (fail closed)")
	assert.Empty(t, vf)
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
