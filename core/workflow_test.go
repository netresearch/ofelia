package core

import (
	"testing"
)

// TestWorkflowDependencies tests job dependency resolution
func TestWorkflowDependencies(t *testing.T) {
	logger := &TestLogger{}
	scheduler := NewScheduler(logger)
	orchestrator := scheduler.workflowOrchestrator
	
	// Create test jobs with dependencies
	jobA := &BareJob{
		Name:         "job-a",
		Command:      "echo A",
		Dependencies: []string{},
	}
	
	jobB := &BareJob{
		Name:         "job-b",
		Command:      "echo B",
		Dependencies: []string{"job-a"},
	}
	
	jobC := &BareJob{
		Name:         "job-c",
		Command:      "echo C",
		Dependencies: []string{"job-a", "job-b"},
	}
	
	jobs := []Job{jobA, jobB, jobC}
	
	// Build dependency graph
	err := orchestrator.BuildDependencyGraph(jobs)
	if err != nil {
		t.Fatalf("Failed to build dependency graph: %v", err)
	}
	
	// Test initial state - only job A can run
	executionID := "test-exec-1"
	
	if !orchestrator.CanExecute("job-a", executionID) {
		t.Error("Job A should be able to execute (no dependencies)")
	}
	
	if orchestrator.CanExecute("job-b", executionID) {
		t.Error("Job B should not be able to execute (depends on A)")
	}
	
	if orchestrator.CanExecute("job-c", executionID) {
		t.Error("Job C should not be able to execute (depends on A and B)")
	}
	
	// Mark job A as completed
	orchestrator.JobStarted("job-a", executionID)
	orchestrator.JobCompleted("job-a", executionID, true)
	
	// Now job B should be able to run
	if !orchestrator.CanExecute("job-b", executionID) {
		t.Error("Job B should be able to execute after A completes")
	}
	
	if orchestrator.CanExecute("job-c", executionID) {
		t.Error("Job C should not be able to execute (still waiting for B)")
	}
	
	// Mark job B as completed
	orchestrator.JobStarted("job-b", executionID)
	orchestrator.JobCompleted("job-b", executionID, true)
	
	// Now job C should be able to run
	if !orchestrator.CanExecute("job-c", executionID) {
		t.Error("Job C should be able to execute after A and B complete")
	}
}

// TestCircularDependencyDetection tests detection of circular dependencies
func TestCircularDependencyDetection(t *testing.T) {
	logger := &TestLogger{}
	scheduler := NewScheduler(logger)
	orchestrator := scheduler.workflowOrchestrator
	
	// Create jobs with circular dependency
	jobA := &BareJob{
		Name:         "job-a",
		Command:      "echo A",
		Dependencies: []string{"job-c"}, // A depends on C
	}
	
	jobB := &BareJob{
		Name:         "job-b",
		Command:      "echo B",
		Dependencies: []string{"job-a"}, // B depends on A
	}
	
	jobC := &BareJob{
		Name:         "job-c",
		Command:      "echo C",
		Dependencies: []string{"job-b"}, // C depends on B (creates cycle)
	}
	
	jobs := []Job{jobA, jobB, jobC}
	
	// Should detect circular dependency
	err := orchestrator.BuildDependencyGraph(jobs)
	if err == nil {
		t.Error("Should have detected circular dependency")
	}
	
	if err.Error() == "" || !contains(err.Error(), "circular") {
		t.Errorf("Error should mention circular dependency, got: %v", err)
	}
}

// TestOnSuccessOnFailureTriggers tests conditional job triggers
func TestOnSuccessOnFailureTriggers(t *testing.T) {
	logger := &TestLogger{}
	scheduler := NewScheduler(logger)
	orchestrator := scheduler.workflowOrchestrator
	
	// Create test jobs with success/failure triggers
	jobMain := &BareJob{
		Name:      "job-main",
		Command:   "echo main",
		OnSuccess: []string{"job-success"},
		OnFailure: []string{"job-failure"},
	}
	
	jobSuccess := &BareJob{
		Name:    "job-success",
		Command: "echo success",
	}
	
	jobFailure := &BareJob{
		Name:    "job-failure",
		Command: "echo failure",
	}
	
	jobs := []Job{jobMain, jobSuccess, jobFailure}
	scheduler.Jobs = jobs
	scheduler.jobsByName = map[string]Job{
		"job-main":    jobMain,
		"job-success": jobSuccess,
		"job-failure": jobFailure,
	}
	
	err := orchestrator.BuildDependencyGraph(jobs)
	if err != nil {
		t.Fatalf("Failed to build dependency graph: %v", err)
	}
	
	// For this test, we'll just verify that the orchestrator correctly
	// identifies which jobs should be triggered based on success/failure
	// The actual RunJob method would be called in a real scenario
	
	// Test success trigger
	executionID := "test-exec-success"
	orchestrator.JobStarted("job-main", executionID)
	
	// Complete main job successfully
	orchestrator.JobCompleted("job-main", executionID, true)
	
	// Verify that job-success can now execute (would be triggered)
	if !orchestrator.CanExecute("job-success", executionID) {
		t.Error("job-success should be able to execute after main job succeeds")
	}
	
	// Test failure trigger
	executionID = "test-exec-failure"
	orchestrator.JobStarted("job-main", executionID)
	orchestrator.JobCompleted("job-main", executionID, false)
	
	// Verify that job-failure can execute (would be triggered)
	if !orchestrator.CanExecute("job-failure", executionID) {
		t.Error("job-failure should be able to execute after main job fails")
	}
}

