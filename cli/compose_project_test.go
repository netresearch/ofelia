// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// The unit tests must not depend on the docker compose CLI installed on the
// host; TestComposeProject_OldComposeCLIRejectsProjects and the
// integration test swap the check explicitly.
func init() {
	composeCLICheck = func(context.Context) error { return nil }
}

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

func TestComposeProject_RejectedTriggerRejectsOnlyTheProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml",
		"name: shop\njobs:\n  backup:\n    image: alpine:3.22\n    triggers:\n      schedule:\n        - \"0 2 * * *\"\n")
	ini := writeFile(t, dir, "ofelia.ini", "[job-local \"other\"]\nschedule = @daily\ncommand = true\n"+
		"[compose-project \"shop\"]\nfile = "+composePath+"\n")
	logger, handler := test.NewTestLoggerWithHandler()

	c, err := BuildFromFile(ini, logger)

	require.NoError(t, err, "a rejected project must not fail the configuration")
	assert.Empty(t, c.ComposeJobs, "the rejected project schedules no job")
	assert.Contains(t, c.LocalJobs, "other", "other jobs are unaffected")
	assert.True(t, handler.HasError(`compose project "shop" rejected`))
	assert.True(t, handler.HasError(`job "backup"`))
	assert.True(t, handler.HasError("set missed_fires: skip"))
}

func TestComposeProject_NameCollisionWithJobCompose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	ini := writeFile(t, dir, "ofelia.ini", "[job-compose \"shop.backup\"]\nschedule = @daily\nservice = web\n"+
		"[compose-project \"shop\"]\nfile = "+composePath+"\n")
	logger, handler := test.NewTestLoggerWithHandler()

	c, err := BuildFromFile(ini, logger)

	require.NoError(t, err)
	assert.Equal(t, "web", c.ComposeJobs["shop.backup"].Service, "the [job-compose] keeps its name")
	assert.NotContains(t, c.ComposeJobs, "shop.cleanup", "a rejected project schedules none of its jobs")
	assert.True(t, handler.HasError(ErrComposeJobNameTaken.Error()))
	assert.False(t, c.composeProjectsChanged(), "an unchanged collision must not reload on every poll")
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

// TestComposeProject_OldComposeCLIRejectsProjects swaps the package check; it
// must not run in parallel with tests that build compose projects.
func TestComposeProject_OldComposeCLIRejectsProjects(t *testing.T) {
	old := composeCLICheck
	cliErr := fmt.Errorf("%w: found docker compose 2.38.2", core.ErrComposeJobsUnsupported)
	composeCLICheck = func(context.Context) error { return cliErr }
	t.Cleanup(func() { composeCLICheck = old })
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	ini := writeFile(t, dir, "ofelia.ini", "[job-local \"other\"]\nschedule = @daily\ncommand = true\n"+
		"[compose-project \"shop\"]\nfile = "+composePath+"\n")
	logger, handler := test.NewTestLoggerWithHandler()

	c, err := BuildFromFile(ini, logger)

	require.NoError(t, err, "a missing or old Compose CLI must not stop other jobs")
	assert.Empty(t, c.ComposeJobs)
	assert.Contains(t, c.LocalJobs, "other")
	assert.True(t, handler.HasError("2.38.2"))
	assert.False(t, c.composeProjectsChanged(), "an unchanged rejection must not reload on every poll")

	composeCLICheck = func(context.Context) error { return nil }
	assert.True(t, c.composeProjectsChanged(), "a CLI upgrade must reload the rejected project")
}

// TestComposeProject_RejectedProjectReloadsWhenFixed checks that a rejected
// project does not reload on every poll and reloads once it loads.
func TestComposeProject_RejectedProjectReloadsWhenFixed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml",
		"name: shop\njobs:\n  backup:\n    image: alpine:3.22\n    triggers:\n      schedule:\n        - \"0 2 * * *\"\n")
	ini := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")

	c, err := BuildFromFile(ini, test.NewTestLogger())
	require.NoError(t, err)
	require.Empty(t, c.ComposeJobs)
	assert.False(t, c.composeProjectsChanged(), "an unchanged rejection must not reload on every poll")

	require.NoError(t, os.WriteFile(composePath, []byte(shopCompose), 0o600))
	assert.True(t, c.composeProjectsChanged(), "a fixed project must reload")
}

