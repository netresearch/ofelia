package core

import (
	"testing"
)

// TestCronJobID tests SetCronJobID and GetCronJobID of BareJob.
func TestCronJobID(t *testing.T) {
	job := &BareJob{}
	if id := job.GetCronJobID(); id != 0 {
		t.Errorf("expected initial CronJobID 0, got %d", id)
	}
	job.SetCronJobID(42)
	if id := job.GetCronJobID(); id != 42 {
		t.Errorf("expected CronJobID 42, got %d", id)
	}
}

// TestHash tests the Hash method for consistency and sensitivity.
func TestHash(t *testing.T) {
	// Two jobs with identical fields should have identical hashes
	job1 := &BareJob{Schedule: "sched", Name: "name", Command: "cmd"}
	job2 := &BareJob{Schedule: "sched", Name: "name", Command: "cmd"}
	h1 := job1.Hash()
	h2 := job2.Hash()
	if h1 == "" {
		t.Errorf("expected non-empty hash, got empty string")
	}
	if h1 != h2 {
		t.Errorf("expected identical hashes for identical jobs, got %q and %q", h1, h2)
	}

	// Changing one field should change the hash
	job2.Command = "other"
	h3 := job2.Hash()
	if h3 == h1 {
		t.Errorf("expected different hash after modifying Command, got %q", h3)
	}
}