// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file pins the fix for GHSA-h7m7-v83x-vfp3: privilege-bearing job
// keys sourced from container labels (privileged, env-file, env-from) must
// be stripped when AllowHostJobsFromLabels=false, exactly as the volume /
// volumes-from vectors from #462 already are. Unlike those (which drop the
// whole job), these keys are stripped in place so the job still runs — just
// unprivileged and without host / cross-container environment injection.
//
// Pre-fix, filterJobsWithHostEscalation covered only job-run /
// job-service-run and only inspected volume / volumes-from, so:
//   - a job-exec with privileged=true reached `docker exec --privileged`;
//   - env-file / env-from leaked host files and sibling-container
//     environments on every job type, including the two the filter did run
//     on.
// These tests fail on the pre-fix tree (the keys survive) and pass once the
// strip is wired into buildFromDockerContainers.

// baseExecJobLabels returns the minimum labels needed to enable a job-exec
// on the (running) attacker container itself. job-exec runs on non-service
// containers, so no ofelia.service label is required.
func baseExecJobLabels(name string) map[string]string {
	return map[string]string{
		"ofelia.enabled":                        "true",
		"ofelia.job-exec." + name + ".schedule": "@daily",
		"ofelia.job-exec." + name + ".command":  "echo ok",
	}
}

// theExecJob returns the single parsed exec job. job-exec names are scoped
// per [global] job-exec-label-scope, so the map key is not the bare job
// name; the tests declare exactly one, so read it back by iteration.
func theExecJob(t *testing.T, c *Config) *ExecJobConfig {
	t.Helper()
	require.Len(t, c.ExecJobs, 1, "expected exactly one parsed exec job")
	for _, j := range c.ExecJobs {
		return j
	}
	return nil
}

// TestLabelPolicyStripsPrivilegedFromExecJob is the primary GHSA vector:
// a self-labeling container declares a privileged job-exec, and with the
// default policy (AllowHostJobsFromLabels=false) the privileged bit must
// not survive into the parsed job — otherwise it reaches
// `docker exec --privileged` and enables a container escape.
func TestLabelPolicyStripsPrivilegedFromExecJob(t *testing.T) {
	t.Parallel()
	labels := baseExecJobLabels("pwn")
	labels["ofelia.job-exec.pwn.privileged"] = "true"

	c, handler := runHostJobPolicy(t, false, labels)

	job := theExecJob(t, c)
	assert.False(t, job.Privileged,
		"privileged=true from a container label must be stripped when AllowHostJobsFromLabels=false (GHSA-h7m7-v83x-vfp3)")
	assert.True(t, handler.HasError("SECURITY POLICY VIOLATION"),
		"stripping a privilege-bearing key must log a SECURITY POLICY VIOLATION for operator triage")
	assert.True(t, handler.HasError("job-exec"),
		"violation log must name the job-exec type")
	assert.True(t, handler.HasError("pwn"),
		"violation log must name the job for operator triage")
}

// TestLabelPolicyStripsEnvFileFromExecJob pins the env-file file-disclosure
// vector on job-exec: env-file reads a path in ofelia's filesystem view and
// injects it as the job env, so a label-sourced env-file must be stripped.
func TestLabelPolicyStripsEnvFileFromExecJob(t *testing.T) {
	t.Parallel()
	labels := baseExecJobLabels("leak")
	labels["ofelia.job-exec.leak.env-file"] = "/root/.aws/credentials"

	c, handler := runHostJobPolicy(t, false, labels)

	job := theExecJob(t, c)
	assert.Empty(t, job.EnvFile,
		"env-file from a container label must be stripped when AllowHostJobsFromLabels=false — it reads files from ofelia's filesystem view (GHSA-h7m7-v83x-vfp3)")
	assert.True(t, handler.HasError("SECURITY POLICY VIOLATION"),
		"stripping env-file must log a SECURITY POLICY VIOLATION")
	assert.True(t, handler.HasError("env-file"),
		"violation log must name the env-file vector for operator triage")
}

// TestLabelPolicyStripsEnvFromFromExecJob pins the env-from cross-container
// secret-theft vector on job-exec: env-from copies another container's
// entire environment, so a label-sourced env-from must be stripped.
func TestLabelPolicyStripsEnvFromFromExecJob(t *testing.T) {
	t.Parallel()
	labels := baseExecJobLabels("steal")
	labels["ofelia.job-exec.steal.env-from"] = `["victim-container"]`

	c, handler := runHostJobPolicy(t, false, labels)

	job := theExecJob(t, c)
	assert.Empty(t, job.EnvFrom,
		"env-from from a container label must be stripped when AllowHostJobsFromLabels=false — it copies a sibling container's whole environment (GHSA-h7m7-v83x-vfp3)")
	assert.True(t, handler.HasError("env-from"),
		"violation log must name the env-from vector for operator triage")
}