// TestComposeProject_NoProjectSkipsTheCLICheck keeps a configuration without
// [compose-project] sections independent of the docker compose CLI.
func TestComposeProject_NoProjectSkipsTheCLICheck(t *testing.T) {
	old := composeCLICheck
	called := false
	composeCLICheck = func(context.Context) error { called = true; return core.ErrComposeJobsUnsupported }
	t.Cleanup(func() { composeCLICheck = old })

	_, err := BuildFromString("[job-local \"x\"]\nschedule = @daily\ncommand = true\n", test.NewTestLogger())

	require.NoError(t, err)
	assert.False(t, called)
}

// prepareReload builds the configuration from iniPath and registers its jobs
// with a scheduler, as the daemon does before a reload can happen.
func prepareReload(t *testing.T, iniPath string) *Config {
	t.Helper()
	logger := test.NewTestLogger()
	c, err := BuildFromFile(iniPath, logger)
	require.NoError(t, err)
	c.sh = core.NewScheduler(logger)
	c.buildSchedulerMiddlewares(c.sh)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	c.levelVar = &slog.LevelVar{}
	for _, j := range c.ComposeJobs {
		require.NoError(t, defaults.Set(j))
		require.NoError(t, c.sh.AddJob(j))
	}
	return c
}

// TestComposeProject_ReloadOnIncludedFileChange changes only a file pulled in
// through include:, which the registration does not name.
func TestComposeProject_ReloadOnIncludedFileChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jobs := func(cron string) string {
		return "jobs:\n  backup:\n    image: alpine:3.22\n    triggers:\n      schedule:\n        - cron: \"" + cron +
			"\"\n          missed_fires: skip\n  cleanup:\n    image: alpine:3.22\n    triggers:\n      schedule:\n" +
			"        - cron: \"30 3 * * *\"\n          missed_fires: skip\n"
	}
	jobsPath := writeFile(t, dir, "jobs.yaml", jobs("0 2 * * *"))
	composePath := writeFile(t, dir, "compose.yaml", "name: shop\ninclude:\n  - jobs.yaml\n")
	c := prepareReload(t, writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n"))
	oldBackup, oldCleanup := c.ComposeJobs["shop.backup"], c.ComposeJobs["shop.cleanup"]
	require.NotNil(t, oldBackup)

	require.NoError(t, os.WriteFile(jobsPath, []byte(jobs("0 5 * * *")), 0o600))
	require.NoError(t, c.iniConfigUpdate())

	assert.Equal(t, "0 5 * * *", c.ComposeJobs["shop.backup"].Schedule, "the included file changed")
	assert.NotSame(t, oldBackup, c.ComposeJobs["shop.backup"])
	assert.Same(t, oldCleanup, c.ComposeJobs["shop.cleanup"], "an unchanged job is kept")
}

// TestComposeProject_ReloadOnDefaultDotEnvChange changes only the default
// .env, which the registration does not name either.
func TestComposeProject_ReloadOnDefaultDotEnvChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	envPath := writeFile(t, dir, ".env", "HOUR=2\n")
	composePath := writeFile(t, dir, "compose.yaml",
		"name: shop\njobs:\n  backup:\n    image: alpine:3.22\n    triggers:\n      schedule:\n"+
			"        - cron: \"0 ${HOUR} * * *\"\n          missed_fires: skip\n")
	c := prepareReload(t, writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n"))
	require.Equal(t, "0 2 * * *", c.ComposeJobs["shop.backup"].Schedule)

	require.NoError(t, os.WriteFile(envPath, []byte("HOUR=6\n"), 0o600))
	require.NoError(t, c.iniConfigUpdate())

	assert.Equal(t, "0 6 * * *", c.ComposeJobs["shop.backup"].Schedule, "the default .env changed")
}

// TestComposeProject_NoChangeKeepsJobs polls without any change: no job is
// replaced.
func TestComposeProject_NoChangeKeepsJobs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	c := prepareReload(t, writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n"))
	before := c.ComposeJobs["shop.cleanup"]

	require.NoError(t, c.iniConfigUpdate())

	assert.Same(t, before, c.ComposeJobs["shop.cleanup"])
}

