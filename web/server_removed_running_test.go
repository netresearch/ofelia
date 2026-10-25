// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
)

// stoppingJob runs until its context ends and then until release is closed,
// the way a job removes its container after a cancellation.
type stoppingJob struct {
	core.BareJob
	started chan struct{}
	release chan struct{}
	ended   chan struct{}
}

func (j *stoppingJob) Run(ctx *core.Context) error {
	defer close(j.ended)
	close(j.started)
	<-ctx.RunContext().Done()
	<-j.release
	return nil
}

func removedRunning(t *testing.T, h http.Handler, name string) bool {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/jobs/removed", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/jobs/removed: status %d", w.Code)
	}
	var jobs []apiJob
	if err := json.NewDecoder(w.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, j := range jobs {
		if j.Name == name {
			return j.Running
		}
	}
	t.Fatalf("%q is not among the removed jobs", name)
	return false
}

// A removed job reports running while its canceled run is still stopping,
// also when a job of the same name has been added and is idle, and stops
// reporting it once the run has ended.
func TestRemovedJobs_RunningShowsTheStoppingRun(t *testing.T) {
	sc := core.NewScheduler(newDiscardLogger())
	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sc.Stop() }()
	h := NewServer("", sc, nil, nil).HTTPServer().Handler

	old := &stoppingJob{
		BareJob: core.BareJob{Name: "gone", Schedule: core.TriggeredSchedule, Command: "true"},
		started: make(chan struct{}),
		release: make(chan struct{}),
		ended:   make(chan struct{}),
	}
	if err := sc.AddJob(old); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if err := sc.RunJob(context.Background(), "gone"); err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	<-old.started
	_ = sc.RemoveJob(old)

	repl := &simpleJob{}
	repl.Name = "gone"
	repl.Schedule = core.TriggeredSchedule
	if err := sc.AddJob(repl); err != nil {
		t.Fatalf("AddJob replacement: %v", err)
	}

	if !removedRunning(t, h, "gone") {
		t.Fatal("the removed job reports running=false while its canceled run is stopping")
	}

	close(old.release)
	<-old.ended
	deadline := time.Now().Add(5 * time.Second)
	for removedRunning(t, h, "gone") {
		if time.Now().After(deadline) {
			t.Fatal("the removed job still reports running after its run ended")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
