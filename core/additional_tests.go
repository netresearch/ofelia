package core

import (
	"testing"
	"time"
)

// TestSchedulerMoreEntries tests the Entries method
func TestSchedulerMoreEntries(t *testing.T) {
	t.Parallel()
	logger := &LogrusAdapter{}
	s := NewScheduler(logger)

	// Add some test jobs
	job1 := &LocalJob{
		BareJob: BareJob{
			Name:     "test-entries-1",
			Schedule: "@every 1h",
			Command:  "echo test1",
		},
	}

	job2 := &LocalJob{
		BareJob: BareJob{
			Name:     "test-entries-2",
			Schedule: "@every 2h",
			Command:  "echo test2",
		},
	}

	if err := s.AddJob(job1); err != nil {
		t.Fatal(err)
	}
	if err := s.AddJob(job2); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop() }()

	// Get entries
	entries := s.Entries()

	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}
}

// TestSchedulerMoreGetDisabledJobs tests the GetDisabledJobs method
func TestSchedulerMoreGetDisabledJobs(t *testing.T) {
	t.Parallel()
	logger := &LogrusAdapter{}
	s := NewScheduler(logger)

	// Add a job and then disable it
	job := &LocalJob{
		BareJob: BareJob{
			Name:     "more-disabled-job",
			Schedule: "@every 1h",
			Command:  "echo disabled",
		},
	}

	if err := s.AddJob(job); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}

	// Disable the job
	if err := s.RemoveJob(job); err != nil {
		t.Error(err)
	}

	// Get disabled jobs
	disabledJobs := s.GetDisabledJobs()

	if len(disabledJobs) != 1 {
		t.Errorf("Expected 1 disabled job, got %d", len(disabledJobs))
	}

	_ = s.Stop()
}

// TestSchedulerMoreRunJob tests the RunJob method
func TestSchedulerMoreRunJob(t *testing.T) {
	t.Parallel()
	logger := &LogrusAdapter{}
	s := NewScheduler(logger)

	job := &LocalJob{
		BareJob: BareJob{
			Name:     "manual-run-job",
			Schedule: "@every 24h",
			Command:  "echo manual",
		},
	}

	if err := s.AddJob(job); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop() }()

	// Run job manually
	err := s.RunJob("manual-run-job")
	if err != nil {
		t.Errorf("Failed to run job: %v", err)
	}

	// Give it a moment to execute
	time.Sleep(100 * time.Millisecond)

	// Test running non-existent job
	err = s.RunJob("non-existent")
	if err == nil {
		t.Error("Expected error when running non-existent job")
	}
}

// TestLocalJobFunctions tests LocalJob functions
func TestLocalJobFunctions(t *testing.T) {
	t.Parallel()
	job := NewLocalJob()

	if job == nil {
		t.Fatal("NewLocalJob returned nil")
	}

	// Set up the job
	job.Name = "test-local"
	job.Command = "echo test"
	job.Dir = "/tmp"

	// Test Hash function
	hash, err := job.Hash()
	if err != nil {
		t.Errorf("Hash returned error: %v", err)
	}

	if hash == "" {
		t.Error("Hash returned empty string")
	}

	// Same job should produce same hash
	hash2, err := job.Hash()
	if err != nil {
		t.Errorf("Hash returned error: %v", err)
	}

	if hash != hash2 {
		t.Error("Hash not consistent")
	}
}

// TestComposeJobFunctions tests ComposeJob functions
func TestComposeJobFunctions(t *testing.T) {
	t.Parallel()
	job := NewComposeJob()

	if job == nil {
		t.Fatal("NewComposeJob returned nil")
	}

	// Set up the job
	job.Name = "test-compose"
	job.Command = "up -d"

	// Test Hash function
	hash, err := job.Hash()
	if err != nil {
		t.Errorf("Hash returned error: %v", err)
	}

	if hash == "" {
		t.Error("Hash returned empty string")
	}
}

