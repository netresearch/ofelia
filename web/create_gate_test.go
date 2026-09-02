// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
)

// TestCreateJob_ForbiddenForConfigOwnedName pins the create-side origin
// gate. Update and delete refuse config-owned jobs; create did not, and
// AddJob is no backstop: a config job whose schedule is empty or
// malformed never reaches go-cron (Scheduler.AddJobWithTags records it
// as unschedulable and returns before cron.AddJob), so its name is free
// and ErrDuplicateName never fires. A create under that name replaced
// the config-owned job with a caller-chosen LocalJob and recorded
// origin=api, after which the update and delete gates no longer saw the
// job as config-owned either.
func TestCreateJob_ForbiddenForConfigOwnedName(t *testing.T) {
	t.Parallel()

	type stubLocalJob struct{ JobSource string }
	cfg := &struct {
		LocalJobs map[string]*stubLocalJob
	}{LocalJobs: map[string]*stubLocalJob{
		"ini-owned": {JobSource: "ini"},
	}}

	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, cfg, nil)

	// The config job has an empty schedule, so it was refused and holds no
	// cron entry — exactly the state in which create used to succeed.
	unschedulable := &deletableLocalJob{}
	unschedulable.Name = "ini-owned"
	require.Error(t, sched.AddJob(unschedulable), "empty schedule must be refused")

	resp := postJSON(t, srv, "/api/jobs/create",
		`{"name":"ini-owned","type":"local","schedule":"@daily","command":"echo hijacked"}`)
	assert.Equal(t, http.StatusForbidden, resp.Code,
		"create under a config-owned job name must return 403")
	assert.Contains(t, resp.Body.String(), "ini",
		"403 body must name the origin so operators know which source to edit")

	assert.Nil(t, sched.GetAnyJob("ini-owned"),
		"the refused create must not register a job")

	// The origin must still resolve to the config, or the update and
	// delete gates would stop recognizing the name.
	resp = postJSON(t, srv, "/api/jobs/delete", `{"name":"ini-owned"}`)
	assert.NotEqual(t, http.StatusNoContent, resp.Code,
		"delete must still refuse the config-owned name after a failed create")
}

// TestCreateJob_AllowedForUnknownName pins that the gate only refuses
// names the config owns: an ordinary create must still succeed.
func TestCreateJob_AllowedForUnknownName(t *testing.T) {
	t.Parallel()

	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, nil, nil)

	resp := postJSON(t, srv, "/api/jobs/create",
		`{"name":"api-owned","type":"local","schedule":"@daily","command":"echo hi"}`)
	require.Equal(t, http.StatusCreated, resp.Code)
	assert.NotNil(t, sched.GetAnyJob("api-owned"))
}
