// Copyright (c) 2026 Netresearch DTT GmbH
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
	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/test"
)

// durableComposeJob is a [compose-project] job that keeps a schedule record.
func durableComposeJob(name string) *ComposeJobConfig {
	return &ComposeJobConfig{ComposeJob: core.ComposeJob{
		BareJob: core.BareJob{Name: name, Schedule: "0 2 * * *"},
		Project: core.ComposeProjectContext{Revision: "r1", MissedFires: "one", Concurrency: "forbid"},
	}}
}

// initPersistStore hands the loaded state file to the scheduler and prunes
// the records of jobs that no longer exist: without the first, no job would
// keep a record at all.
func TestInitPersistStore_WiresScheduleRecords(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	c.config = NewConfig(c.Logger)
	c.config.sh = c.scheduler
	c.config.ComposeJobs["shop.backup"] = durableComposeJob("shop.backup")
	c.StateFile = writePersistFile(t, persist.State{
		Schedules: map[string]*persist.Schedule{
			"shop.backup": {Revision: "r1"},
			"shop.gone":   {Revision: "r1"},
		},
	})

	require.NoError(t, c.initPersistStore())

	assert.Equal(t, []string{"shop.backup"}, c.persistStore.ScheduleNames(),
		"the record of a removed job is pruned, the configured job's record stays")
}

// The daemon builds its configuration knowing whether a state file is set.
func TestBoot_ScheduleStateFollowsStateFile(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "ofelia.ini")
	require.NoError(t, os.WriteFile(ini, []byte("[job-local \"x\"]\nschedule = @daily\ncommand = true\n"), 0o600))

	for stateFile, want := range map[string]scheduleStateMode{
		"":                               scheduleStateMissing,
		filepath.Join(dir, "state.json"): scheduleStateAvailable,
	} {
		cmd := &DaemonCommand{ConfigFile: ini, StateFile: stateFile, Logger: test.NewTestLogger(), LevelVar: &slog.LevelVar{}}
		_ = cmd.boot()
		require.NotNil(t, cmd.config)
		assert.Equal(t, want, cmd.config.scheduleState, "state file %q", stateFile)
		if cmd.scheduler != nil {
			_ = cmd.scheduler.StopWithTimeout(time.Second)
		}
	}
}

// A reload builds the new configuration with the daemon's mode: without a
// state file, a project that now needs one is rejected and keeps its last
// loaded jobs.
func TestReload_KeepsScheduleStateMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")
	logger, handler := test.NewTestLoggerWithHandler()
	c, err := buildFromFile(iniPath, logger, scheduleStateMissing)
	require.NoError(t, err)
	c.sh = core.NewScheduler(logger)
	c.buildSchedulerMiddlewares(c.sh)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	c.levelVar = &slog.LevelVar{}
	old := c.ComposeJobs["shop.backup"]
	require.NotNil(t, old)

	touchFile(t, composePath, plainCronCompose, time.Minute)
	require.NoError(t, c.iniConfigUpdate())

	assert.Same(t, old, c.ComposeJobs["shop.backup"], "the rejected project keeps its last loaded job")
	assert.True(t, handler.HasError("--state-file"), "the reload rejects the project for the missing state file")
}

// A reload prunes the records of jobs it removed.
func TestReload_PrunesScheduleRecords(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")
	c := prepareReload(t, iniPath)
	store := persist.NewStore(filepath.Join(dir, "state.json"))
	require.NoError(t, store.PutSchedule("shop.gone", persist.Schedule{Revision: "r1"}))
	c.sh.SetScheduleStore(store)

	touchFile(t, composePath, shopCompose+"# touched\n", time.Minute)
	require.NoError(t, c.iniConfigUpdate())

	assert.Empty(t, store.ScheduleNames(), "the reload pruned the record of a job that no longer exists")
}

// `ofelia validate` applies the daemon's requirement: a project that needs a
// state file fails without --state-file and passes with it.
func TestValidate_ScheduleStateRequiresStateFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", plainCronCompose)
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")

	without := &ValidateCommand{ConfigFile: iniPath, Logger: test.NewTestLogger(), LevelVar: &slog.LevelVar{}}
	require.ErrorIs(t, without.Execute(nil), ErrComposeProjectRejected)

	with := &ValidateCommand{
		ConfigFile: iniPath, StateFile: filepath.Join(dir, "state.json"),
		Logger: test.NewTestLogger(), LevelVar: &slog.LevelVar{},
	}
	require.NoError(t, with.Execute(nil))
}

// `ofelia validate` accepts a loaded Compose project: its jobs carry their
// files in the project context, which the validator's "file is required"
// check did not see, so every project failed validation.
func TestValidate_AcceptsComposeProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")
	cmd := &ValidateCommand{ConfigFile: iniPath, Logger: test.NewTestLogger(), LevelVar: &slog.LevelVar{}}
	require.NoError(t, cmd.Execute(nil))
}
