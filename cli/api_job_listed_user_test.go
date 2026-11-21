// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// listedUsers returns, per job name, the "user" GET /api/jobs shows, and
// whether the field is present.
func listedUsers(t *testing.T, h http.Handler) map[string]*string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var jobs []struct {
		Name string  `json:"name"`
		User *string `json:"user"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &jobs))
	out := map[string]*string{}
	for _, j := range jobs {
		out[j.Name] = j.User
	}
	return out
}

// GET /api/jobs shows the user an API job's definition names, so the web
// UI can edit it without turning an inherited user into a fixed one.
func TestAPIJob_ListShowsTheDefinedUser(t *testing.T) {
	t.Parallel()
	f := newAPIJobFixture(t, &mockDockerProviderForHandler{})
	for _, b := range []map[string]any{
		{"name": "inherits", "type": "exec", "schedule": "@hourly", "container": "c1"},
		{"name": "own", "type": "exec", "schedule": "@hourly", "container": "c1", "user": "app"},
		{"name": "local", "type": "local", "schedule": "@hourly", "command": "echo"},
	} {
		require.Equal(t, http.StatusCreated, f.create(t, b).Code)
	}
	users := listedUsers(t, f.handler)
	require.NotNil(t, users["inherits"])
	assert.Empty(t, *users["inherits"], "an omitted user is listed as empty, not as the resolved nobody")
	require.NotNil(t, users["own"])
	assert.Equal(t, "app", *users["own"])
	assert.Nil(t, users["local"], "a local job has no user")
}

// A job restored from the state file shows its stored user too.
func TestSetupWebServer_ListsTheRestoredUser(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"restored": {Type: persist.JobTypeExec, Schedule: "@hourly", Container: "c1", User: "default"},
	}})
	require.NoError(t, c.initPersistStore())
	c.WebAddr = "127.0.0.1:0"
	c.shutdownManager = core.NewShutdownManager(c.Logger, time.Second)
	require.NoError(t, c.setupWebServer())

	users := listedUsers(t, c.webServer.HTTPServer().Handler)
	require.NotNil(t, users["restored"])
	assert.Equal(t, "default", *users["restored"])
}
