// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"sync"
	"testing"
)

// TestGetJobSnapshotIsConsistentUnderConcurrentDisable pins that the
// three lists come from one instant.
//
// Read through GetActiveJobs, GetDisabledJobs and GetRemovedJobs in
// sequence, each takes its own lock, so a job disabled between two of
// them appears in both lists (or, disabling the other way round, in
// neither). The dashboard renders the lists without deduplicating by
// name, so the job showed as two rows until the next poll healed it.
func TestGetJobSnapshotIsConsistentUnderConcurrentDisable(t *testing.T) {
	sched := NewScheduler(newDiscardLogger())

	job := &TestJob{}
	job.Name = "flip"
	job.Schedule = "@hourly"
	if err := sched.AddJob(job); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	stop := make(chan struct{})
	var flipper sync.WaitGroup
	flipper.Add(1)
	go func() {
		defer flipper.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = sched.DisableJob("flip")
			_ = sched.EnableJob("flip")
		}
	}()

	for range 2000 {
		active, disabled, removed := sched.GetJobSnapshot()
		seen := 0
		for _, list := range [][]Job{active, disabled, removed} {
			for _, j := range list {
				if j.GetName() == "flip" {
					seen++
				}
			}
		}
		if seen != 1 {
			close(stop)
			flipper.Wait()
			t.Fatalf("job appeared in %d lists, want exactly 1", seen)
		}
	}

	close(stop)
	flipper.Wait()
}
