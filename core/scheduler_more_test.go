package core

import (
	"context"
	"testing"
	"time"

	cron "github.com/netresearch/go-cron"
)

func TestSchedulerDisableEnable(t *testing.T) {
	sc := NewScheduler(newDiscardLogger())
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
	sc := NewScheduler(newDiscardLogger())
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

// TestSchedulerTriggeredJobHasCronEntry tests that @triggered jobs are registered
// with go-cron as native TriggeredSchedule entries. They get a cron entry ID and
// appear in Entries(), but their schedule never fires automatically (Next() returns
// zero time).
func TestSchedulerTriggeredJobNotScheduled(t *testing.T) {
	sc := NewScheduler(newDiscardLogger())

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

	// Job should have a cron ID (registered in go-cron)
	if triggered.GetCronJobID() == 0 {
		t.Error("triggered job should have a non-zero cronID")
	}

	// Triggered job should appear in cron entries with the correct name
	entry := sc.EntryByName("triggered-job")
	if !entry.Valid() {
		t.Fatal("triggered job should have a valid cron entry")
	}

	// The entry's schedule should be a TriggeredSchedule (Next returns zero)
	if next := entry.Schedule.Next(sc.clock.Now()); !next.IsZero() {
		t.Errorf("triggered schedule Next() should return zero time, got %v", next)
	}
}

// TestSchedulerTriggeredJobWithAliases tests @manual and @none aliases get
// proper cron entries just like @triggered.
func TestSchedulerTriggeredJobWithAliases(t *testing.T) {
	sc := NewScheduler(newDiscardLogger())

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
	if manualJob.GetCronJobID() == 0 {
		t.Error("@manual job should have a non-zero cronID")
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
	if noneJob.GetCronJobID() == 0 {
		t.Error("@none job should have a non-zero cronID")
	}

	// Both should have valid cron entries with triggered schedules
	for _, name := range []string{"manual-job", "none-job"} {
		entry := sc.EntryByName(name)
		if !entry.Valid() {
			t.Errorf("job %q should have a valid cron entry", name)
		}
		if next := entry.Schedule.Next(sc.clock.Now()); !next.IsZero() {
			t.Errorf("job %q schedule Next() should return zero time, got %v", name, next)
		}
	}
}

// TestSchedulerTriggeredJobRunManually tests that triggered jobs can be run via
// RunJob which now delegates to go-cron's TriggerEntryByName.
func TestSchedulerTriggeredJobRunManually(t *testing.T) {
	sc := NewScheduler(newDiscardLogger())

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

	// Run the job manually via TriggerEntryByName path - should succeed
	if err := sc.RunJob(context.Background(), "run-me"); err != nil {
		t.Errorf("RunJob should succeed for triggered job: %v", err)
	}

	// Wait briefly for the triggered job to execute (runs asynchronously in go-cron)
	time.Sleep(200 * time.Millisecond)

	if triggered.Called() == 0 {
		t.Error("triggered job should have been called after RunJob")
	}
}

// TestSchedulerTriggeredJobUsesNativeCronIsTriggered verifies that go-cron's
// cron.IsTriggered() correctly identifies triggered schedule entries.
func TestSchedulerTriggeredJobUsesNativeCronIsTriggered(t *testing.T) {
	sc := NewScheduler(newDiscardLogger())

	triggered := &TestJob{}
	triggered.Name = "check-triggered"
	triggered.Schedule = "@triggered"
	_ = sc.AddJob(triggered)

	regular := &TestJob{}
	regular.Name = "check-regular"
	regular.Schedule = "@daily"
	_ = sc.AddJob(regular)

	// Verify cron.IsTriggered works on the entry's schedule
	trigEntry := sc.EntryByName("check-triggered")
	if !trigEntry.Valid() {
		t.Fatal("triggered entry should be valid")
	}
	if !cron.IsTriggered(trigEntry.Schedule) {
		t.Error("cron.IsTriggered should return true for @triggered entry")
	}

	regEntry := sc.EntryByName("check-regular")
	if !regEntry.Valid() {
		t.Fatal("regular entry should be valid")
	}
	if cron.IsTriggered(regEntry.Schedule) {
		t.Error("cron.IsTriggered should return false for @daily entry")
	}
}

// TestSchedulerTriggeredJobStartup verifies that triggered jobs with
// RunOnStartup=true fire on startup via go-cron's WithRunImmediately.
func TestSchedulerTriggeredJobStartup(t *testing.T) {
	sc := NewScheduler(newDiscardLogger())

	// Triggered job with RunOnStartup
	startupJob := &TestJob{}
	startupJob.Name = "triggered-startup"
	startupJob.Schedule = "@triggered"
	startupJob.RunOnStartup = true

	// Triggered job without RunOnStartup
	noStartupJob := &TestJob{}
	noStartupJob.Name = "triggered-no-startup"
	noStartupJob.Schedule = "@triggered"
	noStartupJob.RunOnStartup = false

	_ = sc.AddJob(startupJob)
	_ = sc.AddJob(noStartupJob)

	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for startup execution
	time.Sleep(300 * time.Millisecond)

	sc.Stop()

	if startupJob.Called() == 0 {
		t.Error("triggered job with RunOnStartup=true should have run on startup")
	}
	if noStartupJob.Called() > 0 {
		t.Error("triggered job with RunOnStartup=false should NOT have run on startup")
	}
}

// TestSchedulerTriggeredJobDisableEnable verifies that triggered jobs can be
// disabled and re-enabled using go-cron's native PauseEntryByName/ResumeEntryByName.
func TestSchedulerTriggeredJobDisableEnable(t *testing.T) {
	sc := NewScheduler(newDiscardLogger())

	triggered := &TestJob{}
	triggered.Name = "disable-me"
	triggered.Schedule = "@triggered"
	_ = sc.AddJob(triggered)

	// Disable the triggered job
	if err := sc.DisableJob("disable-me"); err != nil {
		t.Fatalf("DisableJob: %v", err)
	}

	// Job should be disabled
	if sc.GetJob("disable-me") != nil {
		t.Error("disabled triggered job should not appear as active")
	}
	if sc.GetDisabledJob("disable-me") == nil {
		t.Error("triggered job should appear as disabled")
	}

	// Re-enable
	if err := sc.EnableJob("disable-me"); err != nil {
		t.Fatalf("EnableJob: %v", err)
	}

	if sc.GetJob("disable-me") == nil {
		t.Error("re-enabled triggered job should appear as active")
	}
}

// TestSchedulerTriggeredJobRunDisabledFails verifies that RunJob returns an error
// for disabled triggered jobs (go-cron's TriggerEntryByName returns ErrEntryPaused
// for paused entries, but we check disabled state first in our RunJob).
func TestSchedulerTriggeredJobRunDisabledFails(t *testing.T) {
	sc := NewScheduler(newDiscardLogger())

	triggered := &TestJob{}
	triggered.Name = "disabled-trigger"
	triggered.Schedule = "@triggered"
	_ = sc.AddJob(triggered)
	_ = sc.DisableJob("disabled-trigger")

	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sc.Stop()

	err := sc.RunJob(context.Background(), "disabled-trigger")
	if err == nil {
		t.Error("RunJob should fail for disabled triggered job")
	}
}

// TestSchedulerTriggeredJobRemove verifies that triggered jobs can be removed
// from the scheduler and cron properly.
func TestSchedulerTriggeredJobRemove(t *testing.T) {
	sc := NewScheduler(newDiscardLogger())

	triggered := &TestJob{}
	triggered.Name = "remove-me"
	triggered.Schedule = "@triggered"
	_ = sc.AddJob(triggered)

	// Verify it has a cron entry
	entry := sc.EntryByName("remove-me")
	if !entry.Valid() {
		t.Fatal("triggered job should have a valid cron entry before removal")
	}

	// Remove it
	if err := sc.RemoveJob(triggered); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}

	// Verify it's gone from active jobs
	if sc.GetJob("remove-me") != nil {
		t.Error("removed triggered job should not be in active jobs")
	}

	// Verify it's gone from cron
	entry = sc.EntryByName("remove-me")
	if entry.Valid() {
		t.Error("removed triggered job should not have a cron entry")
	}

	// Verify it's in removed list
	removed := sc.GetRemovedJobs()
	found := false
	for _, j := range removed {
		if j.GetName() == "remove-me" {
			found = true
			break
		}
	}
	if !found {
		t.Error("removed triggered job should appear in removed jobs list")
	}
}
