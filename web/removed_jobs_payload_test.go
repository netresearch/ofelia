// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
)

// TestRemovedJobsCarryNoRecentRuns pins that the removed list leaves out
// the recent-run summary.
//
// The removed tab shows a name, a type, a schedule and the last run;
// nothing in the UI reads recentRuns there. Building it walked the
// retained history of every removed job and marshaled the newest runs
// into a payload the 5s poll then discarded.
func TestRemovedJobsCarryNoRecentRuns(t *testing.T) {
	t.Parallel()

	job := &testJob{}
	job.Name = "gone-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	e, _ := core.NewExecution()
	_, _ = e.OutputStream.Write([]byte("out"))
	job.SetLastRun(e)

	sched := &core.Scheduler{Removed: []core.Job{job}, Logger: stubDiscardLogger()}
	handler := webpkg.NewServer("", sched, nil, nil).HTTPServer().Handler

	decode := func(url string) []struct {
		Name       string          `json:"name"`
		LastRun    json.RawMessage `json:"lastRun"`
		RecentRuns json.RawMessage `json:"recentRuns"`
	} {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", url, w.Code)
		}
		var out []struct {
			Name       string          `json:"name"`
			LastRun    json.RawMessage `json:"lastRun"`
			RecentRuns json.RawMessage `json:"recentRuns"`
		}
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("%s: decode: %v", url, err)
		}
		return out
	}

	removed := decode("/api/jobs/removed")
	if len(removed) != 1 {
		t.Fatalf("expected one removed job, got %d", len(removed))
	}
	if removed[0].RecentRuns != nil {
		t.Fatalf("removed jobs must not carry recentRuns, got %s", removed[0].RecentRuns)
	}
	// The last run is what the tab does show — it must still be there.
	if removed[0].LastRun == nil || string(removed[0].LastRun) == "null" {
		t.Fatal("removed jobs must still carry lastRun")
	}
}
