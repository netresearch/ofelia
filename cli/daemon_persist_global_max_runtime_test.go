// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// A restored run job that stored no max-runtime keeps 0 and runs under the
// scheduler's current [global] max-runtime, as an INI job does; nothing is
// copied into the job at restore, so a later change of the global reaches it
// too (#806). This drives the real restore entry point, not the builder.
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

	c := newDaemonForPersistTest(t)
	c.StateFile = path
	c.config = &Config{}
	c.dockerHandler = &DockerHandler{dockerProvider: &mockDockerProvider{}}
	c.scheduler.SetGlobalMaxRuntime(2 * time.Hour)

	if err := c.initPersistStore(); err != nil {
		t.Fatalf("initPersistStore: %v", err)
	}

	for name, want := range map[string]struct{ own, effective time.Duration }{
		"restored-run":         {own: 0, effective: 2 * time.Hour},
		"restored-run-bounded": {own: 45 * time.Minute, effective: 45 * time.Minute},
	} {
		job := c.scheduler.GetJob(name)
		if job == nil {
			t.Fatalf("%s was not applied to the scheduler", name)
		}
		rj, ok := job.(*core.RunJob)
		if !ok {
			t.Fatalf("%s materialized as %T, want *core.RunJob", name, job)
		}
		if rj.MaxRuntime != want.own {
			t.Errorf("%s: MaxRuntime = %v, want %v", name, rj.MaxRuntime, want.own)
		}
		if got := c.scheduler.MaxRuntimeFor(rj); got != want.effective {
			t.Errorf("%s: MaxRuntimeFor = %v, want %v", name, got, want.effective)
		}
	}
}
