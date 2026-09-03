// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// buildPersistedRunJob reads the global off c.config, and boot assigns that
// field at one point and calls the restore at another. Review read those two
// as being in the wrong order, which would have made the inheritance a no-op
// on every real restart while the unit test around the builder stayed green.
//
// They are in the right order — c.config is set before initPersistStore runs
// — but an ordering that a reader can get wrong from the source is not
// something to leave asserted only in a commit message. This drives the real
// restore entry point instead of the builder, so a future reordering of boot
// fails here rather than silently returning every restored job to the 24h
// default (#806).
func TestInitPersistStore_RestoredRunJobInheritsGlobalMaxRuntime(t *testing.T) {
	t.Parallel()

	path := writePersistFile(t, persist.State{
		Jobs: map[string]*persist.Job{
			// No MaxRuntime: the case that has to pick up the global.
			"restored-run": {Type: persist.JobTypeRun, Schedule: "@hourly", Image: "busybox"},
			// An explicit bound must survive untouched.
			"restored-run-bounded": {
				Type: persist.JobTypeRun, Schedule: "@hourly", Image: "busybox",
				MaxRuntime: "45m",
			},
		},
	})

	cfg := &Config{}
	cfg.Global.MaxRuntime = 2 * time.Hour

	c := newDaemonForPersistTest(t)
	c.StateFile = path
	c.config = cfg
	c.dockerHandler = &DockerHandler{dockerProvider: &mockDockerProvider{}}

	if err := c.initPersistStore(); err != nil {
		t.Fatalf("initPersistStore: %v", err)
	}

	for name, want := range map[string]time.Duration{
		"restored-run":         2 * time.Hour,
		"restored-run-bounded": 45 * time.Minute,
	} {
		job := c.scheduler.GetJob(name)
		if job == nil {
			t.Fatalf("%s was not applied to the scheduler", name)
		}
		rj, ok := job.(*core.RunJob)
		if !ok {
			t.Fatalf("%s materialized as %T, want *core.RunJob", name, job)
		}
		if rj.MaxRuntime != want {
			t.Errorf("%s: MaxRuntime = %v, want %v", name, rj.MaxRuntime, want)
		}
	}
}
