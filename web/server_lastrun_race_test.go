// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netresearch/ofelia/core"
)

type outputJob struct{ core.BareJob }

func (j *outputJob) Run(ctx *core.Context) error {
	_, _ = ctx.Execution.OutputStream.Write([]byte("hello"))
	return nil
}

// Reading the job list while runs end must not race the run's buffer
// cleanup; under -race this reported Execution.Cleanup against
// Execution.GetStdout (#898).
func TestJobsHandler_LastRunDoesNotRaceCleanup(t *testing.T) {
	sc := core.NewScheduler(newDiscardLogger())
	j := &outputJob{}
	j.Name, j.Schedule, j.Command = "out", core.TriggeredSchedule, "true"
	if err := sc.AddJob(j); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sc.Stop() }()
	// The handler itself, past the rate limiter, which would refuse most of
	// these requests.
	srv := NewServer("", sc, nil, nil)

	for range 300 {
		_ = sc.RunJob(context.Background(), "out")
		w := httptest.NewRecorder()
		srv.jobsHandler(w, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/jobs: status %d", w.Code)
		}
	}
}
