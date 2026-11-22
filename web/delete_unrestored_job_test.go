// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
	webpkg "github.com/netresearch/ofelia/web"
)

// A state-file job that a restart refused to restore is not in the scheduler.
// Deleting it through the API removes the entry and its report in the health
// check.
func TestDeleteJob_RemovesAJobTheRestoreRefused(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	store := persist.NewStore(path)
	require.NoError(t, store.PutJob("old", persist.Job{Type: persist.JobTypeExec, Schedule: "@daily", Container: "c1"}))
	sched := core.NewScheduler(stubDiscardLogger())
	sched.RecordRefusal("old", errors.New("command is required"))
	srv := withJobBuilder(webpkg.NewServer("", sched, nil, nil))
	srv.SetPersistStore(store)

	resp := postJSON(t, srv, "/api/jobs/delete", `{"name":"old"}`)
	require.Equal(t, http.StatusNoContent, resp.Code, resp.Body.String())

	reader := persist.NewStore(path)
	require.NoError(t, reader.Load())
	assert.NotContains(t, reader.Snapshot().Jobs, "old")
	assert.NotContains(t, sched.GetUnschedulableJobs(), "old")
}

// A name that neither the scheduler nor the state file holds is still not found.
func TestDeleteJob_UnknownNameWithStoreIsNotFound(t *testing.T) {
	t.Parallel()
	srv := newWebServerWithStore(t, persist.NewStore(filepath.Join(t.TempDir(), "state.json")))
	assert.Equal(t, http.StatusNotFound, postJSON(t, srv, "/api/jobs/delete", `{"name":"nobody"}`).Code)
}
