// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// A label run job without an image or a container is not scheduled at
// boot, as an INI job and a label job on reload are not; the valid job
// beside it is.
func TestBoot_RejectsInvalidLabelJob(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	require.NoError(t, c.buildFromDockerContainers([]DockerContainerInfo{{
		Name:  "app",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                 "true",
			"ofelia.service":                 "true",
			"ofelia.job-run.broken.schedule": "@daily",
			"ofelia.job-run.broken.command":  "echo broken",
			"ofelia.job-run.valid.schedule":  "@daily",
			"ofelia.job-run.valid.image":     "alpine",
			"ofelia.job-run.valid.command":   "echo ok",
		},
	}}))
	require.Contains(t, c.RunJobs, "broken", "the label decoder keeps the job; assembly has to reject it")

	c.sh = core.NewScheduler(logger)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	c.registerAllJobs()

	assert.Nil(t, c.sh.GetJob("broken"), "a run job without image or container must not be scheduled")
	assert.NotNil(t, c.sh.GetJob("valid"))
}

func TestAssembleJob_Validates(t *testing.T) {
	t.Parallel()
	cfg := assemblyConfig(t)

	require.ErrorIs(t, cfg.assembleJob("run", &RunJobConfig{}), core.ErrImageOrContainer)
	require.Error(t, cfg.assembleJob("service", &RunServiceConfig{}))

	run := &RunJobConfig{}
	run.Image = "alpine"
	require.NoError(t, cfg.assembleJob("run-ok", run))
	require.NoError(t, cfg.assembleJob("exec", &ExecJobConfig{}))
	require.NoError(t, cfg.assembleJob("local", &LocalJobConfig{}))
}

// A reload rejects an invalid job through the same assembly: it is
// neither scheduled nor recorded in the job map.
func TestReload_RejectsInvalidJob(t *testing.T) {
	t.Parallel()
	cfg := assemblyConfig(t)
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	broken := &RunJobConfig{}
	broken.Schedule, broken.Command = "@daily", "echo broken"
	syncJobMap(cfg, cfg.RunJobs, map[string]*RunJobConfig{"broken": broken},
		assembleAs[*RunJobConfig](cfg), JobSourceLabel, "run")
	assert.Nil(t, cfg.sh.GetJob("broken"))
	assert.NotContains(t, cfg.RunJobs, "broken")
}

// The web API refuses a run job that can never run.
func TestAPIJob_RejectsRunJobWithoutImageOrContainer(t *testing.T) {
	t.Parallel()
	f := newAPIJobFixture(t, &mockDockerProviderForHandler{})
	rec := f.create(t, map[string]any{"name": "r", "type": "run", "schedule": "@hourly", "command": "echo"})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Nil(t, f.cfg.sh.GetJob("r"))
}