// TestLabelPolicyStripsEnvFileFromRunJob confirms env-file is stripped on
// job-run too. Pre-fix, job-run passed through filterJobsWithHostEscalation
// but that filter only inspected volume / volumes-from, so env-file leaked
// despite the job being "covered".
func TestLabelPolicyStripsEnvFileFromRunJob(t *testing.T) {
	t.Parallel()
	labels := baseRunJobLabels("run-leak")
	labels["ofelia.job-run.run-leak.env-file"] = "/etc/ofelia-secrets.env"

	c, handler := runHostJobPolicy(t, false, labels)

	require.Contains(t, c.RunJobs, "run-leak",
		"the job itself has no volume/volumes-from vector, so it must survive — only the env-file key is stripped")
	assert.Empty(t, c.RunJobs["run-leak"].EnvFile,
		"env-file from a container label must be stripped on job-run when AllowHostJobsFromLabels=false (GHSA-h7m7-v83x-vfp3)")
	assert.True(t, handler.HasError("env-file"),
		"violation log must name the env-file vector")
}

// TestLabelPolicyStripsEnvFromFromServiceJob confirms env-from is stripped
// on job-service-run too.
func TestLabelPolicyStripsEnvFromFromServiceJob(t *testing.T) {
	t.Parallel()
	labels := map[string]string{
		"ofelia.enabled": "true",
		"ofelia.service": "true",
		"ofelia.job-service-run.svc-leak.schedule": "@daily",
		"ofelia.job-service-run.svc-leak.image":    "alpine",
		"ofelia.job-service-run.svc-leak.command":  "env",
		"ofelia.job-service-run.svc-leak.env-from": `["victim-container"]`,
	}

	c, handler := runHostJobPolicy(t, false, labels)

	require.Contains(t, c.ServiceJobs, "svc-leak",
		"the service job has no host-mount vector, so it survives — only env-from is stripped")
	assert.Empty(t, c.ServiceJobs["svc-leak"].EnvFrom,
		"env-from from a container label must be stripped on job-service-run when AllowHostJobsFromLabels=false (GHSA-h7m7-v83x-vfp3)")
	assert.True(t, handler.HasError("env-from"),
		"violation log must name the env-from vector")
}

// TestLabelPolicyStripsPrivilegedCaseInsensitive pins that the strip
// matches every casing / separator variant the decoder would honor. The
// mapstructure decoder matches keys via normalizeKey (lowercase, strip
// - and _), so `Privileged`, `ENV_FILE`, etc. all decode into the fields;
// a naive delete(job, "privileged") would miss them and leave the bypass
// open. env_from -> normalizes to envfrom -> matches EnvFrom.
func TestLabelPolicyStripsPrivilegedCaseInsensitive(t *testing.T) {
	t.Parallel()
	labels := baseExecJobLabels("mixed-case")
	labels["ofelia.job-exec.mixed-case.Privileged"] = "true"
	labels["ofelia.job-exec.mixed-case.ENV_FILE"] = "/root/.aws/credentials"

	c, _ := runHostJobPolicy(t, false, labels)

	job := theExecJob(t, c)
	assert.False(t, job.Privileged,
		"a mixed-case Privileged label must still be stripped — the decoder matches it case-insensitively, so the strip must too")
	assert.Empty(t, job.EnvFile,
		"an ENV_FILE label (underscore variant) must still be stripped — normalizeKey collapses it to the same field")
}

// TestLabelPolicyHonorsPrivilegedWhenAllowed is the inverse contract: with
// AllowHostJobsFromLabels=true the operator has opted in (trusted,
// single-tenant), so privileged / env-file / env-from are honored unchanged
// — the strip must not over-block.
func TestLabelPolicyHonorsPrivilegedWhenAllowed(t *testing.T) {
	t.Parallel()
	labels := baseExecJobLabels("trusted")
	labels["ofelia.job-exec.trusted.privileged"] = "true"
	labels["ofelia.job-exec.trusted.env-file"] = "/etc/app.env"

	c, _ := runHostJobPolicy(t, true, labels)

	job := theExecJob(t, c)
	assert.True(t, job.Privileged,
		"AllowHostJobsFromLabels=true must honor privileged from labels (operator opt-in)")
	assert.Equal(t, []string{"/etc/app.env"}, job.EnvFile,
		"AllowHostJobsFromLabels=true must honor env-file from labels (operator opt-in)")
}

// TestLabelPolicyKeepsCleanExecJob confirms the strip does not over-block:
// a job-exec with no privilege-bearing keys survives untouched and logs no
// violation.
func TestLabelPolicyKeepsCleanExecJob(t *testing.T) {
	t.Parallel()
	c, handler := runHostJobPolicy(t, false, baseExecJobLabels("clean"))

	job := theExecJob(t, c)
	assert.False(t, job.Privileged, "a clean exec job has no privileged bit")
	assert.Empty(t, job.EnvFile, "a clean exec job has no env-file")
	assert.Empty(t, job.EnvFrom, "a clean exec job has no env-from")
	assert.False(t, handler.HasError("SECURITY POLICY VIOLATION"),
		"a job with no escalation vectors must not trigger the policy")
}
