// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// TestIniReloadAppliesExecJobUserChange is the reproduction from issue #876:
// adding `user = root` to a running job-exec and reloading the INI must
// re-register the job with the new user. Before the fix the hash only covered
// the BareJob fields, so the job kept running as the old user until a restart.
func TestIniReloadAppliesExecJobUserChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.ini")
	write := func(body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	write("[global]\n")

	logger := test.NewTestLogger()
	cfg, err := BuildFromFile(path, logger)
	require.NoError(t, err)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.configPath = path
	cfg.levelVar = &slog.LevelVar{}

	// The first reload adds the job through the normal add path, which
	// applies the default user, as on a running daemon.
	cfg.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	write(`[job-exec "probe"]
schedule = 0 2 * * *
container = target
command = id
`)
	require.NoError(t, cfg.iniConfigUpdate())
	before := cfg.ExecJobs["probe"]
	require.NotNil(t, before, "precondition: the first reload registers the job")
	require.Equal(t, "nobody", before.User, "precondition: the default user applies")

	cfg.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	write(`[job-exec "probe"]
schedule = 0 2 * * *
container = target
user = root
command = id
`)
	require.NoError(t, cfg.iniConfigUpdate())

	after := cfg.ExecJobs["probe"]
	require.NotNil(t, after)
	assert.Equal(t, "root", after.User, "the reload must apply the new user")
	assert.NotSame(t, before, after, "the job must be re-registered, not left as the old object")
	assert.Same(t, after, cfg.sh.GetJob("probe"), "the scheduler must run the new definition")
}

// TestLabelReloadWithUnchangedLabelsKeepsTheJob pins the other direction: the
// hash now covers user, environment and the rest, and a label reload runs on
// every container event. If any of those fields hashed differently for the
// same labels, every event would remove and re-register the job.
func TestLabelReloadWithUnchangedLabelsKeepsTheJob(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.WebhookConfigs = NewWebhookConfigs()

	containers := []DockerContainerInfo{{
		Name:  "target",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                    "true",
			"ofelia.job-exec.probe.schedule":    "@daily",
			"ofelia.job-exec.probe.command":     "id",
			"ofelia.job-exec.probe.user":        "root",
			"ofelia.job-exec.probe.environment": `["A=1","B=2","C=3"]`,
		},
	}}
	cfg.dockerContainersUpdate(containers)
	require.Len(t, cfg.ExecJobs, 1)
	var key string
	for k := range cfg.ExecJobs {
		key = k
	}
	before := cfg.ExecJobs[key]

	for range 3 {
		cfg.dockerContainersUpdate(containers)
	}
	assert.Same(t, before, cfg.ExecJobs[key], "identical labels must not re-register the job")
}

// TestLabelReloadDoesNotSmuggleInPrivileged is the security half of the fix.
// Before it, a reload that changed only job-exec fields never reached the
// replace path at all. Now it does, so the label policy must hold there too:
// with allow-host-jobs-from-labels off, a reload that adds privileged=true
// next to a legitimate change re-registers the job with that change and
// without the privileged bit (GHSA-h7m7-v83x-vfp3).
func TestLabelReloadDoesNotSmuggleInPrivileged(t *testing.T) {
	t.Parallel()

	logger, handler := test.NewTestLoggerWithHandler()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.WebhookConfigs = NewWebhookConfigs()
	cfg.Global.AllowHostJobsFromLabels = false

	container := func(labels map[string]string) []DockerContainerInfo {
		return []DockerContainerInfo{{
			Name:   "target",
			State:  domain.ContainerState{Running: true},
			Labels: labels,
		}}
	}
	labels := map[string]string{
		"ofelia.enabled":                 "true",
		"ofelia.job-exec.probe.schedule": "@daily",
		"ofelia.job-exec.probe.command":  "id",
	}
	cfg.dockerContainersUpdate(container(labels))
	require.Len(t, cfg.ExecJobs, 1)
	var key string
	for k := range cfg.ExecJobs {
		key = k
	}
	before := cfg.ExecJobs[key]

	labels["ofelia.job-exec.probe.user"] = "root"
	labels["ofelia.job-exec.probe.privileged"] = "true"
	cfg.dockerContainersUpdate(container(labels))

	after := cfg.ExecJobs[key]
	require.NotNil(t, after)
	assert.NotSame(t, before, after, "the user change must re-register the job")
	assert.Equal(t, "root", after.User, "the legitimate change must apply")
	assert.False(t, after.Privileged, "privileged from a label must still be stripped on the replace path")
	assert.True(t, handler.HasError("SECURITY POLICY VIOLATION"),
		"the stripped key must be logged on reload as it is on first registration")
}
