// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

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