// TestComposeProject_RunUsesResolvedContext: profiles and the project name
// that came from the project's .env reach the job, so `docker compose run`
// gets exactly the context the project was loaded with.
func TestComposeProject_RunUsesResolvedContext(t *testing.T) {
	t.Setenv("COMPOSE_PROFILES", "")
	require.NoError(t, os.Unsetenv("COMPOSE_PROFILES"))
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml",
		"jobs:\n  backup:\n    image: alpine:3.22\n    profiles: [\"sched\"]\n    triggers:\n      schedule:\n"+
			"        - cron: \"0 2 * * *\"\n          missed_fires: skip\n")
	writeFile(t, dir, ".env", "COMPOSE_PROFILES=sched\nCOMPOSE_PROJECT_NAME=fromenv\n")
	ini := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")

	c, err := BuildFromFile(ini, test.NewTestLogger())
	require.NoError(t, err)

	j := c.ComposeJobs["shop.backup"]
	require.NotNil(t, j, "COMPOSE_PROFILES from .env arms the job")
	assert.Equal(t, []string{"sched"}, j.Project.Profiles)
	assert.Equal(t, "fromenv", j.Project.ProjectName)
	assert.Equal(t, dir, j.Project.ProjectDirectory)
}

// TestComposeProject_ReloadKeepsJobsOfRejectedProject breaks a loaded
// project's Compose file: the reload must keep its last loaded jobs, stay quiet
// until the file changes again, and replace them once the project loads.
func TestComposeProject_ReloadKeepsJobsOfRejectedProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")
	c := prepareReload(t, iniPath)
	oldBackup := c.ComposeJobs["shop.backup"]
	require.NotNil(t, oldBackup)

	touch := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(composePath, []byte(content), 0o600))
		future := time.Now().Add(time.Minute)
		require.NoError(t, os.Chtimes(composePath, future, future))
	}
	touch("name: shop\njobs:\n  backup:\n    image: alpine:3.22\n    triggers:\n      schedule:\n        - \"0 2 * * *\"\n")
	require.NoError(t, c.iniConfigUpdate())
	assert.Same(t, oldBackup, c.ComposeJobs["shop.backup"], "a rejected reload keeps the last loaded job")
	assert.Contains(t, c.ComposeJobs, "shop.cleanup")
	assert.False(t, c.composeProjectsChanged(), "the rejection is not reloaded on every poll")

	fixed := strings.Replace(shopCompose, `cron: "0 2 * * *"`, `cron: "0 6 * * *"`, 1)
	touch(fixed)
	future := time.Now().Add(2 * time.Minute)
	require.NoError(t, os.Chtimes(composePath, future, future))
	require.NoError(t, c.iniConfigUpdate())
	newBackup := c.ComposeJobs["shop.backup"]
	require.NotNil(t, newBackup)
	assert.Equal(t, "CRON_TZ=Europe/Berlin 0 6 * * *", newBackup.Schedule, "the fixed project is loaded")
}

// touchFile writes content to p and moves its modification time d ahead, so
// the next poll sees the file as changed.
func touchFile(t *testing.T, p, content string, d time.Duration) {
	t.Helper()
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	f := time.Now().Add(d)
	require.NoError(t, os.Chtimes(p, f, f))
}

const brokenShopCompose = "name: shop\njobs:\n  backup:\n    image: alpine:3.22\n    triggers:\n      schedule:\n        - \"0 2 * * *\"\n"

