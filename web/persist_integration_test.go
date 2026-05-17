// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
	webpkg "github.com/netresearch/ofelia/web"
)

// TestPersist_CreateJob_WritesToStateFile pins the end-to-end #593
// contract: POST /api/jobs/create that succeeds must drop a matching
// entry into the persist.Store's file. Round-trips via a real
// httptest server + real on-disk file so the JSON encoding, atomic
// rename, and handler hook are all exercised.
func TestPersist_CreateJob_WritesToStateFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	store := persist.NewStore(path)
	srv := newWebServerWithStore(t, store)

	body := `{"name":"persisted-local","type":"local","schedule":"@hourly","command":"echo hi"}`
	resp := postJSON(t, srv, "/api/jobs/create", body)
	require.Equal(t, http.StatusCreated, resp.Code,
		"create with valid local-job payload must return 201")

	// Re-read the on-disk state from a fresh Store to prove durability
	// (not just in-memory cache).
	reader := persist.NewStore(path)
	require.NoError(t, reader.Load())
	snap := reader.Snapshot()
	require.Contains(t, snap.Jobs, "persisted-local",
		"create handler must call PutJob on the persist store")
	got := snap.Jobs["persisted-local"]
	assert.Equal(t, persist.JobTypeLocal, got.Type)
	assert.Equal(t, "@hourly", got.Schedule)
	assert.Equal(t, "echo hi", got.Command)
}

// TestPersist_DeleteJob_ForbiddenForNonAPIOrigin pins the design
// decision: API delete must refuse jobs that came from INI/labels.
// Pre-#593 this handler silently removed any job; the new behavior
// surfaces a 403 with the actual origin in the message so operators
// know to edit the source instead. Builds the scenario by passing a
// stub config struct shaped like cli.Config — the existing
// reflect-based jobOrigin() helper in server.go reads `JobSource`
// off the matching entry.
func TestPersist_DeleteJob_ForbiddenForNonAPIOrigin(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	store := persist.NewStore(path)

	// Stub config that jobOrigin() can reflect into — must expose a
	// LocalJobs map whose entries have a JobSource field. Matches the
	// cli.Config / cli.LocalJobConfig shape.
	type stubLocalJob struct{ JobSource string }
	cfg := &struct {
		LocalJobs map[string]*stubLocalJob
	}{LocalJobs: map[string]*stubLocalJob{
		"ini-owned": {JobSource: "ini"},
	}}

	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, cfg, nil)
	srv.SetPersistStore(store)

	// Add the matching job directly to the scheduler so the delete
	// handler can find it but won't see origin="api" in s.origins.
	job := &deletableLocalJob{}
	job.Name = "ini-owned"
	job.Schedule = "@hourly"
	require.NoError(t, sched.AddJob(job))

	resp := postJSON(t, srv, "/api/jobs/delete", `{"name":"ini-owned"}`)
	assert.Equal(t, http.StatusForbidden, resp.Code,
		"delete of non-api-origin job must return 403")
	assert.Contains(t, resp.Body.String(), "ini",
		"403 body must name the origin so operators know which source to edit")
}

// TestPersist_DeleteJob_RemovesFromStateFileWhenAPIOrigin pins that
// for an API-origin job, delete actually removes both the live
// scheduler entry AND the persisted entry — otherwise the file
// would resurrect the deleted job on the next restart.
func TestPersist_DeleteJob_RemovesFromStateFileWhenAPIOrigin(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	store := persist.NewStore(path)
	srv := newWebServerWithStore(t, store)

	require.Equal(t, http.StatusCreated,
		postJSON(t, srv, "/api/jobs/create",
			`{"name":"tmp","type":"local","schedule":"@hourly"}`).Code)
	require.Equal(t, http.StatusNoContent,
		postJSON(t, srv, "/api/jobs/delete", `{"name":"tmp"}`).Code)

	reader := persist.NewStore(path)
	require.NoError(t, reader.Load())
	snap := reader.Snapshot()
	assert.NotContains(t, snap.Jobs, "tmp",
		"API delete of api-origin job must remove the persisted entry — otherwise restart resurrects it")
}

// TestPersist_DisableJob_WritesToStateFile pins the per-design that
// disable IS persisted regardless of origin — so operators can pause
// an INI-defined job from the UI and have the pause survive restart.
// This is the second half of the "forbid delete, allow disable"
// design lock-in.
func TestPersist_DisableJob_WritesToStateFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	store := persist.NewStore(path)
	srv := newWebServerWithStore(t, store)

	// Job created via API (origin=api).
	require.Equal(t, http.StatusCreated,
		postJSON(t, srv, "/api/jobs/create",
			`{"name":"pauseme","type":"local","schedule":"@hourly"}`).Code)
	require.Equal(t, http.StatusNoContent,
		postJSON(t, srv, "/api/jobs/disable", `{"name":"pauseme"}`).Code)

	reader := persist.NewStore(path)
	require.NoError(t, reader.Load())
	snap := reader.Snapshot()
	assert.Contains(t, snap.Disabled, "pauseme",
		"disable handler must persist the name so the pause survives restart")
}

