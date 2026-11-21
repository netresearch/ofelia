// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// busyDurableJob keeps a schedule record and runs until released.
type busyDurableJob struct {
	core.BareJob
	started chan struct{}
	release chan struct{}
}

func (j *busyDurableJob) SchedulePolicy() (core.SchedulePolicy, bool) {
	return core.SchedulePolicy{CatchUp: true, Revision: core.ScheduleRevision(j.Schedule, true, false)}, true
}

func (j *busyDurableJob) Run(ctx *core.Context) error {
	close(j.started)
	select {
	case <-j.release:
	case <-ctx.RunContext().Done():
	}
	return nil
}

// holdJob takes a slot of the global limit until released.
type holdJob struct {
	core.BareJob
	started chan struct{}
	release chan struct{}
}

func (j *holdJob) Run(_ *core.Context) error {
	close(j.started)
	<-j.release
	return nil
}

// A manual run that the global limit would skip is refused with 409 and a
// message that names the limit.
func TestRunJobHandler_FullLimitIsConflict(t *testing.T) {
	sc := core.NewScheduler(newDiscardLogger())
	sc.SetMaxConcurrentJobs(1)
	holder := &holdJob{started: make(chan struct{}), release: make(chan struct{})}
	holder.Name, holder.Schedule, holder.Command = "holder", "@every 1h", "hold"
	other := &holdJob{started: make(chan struct{}), release: make(chan struct{})}
	other.Name, other.Schedule, other.Command = "other", "@every 1h", "other"
	for _, j := range []core.Job{holder, other} {
		if err := sc.AddJob(j); err != nil {
			t.Fatalf("AddJob: %v", err)
		}
	}
	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { close(holder.release); close(other.release); _ = sc.StopWithTimeout(5 * time.Second) })
	httpSrv := NewServer("", sc, nil, nil).HTTPServer()

	run := func(name string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"name": name})
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/jobs/run", bytes.NewReader(body)))
		return w
	}
	if w := run("holder"); w.Code != http.StatusNoContent {
		t.Fatalf("run holder: status %d, want 204", w.Code)
	}
	select {
	case <-holder.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the holder did not start")
	}
	w := run("other")
	if w.Code != http.StatusConflict {
		t.Fatalf("run at a full limit: status %d, want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "max-concurrent-jobs") {
		t.Errorf("409 body = %q, want it to name max-concurrent-jobs", w.Body.String())
	}
}

// A manual run of a job with a schedule record is refused with 409 while a
// run of it is in progress.
func TestRunJobHandler_BusyIsConflict(t *testing.T) {
	sc := core.NewScheduler(newDiscardLogger())
	sc.SetScheduleStore(persist.NewStore(filepath.Join(t.TempDir(), "state.json")))
	j := &busyDurableJob{started: make(chan struct{}), release: make(chan struct{})}
	j.Name = "shop.backup"
	j.Schedule = "0 2 * * *"
	if err := sc.AddJob(j); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { close(j.release); _ = sc.StopWithTimeout(5 * time.Second) })
	httpSrv := NewServer("", sc, nil, nil).HTTPServer()

	run := func() int {
		body, _ := json.Marshal(map[string]string{"name": "shop.backup"})
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/jobs/run", bytes.NewReader(body)))
		return w.Code
	}
	if code := run(); code != http.StatusNoContent {
		t.Fatalf("first run: status %d, want 204", code)
	}
	select {
	case <-j.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the job did not start")
	}
	if code := run(); code != http.StatusConflict {
		t.Fatalf("run while busy: status %d, want 409", code)
	}
}
