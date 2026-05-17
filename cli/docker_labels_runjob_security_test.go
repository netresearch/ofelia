// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// runHostJobPolicy is the shared scaffolding for the
// AllowHostJobsFromLabels integration tests for job-run / job-service-run
// (see https://github.com/netresearch/ofelia/issues/462). Each caller
// supplies the policy bit and a single attacker-container's labels;
// the helper returns the parsed Config and the captured log handler so
// the per-test assertions can pin (a) which jobs survived the filter
// and (b) the SECURITY POLICY VIOLATION log content for triage.
//
// Centralizing the boilerplate keeps SonarCloud's "duplicated code on
// new lines" gate happy without compromising per-test readability — the
// test bodies still each read top-to-bottom as "set up labels, assert
// expected job survival, assert expected log contents".
func runHostJobPolicy(t *testing.T, allowHost bool, labels map[string]string) (*Config, *test.Handler) {
	t.Helper()
	logger, handler := test.NewTestLoggerWithHandler()
	c := NewConfig(logger)
	c.Global.AllowHostJobsFromLabels = allowHost

	containers := []DockerContainerInfo{
		{
			Name:   "test-container",
			State:  domain.ContainerState{Running: true},
			Labels: labels,
		},
	}

	require.NoError(t, c.buildFromDockerContainers(containers))
	return c, handler
}

// baseRunJobLabels returns the minimum labels needed to enable a
// runJob from a service container, parametrised by name.
func baseRunJobLabels(name string) map[string]string {
	return map[string]string{
		"ofelia.enabled":                       "true",
		"ofelia.service":                       "true",
		"ofelia.job-run." + name + ".schedule": "@daily",
		"ofelia.job-run." + name + ".image":    "alpine",
		"ofelia.job-run." + name + ".command":  "echo ok",
	}
}

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
	labels := baseRunJobLabels("host-pwn")
	labels["ofelia.job-run.host-pwn.volume"] = "/:/host:rw"

	c, handler := runHostJobPolicy(t, false, labels)

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
	labels := baseRunJobLabels("named-vol")
	labels["ofelia.job-run.named-vol.volume"] = "my-named-volume:/data"

	c, _ := runHostJobPolicy(t, false, labels)

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
	labels := baseRunJobLabels("mixed")
	labels["ofelia.job-run.mixed.volume"] = `["my-named:/data","/etc:/host-etc:ro"]`

	c, handler := runHostJobPolicy(t, false, labels)

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
	for i, tc := range cases {
		// Use index+why for the subtest name: tc.spec can be empty or
		// contain whitespace/colons that t.Run will rewrite (per Copilot
		// review on PR #698).
		name := fmt.Sprintf("%02d_%s", i, tc.why)
		t.Run(name, func(t *testing.T) {
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
	labels := baseRunJobLabels("vf-pwn")
	labels["ofelia.job-run.vf-pwn.volumes-from"] = "ofelia"

	c, handler := runHostJobPolicy(t, false, labels)

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
	c, _ := runHostJobPolicy(t, false, baseRunJobLabels("safe"))

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
	logger := slog.New(slog.DiscardHandler)

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

// TestGlobalLabelAllowListBlocksServiceJobHostMount pins the
// job-service-run coverage flagged by Gemini's security-high review on
// PR #698. RunServiceJob.Volume has the same shape and same Docker SDK
// landing site as RunJob.Volume — a Swarm-orchestrated job that mounts
// host paths is the same container-to-host escape vector despite the
// different orchestration layer. Without this test, a future code
// change that re-introduces the job-service-run bypass would pass CI.
func TestGlobalLabelAllowListBlocksServiceJobHostMount(t *testing.T) {
	t.Parallel()
	labels := map[string]string{
		"ofelia.enabled": "true",
		"ofelia.service": "true",
		"ofelia.job-service-run.svc-pwn.schedule": "@daily",
		"ofelia.job-service-run.svc-pwn.image":    "alpine",
		"ofelia.job-service-run.svc-pwn.command":  "sh -c 'cat /host/etc/shadow'",
		"ofelia.job-service-run.svc-pwn.volume":   "/:/host:rw",
	}

	c, handler := runHostJobPolicy(t, false, labels)

	assert.Empty(t, c.ServiceJobs,
		"job-service-run with host volume mount must be blocked when AllowHostJobsFromLabels=false — same vector as job-run (#462)")
	assert.True(t, handler.HasError("job-service-run"),
		"violation log must name the job-service-run vector class for operator triage")
	assert.True(t, handler.HasError("svc-pwn"),
		"violation log must name the dropped service job")
}

// TestGlobalLabelAllowListAcceptsRunJobHostMountWhenAllowed verifies the
// inverse: when AllowHostJobsFromLabels=true (operator opt-in for
// trusted single-tenant environments), a job-run with a host mount
// passes through unchanged.
func TestGlobalLabelAllowListAcceptsRunJobHostMountWhenAllowed(t *testing.T) {
	t.Parallel()
	labels := baseRunJobLabels("host-job")
	labels["ofelia.job-run.host-job.volume"] = "/host:/container"

	c, _ := runHostJobPolicy(t, true, labels)

	assert.Contains(t, c.RunJobs, "host-job",
		"AllowHostJobsFromLabels=true must permit host-mounting job-runs from labels (operator opt-in)")
}