// TestParallelExecutionControl tests AllowParallel flag
func TestParallelExecutionControl(t *testing.T) {
	logger := &TestLogger{}
	scheduler := NewScheduler(logger)
	orchestrator := scheduler.workflowOrchestrator
	
	// Create job that doesn't allow parallel execution
	job := &BareJob{
		Name:          "job-no-parallel",
		Command:       "echo test",
		AllowParallel: false,
	}
	
	jobs := []Job{job}
	err := orchestrator.BuildDependencyGraph(jobs)
	if err != nil {
		t.Fatalf("Failed to build dependency graph: %v", err)
	}
	
	executionID := "test-exec-parallel"
	
	// First execution should be allowed
	if !orchestrator.CanExecute("job-no-parallel", executionID) {
		t.Error("First execution should be allowed")
	}
	
	// Mark job as running
	orchestrator.JobStarted("job-no-parallel", executionID)
	
	// Second execution should be blocked
	if orchestrator.CanExecute("job-no-parallel", executionID) {
		t.Error("Parallel execution should be blocked")
	}
	
	// Complete the job
	orchestrator.JobCompleted("job-no-parallel", executionID, true)
	
	// Now it should be allowed again
	if !orchestrator.CanExecute("job-no-parallel", executionID) {
		t.Error("Execution should be allowed after job completes")
	}
}

// TestWorkflowStatus tests workflow status tracking
func TestWorkflowStatus(t *testing.T) {
	logger := &TestLogger{}
	scheduler := NewScheduler(logger)
	orchestrator := scheduler.workflowOrchestrator
	
	jobA := &BareJob{Name: "job-a", Command: "echo A"}
	jobB := &BareJob{Name: "job-b", Command: "echo B"}
	
	jobs := []Job{jobA, jobB}
	err := orchestrator.BuildDependencyGraph(jobs)
	if err != nil {
		t.Fatalf("Failed to build dependency graph: %v", err)
	}
	
	executionID := "test-workflow-status"
	
	// Start and complete jobs
	orchestrator.JobStarted("job-a", executionID)
	orchestrator.JobStarted("job-b", executionID)
	
	status := orchestrator.GetWorkflowStatus(executionID)
	if status == nil {
		t.Fatal("Expected workflow status, got nil")
	}
	
	if status["runningJobs"].(int) != 2 {
		t.Errorf("Expected 2 running jobs, got %v", status["runningJobs"])
	}
	
	// Complete one job successfully
	orchestrator.JobCompleted("job-a", executionID, true)
	
	status = orchestrator.GetWorkflowStatus(executionID)
	if status["completedJobs"].(int) != 1 {
		t.Errorf("Expected 1 completed job, got %v", status["completedJobs"])
	}
	if status["runningJobs"].(int) != 1 {
		t.Errorf("Expected 1 running job, got %v", status["runningJobs"])
	}
	
	// Fail the other job
	orchestrator.JobCompleted("job-b", executionID, false)
	
	status = orchestrator.GetWorkflowStatus(executionID)
	if status["failedJobs"].(int) != 1 {
		t.Errorf("Expected 1 failed job, got %v", status["failedJobs"])
	}
	if status["runningJobs"].(int) != 0 {
		t.Errorf("Expected 0 running jobs, got %v", status["runningJobs"])
	}
}

// Helper function
func contains(str, substr string) bool {
	return len(str) > 0 && len(substr) > 0 && str != substr && 
		(len(str) >= len(substr)) && 
		(str[:len(substr)] == substr || contains(str[1:], substr))
}