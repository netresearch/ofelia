// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"testing"
)

// applyUpdatedJob is the part of UpdateJob that runs after the cron entry
// has been replaced. RemoveJob drops the cron entry and waits for
// in-flight runs BEFORE it takes s.mu, so a delete that began after
// UpdateJob's pre-check can complete entirely inside that window — which
// is exactly the state these tests hand it.

// TestApplyUpdatedJobRefusesAVanishedJob pins that an update landing
// after a concurrent delete does not resurrect the job. Writing
// s.jobsByName unconditionally left a ghost: present in the map, absent
// from s.Jobs and holding no cron entry, so it never fired again while
// the API kept reporting it as live.
func TestApplyUpdatedJobRefusesAVanishedJob(t *testing.T) {
	sched := NewScheduler(newDiscardLogger())

	old := &TestJob{}
	old.Name = "vanished"
	old.Schedule = "@hourly"
	if err := sched.AddJob(old); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if err := sched.RemoveJob(old); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}

	replacement := &TestJob{}
	replacement.Name = "vanished"
	replacement.Schedule = "@daily"

	if err := sched.applyUpdatedJob("vanished", replacement); err == nil {
		t.Fatal("applying an update to a removed job must fail")
	}
	if sched.GetAnyJob("vanished") != nil {
		t.Fatal("the update reinserted a job the delete had removed")
	}
	for _, j := range sched.Jobs {
		if j.GetName() == "vanished" {
			t.Fatal("the removed job is back in s.Jobs")
		}
	}
}

// TestApplyUpdatedJobKeepsStateOnRePauseFailure pins that the fallible
// re-pause runs before the maps change. With the maps written first, a
// re-pause error reported a failed update that had in fact taken effect,
// leaving a job in s.Jobs with no cron entry behind it.
func TestApplyUpdatedJobKeepsStateOnRePauseFailure(t *testing.T) {
	sched := NewScheduler(newDiscardLogger())

	old := &TestJob{}
	old.Name = "paused"
	old.Schedule = "@hourly"
	old.Command = "original"
	if err := sched.AddJob(old); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if err := sched.DisableJob("paused"); err != nil {
		t.Fatalf("DisableJob: %v", err)
	}

	// The cron entry is gone but the scheduler's own maps still hold the
	// job: the state a delete leaves behind before it takes s.mu.
	sched.cron.RemoveByName("paused")

	replacement := &TestJob{}
	replacement.Name = "paused"
	replacement.Schedule = "@daily"
	replacement.Command = "replacement"

	if err := sched.applyUpdatedJob("paused", replacement); err == nil {
		t.Fatal("applyUpdatedJob must fail when the entry cannot be re-paused")
	}

	live := sched.GetAnyJob("paused")
	if live == nil {
		t.Fatal("the failed update dropped the job")
	}
	if live.GetCommand() != "original" {
		t.Fatalf("a failed update was applied anyway: command = %q, want %q",
			live.GetCommand(), "original")
	}
	for _, j := range sched.Jobs {
		if j.GetName() == "paused" && j.GetCommand() != "original" {
			t.Fatalf("s.Jobs holds the replacement after a failed update: %q", j.GetCommand())
		}
	}
}
