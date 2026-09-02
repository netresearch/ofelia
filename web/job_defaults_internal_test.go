// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"io"
	"log/slog"
	"testing"

	"github.com/netresearch/ofelia/core"
)

// TestJobFromRequest_DefaultsForDockerBackedTypes closes the two job
// types TestCreatedJobsCarryStructTagDefaults cannot reach: exec and run
// need a Docker provider, so they are driven here with the stub one
// rather than through the HTTP handler.
//
// Exec was one of the three types the finding named — only the run-job
// constructor applied defaults.Set, so an exec job came out with
// HistoryLimit 0 and BareJob.GetHistory never trimmed.
func TestJobFromRequest_DefaultsForDockerBackedTypes(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer("", core.NewScheduler(logger), nil, newHangingDockerProvider())

	for _, tc := range []struct {
		name string
		req  jobRequest
	}{
		{"exec", jobRequest{Name: "d-exec", Type: "exec", Schedule: "@hourly", Container: "app", Command: "echo hi"}},
		{"run", jobRequest{Name: "d-run", Type: "run", Schedule: "@hourly", Image: "busybox"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			job, err := s.jobFromRequest(&tc.req)
			if err != nil {
				t.Fatalf("jobFromRequest: %v", err)
			}

			var limit int
			switch j := job.(type) {
			case *core.ExecJob:
				limit = j.HistoryLimit
			case *core.RunJob:
				limit = j.HistoryLimit
			default:
				t.Fatalf("unexpected job type %T", job)
			}
			if limit <= 0 {
				t.Fatalf("HistoryLimit = %d — every Execution is retained forever", limit)
			}
		})
	}
}
