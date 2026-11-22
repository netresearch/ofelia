// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
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
