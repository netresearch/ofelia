//go:build integration

// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/persist"
)

// A real labeled container keeps the job occupied across two state-file
// reloads; removing that container allows the next Compose execution to start.
func TestScheduleWorkloadIntegration_RepeatedRestartWaitsForComposeContainer(t *testing.T) {
	t.Parallel()
	provider := newIntegrationProvider(t)
	file, project := composeProject(t, `
  task:
    image: alpine:3.22
    command: ["echo", "next execution"]
`)
	executionID := project + "-interrupted"
	label := ComposeExecutionLabel + "=" + executionID
	containerID, err := exec.Command("docker", "compose", "-f", file,
		"run", "--rm", "-d", "--label", label, "task", "sleep", "120").Output()
	require.NoError(t, err)
	id := strings.TrimSpace(string(containerID))
	t.Cleanup(func() { _ = provider.RemoveContainer(context.Background(), id, true) })
	waitForLabeled(t, provider, label)

	h := newDurableHarness(t, at(23, 10))
	newJob := func() *ComposeJob {
		job := NewComposeJob()
		job.Name, job.Schedule, job.Service = "shop.backup", durableSchedule, "task"
		job.File, job.Provider = file, provider
		job.Project = ComposeProjectContext{Files: []string{file}, Revision: "accepted", MissedFires: "one", Concurrency: "queue"}
		return job
	}
	job := newJob()
	policy, _ := job.SchedulePolicy()
	rec := interruptedRecord(policy.Revision)
	rec.Dispatch.Execution = executionID
	h.put(job.Name, rec)
	for restart := range 2 {
		h.s.orphanPollInterval = 10 * time.Millisecond
		require.NoError(t, h.s.AddJob(job))
		h.start()
		require.ErrorIs(t, h.s.RunJob(context.Background(), job.Name), ErrJobBusy, "restart %d", restart+1)
		assert.Equal(t, executionID, h.record(job.Name).Workload)
		require.Len(t, listByLabel(t, provider, label, false), 1)
		if restart == 0 {
			require.NoError(t, h.s.StopWithTimeout(time.Second))
			store := persist.NewStore(h.store.Path())
			require.NoError(t, store.Load())
			h = newDurableHarnessWithStore(t, at(23, 11), store)
			job = newJob()
		}
	}
	require.NoError(t, provider.RemoveContainer(context.Background(), id, true))
	h.eventually(job.Name, func(r persist.Schedule) bool { return r.Workload == "" }, "Docker confirms the old container ended")
	require.NoError(t, h.s.RunJob(context.Background(), job.Name))
	h.eventually(job.Name, func(r persist.Schedule) bool {
		return ended(r) && r.Dispatch.Execution != executionID && r.Dispatch.Outcome == outcomeSuccess && r.Workload == ""
	}, "the next Compose execution completes after the old container ends")
}
