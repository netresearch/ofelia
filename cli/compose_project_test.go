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

	defaults "github.com/creasty/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

const shopCompose = `name: shop
jobs:
  backup:
    image: alpine:3.22
    triggers:
      schedule:
        - cron: "0 2 * * *"
          timezone: Europe/Berlin
          missed_fires: skip
  cleanup:
    image: alpine:3.22
    triggers:
      schedule:
        - cron: "30 3 * * *"
          missed_fires: skip
  report:
    image: alpine:3.22
    triggers:
      manual: true
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestComposeProject_ExpandsScheduledJobs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	ini := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\nprofile = scheduled-jobs\n")

	c, err := BuildFromFile(ini, test.NewTestLogger())
	require.NoError(t, err)

	require.Contains(t, c.ComposeJobs, "shop.backup")
	require.Contains(t, c.ComposeJobs, "shop.cleanup")
	assert.NotContains(t, c.ComposeJobs, "shop.report", "a job without a schedule is not scheduled")

	j := c.ComposeJobs["shop.backup"]
	assert.Equal(t, "CRON_TZ=Europe/Berlin 0 2 * * *", j.Schedule)
	assert.Equal(t, "backup", j.Service)
	assert.Equal(t, []string{composePath}, j.Project.Files)
	assert.Equal(t, []string{"scheduled-jobs"}, j.Project.Profiles)
	assert.Equal(t, "shop", j.Project.ProjectName)
	assert.Equal(t, "Europe/Berlin", j.Project.TimeZone)
	assert.NotEmpty(t, j.Project.Revision)
	assert.True(t, j.NoOverlap, "concurrency: forbid is the per-job overlap prevention")
	assert.Equal(t, JobSourceINI, j.GetJobSource())
	assert.Equal(t, []string{composePath}, c.composeFiles, "the compose file is watched for reloads")
}

func TestComposeProject_FilesAndProfilesKeepTheirOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := writeFile(t, dir, "base.yaml", shopCompose)
	extra := writeFile(t, dir, "extra.yaml", "jobs:\n  backup:\n    command: [\"echo\", \"extra\"]\n")
	ini := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+base+"\nfile = "+extra+
		"\nprofile = a\nprofile = b\nproject-name = shop-prod\n")

	c, err := BuildFromFile(ini, test.NewTestLogger())
	require.NoError(t, err)
	j := c.ComposeJobs["shop.backup"]
	require.NotNil(t, j)
	assert.Equal(t, []string{base, extra}, j.Project.Files)
	assert.Equal(t, []string{"a", "b"}, j.Project.Profiles)
	assert.Equal(t, "shop-prod", j.Project.ProjectName)
}

func TestComposeProject_RejectedTriggerFailsTheConfiguration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml",
		"name: shop\njobs:\n  backup:\n    image: alpine:3.22\n    triggers:\n      schedule:\n        - \"0 2 * * *\"\n")
	ini := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")

	_, err := BuildFromFile(ini, test.NewTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `job "backup"`)
	assert.Contains(t, err.Error(), "set missed_fires: skip")
}

func TestComposeProject_NameCollisionWithJobCompose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	ini := writeFile(t, dir, "ofelia.ini", "[job-compose \"shop.backup\"]\nschedule = @daily\nservice = web\n"+
		"[compose-project \"shop\"]\nfile = "+composePath+"\n")

	_, err := BuildFromFile(ini, test.NewTestLogger())
	require.ErrorIs(t, err, ErrComposeJobNameTaken)
}

func TestComposeProject_JobComposeKeepsWorking(t *testing.T) {
	t.Parallel()
	c, err := BuildFromString("[job-compose \"web\"]\nschedule = @daily\nservice = web\nfile = c.yml\n", test.NewTestLogger())
	require.NoError(t, err)
	j := c.ComposeJobs["web"]
	require.NotNil(t, j)
	assert.Equal(t, "c.yml", j.File)
	assert.Empty(t, j.Project.Files, "a [job-compose] has no registered project")
}

// TestComposeProject_ReloadOnComposeFileChange changes only the compose file:
// the reload must see it, replace the changed job and keep the unchanged one.
func TestComposeProject_ReloadOnComposeFileChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")

	logger := test.NewTestLogger()
	c, err := BuildFromFile(iniPath, logger)
	require.NoError(t, err)
	c.sh = core.NewScheduler(logger)
	c.buildSchedulerMiddlewares(c.sh)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	c.levelVar = &slog.LevelVar{}
	for _, j := range c.ComposeJobs { // prepared as registerAllJobs does
		require.NoError(t, defaults.Set(j))
		require.NoError(t, c.sh.AddJob(j))
	}
	oldBackup, oldCleanup := c.ComposeJobs["shop.backup"], c.ComposeJobs["shop.cleanup"]

	changed := `name: shop
jobs:
  backup:
    image: alpine:3.22
    triggers:
      schedule:
        - cron: "0 5 * * *"
          timezone: Europe/Berlin
          missed_fires: skip
  cleanup:
    image: alpine:3.22
    triggers:
      schedule:
        - cron: "30 3 * * *"
          missed_fires: skip
`
	require.NoError(t, os.WriteFile(composePath, []byte(changed), 0o600))
	future := time.Now().Add(time.Minute)
	require.NoError(t, os.Chtimes(composePath, future, future))

	require.NoError(t, c.iniConfigUpdate())

	newBackup := c.ComposeJobs["shop.backup"]
	require.NotNil(t, newBackup)
	assert.Equal(t, "CRON_TZ=Europe/Berlin 0 5 * * *", newBackup.Schedule, "only the compose file changed")
	assert.NotEqual(t, oldBackup.Project.Revision, newBackup.Project.Revision)
	assert.NotSame(t, oldBackup, newBackup, "the changed job is replaced")
	assert.Same(t, oldCleanup, c.ComposeJobs["shop.cleanup"], "an unchanged job is kept")
}
