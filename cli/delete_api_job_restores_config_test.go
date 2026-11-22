// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/persist"
)

// Deleting an API job that took the name of an INI job hands the name back:
// the INI job is registered again and stays owned by the INI file.
func TestDeleteAPIJob_RegistersTheConfigJobItShadowed(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	cfg, err := BuildFromString("[job-local \"dup\"]\nschedule = @hourly\ncommand = echo ini\n", c.Logger)
	require.NoError(t, err)
	c.config = cfg
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	cfg.sh = c.scheduler
	cfg.registerAllJobs()

	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"dup": {Type: persist.JobTypeLocal, Schedule: "@daily", Command: "echo api"},
	}})
	require.NoError(t, c.initPersistStore())
	require.Equal(t, "echo api", c.scheduler.GetAnyJob("dup").GetCommand(), "precondition: the state file wins")
	c.WebAddr = "127.0.0.1:0"
	c.shutdownManager = core.NewShutdownManager(c.Logger, time.Second)
	require.NoError(t, c.setupWebServer())

	post := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		c.webServer.HTTPServer().Handler.ServeHTTP(rec, req)
		return rec
	}
	rec := post("/api/jobs/delete", `{"name":"dup"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	live := c.scheduler.GetAnyJob("dup")
	require.NotNil(t, live, "the INI job is not registered again")
	assert.Equal(t, "echo ini", live.GetCommand())
	assert.True(t, c.scheduler.EntryByName("dup").Valid())
	rec = post("/api/jobs/delete", `{"name":"dup"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, "the INI job became deletable through the API: %s", rec.Body.String())
}

// deleteThroughAPI deletes name through the daemon's web server.
func deleteThroughAPI(t *testing.T, c *DaemonCommand, name string) {
	t.Helper()
	c.WebAddr = "127.0.0.1:0"
	c.shutdownManager = core.NewShutdownManager(c.Logger, time.Second)
	require.NoError(t, c.setupWebServer())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/delete", strings.NewReader(`{"name":"`+name+`"}`))
	req.Header.Set("Content-Type", "application/json")
	c.webServer.HTTPServer().Handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// A pause of the deleted API job carries over to the configured job, as the
// persisted pause does after a restart.
func TestDeleteAPIJob_ConfigJobKeepsThePause(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	cfg, err := BuildFromString("[job-local \"dup\"]\nschedule = @hourly\ncommand = echo ini\n", c.Logger)
	require.NoError(t, err)
	c.config = cfg
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	cfg.sh = c.scheduler
	cfg.registerAllJobs()
	c.StateFile = writePersistFile(t, persist.State{
		Jobs:     map[string]*persist.Job{"dup": {Type: persist.JobTypeLocal, Schedule: "@daily", Command: "echo api"}},
		Disabled: []string{"dup"},
	})
	require.NoError(t, c.initPersistStore())
	require.True(t, c.scheduler.IsJobDisabled("dup"), "precondition: the API job is paused")

	deleteThroughAPI(t, c, "dup")

	require.NotNil(t, c.scheduler.GetAnyJob("dup"))
	assert.Equal(t, "echo ini", c.scheduler.GetAnyJob("dup").GetCommand())
	assert.True(t, c.scheduler.IsJobDisabled("dup"), "the configured job runs although the pause persists")
}

// A label job that boot rejected stays unregistered when the API job that
// held its name is deleted.
func TestDeleteAPIJob_InvalidConfigJobStaysUnregistered(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	cfg := NewConfig(c.Logger)
	require.NoError(t, cfg.buildFromDockerContainers([]DockerContainerInfo{{
		Name:  "app",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                      "true",
			"ofelia.service":                      "true",
			"ofelia.job-service-run.job.schedule": "@daily",
			"ofelia.job-service-run.job.command":  "echo hi",
		},
	}}))
	c.config = cfg
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	cfg.sh = c.scheduler
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: c.Logger}
	cfg.registerAllJobs()
	require.Nil(t, c.scheduler.GetAnyJob("job"), "precondition: boot rejects the label job")
	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"job": {Type: persist.JobTypeLocal, Schedule: "@daily", Command: "echo api"},
	}})
	require.NoError(t, c.initPersistStore())
	require.NotNil(t, c.scheduler.GetAnyJob("job"))

	deleteThroughAPI(t, c, "job")

	assert.Nil(t, c.scheduler.GetAnyJob("job"), "the rejected label job was registered")
}

// When the first job kind defining the name is refused, the next kind is
// tried, as boot does: here boot refused the exec job and registered the
// local job of the same name.
func TestDeleteAPIJob_FallsBackToTheNextJobKind(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	cfg, err := BuildFromString("[job-exec \"dup\"]\nschedule = not-a-schedule\ncontainer = app\ncommand = id\n"+
		"[job-local \"dup\"]\nschedule = @hourly\ncommand = echo ini\n", c.Logger)
	require.NoError(t, err)
	c.config = cfg
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	cfg.sh = c.scheduler
	cfg.registerAllJobs()
	require.Equal(t, "echo ini", c.scheduler.GetAnyJob("dup").GetCommand(), "precondition: boot registers the local job")
	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"dup": {Type: persist.JobTypeLocal, Schedule: "@daily", Command: "echo api"},
	}})
	require.NoError(t, c.initPersistStore())

	deleteThroughAPI(t, c, "dup")

	live := c.scheduler.GetAnyJob("dup")
	require.NotNil(t, live, "the name stays unregistered although the local job is valid")
	assert.Equal(t, "echo ini", live.GetCommand())
}

// A paused API job that shadowed a label job boot rejected leaves the pause
// for the label job once it is fixed.
func TestDeleteAPIJob_RejectedConfigJobKeepsThePauseForLater(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	cfg := NewConfig(c.Logger)
	require.NoError(t, cfg.buildFromDockerContainers([]DockerContainerInfo{{
		Name:  "app",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                      "true",
			"ofelia.service":                      "true",
			"ofelia.job-service-run.job.schedule": "@daily",
			"ofelia.job-service-run.job.command":  "echo hi",
		},
	}}))
	c.config = cfg
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	cfg.sh = c.scheduler
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: c.Logger}
	cfg.registerAllJobs()
	c.StateFile = writePersistFile(t, persist.State{
		Jobs:     map[string]*persist.Job{"job": {Type: persist.JobTypeLocal, Schedule: "@daily", Command: "echo api"}},
		Disabled: []string{"job"},
	})
	require.NoError(t, c.initPersistStore())

	deleteThroughAPI(t, c, "job")

	require.Nil(t, c.scheduler.GetAnyJob("job"))
	assert.True(t, cfg.wasDisabled("job"), "the pause is lost for when the label job is fixed")
}
