// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	defaults "github.com/creasty/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// A reload that replaces a changed job removes the old entry and adds a new
// one. RemoveJob forgets the disabled state, so without carrying it over the
// replacement ran on schedule although the job had been disabled.
func TestReplaceIfChanged_KeepsDisabledState(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	oldJob := &ExecJobConfig{Schedule: "@every 5s", Command: "echo old"}
	_ = defaults.Set(oldJob)
	oldJob.Name = "testjob"
	oldJob.buildMiddlewares(nil, nil)
	require.NoError(t, cfg.sh.AddJob(oldJob))
	require.NoError(t, cfg.sh.DisableJob("testjob"))

	newJob := &ExecJobConfig{Schedule: "@every 10s", Command: "echo new"}
	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	require.True(t, replaceIfChanged(cfg, "testjob", oldJob, newJob, prep, JobSourceINI))

	assert.True(t, cfg.sh.IsJobDisabled("testjob"), "the replacement must stay disabled")
	entries := cfg.sh.Entries()
	require.Len(t, entries, 1)
	assert.True(t, entries[0].Paused, "the replacement's cron entry must be paused")
}

// An INI job that takes over a disabled label job of the same name replaces
// it through RemoveJob and a fresh registration; the name stays disabled.
func TestSyncJobMap_INIOverridingDisabledLabelJobStaysDisabled(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	labelJob := &ExecJobConfig{Schedule: "@every 5s", Command: "echo label"}
	_ = defaults.Set(labelJob)
	labelJob.Name = "testjob"
	labelJob.SetJobSource(JobSourceLabel)
	labelJob.buildMiddlewares(nil, nil)
	require.NoError(t, cfg.sh.AddJob(labelJob))
	require.NoError(t, cfg.sh.DisableJob("testjob"))

	current := map[string]*ExecJobConfig{"testjob": labelJob}
	parsed := map[string]*ExecJobConfig{"testjob": {Schedule: "@every 10s", Command: "echo ini"}}
	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	syncJobMap(cfg, current, parsed, prep, JobSourceINI, "exec")

	require.Equal(t, "echo ini", current["testjob"].Command, "the INI job must have replaced the label job")
	assert.True(t, cfg.sh.IsJobDisabled("testjob"), "the INI job must stay disabled")
	entries := cfg.sh.Entries()
	require.Len(t, entries, 1)
	assert.True(t, entries[0].Paused)
}

// The enabled direction of the takeover: an enabled label job taken over by
// an INI job stays enabled.
func TestSyncJobMap_INIOverridingEnabledLabelJobStaysEnabled(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	labelJob := &ExecJobConfig{Schedule: "@every 5s", Command: "echo label"}
	_ = defaults.Set(labelJob)
	labelJob.Name = "testjob"
	labelJob.SetJobSource(JobSourceLabel)
	labelJob.buildMiddlewares(nil, nil)
	require.NoError(t, cfg.sh.AddJob(labelJob))

	current := map[string]*ExecJobConfig{"testjob": labelJob}
	parsed := map[string]*ExecJobConfig{"testjob": {Schedule: "@every 10s", Command: "echo ini"}}
	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	syncJobMap(cfg, current, parsed, prep, JobSourceINI, "exec")

	require.Equal(t, "echo ini", current["testjob"].Command)
	assert.False(t, cfg.sh.IsJobDisabled("testjob"))
	entries := cfg.sh.Entries()
	require.Len(t, entries, 1)
	assert.False(t, entries[0].Paused)
}

