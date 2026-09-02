// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
)

// TestNewRunJobFromRequest_MaxRuntime covers the jobRequest.MaxRuntime
// wiring: config.ini/label run-jobs have always been able to set a
// per-job `max-runtime`, but the API had no field for it — every
// API-created run job was stuck with the scheduler's 24h default
// (core.MaxRuntimeProvider / issue #638). A valid value must land on
// RunJob.MaxRuntime; an invalid one must fail the request instead of
// silently falling back to the default.
func TestNewRunJobFromRequest_MaxRuntime(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("valid duration is applied", func(t *testing.T) {
		t.Parallel()

		s := NewServer("", core.NewScheduler(logger), nil, newHangingDockerProvider())
		job, err := s.newRunJobFromRequest(&jobRequest{
			Name:       "api-run-job",
			Type:       "run",
			Schedule:   "@hourly",
			Image:      "busybox",
			MaxRuntime: "45m",
		})
		if err != nil {
			t.Fatalf("newRunJobFromRequest: %v", err)
		}
		rj, ok := job.(*core.RunJob)
		if !ok {
			t.Fatalf("expected *core.RunJob, got %T", job)
		}
		if rj.MaxRuntime != 45*time.Minute {
			t.Fatalf("MaxRuntime = %v, want 45m", rj.MaxRuntime)
		}
	})

	t.Run("omitted duration leaves the scheduler default in effect", func(t *testing.T) {
		t.Parallel()

		s := NewServer("", core.NewScheduler(logger), nil, newHangingDockerProvider())
		job, err := s.newRunJobFromRequest(&jobRequest{
			Name:     "api-run-job",
			Type:     "run",
			Schedule: "@hourly",
			Image:    "busybox",
		})
		if err != nil {
			t.Fatalf("newRunJobFromRequest: %v", err)
		}
		rj, ok := job.(*core.RunJob)
		if !ok {
			t.Fatalf("expected *core.RunJob, got %T", job)
		}
		if rj.MaxRuntime != 0 {
			t.Fatalf("MaxRuntime = %v, want 0 (no per-job override)", rj.MaxRuntime)
		}
	})

	t.Run("invalid duration is rejected", func(t *testing.T) {
		t.Parallel()

		s := NewServer("", core.NewScheduler(logger), nil, newHangingDockerProvider())
		_, err := s.newRunJobFromRequest(&jobRequest{
			Name:       "api-run-job",
			Type:       "run",
			Schedule:   "@hourly",
			Image:      "busybox",
			MaxRuntime: "not-a-duration",
		})
		if err == nil {
			t.Fatal("newRunJobFromRequest: expected error for invalid maxRuntime, got nil")
		}
	})
}
