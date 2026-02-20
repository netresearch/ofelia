package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/netresearch/ofelia/test/testutil"
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
	t.Parallel()
	scheduler := NewScheduler(newDiscardLogger())
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
	if err := scheduler.RunJob(context.Background(), "panic-job"); err != nil {
		t.Logf("RunJob for panic job returned error (expected): %v", err)
	}

	if err := scheduler.RunJob(context.Background(), "error-job"); err != nil {
		t.Logf("RunJob for error job returned error: %v", err)
	}

	if err := scheduler.RunJob(context.Background(), "normal-job"); err != nil {
		t.Errorf("RunJob for normal job should not error: %v", err)
	}

	testutil.Eventually(t, func() bool {
		return panicJob.GetRunCount() > 0 && errorJob.GetRunCount() > 0 && normalJob.GetRunCount() > 0
	}, testutil.WithTimeout(500*time.Millisecond), testutil.WithMessage("jobs should have run"))

	if !scheduler.IsRunning() {
		t.Error("Scheduler should still be running after job errors/panics")
	}
}

// TestSchedulerInvalidJobOperations tests scheduler's handling of invalid operations
func TestSchedulerInvalidJobOperations(t *testing.T) {
	t.Parallel()
	scheduler := NewScheduler(newDiscardLogger())

	// Test operations on non-existent jobs
	if err := scheduler.DisableJob("non-existent"); err == nil {
		t.Error("DisableJob should fail for non-existent job")
	}

	if err := scheduler.EnableJob("non-existent"); err == nil {
		t.Error("EnableJob should fail for non-existent job")
	}

	if err := scheduler.RunJob(context.Background(), "non-existent"); err == nil {
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
	t.Parallel()
	scheduler := NewScheduler(newDiscardLogger())
	scheduler.SetMaxConcurrentJobs(5)

	// Reduced worker count to avoid CI timeouts with race detector
	const numWorkers = 5
	const jobsPerWorker = 3

	// Pre-add jobs before starting the scheduler
	for worker := range numWorkers {
		for jobIdx := range jobsPerWorker {
			jobName := fmt.Sprintf("worker%d-job%d", worker, jobIdx)
			job := NewErrorJob(jobName, "@daily")
			if err := scheduler.AddJob(job); err != nil {
				t.Fatalf("Failed to pre-add job %s: %v", jobName, err)
			}
		}
	}

	// Start scheduler
	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}
	defer scheduler.Stop()

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// Launch concurrent workers with staggered start
	// Only test operations that are safe on a running scheduler
	for worker := range numWorkers {
		go func(workerID int) {
			defer wg.Done()

			// Stagger worker start to reduce initial contention
			time.Sleep(time.Duration(workerID) * time.Millisecond)

			for jobIdx := range jobsPerWorker {
				jobName := fmt.Sprintf("worker%d-job%d", workerID, jobIdx)

				// Cycle through safe operations on existing jobs
				switch jobIdx % 4 {
				case 0: // Get job
					scheduler.GetJob(jobName)

				case 1: // Run job
					scheduler.RunJob(context.Background(), jobName)

				case 2: // Disable job
					scheduler.DisableJob(jobName)

				case 3: // Enable job (may fail if not disabled, that's OK)
					scheduler.EnableJob(jobName)
				}

				// Small delay between operations to reduce lock contention
				time.Sleep(time.Microsecond * 100)
			}
		}(worker)
	}

	wg.Wait()

	// Verify scheduler is still functional after concurrent stress
	if !scheduler.IsRunning() {
		t.Error("Scheduler should still be running after concurrent operations")
	}

	// Test adding a new job while scheduler is running (go-cron v0.7.1 fixed race conditions)
	testJob := NewErrorJob("final-test", "@daily")
	if err := scheduler.AddJob(testJob); err != nil {
		t.Errorf("Scheduler should accept jobs while running: %v", err)
	}
}

