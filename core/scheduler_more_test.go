package core

import (
	"context"
	"testing"
)

func TestSchedulerDisableEnable(t *testing.T) {
	sc := NewScheduler(&TestLogger{})
	job := &TestJob{}
	job.Name = "job1"
	job.Schedule = "@daily"
	if err := sc.AddJob(job); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if sc.GetJob("job1") == nil {
		t.Fatalf("job not found after add")
	}
	if err := sc.DisableJob("job1"); err != nil {
		t.Fatalf("DisableJob: %v", err)
	}
	if sc.GetJob("job1") != nil {
		t.Fatalf("job should be disabled")
	}
	if sc.GetDisabledJob("job1") == nil {
		t.Fatalf("disabled job not found")
	}
	if err := sc.EnableJob("job1"); err != nil {
		t.Fatalf("EnableJob: %v", err)
	}
	if sc.GetJob("job1") == nil {
		t.Fatalf("job not re-enabled")
	}
}

func TestSchedulerRemoveJobTracksRemoved(t *testing.T) {
	sc := NewScheduler(&TestLogger{})
	a := &TestJob{}
	a.Name = "a"
	a.Schedule = "@daily"
	b := &TestJob{}
	b.Name = "b"
	b.Schedule = "@hourly"
	_ = sc.AddJob(a)
	_ = sc.AddJob(b)
	if err := sc.RemoveJob(a); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}
	// a should be gone from active jobs
	if sc.GetJob("a") != nil {
		t.Fatalf("a still present in active jobs")
	}
	// removed list should contain a
	removed := sc.GetRemovedJobs()
	found := false
	for _, j := range removed {
		if j.GetName() == "a" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("removed jobs missing 'a': %#v", removed)
	}
}

// TestIsTriggeredSchedule tests the IsTriggeredSchedule helper function
func TestIsTriggeredSchedule(t *testing.T) {
	tests := []struct {
		schedule string
		expected bool
	}{
		{"@triggered", true},
		{"@manual", true},
		{"@none", true},
		{"@daily", false},
		{"@hourly", false},
		{"@every 5m", false},
		{"0 0 * * *", false},
		{"", false},
	}

	for _, tc := range tests {
		got := IsTriggeredSchedule(tc.schedule)
		if got != tc.expected {
			t.Errorf("IsTriggeredSchedule(%q) = %v, want %v", tc.schedule, got, tc.expected)
		}
	}
}

// TestSchedulerTriggeredJobNotScheduled tests that @triggered jobs are stored but not added to cron
func TestSchedulerTriggeredJobNotScheduled(t *testing.T) {
	sc := NewScheduler(&TestLogger{})

	// Add a triggered job
	triggered := &TestJob{}
	triggered.Name = "triggered-job"
	triggered.Schedule = "@triggered"

	if err := sc.AddJob(triggered); err != nil {
		t.Fatalf("AddJob(@triggered): %v", err)
	}

	// Job should be in the jobs list
	if sc.GetJob("triggered-job") == nil {
		t.Fatal("triggered job not found in jobs list")
	}

	// Job should NOT have a cron ID (not scheduled in cron)
	if triggered.GetCronJobID() != 0 {
		t.Errorf("triggered job should have cronID=0, got %d", triggered.GetCronJobID())
	}

	// Cron entries should be empty (no scheduled jobs)
	entries := sc.Entries()
	for _, e := range entries {
		if e.Job != nil {
			t.Errorf("cron should have no entries for triggered jobs, but found entry")
		}
	}
}

// TestSchedulerTriggeredJobWithAliases tests @manual and @none aliases
func TestSchedulerTriggeredJobWithAliases(t *testing.T) {
	sc := NewScheduler(&TestLogger{})

	// Test @manual alias
	manualJob := &TestJob{}
	manualJob.Name = "manual-job"
	manualJob.Schedule = "@manual"

	if err := sc.AddJob(manualJob); err != nil {
		t.Fatalf("AddJob(@manual): %v", err)
	}
	if sc.GetJob("manual-job") == nil {
		t.Fatal("@manual job not found")
	}

	// Test @none alias
	noneJob := &TestJob{}
	noneJob.Name = "none-job"
	noneJob.Schedule = "@none"

	if err := sc.AddJob(noneJob); err != nil {
		t.Fatalf("AddJob(@none): %v", err)
	}
	if sc.GetJob("none-job") == nil {
		t.Fatal("@none job not found")
	}
}

// TestSchedulerTriggeredJobRunManually tests that triggered jobs can be run via RunJob
func TestSchedulerTriggeredJobRunManually(t *testing.T) {
	sc := NewScheduler(&TestLogger{})

	triggered := &TestJob{}
	triggered.Name = "run-me"
	triggered.Schedule = "@triggered"
	triggered.Command = "echo test"

	if err := sc.AddJob(triggered); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	// Start the scheduler
	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sc.Stop()

	// Run the job manually - should succeed
	if err := sc.RunJob(context.Background(), "run-me"); err != nil {
		t.Errorf("RunJob should succeed for triggered job: %v", err)
	}
}
