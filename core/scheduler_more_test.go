package core

import "testing"

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
