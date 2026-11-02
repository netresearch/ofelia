// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build integration

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// TestComposeProjectIntegration_RejectedReloadRunsSnapshot verifies against Docker that rejecting edited inputs
// preserves the accepted command, environment and readable config/secret mounts for a non-root read-only job.
func TestComposeProjectIntegration_RejectedReloadRunsSnapshot(t *testing.T) {
	version, err := core.ComposeCLIVersion(context.Background())
	if err != nil || !core.ComposeVersionAtLeast(version, core.MinComposeJobsVersion) {
		t.Skipf("docker compose %q cannot run compose jobs (%v)", version, err)
	}
	provider, err := core.NewSDKDockerProviderDefault()
	require.NoError(t, err)
	require.NoError(t, provider.Ping(context.Background()))
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	project := fmt.Sprintf("ofelia-snapshot-%d", time.Now().UnixNano())
	writeFile(t, dir, "job.env", "SNAPSHOT_VALUE=accepted-env\nLITERAL=accepted-dollar\n")
	configPath := writeFile(t, dir, "settings.conf", "accepted-config\n")
	secretPath := writeFile(t, dir, "secret.txt", "accepted-secret\n")
	require.NoError(t, os.Chmod(configPath, 0o644))
	require.NoError(t, os.Chmod(secretPath, 0o644))
	accepted := `name: ` + project + `
configs:
  settings:
    file: ./settings.conf
secrets:
  token:
    file: ./secret.txt
jobs:
  backup:
    image: alpine:3.22
    user: "1000:1000"
    read_only: true
    env_file: job.env
    configs: [settings]
    secrets: [token]
    command: [sh, -c, 'echo accepted-command; echo "$$SNAPSHOT_VALUE"; echo "$$LITERAL"; cat /settings /run/secrets/token']
    triggers:
      schedule:
        - cron: "0 0 1 1 *"
          missed_fires: skip
`
	path := writeFile(t, dir, "compose.yaml", accepted)
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", project, "-f", path, "down", "--remove-orphans").Run() //nolint:gosec // test-owned values
	})
	c := prepareReload(t, writeFile(t, dir, "ofelia.ini", "[compose-project \"snapshot\"]\nfile = "+path+"\n"))
	job := c.ComposeJobs["snapshot.backup"]
	require.NotNil(t, job)
	writeFile(t, dir, "job.env", "SNAPSHOT_VALUE=rejected-env\nLITERAL=rejected-dollar\n")
	writeFile(t, dir, "settings.conf", "rejected-config\n")
	writeFile(t, dir, "secret.txt", "rejected-secret\n")
	rejected := strings.Replace(accepted, "accepted-command", "rejected-command", 1)
	rejected = strings.Replace(rejected, "      schedule:\n", "      schedule:\n        - cron: \"1 1 * * *\"\n", 1)
	touchFile(t, path, rejected, time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	require.Same(t, job, c.ComposeJobs["snapshot.backup"])
	job.Provider = provider
	execution, err := core.NewExecution()
	require.NoError(t, err)
	runCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx := core.NewContextWithContext(runCtx, core.NewScheduler(test.NewTestLogger()), job, execution)
	require.NoError(t, job.Run(ctx), "compose stderr: %s", execution.GetStderr())
	stdout := execution.GetStdout()
	for _, want := range []string{"accepted-command", "accepted-env", "accepted-dollar", "accepted-config", "accepted-secret"} {
		assert.Contains(t, stdout, want)
	}
	assert.NotContains(t, stdout, "rejected-")
}

// TestComposeProjectIntegration_DependencySnapshotSurvivesRestart proves retained input files outlive the job
// CLI so an existing non-root dependency can restart with its original accepted configuration.
func TestComposeProjectIntegration_DependencySnapshotSurvivesRestart(t *testing.T) {
	version, err := core.ComposeCLIVersion(context.Background())
	if err != nil || !core.ComposeVersionAtLeast(version, core.MinComposeJobsVersion) {
		t.Skipf("docker compose %q cannot run compose jobs (%v)", version, err)
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	project := fmt.Sprintf("ofelia-snapshot-dep-%d", time.Now().UnixNano())
	config := writeFile(t, dir, "settings.conf", "accepted-settings")
	require.NoError(t, os.Chmod(config, 0o644))
	model := `name: ` + project + `
configs:
  settings:
    file: ./settings.conf
services:
  dep:
    image: alpine:3.22
    command: [sleep, '300']
    user: "1000:1000"
    read_only: true
    configs: [settings]
jobs:
  backup:
    image: alpine:3.22
    command: [echo, accepted]
    depends_on: [dep]
    triggers:
      schedule:
        - cron: "0 2 * * *"
          missed_fires: skip
`
	path := writeFile(t, dir, "compose.yaml", model)
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", project, "-f", path, "down", "--remove-orphans", "-t", "0").Run() //nolint:gosec // test-owned values
	})
	c, err := BuildFromString("[compose-project \"snapshot\"]\nfile = "+path+"\n", test.NewTestLogger())
	require.NoError(t, err)
	job := c.ComposeJobs["snapshot.backup"]
	require.NotNil(t, job)
	job.Provider, err = core.NewSDKDockerProviderDefault()
	require.NoError(t, err)
	execution, err := core.NewExecution()
	require.NoError(t, err)
	defer execution.Cleanup()
	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx := core.NewContextWithContext(runCtx, core.NewScheduler(test.NewTestLogger()), job, execution)
	require.NoError(t, job.Run(ctx), "compose stderr: %s", execution.GetStderr())
	writeFile(t, dir, "settings.conf", "new-settings")
	out, err := exec.Command("docker", "compose", "-p", project, "-f", path, "ps", "-q", "dep").CombinedOutput() //nolint:gosec // test-owned values
	require.NoError(t, err, "%s", out)
	id := strings.TrimSpace(string(out))
	require.NotEmpty(t, id)
	out, err = exec.Command("docker", "restart", "-t", "0", id).CombinedOutput() //nolint:gosec // test container ID
	require.NoError(t, err, "dependency must restart after the job exits: %s", out)
	out, err = exec.Command("docker", "exec", id, "cat", "/settings").CombinedOutput() //nolint:gosec // test container ID
	require.NoError(t, err, "%s", out)
	assert.Equal(t, "accepted-settings", string(out), "a restarted dependency still mounts the accepted input")
}