// A replacement with run-on-startup must not run once before it is paused:
// it is registered paused, so go-cron never dispatches it.
func TestReplaceIfChanged_DisabledRunOnStartupDoesNotRun(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	require.NoError(t, cfg.sh.Start())
	t.Cleanup(func() { _ = cfg.sh.Stop() })

	marker := filepath.Join(t.TempDir(), "ran")
	oldJob := &LocalJobConfig{LocalJob: core.LocalJob{BareJob: core.BareJob{Schedule: "@every 1h", Command: "true"}}}
	_ = defaults.Set(oldJob)
	oldJob.Name = "testjob"
	oldJob.buildMiddlewares(nil, nil)
	require.NoError(t, cfg.sh.AddJob(oldJob))
	require.NoError(t, cfg.sh.DisableJob("testjob"))

	newJob := &LocalJobConfig{LocalJob: core.LocalJob{BareJob: core.BareJob{
		Schedule: "@every 2h", Command: "touch " + marker, RunOnStartup: true,
	}}}
	prep := func(name string, j *LocalJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	require.True(t, replaceIfChanged(cfg, "testjob", oldJob, newJob, prep, JobSourceINI))
	assert.True(t, cfg.sh.IsJobDisabled("testjob"))

	// Give an immediate dispatch time to happen; a paused entry never starts.
	time.Sleep(500 * time.Millisecond)
	_, err := os.Stat(marker)
	assert.True(t, os.IsNotExist(err), "the disabled replacement must not run on startup")
}

// A replacement the scheduler rejects (a bad schedule) leaves no job, and the
// next, corrected definition must come back disabled.
func TestReplaceIfChanged_DisabledSurvivesRejectedReplacement(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	oldJob := &ExecJobConfig{Schedule: "@every 5s", Command: "echo old"}
	_ = defaults.Set(oldJob)
	oldJob.Name = "testjob"
	oldJob.buildMiddlewares(nil, nil)
	require.NoError(t, cfg.sh.AddJob(oldJob))
	require.NoError(t, cfg.sh.DisableJob("testjob"))

	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	badJob := &ExecJobConfig{Schedule: "not a schedule", Command: "echo bad"}
	require.True(t, replaceIfChanged(cfg, "testjob", oldJob, badJob, prep, JobSourceINI))
	require.Empty(t, cfg.sh.Entries(), "the rejected replacement must not be scheduled")

	fixedJob := &ExecJobConfig{Schedule: "@every 10s", Command: "echo fixed"}
	require.True(t, replaceIfChanged(cfg, "testjob", badJob, fixedJob, prep, JobSourceINI))

	assert.True(t, cfg.sh.IsJobDisabled("testjob"), "the corrected job must come back disabled")
	entries := cfg.sh.Entries()
	require.Len(t, entries, 1)
	assert.True(t, entries[0].Paused)

	// Once the corrected job is registered, the remembered name is gone: an
	// operator who enables it keeps it enabled through the next reload.
	require.NoError(t, cfg.sh.EnableJob("testjob"))
	nextJob := &ExecJobConfig{Schedule: "@every 20s", Command: "echo next"}
	require.True(t, replaceIfChanged(cfg, "testjob", fixedJob, nextJob, prep, JobSourceINI))
	assert.False(t, cfg.sh.IsJobDisabled("testjob"), "an enabled job must stay enabled through the next reload")
	entries = cfg.sh.Entries()
	require.Len(t, entries, 1)
	assert.False(t, entries[0].Paused)
}

// The other direction: an enabled job stays enabled after the replacement.
func TestReplaceIfChanged_KeepsEnabledState(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	oldJob := &ExecJobConfig{Schedule: "@every 5s", Command: "echo old"}
	_ = defaults.Set(oldJob)
	oldJob.Name = "testjob"
	oldJob.buildMiddlewares(nil, nil)
	require.NoError(t, cfg.sh.AddJob(oldJob))

	newJob := &ExecJobConfig{Schedule: "@every 10s", Command: "echo new"}
	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	require.True(t, replaceIfChanged(cfg, "testjob", oldJob, newJob, prep, JobSourceINI))

	assert.False(t, cfg.sh.IsJobDisabled("testjob"))
	entries := cfg.sh.Entries()
	require.Len(t, entries, 1)
	assert.False(t, entries[0].Paused)
}