// Kept jobs must survive a second reload while the project is still rejected.
func TestComposeProject_KeptJobsSurviveSecondReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	iniBody := "[compose-project \"shop\"]\nfile = " + composePath + "\n"
	iniPath := writeFile(t, dir, "ofelia.ini", iniBody)
	c := prepareReload(t, iniPath)
	old := c.ComposeJobs["shop.backup"]
	require.NotNil(t, old)

	touchFile(t, composePath, brokenShopCompose, time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	require.Same(t, old, c.ComposeJobs["shop.backup"], "first reload keeps")

	// an unrelated INI edit
	touchFile(t, iniPath, "[job-local \"x\"]\nschedule = @daily\ncommand = true\n"+iniBody, 2*time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	assert.Same(t, old, c.ComposeJobs["shop.backup"], "second reload while still rejected must keep")
}

// Editing a registration in the INI must not leave a reload on every poll.
func TestComposeProject_EditedRegistrationDoesNotLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", "name: shop\njobs:\n  backup:\n    image: alpine:3.22\n    profiles: [a]\n    triggers:\n      schedule:\n        - cron: \"0 2 * * *\"\n          missed_fires: skip\n  other:\n    image: alpine:3.22\n    profiles: [b]\n    triggers:\n      schedule:\n        - cron: \"0 3 * * *\"\n          missed_fires: skip\n")
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\nprofile = a\n")
	c := prepareReload(t, iniPath)
	require.Contains(t, c.ComposeJobs, "shop.backup")

	touchFile(t, iniPath, "[compose-project \"shop\"]\nfile = "+composePath+"\nprofile = b\n", time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	require.Contains(t, c.ComposeJobs, "shop.other")
	assert.False(t, c.composeProjectsChanged(), "after applying the edit, the next poll must not reload again")
}

// Removing one of two projects must not leave a reload on every poll.
func TestComposeProject_RemovedProjectDoesNotLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := writeFile(t, dir, "a.yaml", shopCompose)
	b := writeFile(t, dir, "b.yaml", "name: other\n"+shopCompose[len("name: shop\n"):])
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+a+"\n[compose-project \"other\"]\nfile = "+b+"\n")
	c := prepareReload(t, iniPath)
	touchFile(t, iniPath, "[compose-project \"shop\"]\nfile = "+a+"\n", time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	assert.NotContains(t, c.ComposeJobs, "other.backup")
	assert.False(t, c.composeProjectsChanged(), "next poll must not reload again")
}

// A project added at runtime must reload on a default .env change.
func TestComposeProject_AddedProjectDotEnvDetected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	envPath := writeFile(t, dir, ".env", "HOUR=2\n")
	composePath := writeFile(t, dir, "compose.yaml",
		"name: shop\njobs:\n  backup:\n    image: alpine:3.22\n    triggers:\n      schedule:\n"+
			"        - cron: \"0 ${HOUR} * * *\"\n          missed_fires: skip\n")
	iniPath := writeFile(t, dir, "ofelia.ini", "[job-local \"x\"]\nschedule = @daily\ncommand = true\n")
	c := prepareReload(t, iniPath)
	touchFile(t, iniPath, "[job-local \"x\"]\nschedule = @daily\ncommand = true\n[compose-project \"shop\"]\nfile = "+composePath+"\n", time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	require.Equal(t, "0 2 * * *", c.ComposeJobs["shop.backup"].Schedule)
	require.NoError(t, os.WriteFile(envPath, []byte("HOUR=6\n"), 0o600))
	assert.True(t, c.composeProjectsChanged(), "a .env change of a project added at runtime must be detected")
}

// TestValidate_FailsOnRejectedComposeProject: the daemon runs without a
// rejected project, so `ofelia validate` is where the rejection must surface.
func TestValidate_FailsOnRejectedComposeProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", brokenShopCompose)
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n")
	cmd := &ValidateCommand{ConfigFile: iniPath, Logger: test.NewTestLogger(), LevelVar: &slog.LevelVar{}}

	err := cmd.Execute(nil)

	require.ErrorIs(t, err, ErrComposeProjectRejected)
	assert.Contains(t, err.Error(), "shop")
}

const shopProdCompose = "name: shopprod\njobs:\n  nightly:\n    image: alpine:3.22\n    triggers:\n      schedule:\n" +
	"        - cron: \"0 4 * * *\"\n          missed_fires: skip\n"

