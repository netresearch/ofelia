// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/test"
)

// An operator paused a label job that could never run. Boot now rejects
// that job, so the persisted pause has nothing to apply to; it must still
// hold when the label is fixed, or the fixed job starts running.
func TestPersistedDisable_SurvivesARejectedLabelJob(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	broken := func() []DockerContainerInfo {
		return []DockerContainerInfo{{
			Name:  "app",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":              "true",
				"ofelia.service":              "true",
				"ofelia.job-run.job.schedule": "@daily",
				"ofelia.job-run.job.command":  "echo hi",
			},
		}}
	}
	require.NoError(t, cfg.buildFromDockerContainers(broken()))
	cfg.sh = core.NewScheduler(logger)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.registerAllJobs()
	require.Nil(t, cfg.sh.GetJob("job"), "boot rejects the job")

	c := &DaemonCommand{Logger: logger, scheduler: cfg.sh, config: cfg}
	c.StateFile = writePersistFile(t, persist.State{Disabled: []string{"job"}})
	require.NoError(t, c.initPersistStore())

	fixed := broken()
	fixed[0].Labels["ofelia.job-run.job.image"] = "alpine"
	cfg.dockerContainersUpdate(fixed)

	require.NotNil(t, cfg.sh.GetAnyJob("job"), "the fixed job is registered")
	assert.True(t, cfg.sh.IsJobDisabled("job"), "the persisted pause still holds")
}
