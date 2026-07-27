// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// TestBuildPersistedRunJob_DefaultsDelete pins the restore-path half of
// the container-leak fix. buildPersistedRunJob rebuilds a persisted
// job-run on every daemon restart (initPersistStore -> newDaemonForPersistTest
// et al.) through a path entirely separate from newRunJobFromRequest
// (web/server.go) — a job created before this fix, or reloaded across
// any restart, would keep leaking containers even after the API path
// was patched if this path is not fixed too.
func TestBuildPersistedRunJob_DefaultsDelete(t *testing.T) {
	t.Parallel()

	c := newDaemonForPersistTest(t)
	job, err := c.buildPersistedRunJob("persisted-run-job", &persist.Job{
		Type:     persist.JobTypeRun,
		Schedule: "@hourly",
		Image:    "busybox",
	}, &mockDockerProvider{})
	if err != nil {
		t.Fatalf("buildPersistedRunJob: %v", err)
	}

	rj, ok := job.(*core.RunJob)
	if !ok {
		t.Fatalf("expected *core.RunJob, got %T", job)
	}
	if rj.Delete != "true" {
		t.Fatalf("Delete = %q, want \"true\" — persisted run jobs must clean up their container on every restart", rj.Delete)
	}
}
