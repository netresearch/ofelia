package core

import (
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

// TestExecJob_InitializeRuntimeFields_NilClient tests that InitializeRuntimeFields
// handles a nil client gracefully
func TestExecJob_InitializeRuntimeFields_NilClient(t *testing.T) {
	job := &ExecJob{}
	job.InitializeRuntimeFields()

	// Should not panic and dockerOps should remain nil
	if job.dockerOps != nil {
		t.Error("Expected dockerOps to be nil when client is nil")
	}
}

// TestExecJob_InitializeRuntimeFields_WithClient tests that InitializeRuntimeFields
// initializes dockerOps when a client is set
func TestExecJob_InitializeRuntimeFields_WithClient(t *testing.T) {
	client, _ := docker.NewClient("unix:///var/run/docker.sock")
	job := &ExecJob{
		Client: client,
	}

	job.InitializeRuntimeFields()

	// dockerOps should now be initialized
	if job.dockerOps == nil {
		t.Error("Expected dockerOps to be initialized when client is set")
	}
}

// TestExecJob_InitializeRuntimeFields_Idempotent tests that InitializeRuntimeFields
// can be called multiple times without side effects
func TestExecJob_InitializeRuntimeFields_Idempotent(t *testing.T) {
	client, _ := docker.NewClient("unix:///var/run/docker.sock")
	job := &ExecJob{
		Client: client,
	}

	job.InitializeRuntimeFields()
	firstOps := job.dockerOps

	job.InitializeRuntimeFields()
	secondOps := job.dockerOps

	// Should be the same instance
	if firstOps != secondOps {
		t.Error("Expected dockerOps to remain the same after multiple InitializeRuntimeFields calls")
	}
}

// TestExecJob_NoNilPointerAfterInitialization verifies that an ExecJob
// created without NewExecJob can still access dockerOps without panic
// after calling InitializeRuntimeFields
func TestExecJob_NoNilPointerAfterInitialization(t *testing.T) {
	client, _ := docker.NewClient("unix:///var/run/docker.sock")

	// Simulate how a job is created from config files/labels
	job := &ExecJob{
		BareJob: BareJob{
			Name:    "test-job",
			Command: "echo hello",
		},
		Client:    client,
		Container: "test-container",
		User:      "nobody",
	}

	// Initialize runtime fields (this is what the config loader should do)
	job.InitializeRuntimeFields()

	// Verify dockerOps is initialized
	if job.dockerOps == nil {
		t.Fatal("Expected dockerOps to be initialized after InitializeRuntimeFields")
	}

	// Create a context
	scheduler := NewScheduler(&SimpleLogger{})
	exec, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}
	defer exec.Cleanup()

	ctx := NewContext(scheduler, job, exec)

	// This should not panic even though job wasn't created with NewExecJob
	// We expect an error because the container doesn't exist, but not a panic
	_, err = job.buildExec(ctx)

	// Verify no nil pointer dereference occurred
	if job.dockerOps == nil {
		t.Error("dockerOps became nil during execution")
	}
}