// TestSchedulerStopDuringJobExecution tests stopping scheduler while jobs are executing
func TestSchedulerStopDuringJobExecution(t *testing.T) {
	t.Parallel()
	scheduler := NewScheduler(newDiscardLogger())
	scheduler.SetMaxConcurrentJobs(3)

	longJob1 := NewErrorJob("long-job-1", "@daily")
	longJob1.SetRunDuration(50 * time.Millisecond)
	longJob2 := NewErrorJob("long-job-2", "@daily")
	longJob2.SetRunDuration(50 * time.Millisecond)
	longJob3 := NewErrorJob("long-job-3", "@daily")
	longJob3.SetRunDuration(50 * time.Millisecond)

	scheduler.AddJob(longJob1)
	scheduler.AddJob(longJob2)
	scheduler.AddJob(longJob3)

	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}

	go scheduler.RunJob(context.Background(), "long-job-1")
	go scheduler.RunJob(context.Background(), "long-job-2")
	go scheduler.RunJob(context.Background(), "long-job-3")

	testutil.Eventually(t, func() bool {
		return longJob1.GetRunCount() > 0 && longJob2.GetRunCount() > 0 && longJob3.GetRunCount() > 0
	}, testutil.WithTimeout(200*time.Millisecond), testutil.WithInterval(5*time.Millisecond),
		testutil.WithMessage("all jobs should start"))

	stopStart := time.Now()
	stopErr := scheduler.Stop()
	stopDuration := time.Since(stopStart)

	if stopErr != nil {
		t.Errorf("Stop() should not return error: %v", stopErr)
	}

	if stopDuration < 25*time.Millisecond {
		t.Errorf("Stop() completed too quickly (%v), should wait for running jobs", stopDuration)
	}

	if scheduler.IsRunning() {
		t.Error("Scheduler should not be running after Stop()")
	}

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
	t.Parallel()
	scheduler := NewScheduler(newDiscardLogger())

	scheduler.SetMaxConcurrentJobs(0)
	scheduler.SetMaxConcurrentJobs(-5)

	const numJobs = 5
	jobs := make([]*ErrorJob, numJobs)
	for i := range numJobs {
		jobs[i] = NewErrorJob(fmt.Sprintf("limit-job-%d", i), "@daily")
		jobs[i].SetRunDuration(30 * time.Millisecond)
		scheduler.AddJob(jobs[i])
	}

	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}
	defer scheduler.Stop()

	for i := range numJobs {
		go scheduler.RunJob(context.Background(), fmt.Sprintf("limit-job-%d", i))
	}

	testutil.Eventually(t, func() bool {
		for _, job := range jobs {
			if job.GetRunCount() > 0 {
				return true
			}
		}
		return false
	}, testutil.WithTimeout(100*time.Millisecond), testutil.WithMessage("at least one job should start"))
}

// TestSchedulerJobStateConsistency tests consistency of job states during operations
func TestSchedulerJobStateConsistency(t *testing.T) {
	t.Parallel()
	scheduler := NewScheduler(newDiscardLogger())

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
	t.Parallel()
	scheduler := NewScheduler(newDiscardLogger())

	// Create a job to trigger workflow orchestrator initialization
	job := NewErrorJob("workflow-test", "@daily")
	scheduler.AddJob(job)

	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}

	// Verify cron instance is initialized and running
	if !scheduler.IsRunning() {
		t.Error("Scheduler should be running after Start()")
	}

	// Stop scheduler and verify clean shutdown
	if err := scheduler.Stop(); err != nil {
		t.Fatalf("Failed to stop scheduler: %v", err)
	}

	// Give time for cleanup routine to stop
	time.Sleep(100 * time.Millisecond)
}

// TestSchedulerEmptyStart tests starting scheduler with no jobs
func TestSchedulerEmptyStart(t *testing.T) {
	t.Parallel()
	scheduler := NewScheduler(newDiscardLogger())

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
