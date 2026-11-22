// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

func activeNames(s *Scheduler) []string {
	active, _, _ := s.GetJobSnapshot()
	names := make([]string, 0, len(active))
	for _, j := range active {
		names = append(names, j.GetName())
	}
	return names
}

// Removing a job the scheduler never registered leaves every registered job
// alone, also one that UpdateJob swapped in and that therefore shares the
// unregistered job's cron ID 0.
func TestRemoveJob_UnregisteredJobRemovesNothing(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	require.NoError(t, s.AddJob(&BareJob{Name: "shop.export", Schedule: "@daily", Command: "true"}))
	require.NoError(t, s.UpdateJob("shop.export", "@daily", &BareJob{Name: "shop.export", Schedule: "@daily", Command: "true"}))

	require.NoError(t, s.RemoveJob(&BareJob{Name: "shop.import", Schedule: "not-a-schedule"}))

	assert.Equal(t, []string{"shop.export"}, activeNames(s))
	assert.Empty(t, s.GetRemovedJobs())
}

// Removing a job that lost its name to another registration leaves that
// registration's entry in place: the removal is about the job passed in.
func TestRemoveJob_RefusedTwinLeavesTheRegisteredJob(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	registered := &BareJob{Name: "shop.backup", Schedule: "@daily", Command: "true"}
	require.NoError(t, s.AddJob(registered))
	twin := &BareJob{Name: "shop.backup", Schedule: "@hourly", Command: "false"}
	require.Error(t, s.AddJob(twin))

	require.NoError(t, s.RemoveJob(twin))

	assert.True(t, s.cron.EntryByName("shop.backup").Valid(), "the registered job lost its cron entry")
	assert.Same(t, registered, s.GetAnyJob("shop.backup"))
	assert.Equal(t, []string{"shop.backup"}, activeNames(s))
	assert.Empty(t, s.GetRemovedJobs())
}

// UpdateJob keeps the entry's cron ID on the job it swaps in.
func TestUpdateJob_KeepsTheCronID(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	old := &BareJob{Name: "shop.export", Schedule: "@daily", Command: "true"}
	require.NoError(t, s.AddJob(old))
	repl := &BareJob{Name: "shop.export", Schedule: "@hourly", Command: "true"}
	require.NoError(t, s.UpdateJob("shop.export", "@hourly", repl))

	assert.NotZero(t, repl.GetCronJobID())
	assert.Equal(t, old.GetCronJobID(), repl.GetCronJobID())
}
