// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	defaults "github.com/creasty/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// TestComposeProjectIntegration_RunsAScheduledJob registers a Compose project
// whose scheduled job sits behind a profile, as docs/jobs.md advises, and runs
// one execution of it through the scheduler against the real daemon.
func TestComposeProjectIntegration_RunsAScheduledJob(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not available")
	}
	// The registration checks the CLI for real here (the unit tests stub it).
	// GitHub's ubuntu runner ships Docker Compose 2.38.2, which rejects
	// top-level jobs, so this test skips there.
	version, err := core.ComposeCLIVersion(context.Background())
	if err != nil || !core.ComposeVersionAtLeast(version, core.MinComposeJobsVersion) {
		t.Skipf("docker compose %q cannot run compose jobs (%v); %s or later is required",
			version, err, core.MinComposeJobsVersion)
	}
	old := composeCLICheck
	composeCLICheck = core.CheckComposeJobsSupport
	t.Cleanup(func() { composeCLICheck = old })
	dir := t.TempDir()
	project := "ofelia-it-cp-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))[:20]
	compose := `name: ` + project + `
services:
  app:
    image: alpine:3.22
    command: ["sleep", "300"]
jobs:
  hello:
    image: alpine:3.22
    command: ["echo", "hello-from-compose-job"]
    profiles: ["scheduled-jobs"]
    triggers:
      schedule:
        - cron: "0 0 1 1 *"
          missed_fires: skip
`
	composePath := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(compose), 0o600))
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-f", composePath, "-p", project, //nolint:gosec // test-owned values
			"--profile", "scheduled-jobs", "down", "--remove-orphans").Run()
	})

	// `docker compose up` must not fail on the scheduled job behind the profile.
	up := exec.Command("docker", "compose", "-f", composePath, "up", "-d", "--wait") //nolint:gosec // test-owned path
	out, err := up.CombinedOutput()
	require.NoError(t, err, "compose up with an inactive profile: %s", out)

	iniPath := filepath.Join(dir, "ofelia.ini")
	require.NoError(t, os.WriteFile(iniPath,
		[]byte("[compose-project \"shop\"]\nfile = "+composePath+"\nprofile = scheduled-jobs\n"), 0o600))
	logger := test.NewTestLogger()
	c, err := BuildFromFile(iniPath, logger)
	require.NoError(t, err)
	j := c.ComposeJobs["shop.hello"]
	require.NotNil(t, j, "the scheduled job behind the active profile is registered")

	provider, err := core.NewSDKDockerProviderDefault()
	require.NoError(t, err)
	require.NoError(t, defaults.Set(j))
	j.Provider = provider

	sh := core.NewScheduler(logger)
	require.NoError(t, sh.AddJob(j))
	require.NoError(t, sh.Start())
	t.Cleanup(sh.StopAndWait)

	require.NoError(t, sh.RunJob(context.Background(), "shop.hello"))
	require.Eventually(t, func() bool {
		last := j.GetLastRun()
		return last != nil && !last.IsRunning
	}, 90*time.Second, 200*time.Millisecond, "the triggered execution finishes")

	last := j.GetLastRun()
	require.False(t, last.Failed, "execution failed: %v, stderr: %s", last.Error, last.GetStderr())
	assert.Contains(t, last.GetStdout(), "hello-from-compose-job")
}
