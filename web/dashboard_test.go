// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
)

type dashboardResponse struct {
	Jobs               []json.RawMessage `json:"jobs"`
	Disabled           []json.RawMessage `json:"disabled"`
	Removed            []json.RawMessage `json:"removed"`
	Config             json.RawMessage   `json:"config"`
	History            []apiExecution    `json:"history"`
	HistoryFingerprint string            `json:"historyFingerprint"`
}

// TestDashboardEndpoint pins the aggregate poll endpoint: one request
// returns what /api/jobs, /api/jobs/disabled, /api/jobs/removed and
// /api/config return individually, and ?history=<job> piggybacks that
// job's history. The individual endpoints stay untouched — external API
// consumers keep using them; /api/dashboard exists so the UI's 5s poll
// costs one request instead of five.
func TestDashboardEndpoint(t *testing.T) {
	t.Parallel()

	job := &testJob{}
	job.Name = "dash-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	e, _ := core.NewExecution()
	_, _ = e.OutputStream.Write([]byte("dash-out"))
	e.Error = fmt.Errorf("dash-err")
	e.Failed = true
	job.SetLastRun(e)
	empty := &testJob{}
	empty.Name = "empty-job"
	empty.Schedule = schedDaily
	empty.Command = cmdEcho
	sched := &core.Scheduler{Jobs: []core.Job{job, empty}, Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)
	handler := srv.HTTPServer().Handler

	get := func(url string) dashboardResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: unexpected status %d", url, w.Code)
		}
		var d dashboardResponse
		if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
			t.Fatalf("%s: decode: %v", url, err)
		}
		return d
	}

	// Without the history param: all sections present, no history.
	d := get("/api/dashboard")
	if len(d.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(d.Jobs))
	}
	if d.Disabled == nil || d.Removed == nil {
		t.Fatalf("disabled/removed must be present (empty arrays), got %v / %v", d.Disabled, d.Removed)
	}
	if d.History != nil {
		t.Fatalf("history must be absent without the query param")
	}

	// Jobs carry a light recent-run summary for the UI sparkline.
	var jobs []struct {
		Name       string `json:"name"`
		RecentRuns []struct {
			Duration int64 `json:"duration"`
			Failed   bool  `json:"failed"`
			Skipped  bool  `json:"skipped"`
		} `json:"recentRuns"`
	}
	raw, _ := json.Marshal(d.Jobs)
	if err := json.Unmarshal(raw, &jobs); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	if len(jobs[0].RecentRuns) != 1 || !jobs[0].RecentRuns[0].Failed {
		t.Fatalf("expected one failed recent run, got %+v", jobs[0].RecentRuns)
	}

	// The jobs section must byte-match what /api/jobs returns on its own.
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var solo []json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&solo); err != nil {
		t.Fatalf("decode /api/jobs: %v", err)
	}
	if len(solo) != 2 || string(solo[0]) != string(d.Jobs[0]) {
		t.Fatalf("dashboard jobs diverge from /api/jobs:\n%s\nvs\n%s", d.Jobs[0], solo[0])
	}

	// With the history param: that job's history rides along.
	d = get("/api/dashboard?history=dash-job")
	if len(d.History) != 1 || d.History[0].Stdout != "dash-out" || d.History[0].Error != "dash-err" {
		t.Fatalf("expected piggybacked history, got %+v", d.History)
	}

	// An unknown job name must not fail the poll — history is simply null.
	d = get("/api/dashboard?history=vanished")
	if d.History != nil {
		t.Fatalf("unknown history job must yield null history, got %+v", d.History)
	}

	// An existing job with no runs yields [], not null — the UI must be
	// able to distinguish "history is empty, render the empty state" from
	// "no history requested/job vanished".
	d = get("/api/dashboard?history=empty-job")
	if d.History == nil || len(d.History) != 0 {
		t.Fatalf("empty history must be [], got %+v", d.History)
	}
}
