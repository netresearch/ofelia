package core

import (
	"testing"
)

// TestRandomIDLength verifies that randomID produces a string of length 12.
func TestRandomIDLength(t *testing.T) {
	id, err := randomID()
	if err != nil {
		t.Fatalf("randomID error: %v", err)
	}
	if len(id) != 12 {
		t.Errorf("expected ID length 12, got %d", len(id))
	}
}

// TestRandomIDUniqueness verifies that multiple randomID calls produce unique values.
func TestRandomIDUniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		id, err := randomID()
		if err != nil {
			t.Fatalf("randomID error: %v", err)
		}
		if ids[id] {
			t.Errorf("duplicate ID found: %s", id)
		}
		ids[id] = true
	}
}

// TestBareJobHashValue verifies that BareJob.Hash concatenates fields in order.
func TestBareJobHashValue(t *testing.T) {
	job := &BareJob{
		Schedule: "sched",
		Name:     "name",
		Command:  "cmd",
	}
	// Expect "schednamecmd"
	want := "schednamecmd"
	got, err := job.Hash()
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	if got != want {
		t.Errorf("expected hash %q, got %q", want, got)
	}
}

// TestBareJobHashConsistency verifies that identical BareJob structs produce the same hash.
func TestBareJobHashConsistency(t *testing.T) {
	job1 := &BareJob{Schedule: "s", Name: "n", Command: "c"}
	job2 := &BareJob{Schedule: "s", Name: "n", Command: "c"}
	h1, err := job1.Hash()
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	h2, err := job2.Hash()
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	if h1 != h2 {
		t.Errorf("expected identical hashes, got %s and %s", h1, h2)
	}
}

// TestBareJobHashDifference verifies that differences in fields produce different hashes.
func TestBareJobHashDifference(t *testing.T) {
	job1 := &BareJob{Schedule: "s1", Name: "n", Command: "c"}
	job2 := &BareJob{Schedule: "s2", Name: "n", Command: "c"}
	h1, err := job1.Hash()
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	h2, err := job2.Hash()
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	if h1 == h2 {
		t.Errorf("expected different hashes, both %s", h1)
	}
}
