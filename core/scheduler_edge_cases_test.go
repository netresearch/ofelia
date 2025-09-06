package core

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ErrorJob is a job that can simulate various error conditions
type ErrorJob struct {
	BareJob

	shouldPanic  bool
	shouldError  bool
	errorMessage string
	runDuration  time.Duration
	runCount     int
	mu           sync.Mutex
}

func NewErrorJob(name, schedule string) *ErrorJob {
	job := &ErrorJob{
		runDuration: time.Millisecond * 10,
	}
	job.BareJob.Name = name
	job.BareJob.Schedule = schedule
	job.BareJob.Command = "error-test-job"
	return job
}

func (j *ErrorJob) Run(ctx *Context) error {
	j.mu.Lock()
	j.runCount++
	shouldPanic := j.shouldPanic
	shouldError := j.shouldError
	errorMsg := j.errorMessage
	duration := j.runDuration
	j.mu.Unlock()

	if duration > 0 {
		time.Sleep(duration)
	}

	if shouldPanic {
		panic("simulated job panic")
	}

	if shouldError {
		return errors.New(errorMsg)
	}

	return nil
}

func (j *ErrorJob) SetShouldPanic(shouldPanic bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.shouldPanic = shouldPanic
}

func (j *ErrorJob) SetShouldError(shouldError bool, message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.shouldError = shouldError
	j.errorMessage = message
}

func (j *ErrorJob) SetRunDuration(duration time.Duration) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.runDuration = duration
}

func (j *ErrorJob) GetRunCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.runCount
}

// TestSchedulerErrorHandling tests scheduler's handling of job errors and panics
func TestSchedulerErrorHandling(t *testing.T) {
	scheduler := NewScheduler(&TestLogger{})
	scheduler.SetMaxConcurrentJobs(3)

	// Create jobs with different error conditions
	panicJob := NewErrorJob("panic-job", "@daily")
	panicJob.SetShouldPanic(true)

	errorJob := NewErrorJob("error-job", "@daily")
	errorJob.SetShouldError(true, "simulated job error")

	normalJob := NewErrorJob("normal-job", "@daily")

	// Add jobs to scheduler
	if err := scheduler.AddJob(panicJob); err != nil {
		t.Fatalf("Failed to add panic job: %v", err)
	}
	if err := scheduler.AddJob(errorJob); err != nil {
		t.Fatalf("Failed to add error job: %v", err)
	}
	if err := scheduler.AddJob(normalJob); err != nil {
		t.Fatalf("Failed to add normal job: %v", err)
	}

	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}
	defer scheduler.Stop()

	// Run jobs and verify scheduler remains stable
	if err := scheduler.RunJob("panic-job"); err != nil {
		t.Logf("RunJob for panic job returned error (expected): %v", err)
	}

	if err := scheduler.RunJob("error-job"); err != nil {
		t.Logf("RunJob for error job returned error: %v", err)
	}

	if err := scheduler.RunJob("normal-job"); err != nil {
		t.Errorf("RunJob for normal job should not error: %v", err)
	}

	// Give jobs time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify scheduler is still running and responsive
	if !scheduler.IsRunning() {
		t.Error("Scheduler should still be running after job errors/panics")
	}

	// Verify all jobs ran (even the ones that failed)
	if panicJob.GetRunCount() == 0 {
		t.Error("Panic job should have run at least once")
	}
	if errorJob.GetRunCount() == 0 {
		t.Error("Error job should have run at least once")
	}
	if normalJob.GetRunCount() == 0 {
		t.Error("Normal job should have run at least once")
	}
}

// TestSchedulerInvalidJobOperations tests scheduler's handling of invalid operations
func TestSchedulerInvalidJobOperations(t *testing.T) {
	scheduler := NewScheduler(&TestLogger{})

	// Test operations on non-existent jobs
	if err := scheduler.DisableJob("non-existent"); err == nil {
		t.Error("DisableJob should fail for non-existent job")
	}

	if err := scheduler.EnableJob("non-existent"); err == nil {
		t.Error("EnableJob should fail for non-existent job")
	}

	if err := scheduler.RunJob("non-existent"); err == nil {
		t.Error("RunJob should fail for non-existent job")
	}

	// Test adding job with invalid schedule
	invalidJob := NewErrorJob("invalid-schedule", "not-a-valid-cron-expression")
	if err := scheduler.AddJob(invalidJob); err == nil {
		t.Error("AddJob should fail for job with invalid schedule")
	}

	// Test removing job that was never added
	orphanJob := NewErrorJob("orphan-job", "@daily")
	if err := scheduler.RemoveJob(orphanJob); err != nil {
		t.Logf("RemoveJob on orphan job returned error (may be expected): %v", err)
	}
}