// Registrations may contain dots: shop is rejected (still broken), then
// shop.prod is removed from the INI and must lose its jobs.
func TestComposeProject_DottedRegistrationRemovedWhileOtherRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shop := writeFile(t, dir, "shop.yaml", shopCompose)
	prod := writeFile(t, dir, "prod.yaml", shopProdCompose)
	shopReg := "[compose-project \"shop\"]\nfile = " + shop + "\n"
	prodReg := "[compose-project \"shop.prod\"]\nfile = " + prod + "\n"
	iniPath := writeFile(t, dir, "ofelia.ini", shopReg+prodReg)
	c := prepareReload(t, iniPath)
	require.Contains(t, c.ComposeJobs, "shop.prod.nightly")

	touchFile(t, shop, brokenShopCompose, time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	require.Contains(t, c.ComposeJobs, "shop.backup", "shop kept")

	touchFile(t, iniPath, shopReg, 2*time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	assert.NotContains(t, c.ComposeJobs, "shop.prod.nightly", "shop.prod was removed from the INI")
}

// Both edited in one go: shop broken and shop.prod removed.
func TestComposeProject_DottedRegistrationRemovedInSameReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shop := writeFile(t, dir, "shop.yaml", shopCompose)
	prod := writeFile(t, dir, "prod.yaml", shopProdCompose)
	shopReg := "[compose-project \"shop\"]\nfile = " + shop + "\n"
	prodReg := "[compose-project \"shop.prod\"]\nfile = " + prod + "\n"
	iniPath := writeFile(t, dir, "ofelia.ini", shopReg+prodReg)
	c := prepareReload(t, iniPath)

	touchFile(t, shop, brokenShopCompose, time.Minute)
	touchFile(t, iniPath, shopReg, time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	assert.NotContains(t, c.ComposeJobs, "shop.prod.nightly", "shop.prod was removed from the INI")
}

// shop rejected; shop.prod drops a job from its compose file.
func TestComposeProject_DottedRegistrationDropsJobWhileOtherRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shop := writeFile(t, dir, "shop.yaml", shopCompose)
	prod := writeFile(t, dir, "prod.yaml", shopProdCompose+"  weekly:\n    image: alpine:3.22\n    triggers:\n      schedule:\n"+
		"        - cron: \"0 5 * * 0\"\n          missed_fires: skip\n")
	iniPath := writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+shop+
		"\n[compose-project \"shop.prod\"]\nfile = "+prod+"\n")
	c := prepareReload(t, iniPath)
	require.Contains(t, c.ComposeJobs, "shop.prod.weekly")

	touchFile(t, shop, brokenShopCompose, time.Minute)
	touchFile(t, prod, shopProdCompose, time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	assert.NotContains(t, c.ComposeJobs, "shop.prod.weekly", "shop.prod dropped the job")
}

// Claim (a) control: one project, removed from the INI while rejected.
func TestComposeProject_RemovedWhileRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shop := writeFile(t, dir, "shop.yaml", shopCompose)
	iniPath := writeFile(t, dir, "ofelia.ini", "[job-local \"x\"]\nschedule = @daily\ncommand = true\n[compose-project \"shop\"]\nfile = "+shop+"\n")
	c := prepareReload(t, iniPath)
	touchFile(t, shop, brokenShopCompose, time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	require.Contains(t, c.ComposeJobs, "shop.backup")
	touchFile(t, shop, brokenShopCompose+"# again\n", 2*time.Minute) // third reload, still rejected
	require.NoError(t, c.iniConfigUpdate())
	require.Contains(t, c.ComposeJobs, "shop.backup", "third reload keeps")
	touchFile(t, iniPath, "[job-local \"x\"]\nschedule = @daily\ncommand = true\n", 3*time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	assert.Empty(t, c.ComposeJobs, "removed project loses its jobs")
	assert.False(t, c.composeProjectsChanged())
}

// Never loaded project added at runtime keeps nothing; and no loop.
func TestComposeProject_NeverLoadedKeepsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shop := writeFile(t, dir, "shop.yaml", brokenShopCompose)
	iniPath := writeFile(t, dir, "ofelia.ini", "[job-local \"x\"]\nschedule = @daily\ncommand = true\n")
	c := prepareReload(t, iniPath)
	touchFile(t, iniPath, "[job-local \"x\"]\nschedule = @daily\ncommand = true\n[compose-project \"shop\"]\nfile = "+shop+"\n", time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	assert.Empty(t, c.ComposeJobs)
	assert.False(t, c.composeProjectsChanged(), "rejected new project must not loop")
}
