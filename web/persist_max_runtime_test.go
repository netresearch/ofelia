// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// TestPersist_CreateRunJob_WritesMaxRuntime closes the one link of the
// maxRuntime persistence chain that nothing else reaches.
//
// The API-side test drives newRunJobFromRequest and stops at the job; the
// cli-side test hand-builds a persist.Job and starts from there. Neither
// crosses the handler's own `j.MaxRuntime = req.MaxRuntime` in persistJob,
// so deleting that line left the whole suite green while a restart
// silently lost every override — the exact drift this PR exists to
// prevent, one link further along.
//
// This drives the real create handler against a real on-disk store and
// reads the value back through a fresh Store, so the handler hook, the
// JSON encoding and the atomic rename are all exercised.
func TestPersist_CreateRunJob_WritesMaxRuntime(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store := persist.NewStore(path)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer("", core.NewScheduler(logger), nil, newHangingDockerProvider())
	srv.SetPersistStore(store)

	body := `{"name":"bounded","type":"run","schedule":"@hourly","image":"busybox","maxRuntime":"30m"}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create returned %d, want 201: %s", w.Code, w.Body.String())
	}

	// A fresh Store proves durability rather than an in-memory cache.
	reader := persist.NewStore(path)
	if err := reader.Load(); err != nil {
		t.Fatalf("reload the state file: %v", err)
	}
	got, ok := reader.Snapshot().Jobs["bounded"]
	if !ok {
		t.Fatal("the created job is not in the state file")
	}
	if got.MaxRuntime != "30m" {
		t.Fatalf("MaxRuntime = %q in the state file, want %q — the override would be "+
			"lost on the next daemon restart", got.MaxRuntime, "30m")
	}
}
