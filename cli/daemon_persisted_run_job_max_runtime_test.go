// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// TestBuildPersistedRunJob_MaxRuntime pins the restore-path half of the
// jobRequest.MaxRuntime feature: web.persistJob writes req.MaxRuntime
// into persist.Job on create/update, and buildPersistedRunJob must
// reparse it into RunJob.MaxRuntime on every daemon restart — otherwise
// a job that survives a restart with the CONTRACT-mismatch bug class
// documented on persistedJobToScheduler would silently drop back to
// the scheduler's 24h default.
func TestBuildPersistedRunJob_MaxRuntime(t *testing.T) {
	t.Parallel()

	t.Run("valid duration round-trips", func(t *testing.T) {
		t.Parallel()

		c := newDaemonForPersistTest(t)
		job, err := c.buildPersistedRunJob("persisted-run-job", &persist.Job{
			Type:       persist.JobTypeRun,
			Schedule:   "@hourly",
			Image:      "busybox",
			MaxRuntime: "45m",
		}, &mockDockerProvider{})
		if err != nil {
			t.Fatalf("buildPersistedRunJob: %v", err)
		}
		rj, ok := job.(*core.RunJob)
		if !ok {
			t.Fatalf("expected *core.RunJob, got %T", job)
		}
		if rj.MaxRuntime != 45*time.Minute {
			t.Fatalf("MaxRuntime = %v, want 45m", rj.MaxRuntime)
		}
	})

	t.Run("invalid duration fails materialization", func(t *testing.T) {
		t.Parallel()

		c := newDaemonForPersistTest(t)
		_, err := c.buildPersistedRunJob("persisted-run-job", &persist.Job{
			Type:       persist.JobTypeRun,
			Schedule:   "@hourly",
			Image:      "busybox",
			MaxRuntime: "not-a-duration",
		}, &mockDockerProvider{})
		if err == nil {
			t.Fatal("buildPersistedRunJob: expected error for invalid maxRuntime, got nil")
		}
	})
}
