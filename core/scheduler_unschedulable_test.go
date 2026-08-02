// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A refused job is remembered so /health can name it. That memory has to end
// when the job does, or an operator who fixed the problem by deleting the job
// would be left with a permanently degraded daemon and no job to blame it on.

func TestSchedulerRecordsRefusedJob(t *testing.T) {
	t.Parallel()

	job := &TestJob{}
	job.Name = "broken"
	job.Schedule = "not-a-schedule"

	sc := NewScheduler(newDiscardLogger())
	require.Error(t, sc.AddJob(job))

	refused := sc.GetUnschedulableJobs()
	require.Contains(t, refused, "broken")
	assert.NotEmpty(t, refused["broken"], "the recorded reason is what makes the report actionable")
}

// TestSchedulerRemoveJobClearsUnschedulable covers the removal half: dropping
// the job from the config has to drop the complaint with it.
func TestSchedulerRemoveJobClearsUnschedulable(t *testing.T) {
	t.Parallel()

	job := &TestJob{}
	job.Name = "broken"
	job.Schedule = "not-a-schedule"

	sc := NewScheduler(newDiscardLogger())
	require.Error(t, sc.AddJob(job))
	require.Contains(t, sc.GetUnschedulableJobs(), "broken")

	require.NoError(t, sc.RemoveJob(job))

	assert.NotContains(t, sc.GetUnschedulableJobs(), "broken",
		"a job removed from the config still held /health degraded")
}

// TestSchedulerGetUnschedulableJobsIsACopy pins that callers cannot reach into
// the scheduler's state through the returned map: the health checker reads it
// on every check, and a shared map would be a data race as well as a way to
// silently erase a refusal.
func TestSchedulerGetUnschedulableJobsIsACopy(t *testing.T) {
	t.Parallel()

	job := &TestJob{}
	job.Name = "broken"
	job.Schedule = "not-a-schedule"

	sc := NewScheduler(newDiscardLogger())
	require.Error(t, sc.AddJob(job))

	delete(sc.GetUnschedulableJobs(), "broken")

	assert.Contains(t, sc.GetUnschedulableJobs(), "broken")
}
