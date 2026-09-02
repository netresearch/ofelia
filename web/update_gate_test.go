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

// TestUpdateJob_ForbiddenForConfigOwnedOrigin pins the update-side origin
// gate, mirroring the delete gate (#593). Before the gate, an update
// silently overrode a config-owned job in memory until the next config
// sync — and rewrote the job's origin to api/web, after which the DELETE
// gate no longer recognized the job as config-owned: editing a label job
// unlocked deleting it. Update must refuse ini/label jobs the same way
// delete does.
func TestUpdateJob_ForbiddenForConfigOwnedOrigin(t *testing.T) {
	t.Parallel()

	type stubLocalJob struct{ JobSource string }
	cfg := &struct {
		LocalJobs map[string]*stubLocalJob
	}{LocalJobs: map[string]*stubLocalJob{
		"ini-owned": {JobSource: "ini"},
	}}

	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, cfg, nil)

	job := &deletableLocalJob{}
	job.Name = "ini-owned"
	job.Schedule = "@hourly"
	require.NoError(t, sched.AddJob(job))

	resp := postJSON(t, srv, "/api/jobs/update",
		`{"name":"ini-owned","type":"local","schedule":"@daily","command":"echo hijacked"}`)
	assert.Equal(t, http.StatusForbidden, resp.Code,
		"update of a config-owned job must return 403")
	assert.Contains(t, resp.Body.String(), "ini",
		"403 body must name the origin so operators know which source to edit")

	// The job must be untouched — schedule and command as before.
	live := sched.GetAnyJob("ini-owned")
	require.NotNil(t, live)
	assert.Equal(t, "@hourly", live.GetSchedule(), "update must not modify the job")
}

// TestUpdateJob_PausedJobStaysPaused pins that editing a paused job neither
// resumes it nor files a phantom entry under Removed. The scheduler's atomic
// update used to refuse disabled jobs, so this handler fell back to
// RemoveJob+AddJob: RemoveJob drops the disabledNames entry (the job came back
// ACTIVE and could fire on its next slot) and appends the old copy to Removed,
// where nothing prunes it — so the Removed tab listed a job that was still
// alive, one row per edit. Non-browser API clients hit this with no client-side
// compensation at all.
func TestUpdateJob_PausedJobStaysPaused(t *testing.T) {
	t.Parallel()

	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, nil, nil)

	job := &deletableLocalJob{}
	job.Name = "paused-edit"
	job.Schedule = "@hourly"
	job.Command = "original"
	require.NoError(t, sched.AddJob(job))
	require.NoError(t, sched.DisableJob("paused-edit"))

	resp := postJSON(t, srv, "/api/jobs/update",
		`{"name":"paused-edit","type":"local","schedule":"@daily","command":"updated"}`)
	require.Equal(t, http.StatusOK, resp.Code, "update of a paused job must succeed")

	live := sched.GetAnyJob("paused-edit")
	require.NotNil(t, live)
	assert.Equal(t, "@daily", live.GetSchedule(), "the edit must be applied")

	disabled := sched.GetDisabledJobs()
	require.Len(t, disabled, 1, "the job must still be paused after the edit")
	assert.Equal(t, "paused-edit", disabled[0].GetName())
	assert.Empty(t, sched.GetActiveJobs(), "editing a paused job must not resume it")
	assert.Empty(t, sched.GetRemovedJobs(), "editing a job must not file it under Removed")
}
