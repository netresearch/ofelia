// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/test"
)

// ofelia validate refuses the compose jobs the daemon refuses at load, and
// passes one without a file key, which runs with compose.yml.
func TestValidateExecute_RefusesComposeJobsThatCannotRun(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"sensitive file": "[job-compose \"backup\"]\nschedule = @daily\nfile = /etc/compose.yml\nservice = db\n",
		"bad service":    "[job-compose \"backup\"]\nschedule = @daily\nservice = bad name\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.ini")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
			cmd := &ValidateCommand{ConfigFile: path, Logger: test.NewTestLogger(), LevelVar: &slog.LevelVar{}}
			err := cmd.Execute(nil)
			require.Error(t, err, "validate passes a job the daemon refuses")
			assert.Contains(t, err.Error(), "backup")
		})
	}
}

func TestValidateExecute_PassesComposeJobWithoutFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.ini")
	require.NoError(t, os.WriteFile(path, []byte("[job-compose \"backup\"]\nschedule = @daily\nservice = db\n"), 0o644))
	cmd := &ValidateCommand{ConfigFile: path, Logger: test.NewTestLogger(), LevelVar: &slog.LevelVar{}}
	require.NoError(t, cmd.Execute(nil))
}

// A state-file job that can no longer be restored is reported, unless a job
// of the configuration runs under its name.
func TestRestore_RefusedPersistedJobIsReported(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"api-exec": {Type: persist.JobTypeExec, Schedule: "@hourly", Container: "c1"},
	}})
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	require.NoError(t, c.initPersistStore())

	assert.Nil(t, c.scheduler.GetAnyJob("api-exec"))
	assert.Contains(t, c.scheduler.GetUnschedulableJobs(), "api-exec")
}

func TestRestore_RefusedPersistedJobBesideAConfigJobIsNotReported(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	ini := &core.LocalJob{}
	ini.Name, ini.Schedule, ini.Command = "shared", "@daily", "true"
	require.NoError(t, c.scheduler.AddJob(ini))
	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"shared": {Type: persist.JobTypeExec, Schedule: "@hourly", Container: "c1"},
	}})
	require.NoError(t, c.initPersistStore())

	assert.Same(t, ini, c.scheduler.GetAnyJob("shared"))
	assert.NotContains(t, c.scheduler.GetUnschedulableJobs(), "shared")
}

// An exec label job takes its container's name, so it needs no container
// key; one without a command is refused and reported.
func TestBoot_LabelExecJobs(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	require.NoError(t, c.buildFromDockerContainers([]DockerContainerInfo{{
		Name:  "app",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                   "true",
			"ofelia.job-exec.backup.schedule":  "@daily",
			"ofelia.job-exec.backup.command":   "true",
			"ofelia.job-exec.nothing.schedule": "@daily",
		},
	}}))
	c.sh = core.NewScheduler(logger)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	c.registerAllJobs()

	assert.NotNil(t, c.sh.GetAnyJob("app.backup"), "an exec label job without a container key was refused: %v", c.sh.GetUnschedulableJobs())
	assert.Nil(t, c.sh.GetAnyJob("app.nothing"))
	assert.Contains(t, c.sh.GetUnschedulableJobs(), "app.nothing")
}
