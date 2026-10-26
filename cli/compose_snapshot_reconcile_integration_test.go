// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build integration

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

func TestComposeProjectIntegration_ValidReloadUpdatesOnlyAffectedDependency(t *testing.T) {
	version, err := core.ComposeCLIVersion(context.Background())
	if err != nil || !core.ComposeVersionAtLeast(version, core.MinComposeJobsVersion) {
		t.Skipf("docker compose %q cannot run compose jobs (%v)", version, err)
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("OFELIA_REVIEW_NATIVE", "accepted-native")
	writeFile(t, dir, "token.txt", "accepted-token")
	project := fmt.Sprintf("ofelia-snapshot-dep-%d", time.Now().UnixNano())
	config := writeFile(t, dir, "settings.conf", "accepted-settings")
	require.NoError(t, os.Chmod(config, 0o644))
	require.NoError(t, os.Chmod(filepath.Join(dir, "token.txt"), 0o644))
	model := `name: ` + project + `
configs:
  settings:
    file: ./settings.conf
secrets:
  token:
    file: ./token.txt
  native:
    environment: OFELIA_REVIEW_NATIVE
services:
  dep:
    image: alpine:3.22
    command: [sleep, '300']
    user: "1000:1000"
    configs: [settings]
    secrets: [token, native]
    stop_grace_period: 1s
  untouched:
    image: alpine:3.22
    command: [sleep, '300']
jobs:
  backup:
    image: alpine:3.22
    command: [echo, accepted]
    depends_on: [dep, untouched]
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
	runCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ctx := core.NewContextWithContext(runCtx, core.NewScheduler(test.NewTestLogger()), job, execution)
	require.NoError(t, job.Run(ctx), "compose stderr: %s", execution.GetStderr())
	containerID := func(service string) string {
		t.Helper()
		out, err := exec.Command("docker", "compose", "-p", project, "-f", path, "ps", "-q", service).CombinedOutput() //nolint:gosec // test-owned values
		require.NoError(t, err, "%s", out)
		id := strings.TrimSpace(string(out))
		require.NotEmpty(t, id)
		return id
	}
	original, untouched := containerID("dep"), containerID("untouched")
	runReload := func() {
		t.Helper()
		next, err := BuildFromString("[compose-project \"snapshot\"]\nfile = "+path+"\n", test.NewTestLogger())
		require.NoError(t, err)
		nextJob := next.ComposeJobs["snapshot.backup"]
		require.NotNil(t, nextJob)
		nextJob.Provider = job.Provider
		nextExecution, err := core.NewExecution()
		require.NoError(t, err)
		defer nextExecution.Cleanup()
		nextCtx := core.NewContextWithContext(runCtx, core.NewScheduler(test.NewTestLogger()), nextJob, nextExecution)
		require.NoError(t, nextJob.Run(nextCtx), "compose stderr: %s", nextExecution.GetStderr())
	}
	runReload()
	assert.Equal(t, original, containerID("dep"), "unchanged inputs must reuse the dependency")
	for _, name := range []string{"settings.conf", "token.txt", "native"} {
		if name == "native" {
			t.Setenv("OFELIA_REVIEW_NATIVE", "updated-native")
		} else {
			writeFile(t, dir, name, "updated-"+name)
		}
		runReload()
		current := containerID("dep")
		assert.NotEqual(t, original, current, name+" must recreate the dependency")
		assert.Equal(t, untouched, containerID("untouched"), "unaffected dependencies must not be recreated")
		original = current
	}
	out, err := exec.Command("docker", "exec", original, "sh", "-c", "cat /settings /run/secrets/token /run/secrets/native").CombinedOutput() //nolint:gosec // test container ID
	require.NoError(t, err, "%s", out)
	assert.Equal(t, "updated-settings.confupdated-token.txtupdated-native", string(out))
}