// TestPersist_EnableJob_ClearsFromStateFile pins the inverse —
// re-enabling removes the entry, so restart sees the job as enabled
// (matching the live state at the time of the API call).
func TestPersist_EnableJob_ClearsFromStateFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	store := persist.NewStore(path)
	require.NoError(t, store.SetDisabled("pre-disabled"))

	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, nil, nil)
	srv.SetPersistStore(store)

	// Need the job to exist in the scheduler for enable to succeed.
	job := &deletableLocalJob{}
	job.Name = "pre-disabled"
	job.Schedule = "@hourly"
	require.NoError(t, sched.AddJob(job))
	require.NoError(t, sched.DisableJob("pre-disabled"))

	require.Equal(t, http.StatusNoContent,
		postJSON(t, srv, "/api/jobs/enable", `{"name":"pre-disabled"}`).Code)

	reader := persist.NewStore(path)
	require.NoError(t, reader.Load())
	snap := reader.Snapshot()
	assert.NotContains(t, snap.Disabled, "pre-disabled",
		"enable handler must remove the name from persisted disabled list")
}

// TestPersist_CreateJob_IgnoresMaliciousXOriginIni pins the trust
// hardening: a client setting `X-Origin: ini` (or `label`) on a
// create request must NOT be marked as config-owned in the origin
// map, because that would make the job non-deletable via the API.
// All requests reaching this handler are API mutations regardless
// of what the header claims.
func TestPersist_CreateJob_IgnoresMaliciousXOriginIni(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	store := persist.NewStore(path)
	srv := newWebServerWithStore(t, store)

	body := `{"name":"trapped","type":"local","schedule":"@hourly"}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Origin", "ini") // malicious: claim config ownership
	w := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Job must still be persisted (since it was API-created in fact)
	// AND delete must succeed (since origin was forced back to api).
	reader := persist.NewStore(path)
	require.NoError(t, reader.Load())
	assert.Contains(t, reader.Snapshot().Jobs, "trapped",
		"server must persist any successful API mutation regardless of client-supplied X-Origin")

	resp := postJSON(t, srv, "/api/jobs/delete", `{"name":"trapped"}`)
	assert.Equal(t, http.StatusNoContent, resp.Code,
		"client-supplied X-Origin: ini must NOT lock the operator out of deleting their own API-created job")
}

// TestPersist_CreateJob_PersistsWhenOriginWeb pins that UI requests
// (which send `X-Origin: web`, see static/ui/index.html) get
// persisted too — pre-fix the check `if origin == "api"` only ever
// fired for header-less requests, so the entire web UI path silently
// dropped persistence.
func TestPersist_CreateJob_PersistsWhenOriginWeb(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	store := persist.NewStore(path)
	srv := newWebServerWithStore(t, store)

	body := `{"name":"from-ui","type":"local","schedule":"@hourly"}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Origin", "web") // exactly what static/ui/index.html sends
	w := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	reader := persist.NewStore(path)
	require.NoError(t, reader.Load())
	assert.Contains(t, reader.Snapshot().Jobs, "from-ui",
		"X-Origin: web requests must persist — the UI is the most common API client and the feature is moot without it")
}

// TestPersist_NoStore_HandlersUnchanged pins backward compatibility:
// when no persist store is wired (the default, pre-#593 behavior),
// the create/disable handlers still succeed and no file is touched —
// callers who haven't opted into --state-file see no behavior change.
func TestPersist_NoStore_HandlersUnchanged(t *testing.T) {
	t.Parallel()
	srv := newWebServerWithStore(t, nil) // no store wired

	require.Equal(t, http.StatusCreated,
		postJSON(t, srv, "/api/jobs/create",
			`{"name":"ephemeral","type":"local","schedule":"@hourly"}`).Code)
	require.Equal(t, http.StatusNoContent,
		postJSON(t, srv, "/api/jobs/disable", `{"name":"ephemeral"}`).Code)
}

// helpers ---------------------------------------------------------------

// deletableLocalJob is a minimal core.Job implementation we add to the
// scheduler in tests that need an existing job entry (so handlers find
// it) but don't need real Run behavior. The web handlers operate on
// name/origin so this stub is sufficient.
type deletableLocalJob struct{ core.BareJob }

func (*deletableLocalJob) Run(*core.Context) error { return nil }

func newWebServerWithStore(t *testing.T, store *persist.Store) *webpkg.Server {
	t.Helper()
	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, nil, nil)
	srv.SetPersistStore(store)
	return srv
}

func postJSON(t *testing.T, srv *webpkg.Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(w, req)
	return w
}
