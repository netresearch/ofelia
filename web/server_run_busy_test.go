// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