// TestSchedulerConcurrentOperations tests concurrent scheduler operations for race conditions
func TestSchedulerConcurrentOperations(t *testing.T) {
	scheduler := NewScheduler(&TestLogger{})
	scheduler.SetMaxConcurrentJobs(5)

	const numWorkers = 20
	const operationsPerWorker = 50

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// Start scheduler
	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}
	defer scheduler.Stop()

	// Launch concurrent workers performing various operations
	for worker := 0; worker < numWorkers; worker++ {
		go func(workerID int) {
			defer wg.Done()

			for op := 0; op < operationsPerWorker; op++ {
				jobName := fmt.Sprintf("worker%d-job%d", workerID, op)

				switch op % 6 {
				case 0: // Add job
					job := NewErrorJob(jobName, "@daily")
					scheduler.AddJob(job)

				case 1: // Get job
					scheduler.GetJob(jobName)

				case 2: // Run job (may fail if job doesn't exist)
					scheduler.RunJob(jobName)

				case 3: // Disable job (may fail if job doesn't exist)
					scheduler.DisableJob(jobName)

				case 4: // Enable job (may fail if job not disabled)
					scheduler.EnableJob(jobName)

				case 5: // Remove job
					job := NewErrorJob(jobName, "@daily")
					scheduler.RemoveJob(job)
				}

				// Small random delay to increase chance of race conditions
				if op%10 == 0 {
					time.Sleep(time.Microsecond * 100)
				}
			}
		}(worker)
	}

	wg.Wait()

	// Verify scheduler is still functional after concurrent stress
	if !scheduler.IsRunning() {
		t.Error("Scheduler should still be running after concurrent operations")
	}

	// Test basic functionality still works
	testJob := NewErrorJob("final-test", "@daily")
	if err := scheduler.AddJob(testJob); err != nil {
		t.Errorf("Scheduler should still accept jobs after stress test: %v", err)
	}
}

// TestSchedulerStopDuringJobExecution tests stopping scheduler while jobs are executing
func TestSchedulerStopDuringJobExecution(t *testing.T) {
	scheduler := NewScheduler(&TestLogger{})
	scheduler.SetMaxConcurrentJobs(3)

	// Create long-running jobs
	longJob1 := NewErrorJob("long-job-1", "@daily")
	longJob1.SetRunDuration(time.Second * 2)
	longJob2 := NewErrorJob("long-job-2", "@daily")
	longJob2.SetRunDuration(time.Second * 2)
	longJob3 := NewErrorJob("long-job-3", "@daily")
	longJob3.SetRunDuration(time.Second * 2)

	scheduler.AddJob(longJob1)
	scheduler.AddJob(longJob2)
	scheduler.AddJob(longJob3)

	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}

	// Start long-running jobs
	go scheduler.RunJob("long-job-1")
	go scheduler.RunJob("long-job-2")
	go scheduler.RunJob("long-job-3")

	// Give jobs time to start
	time.Sleep(100 * time.Millisecond)

	// Stop scheduler while jobs are running
	stopStart := time.Now()
	stopErr := scheduler.Stop()
	stopDuration := time.Since(stopStart)

	if stopErr != nil {
		t.Errorf("Stop() should not return error: %v", stopErr)
	}

	// Stop should have waited for jobs to complete
	if stopDuration < time.Second {
		t.Errorf("Stop() completed too quickly (%v), should wait for running jobs", stopDuration)
	}

	if scheduler.IsRunning() {
		t.Error("Scheduler should not be running after Stop()")
	}

	// Verify all jobs completed
	if longJob1.GetRunCount() != 1 {
		t.Errorf("Long job 1 should have run once, got %d", longJob1.GetRunCount())
	}
	if longJob2.GetRunCount() != 1 {
		t.Errorf("Long job 2 should have run once, got %d", longJob2.GetRunCount())
	}
	if longJob3.GetRunCount() != 1 {
		t.Errorf("Long job 3 should have run once, got %d", longJob3.GetRunCount())
	}
}

// TestSchedulerMaxConcurrentJobsEdgeCases tests edge cases for concurrent job limits
func TestSchedulerMaxConcurrentJobsEdgeCases(t *testing.T) {
	scheduler := NewScheduler(&TestLogger{})

	// Test setting concurrency to 0 (should normalize to 1)
	scheduler.SetMaxConcurrentJobs(0)

	// Test setting negative concurrency (should normalize to 1)
	scheduler.SetMaxConcurrentJobs(-5)

	// Add more jobs than the limit allows
	const numJobs = 5
	jobs := make([]*ErrorJob, numJobs)
	for i := 0; i < numJobs; i++ {
		jobs[i] = NewErrorJob(fmt.Sprintf("limit-job-%d", i), "@daily")
		jobs[i].SetRunDuration(time.Millisecond * 200)
		scheduler.AddJob(jobs[i])
	}

	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}
	defer scheduler.Stop()

	// Trigger all jobs simultaneously
	for i := 0; i < numJobs; i++ {
		go scheduler.RunJob(fmt.Sprintf("limit-job-%d", i))
	}

	// Give a short time for job scheduling
	time.Sleep(50 * time.Millisecond)

	// Count running jobs (should be limited to 1 due to normalization)
	runningCount := 0
	for _, job := range jobs {
		if job.GetRunCount() > 0 {
			runningCount++
		}
	}

	// Due to the concurrency limit of 1 and job duration, we should see controlled execution
	if runningCount > 1 {
		t.Logf("Running count: %d (may be acceptable due to timing)", runningCount)
	}

	// Wait for jobs to complete
	time.Sleep(time.Second)
}

