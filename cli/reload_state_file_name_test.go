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

// A state-file job replaces an INI job of the same name at boot and stays
// the job of that name when a reload changes or removes the INI job: the
// state file wins (initPersistStore), and the reload neither removes the
// state-file job's entry nor leaves a second entry for the name.
func TestReload_StateFileJobKeepsItsNameAgainstTheINIJob(t *testing.T) {
	t.Parallel()
	for name, reloaded := range map[string]string{
		"INI job changed": "[job-local \"shop.report\"]\nschedule = @daily\ncommand = echo ini-changed\n",
		"INI job removed": "[global]\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "config.ini")
			write := func(body string) {
				t.Helper()
				require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
			}
			write("[job-local \"shop.report\"]\nschedule = @daily\ncommand = echo ini\n")
			logger := test.NewTestLogger()
			cfg, err := BuildFromFile(path, logger)
			require.NoError(t, err)
			cfg.sh = core.NewScheduler(logger)
			cfg.buildSchedulerMiddlewares(cfg.sh)
			cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
			cfg.configPath = path
			cfg.levelVar = &slog.LevelVar{}
			cfg.registerAllJobs()

			c := &DaemonCommand{Logger: logger, scheduler: cfg.sh, config: cfg}
			c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
				"shop.report": {Type: persist.JobTypeLocal, Schedule: "@hourly", Command: "echo api"},
			}})
			require.NoError(t, c.initPersistStore())
			require.Equal(t, "echo api", cfg.sh.GetAnyJob("shop.report").GetCommand(), "precondition: the state file wins at boot")

			cfg.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			write(reloaded)
			require.NoError(t, cfg.iniConfigUpdate())

			live := cfg.sh.GetAnyJob("shop.report")
			require.NotNil(t, live, "the name has no registered job")
			assert.Equal(t, "echo api", live.GetCommand(), "the state-file job lost its name")
			active, _, _ := cfg.sh.GetJobSnapshot()
			require.Len(t, active, 1, "the name is listed more than once")
			assert.Same(t, live, active[0])
			assert.True(t, cfg.sh.EntryByName("shop.report").Valid(), "the state-file job lost its cron entry")
			assert.Empty(t, cfg.sh.GetUnschedulableJobs(), "the reload reports the shadowed INI job as not scheduled")
		})
	}
}

// An INI job added on a reload while a job from the state file or the web API
// holds its name is kept unregistered, as at boot, and is not reported as a
// job the scheduler refused.
func TestReload_AddedINIJobLeavesTheAPIJobItsName(t *testing.T) {
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
	cfg.registerAllJobs()
	api, err := cfg.BuildAPIJob("shop.report", persist.Job{Type: persist.JobTypeLocal, Schedule: "@hourly", Command: "echo api"})
	require.NoError(t, err)
	require.NoError(t, cfg.sh.AddJob(api))

	cfg.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	write("[job-local \"shop.report\"]\nschedule = @daily\ncommand = echo ini\n")
	require.NoError(t, cfg.iniConfigUpdate())

	assert.Same(t, api, cfg.sh.GetAnyJob("shop.report"))
	assert.Empty(t, cfg.sh.GetUnschedulableJobs(), "the added INI job is reported as refused")
}
