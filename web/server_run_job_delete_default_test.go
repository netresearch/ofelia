// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"io"
	"log/slog"
	"testing"

	"github.com/netresearch/ofelia/core"
)

// TestNewRunJobFromRequest_DefaultsDelete pins the fix for the
// container-leak bug: jobRequest has no "delete" field, and unlike
// config.ini-sourced jobs (whose RunJob.Delete defaults to "true" via
// the decoder's default-tag handling), a job built here from the API
// request used to leave Delete at its zero value (""). deleteContainer
// treats that as false, so every API-created run job's container was
// left behind and collided with the next run's `docker create` on the
// same name ("resource conflict"). Delete must default to "true" here
// to match the config.ini behavior.
func TestNewRunJobFromRequest_DefaultsDelete(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
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
	if rj.Delete != "true" {
		t.Fatalf("Delete = %q, want \"true\" — API-created run jobs must clean up their container like config.ini ones do", rj.Delete)
	}
}
