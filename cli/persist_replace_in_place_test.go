// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
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

// A state-file job takes the place of the INI job of its name at boot
// without listing that INI job as removed: it is still in the configuration.
// A state-file job the scheduler refuses leaves the INI job running.
func TestInitPersistStore_ReplacesTheConfigJobInPlace(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schedule string
		command  string
	}{
		"accepted": {"@hourly", "echo api"},
		"refused":  {"not-a-schedule", "echo ini"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			logger := test.NewTestLogger()
			cfg := NewConfig(logger)
			cfg.sh = core.NewScheduler(logger)
			cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
			ini := &LocalJobConfig{}
			ini.Name, ini.Schedule, ini.Command = "shop.report", "@daily", "echo ini"
			require.NoError(t, cfg.sh.AddJob(ini))

			c := &DaemonCommand{Logger: logger, scheduler: cfg.sh, config: cfg}
			c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
				"shop.report": {Type: persist.JobTypeLocal, Schedule: tc.schedule, Command: "echo api"},
			}})
			require.NoError(t, c.initPersistStore())

			live := cfg.sh.GetAnyJob("shop.report")
			require.NotNil(t, live)
			assert.Equal(t, tc.command, live.GetCommand())
			assert.True(t, cfg.sh.EntryByName("shop.report").Valid())
			assert.Empty(t, cfg.sh.GetRemovedJobs(), "the INI job is listed as removed")
		})
	}
}

// A state-file job that replaces a run-on-startup INI job at boot does not
// inherit the startup run: the state file has no run-on-startup setting.
func TestInitPersistStore_ReplacedStartupJobDoesNotRunTheRecordAtStart(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	ini := &LocalJobConfig{}
	ini.Name, ini.Schedule, ini.Command, ini.RunOnStartup = "shop.report", "@daily", "true", true
	require.NoError(t, cfg.sh.AddJob(ini))

	marker := filepath.Join(t.TempDir(), "ran")
	c := &DaemonCommand{Logger: logger, scheduler: cfg.sh, config: cfg}
	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"shop.report": {Type: persist.JobTypeLocal, Schedule: "@hourly", Command: "touch " + marker},
	}})
	require.NoError(t, c.initPersistStore())
	require.NoError(t, cfg.sh.Start())
	t.Cleanup(func() { _ = cfg.sh.StopWithTimeout(5 * time.Second) })

	time.Sleep(300 * time.Millisecond)
	_, err := os.Stat(marker)
	assert.True(t, os.IsNotExist(err), "the state-file job ran at start")
}

// A record the scheduler refuses registers nothing when no job holds its
// name, and gives the name back to the run-on-startup job it was to replace.
func TestInitPersistStore_RefusedRecord(t *testing.T) {
	t.Parallel()
	for name, startupINI := range map[string]*bool{"no config job": nil, "run-on-startup config job": new(bool)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			logger := test.NewTestLogger()
			cfg := NewConfig(logger)
			cfg.sh = core.NewScheduler(logger)
			cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
			var ini *LocalJobConfig
			if startupINI != nil {
				ini = &LocalJobConfig{}
				ini.Name, ini.Schedule, ini.Command, ini.RunOnStartup = "shop.report", "@daily", "echo ini", true
				require.NoError(t, cfg.sh.AddJob(ini))
			}
			c := &DaemonCommand{Logger: logger, scheduler: cfg.sh, config: cfg}
			c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
				"shop.report": {Type: persist.JobTypeLocal, Schedule: "not-a-schedule", Command: "echo api"},
			}})
			require.NoError(t, c.initPersistStore())

			live := cfg.sh.GetAnyJob("shop.report")
			if ini == nil {
				assert.Nil(t, live)
				return
			}
			assert.Same(t, core.Job(ini), live, "the run-on-startup INI job was not restored")
			assert.True(t, cfg.sh.EntryByName("shop.report").Valid())
		})
	}
}
