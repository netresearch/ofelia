// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/persist"
)

// An API job's own user works like an INI job's: a name wins over
// [global] default-user, and "default" selects the container's user.
func TestAPIJob_OwnUser(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, typ, user, want string
	}{
		{"run with a user", "run", "app", "app"},
		{"exec with a user", "exec", "app", "app"},
		{"run with user default", "run", UserContainerDefault, ""},
		{"exec with user default", "exec", UserContainerDefault, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAPIJobFixture(t, &mockDockerProviderForHandler{})
			rec := f.create(t, map[string]any{
				"name": "j", "type": tc.typ, "schedule": "@hourly",
				"image": "alpine", "container": "c1", "command": "true", "user": tc.user,
			})
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			user, ok := userOfCoreJob(f.cfg.sh.GetJob("j"))
			require.True(t, ok)
			assert.Equal(t, tc.want, user)
		})
	}
}

// The user reaches the state file through the create handler and comes
// back on restore.
func TestAPIJob_UserSurvivesRestart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	f := newAPIJobFixture(t, &mockDockerProviderForHandler{})
	srv := newAPIJobServer(f.cfg, &mockDockerProviderForHandler{})
	srv.SetPersistStore(persist.NewStore(path))
	body := `{"name":"owned","type":"exec","schedule":"@hourly","container":"c1","command":"true","user":"app"}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	reader := persist.NewStore(path)
	require.NoError(t, reader.Load())
	stored, ok := reader.Snapshot().Jobs["owned"]
	require.True(t, ok)
	assert.Equal(t, "app", stored.User)

	c := newDaemonForPersistTest(t)
	c.StateFile = path
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	require.NoError(t, c.initPersistStore())
	user, _ := userOfCoreJob(c.scheduler.GetJob("owned"))
	assert.Equal(t, "app", user)
}

// A state file written before the field existed has no user; its jobs
// take [global] default-user.
func TestAPIJob_StateFileWithoutUserLoads(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"old": {Type: persist.JobTypeExec, Schedule: "@hourly", Container: "c1", Command: "true"},
	}})
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	require.NoError(t, c.initPersistStore())
	user, _ := userOfCoreJob(c.scheduler.GetJob("old"))
	assert.Equal(t, "nobody", user)
}
