// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

func buildAPIJob(t *testing.T, def persist.Job) (core.Job, error) {
	t.Helper()
	f := newAPIJobFixture(t, &mockDockerProviderForHandler{})
	return f.cfg.BuildAPIJob("guard", def)
}

// A run job from the API or the state file names its own max-runtime or
// none; without one it runs under the scheduler's [global] max-runtime.
// An unparsable value fails the request instead of falling back.
func TestBuildAPIJob_RunMaxRuntime(t *testing.T) {
	t.Parallel()

	job, err := buildAPIJob(t, persist.Job{Type: persist.JobTypeRun, Image: "busybox", MaxRuntime: "45m"})
	require.NoError(t, err)
	assert.Equal(t, 45*time.Minute, job.(*core.RunJob).MaxRuntime)

	job, err = buildAPIJob(t, persist.Job{Type: persist.JobTypeRun, Image: "busybox"})
	require.NoError(t, err)
	assert.Zero(t, job.(*core.RunJob).MaxRuntime, "no per-job override")

	_, err = buildAPIJob(t, persist.Job{Type: persist.JobTypeRun, Image: "busybox", MaxRuntime: "not-a-duration"})
	assert.Error(t, err)
}

// A compose job follows the same max-runtime ladder as a run job and an
// INI compose job: its own value, else the scheduler's global at run time.
// Only a run-mode job inherits.
func TestBuildAPIJob_ComposeMaxRuntime(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		def      persist.Job
		want     time.Duration
		inherits bool
	}{
		"omitted inherits the global": {def: persist.Job{}, want: 0, inherits: true},
		"explicit value wins":         {def: persist.Job{MaxRuntime: "30m"}, want: 30 * time.Minute, inherits: true},
		"exec mode inherits nothing":  {def: persist.Job{Exec: true}, want: 0, inherits: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			def := tc.def
			def.Type, def.Service = persist.JobTypeCompose, "task"
			job, err := buildAPIJob(t, def)
			require.NoError(t, err)
			cj, ok := job.(*core.ComposeJob)
			require.True(t, ok, "got %T", job)
			assert.Equal(t, tc.want, cj.MaxRuntime)
			assert.Equal(t, tc.inherits, cj.InheritsGlobalMaxRuntime())
		})
	}
}

func TestBuildAPIJob_ComposeRejectsMaxRuntime(t *testing.T) {
	t.Parallel()

	_, err := buildAPIJob(t, persist.Job{Type: persist.JobTypeCompose, Service: "task", MaxRuntime: "-5m"})
	require.ErrorIs(t, err, core.ErrMaxRuntimeNegative)

	_, err = buildAPIJob(t, persist.Job{Type: persist.JobTypeCompose, Service: "task", Exec: true, MaxRuntime: "5m"})
	require.ErrorIs(t, err, core.ErrComposeExecMaxRuntime,
		"the strict check runs before assembly, which would drop the value with a warning")
}

// A run job restored from the state file removes its container after the
// run, like one the API creates: every restart rebuilds it.
func TestBuildAPIJob_RestoredRunJobDeletesItsContainer(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	job, err := c.persistedJobToScheduler("persisted-run-job", &persist.Job{
		Type: persist.JobTypeRun, Schedule: "@hourly", Image: "busybox",
	})
	require.NoError(t, err)
	rj, ok := job.(*core.RunJob)
	require.True(t, ok, "got %T", job)
	assert.Equal(t, "true", rj.Delete)
}

// The compose job's maxRuntime reaches the state file through the create
// handler, or a restart drops the override.
func TestAPIJob_CreateComposeJobWritesMaxRuntime(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	f := newAPIJobFixture(t, &mockDockerProviderForHandler{})
	srv := newAPIJobServer(f.cfg, &mockDockerProviderForHandler{})
	srv.SetPersistStore(persist.NewStore(path))

	body := `{"name":"bounded","type":"compose","schedule":"@hourly","service":"task","maxRuntime":"30m"}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	reader := persist.NewStore(path)
	require.NoError(t, reader.Load())
	got, ok := reader.Snapshot().Jobs["bounded"]
	require.True(t, ok, "the created job is not in the state file")
	assert.Equal(t, "30m", got.MaxRuntime)
}
