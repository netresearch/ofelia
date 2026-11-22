// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"testing"

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
