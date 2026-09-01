// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
)

// TestDashboardHistoryElision pins that the open job's history is sent
// once and then only when it changes.
//
// The history rides along on every 5s poll while the modal is open. For a
// chatty job that is the full stdout and stderr of every retained run —
// up to HistoryLimit × 2 × 10 MB — serialized and compressed per tick and
// then discarded client-side on a fingerprint match. The response now
// carries that fingerprint; a client echoing it back gets the history
// omitted.
func TestDashboardHistoryElision(t *testing.T) {
	t.Parallel()

	job := &testJob{}
	job.Name = "chatty"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	first, _ := core.NewExecution()
	_, _ = first.OutputStream.Write([]byte(strings.Repeat("x", 4096)))
	job.SetLastRun(first)

	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: stubDiscardLogger()}
	handler := webpkg.NewServer("", sched, nil, nil).HTTPServer().Handler

	get := func(url string) (dashboardResponse, int) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", url, w.Code)
		}
		size := w.Body.Len()
		var d dashboardResponse
		if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
			t.Fatalf("%s: decode: %v", url, err)
		}
		return d, size
	}

	full, fullSize := get("/api/dashboard?history=chatty")
	if len(full.History) != 1 {
		t.Fatalf("first poll must carry the history, got %d runs", len(full.History))
	}
	if full.HistoryFingerprint == "" {
		t.Fatal("a response carrying history must carry its fingerprint")
	}

	elided, elidedSize := get("/api/dashboard?history=chatty&historyFp=" + full.HistoryFingerprint)
	if elided.History != nil {
		t.Fatalf("a matching fingerprint must omit the history, got %d runs", len(elided.History))
	}
	if elided.HistoryFingerprint != full.HistoryFingerprint {
		t.Fatalf("fingerprint changed without the history changing: %q -> %q",
			full.HistoryFingerprint, elided.HistoryFingerprint)
	}
	if elidedSize >= fullSize {
		t.Fatalf("elided response (%d bytes) is not smaller than the full one (%d bytes)",
			elidedSize, fullSize)
	}

	// A stale fingerprint must deliver the payload again.
	stale, _ := get("/api/dashboard?history=chatty&historyFp=not-the-current-one")
	if len(stale.History) != 1 {
		t.Fatalf("a stale fingerprint must return the history, got %d runs", len(stale.History))
	}

	// A new run changes the fingerprint, so the next poll re-sends.
	second, _ := core.NewExecution()
	_, _ = second.OutputStream.Write([]byte("second"))
	job.SetLastRun(second)

	changed, _ := get("/api/dashboard?history=chatty&historyFp=" + full.HistoryFingerprint)
	if len(changed.History) != 2 {
		t.Fatalf("a changed history must be re-sent, got %d runs", len(changed.History))
	}
	if changed.HistoryFingerprint == full.HistoryFingerprint {
		t.Fatal("the fingerprint did not change after a new run")
	}
}

// TestDashboardHistoryFingerprintTracksOutputGrowth pins that the
// fingerprint covers the run output, not just the run list: output is
// folded in by length, so an execution whose stdout changed is re-sent.
func TestDashboardHistoryFingerprintTracksOutputGrowth(t *testing.T) {
	t.Parallel()

	job := &testJob{}
	job.Name = "growing"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	e, _ := core.NewExecution()
	_, _ = e.OutputStream.Write([]byte("one"))
	job.SetLastRun(e)

	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: stubDiscardLogger()}
	handler := webpkg.NewServer("", sched, nil, nil).HTTPServer().Handler

	fingerprint := func() string {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/dashboard?history=growing", nil))
		var d dashboardResponse
		if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return d.HistoryFingerprint
	}

	before := fingerprint()
	_, _ = e.OutputStream.Write([]byte(" and more"))
	if after := fingerprint(); after == before {
		t.Fatal("the fingerprint ignored a change in run output")
	}
}