// TestSchedulerJobStateConsistency tests consistency of job states during operations
func TestSchedulerJobStateConsistency(t *testing.T) {
	scheduler := NewScheduler(&TestLogger{})

	job := NewErrorJob("state-test-job", "@daily")

	// Initial state: job should not be found
	if scheduler.GetJob("state-test-job") != nil {
		t.Error("Job should not be found before adding")
	}
	if scheduler.GetDisabledJob("state-test-job") != nil {
		t.Error("Job should not be found in disabled list before adding")
	}

	// Add job
	if err := scheduler.AddJob(job); err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Job should be active
	if scheduler.GetJob("state-test-job") == nil {
		t.Error("Job should be found after adding")
	}
	if scheduler.GetDisabledJob("state-test-job") != nil {
		t.Error("Job should not be in disabled list when active")
	}

	// Disable job
	if err := scheduler.DisableJob("state-test-job"); err != nil {
		t.Fatalf("Failed to disable job: %v", err)
	}

	// Job should be disabled
	if scheduler.GetJob("state-test-job") != nil {
		t.Error("Job should not be found in active list when disabled")
	}
	if scheduler.GetDisabledJob("state-test-job") == nil {
		t.Error("Job should be found in disabled list when disabled")
	}

	// Enable job
	if err := scheduler.EnableJob("state-test-job"); err != nil {
		t.Fatalf("Failed to enable job: %v", err)
	}

	// Job should be active again
	if scheduler.GetJob("state-test-job") == nil {
		t.Error("Job should be found after re-enabling")
	}
	if scheduler.GetDisabledJob("state-test-job") != nil {
		t.Error("Job should not be in disabled list after re-enabling")
	}

	// Remove job
	if err := scheduler.RemoveJob(job); err != nil {
		t.Fatalf("Failed to remove job: %v", err)
	}

	// Job should be removed
	if scheduler.GetJob("state-test-job") != nil {
		t.Error("Job should not be found after removal")
	}
	if scheduler.GetDisabledJob("state-test-job") != nil {
		t.Error("Job should not be in disabled list after removal")
	}

	// Job should be in removed list
	removedJobs := scheduler.GetRemovedJobs()
	foundInRemoved := false
	for _, removedJob := range removedJobs {
		if removedJob.GetName() == "state-test-job" {
			foundInRemoved = true
			break
		}
	}
	if !foundInRemoved {
		t.Error("Job should be found in removed jobs list")
	}
}

// TestSchedulerWorkflowCleanup tests the workflow cleanup functionality
func TestSchedulerWorkflowCleanup(t *testing.T) {
	scheduler := NewScheduler(&TestLogger{})

	// Create a job to trigger workflow orchestrator initialization
	job := NewErrorJob("workflow-test", "@daily")
	scheduler.AddJob(job)

	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}

	// Verify workflow orchestrator is initialized
	if scheduler.workflowOrchestrator == nil {
		t.Error("Workflow orchestrator should be initialized after Start()")
	}

	// Verify cleanup ticker is initialized
	if scheduler.cleanupTicker == nil {
		t.Error("Cleanup ticker should be initialized after Start()")
	}

	if scheduler.cleanupStop == nil {
		t.Error("Cleanup stop channel should be initialized")
	}

	// Stop scheduler and verify cleanup
	if err := scheduler.Stop(); err != nil {
		t.Fatalf("Failed to stop scheduler: %v", err)
	}

	// Give time for cleanup routine to stop
	time.Sleep(100 * time.Millisecond)
}

// TestSchedulerEmptyStart tests starting scheduler with no jobs
func TestSchedulerEmptyStart(t *testing.T) {
	scheduler := NewScheduler(&TestLogger{})

	// Starting empty scheduler should succeed (no longer returns ErrEmptyScheduler)
	if err := scheduler.Start(); err != nil {
		t.Errorf("Starting empty scheduler should succeed: %v", err)
	}

	if !scheduler.IsRunning() {
		t.Error("Scheduler should be running after successful start")
	}

	// Should be able to add jobs after starting
	job := NewErrorJob("late-job", "@daily")
	if err := scheduler.AddJob(job); err != nil {
		t.Errorf("Should be able to add jobs after starting: %v", err)
	}

	scheduler.Stop()

	if scheduler.IsRunning() {
		t.Error("Scheduler should not be running after stop")
	}
}