// TestExecJobFunctions tests ExecJob functions
func TestExecJobFunctions(t *testing.T) {
	t.Parallel()
	job := NewExecJob(nil)

	if job == nil {
		t.Fatal("NewExecJob returned nil")
	}

	// Set up the job
	job.Name = "test-exec"
	job.Container = "test-container"

	// Test Hash function
	hash, err := job.Hash()
	if err != nil {
		t.Errorf("Hash returned error: %v", err)
	}

	if hash == "" {
		t.Error("Hash returned empty string")
	}
}

// TestRunJobFunctions tests RunJob functions
func TestRunJobFunctions(t *testing.T) {
	t.Parallel()
	job := NewRunJob(nil)

	if job == nil {
		t.Fatal("NewRunJob returned nil")
	}

	// Set up the job
	job.Name = "test-run"
	job.Image = "alpine:latest"

	// Test Hash function
	hash, err := job.Hash()
	if err != nil {
		t.Errorf("Hash returned error: %v", err)
	}

	if hash == "" {
		t.Error("Hash returned empty string")
	}
}

// TestRunServiceJobFunctions tests RunServiceJob functions
func TestRunServiceJobFunctions(t *testing.T) {
	t.Parallel()
	job := NewRunServiceJob(nil)

	if job == nil {
		t.Fatal("NewRunServiceJob returned nil")
	}

	// Set up the job
	job.Name = "test-service"
	job.Image = "nginx:latest"

	// Test Hash function
	hash, err := job.Hash()
	if err != nil {
		t.Errorf("Hash returned error: %v", err)
	}

	if hash == "" {
		t.Error("Hash returned empty string")
	}
}

// TestBufferPoolMoreGetSized tests GetSized method
func TestBufferPoolMoreGetSized(t *testing.T) {
	t.Parallel()
	pool := NewBufferPool(10, 1024, 4096)

	// Get a sized buffer
	buf := pool.GetSized(2048)

	if buf == nil {
		t.Fatal("GetSized returned nil")
	}

	// Check that size is at least what we requested
	if buf.Size() < 2048 {
		t.Errorf("Buffer size %d is less than requested 2048", buf.Size())
	}

	// Return buffer to pool
	pool.Put(buf)

	// Test getting buffer larger than max
	largeBuf := pool.GetSized(8192)
	if largeBuf == nil {
		t.Fatal("GetSized returned nil for large buffer")
	}

	if largeBuf.Size() < 8192 {
		t.Errorf("Large buffer size %d is less than requested 8192", largeBuf.Size())
	}

	pool.Put(largeBuf)
}

// TestContextCreation tests Context creation
func TestContextCreation(t *testing.T) {
	t.Parallel()
	s := NewScheduler(&LogrusAdapter{})
	job := &LocalJob{
		BareJob: BareJob{
			Name:    "test-job",
			Command: "echo test",
		},
	}
	exec, _ := NewExecution()
	ctx := NewContext(s, job, exec)

	if ctx == nil {
		t.Fatal("NewContext returned nil")
	}

	// Test that context has the expected fields
	if ctx.Scheduler != s {
		t.Error("Context scheduler not set correctly")
	}

	if ctx.Job != job {
		t.Error("Context job not set correctly")
	}

	if ctx.Execution != exec {
		t.Error("Context execution not set correctly")
	}
}

// TestLogrusAdapterFunctions tests logrus adapter functions
func TestLogrusAdapterFunctions(t *testing.T) {
	t.Parallel()
	adapter := &LogrusAdapter{}

	// Test that methods can be called without panicking
	adapter.Criticalf("critical: %s", "test")
	adapter.Debugf("debug: %s", "test")
	adapter.Errorf("error: %s", "test")
	adapter.Noticef("notice: %s", "test")
	adapter.Warningf("warning: %s", "test")

	// If we get here without panicking, the test passes
	t.Log("All LogrusAdapter methods are callable")
}
