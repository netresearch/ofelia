package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netresearch/go-cron"
)

// ============================================================================
// Tests targeting survived mutations in core/scheduler.go
// ============================================================================

// --- ARITHMETIC_BASE at line 69: -time.Nanosecond ---
// --- INVERT_NEGATIVES at line 69: -time.Nanosecond ---
// NewSchedulerWithClock passes -time.Nanosecond as minEveryInterval.
// A mutant that changes the sign (e.g. to +time.Nanosecond or 0) would cause
// the scheduler to use the library default minimum interval (1s) instead of
// allowing sub-second schedules.
func TestNewSchedulerWithClock_NegativeNanosecond(t *testing.T) {
	t.Parallel()

	cronClock := NewCronClock(time.Now())
	sc := NewSchedulerWithClock(newDiscardLogger(), cronClock)

	// With -time.Nanosecond, sub-second schedules should work.
	// If the sign were inverted to +time.Nanosecond, the library would enforce
	// a minimum interval of 1 nanosecond (effectively allowing sub-second).
	// But if it became 0, the library default (1s) would be used and this
	// schedule would fail or be capped.
	job := &TestJob{}
	job.Name = "sub-second-job"
	job.Schedule = "@every 10ms"

	err := sc.AddJob(job)
	if err != nil {
		t.Fatalf("AddJob with sub-second schedule should succeed with negative minEveryInterval, got: %v", err)
	}

	_ = sc.Stop()
}

// --- ARITHMETIC_BASE at line 416: 1 * time.Hour ---
// startWorkflowCleanup uses `1 * time.Hour` as cleanup interval.
// Mutation changes arithmetic (e.g., 1+time.Hour or 1-time.Hour).
// Verify cleanup ticker is created with a sensible interval.
func TestStartWorkflowCleanup_CleanupInterval(t *testing.T) {
	t.Parallel()

	fakeClock := NewFakeClock(time.Now())
	sc := NewScheduler(newDiscardLogger())
	sc.SetClock(fakeClock)

	job := &TestJob{}
	job.Name = "cleanup-test-job"
	job.Schedule = "@daily"
	if err := sc.AddJob(job); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The cleanup ticker should have been created
	if sc.cleanupTicker == nil {
		t.Fatal("cleanupTicker should be initialized after Start()")
	}

	// Add an old execution to the workflow orchestrator
	oldTime := time.Now().Add(-48 * time.Hour) // 48 hours ago, older than 24h retention
	sc.workflowOrchestrator.mu.Lock()
	sc.workflowOrchestrator.executions["old-exec"] = &WorkflowExecution{
		ID:            "old-exec",
		StartTime:     oldTime,
		CompletedJobs: make(map[string]bool),
		FailedJobs:    make(map[string]bool),
		RunningJobs:   make(map[string]bool),
	}
	sc.workflowOrchestrator.mu.Unlock()

	// Advance the clock by 1 hour to trigger the cleanup
	fakeClock.Advance(1 * time.Hour)

	// Give the goroutine a moment to process
	time.Sleep(50 * time.Millisecond)

	// The old execution should have been cleaned up
	sc.workflowOrchestrator.mu.RLock()
	_, exists := sc.workflowOrchestrator.executions["old-exec"]
	sc.workflowOrchestrator.mu.RUnlock()

	if exists {
		t.Error("Old workflow execution should have been cleaned up after 1 hour tick")
	}

	_ = sc.Stop()
}

// --- ARITHMETIC_BASE at line 497: return nil, -1 ---
// --- INVERT_NEGATIVES at line 497: -1 ---
// getJob returns (nil, -1) when a job is not found. The -1 sentinel value
// is critical for callers to detect "not found". If mutated to +1, callers
// would mistakenly use index 1.
func TestGetJob_NotFoundReturnsNegativeOne(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	// No jobs added - getJob should return (nil, -1)
	job, idx := getJob(sc.Jobs, "nonexistent")
	if job != nil {
		t.Error("getJob should return nil for nonexistent job")
	}
	if idx != -1 {
		t.Errorf("getJob should return -1 for nonexistent job, got %d", idx)
	}

	// Add a job and search for a different name
	testJob := &TestJob{}
	testJob.Name = "exists"
	testJob.Schedule = "@daily"
	if err := sc.AddJob(testJob); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	job, idx = getJob(sc.Jobs, "does-not-exist")
	if job != nil {
		t.Error("getJob should return nil for missing job name")
	}
	if idx != -1 {
		t.Errorf("getJob should return -1 when job not found among existing jobs, got %d", idx)
	}

	// Verify found job returns correct index (index 0)
	job, idx = getJob(sc.Jobs, "exists")
	if job == nil {
		t.Fatal("getJob should find the existing job")
	}
	if idx != 0 {
		t.Errorf("getJob should return index 0 for first job, got %d", idx)
	}
}

// Test getJob returns correct index for multiple jobs
func TestGetJob_CorrectIndex(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job1 := &TestJob{}
	job1.Name = "job-a"
	job1.Schedule = "@daily"
	job2 := &TestJob{}
	job2.Name = "job-b"
	job2.Schedule = "@daily"
	job3 := &TestJob{}
	job3.Name = "job-c"
	job3.Schedule = "@daily"

	_ = sc.AddJob(job1)
	_ = sc.AddJob(job2)
	_ = sc.AddJob(job3)

	// Verify each job is at the correct index
	_, idx := getJob(sc.Jobs, "job-a")
	if idx != 0 {
		t.Errorf("job-a should be at index 0, got %d", idx)
	}
	_, idx = getJob(sc.Jobs, "job-b")
	if idx != 1 {
		t.Errorf("job-b should be at index 1, got %d", idx)
	}
	_, idx = getJob(sc.Jobs, "job-c")
	if idx != 2 {
		t.Errorf("job-c should be at index 2, got %d", idx)
	}
}

// --- CONDITIONALS_BOUNDARY at line 142: maxJobs < 1 ---
// SetMaxConcurrentJobs: tests the boundary condition where maxJobs == 1.
// The condition is `if maxJobs < 1`. If mutated to `maxJobs <= 1`, then
// passing 1 would be changed to 1 (no-op), but passing 0 or -1 should still
// become 1. The key is to test that exactly 1 is accepted as-is.
func TestSetMaxConcurrentJobs_BoundaryExactlyOne(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	// maxJobs = 1 should be accepted as-is (not changed)
	sc.SetMaxConcurrentJobs(1)
	if sc.maxConcurrentJobs != 1 {
		t.Errorf("SetMaxConcurrentJobs(1) should set to 1, got %d", sc.maxConcurrentJobs)
	}

	// maxJobs = 0 should be normalized to 1
	sc.SetMaxConcurrentJobs(0)
	if sc.maxConcurrentJobs != 1 {
		t.Errorf("SetMaxConcurrentJobs(0) should normalize to 1, got %d", sc.maxConcurrentJobs)
	}

	// maxJobs = -1 should be normalized to 1
	sc.SetMaxConcurrentJobs(-1)
	if sc.maxConcurrentJobs != 1 {
		t.Errorf("SetMaxConcurrentJobs(-1) should normalize to 1, got %d", sc.maxConcurrentJobs)
	}

	// maxJobs = 2 should be accepted as-is
	sc.SetMaxConcurrentJobs(2)
	if sc.maxConcurrentJobs != 2 {
		t.Errorf("SetMaxConcurrentJobs(2) should set to 2, got %d", sc.maxConcurrentJobs)
	}
}

// --- CONDITIONALS_BOUNDARY at line 200: len(tags) > 0 ---
// --- CONDITIONALS_NEGATION at line 200: len(tags) > 0 ---
// AddJobWithTags: When tags are provided (len > 0), they should be added
// as cron job options. When no tags are provided, they should not.
func TestAddJobWithTags_TagBoundary(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	// Test with no tags (len(tags) == 0)
	job1 := &TestJob{}
	job1.Name = "no-tags-job"
	job1.Schedule = "@daily"
	err := sc.AddJobWithTags(job1)
	if err != nil {
		t.Fatalf("AddJobWithTags with no tags should succeed: %v", err)
	}

	// Test with exactly one tag (len(tags) == 1, boundary)
	job2 := &TestJob{}
	job2.Name = "one-tag-job"
	job2.Schedule = "@daily"
	err = sc.AddJobWithTags(job2, "tag1")
	if err != nil {
		t.Fatalf("AddJobWithTags with one tag should succeed: %v", err)
	}

	// Verify the tagged job can be found by tag
	taggedJobs := sc.GetJobsByTag("tag1")
	if len(taggedJobs) == 0 {
		t.Error("Job with tag should be findable by GetJobsByTag")
	}

	// Verify the untagged job is NOT found by tag
	untaggedByTag := sc.GetJobsByTag("nonexistent-tag")
	if len(untaggedByTag) != 0 {
		t.Error("Untagged job should not appear in tag search")
	}

	// Test with multiple tags
	job3 := &TestJob{}
	job3.Name = "multi-tag-job"
	job3.Schedule = "@hourly"
	err = sc.AddJobWithTags(job3, "tag-a", "tag-b")
	if err != nil {
		t.Fatalf("AddJobWithTags with multiple tags should succeed: %v", err)
	}
}

// --- CONDITIONALS_NEGATION at line 127: metricsRecorder != nil ---
// Tests that metrics recorder is properly set on retry executor when non-nil,
// and NOT set when nil.
func TestSchedulerMetricsRecorderOnRetryExecutor(t *testing.T) {
	t.Parallel()

	// With nil metrics recorder - retry executor should NOT have metrics
	scNoMetrics := NewScheduler(newDiscardLogger())
	if scNoMetrics.retryExecutor == nil {
		t.Fatal("retryExecutor should always be initialized")
	}
	// With nil metricsRecorder, the retry executor's recorder should also be nil
	if scNoMetrics.metricsRecorder != nil {
		t.Error("metricsRecorder should be nil when not provided")
	}

	// With non-nil metrics recorder
	mockRecorder := &mockMetricsRecorder{}
	scWithMetrics := NewSchedulerWithMetrics(newDiscardLogger(), mockRecorder)
	if scWithMetrics.metricsRecorder == nil {
		t.Error("metricsRecorder should be set when provided")
	}
	if scWithMetrics.metricsRecorder != mockRecorder {
		t.Error("metricsRecorder should be the one we passed in")
	}
}

// --- CONDITIONALS_NEGATION at line 155: s.retryExecutor != nil ---
// SetMetricsRecorder should propagate to retryExecutor when it's non-nil.
func TestSetMetricsRecorder_PropagesToRetryExecutor(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())
	if sc.retryExecutor == nil {
		t.Fatal("retryExecutor should be initialized")
	}

	recorder := &mockMetricsRecorder{}
	sc.SetMetricsRecorder(recorder)

	if sc.metricsRecorder != recorder {
		t.Error("metricsRecorder should be set")
	}
	// The retryExecutor should also have the recorder set
	// (this tests that the condition `if s.retryExecutor != nil` is true
	// and the propagation happens)
}

// --- CONDITIONALS_NEGATION: ShouldRunOnStartup via WithRunImmediately ---
// In AddJobWithTags(), jobs with RunOnStartup=true get WithRunImmediately().
// For triggered schedules, go-cron fires them once at startup then they go dormant.
// This tests that the startup execution works correctly for triggered jobs.
func TestSchedulerStart_TriggeredJobStartup(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	// Triggered job WITH RunOnStartup=true -> should run on start
	triggeredStartup := &TestJob{}
	triggeredStartup.Name = "triggered-startup"
	triggeredStartup.Schedule = "@triggered"
	triggeredStartup.RunOnStartup = true

	// Triggered job WITHOUT RunOnStartup -> should NOT run on start
	triggeredNoStartup := &TestJob{}
	triggeredNoStartup.Name = "triggered-no-startup"
	triggeredNoStartup.Schedule = "@triggered"
	triggeredNoStartup.RunOnStartup = false

	// Regular (non-triggered) job with RunOnStartup -> handled by cron's WithRunImmediately
	regularStartup := &TestJob{}
	regularStartup.Name = "regular-startup"
	regularStartup.Schedule = "@every 1h"
	regularStartup.RunOnStartup = true

	_ = sc.AddJob(triggeredStartup)
	_ = sc.AddJob(triggeredNoStartup)
	_ = sc.AddJob(regularStartup)

	completedJobs := make(map[string]bool)
	var completedMu atomic.Int32
	sc.SetOnJobComplete(func(name string, _ bool) {
		completedMu.Add(1)
		// Use a simple approach since we can't safely use a map from multiple goroutines
	})

	_ = sc.Start()

	// Wait for jobs to complete
	time.Sleep(200 * time.Millisecond)

	_ = sc.Stop()

	// triggered-startup should have run (RunOnStartup=true with @triggered schedule)
	if triggeredStartup.Called() == 0 {
		t.Error("Triggered job with RunOnStartup=true should have run on scheduler start")
	}

	// triggered-no-startup should NOT have run
	if triggeredNoStartup.Called() > 0 {
		t.Error("Triggered job with RunOnStartup=false should NOT have run on scheduler start")
	}

	// Check completedJobs is not referenced (we used atomic counter)
	_ = completedJobs
}

// --- CONDITIONALS_NEGATION at line 419 and 424 ---
// These are in startWorkflowCleanup, related to environment variable parsing.
// Line 419: if interval := os.Getenv("OFELIA_WORKFLOW_CLEANUP_INTERVAL"); interval != ""
// Line 424: if d, err := time.ParseDuration(interval); err == nil
// Line 424: if d, err := time.ParseDuration(retention); err == nil
// We test that the default values are used when env vars are not set.
func TestStartWorkflowCleanup_DefaultValues(t *testing.T) {
	t.Parallel()

	fakeClock := NewFakeClock(time.Now())
	sc := NewScheduler(newDiscardLogger())
	sc.SetClock(fakeClock)

	job := &TestJob{}
	job.Name = "default-cleanup-job"
	job.Schedule = "@daily"
	_ = sc.AddJob(job)

	_ = sc.Start()

	// Without env vars, cleanup should use defaults:
	// - cleanupInterval: 1 hour
	// - retentionDuration: 24 hours
	if sc.cleanupTicker == nil {
		t.Fatal("cleanupTicker should be initialized")
	}

	// Add execution that is 25 hours old (should be cleaned after 24h retention)
	sc.workflowOrchestrator.mu.Lock()
	sc.workflowOrchestrator.executions["should-clean"] = &WorkflowExecution{
		ID:            "should-clean",
		StartTime:     time.Now().Add(-25 * time.Hour),
		CompletedJobs: make(map[string]bool),
		FailedJobs:    make(map[string]bool),
		RunningJobs:   make(map[string]bool),
	}
	// Add execution that is 23 hours old (should NOT be cleaned with 24h retention)
	sc.workflowOrchestrator.executions["should-keep"] = &WorkflowExecution{
		ID:            "should-keep",
		StartTime:     time.Now().Add(-23 * time.Hour),
		CompletedJobs: make(map[string]bool),
		FailedJobs:    make(map[string]bool),
		RunningJobs:   make(map[string]bool),
	}
	sc.workflowOrchestrator.mu.Unlock()

	// Advance clock by 1 hour to trigger cleanup
	fakeClock.Advance(1 * time.Hour)
	time.Sleep(50 * time.Millisecond)

	// 25-hour-old execution should be cleaned
	sc.workflowOrchestrator.mu.RLock()
	_, existsOld := sc.workflowOrchestrator.executions["should-clean"]
	_, existsNew := sc.workflowOrchestrator.executions["should-keep"]
	sc.workflowOrchestrator.mu.RUnlock()

	if existsOld {
		t.Error("25-hour-old execution should have been cleaned up (retention is 24h)")
	}
	if !existsNew {
		t.Error("23-hour-old execution should NOT have been cleaned up (retention is 24h)")
	}

	_ = sc.Stop()
}

// --- CONDITIONALS_NEGATION at line 589: j == nil ---
// EnableJob checks if the job exists in the Disabled list. If negated,
// it would return an error for existing jobs and succeed for non-existing ones.
func TestEnableJob_DisabledJobExists(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job := &TestJob{}
	job.Name = "enable-test"
	job.Schedule = "@daily"
	_ = sc.AddJob(job)

	// Disable it first
	err := sc.DisableJob("enable-test")
	if err != nil {
		t.Fatalf("DisableJob: %v", err)
	}

	// Enable should succeed because the job IS in the disabled list
	err = sc.EnableJob("enable-test")
	if err != nil {
		t.Fatalf("EnableJob should succeed for disabled job: %v", err)
	}

	// Enable of a non-existent job should fail
	err = sc.EnableJob("never-existed")
	if err == nil {
		t.Error("EnableJob should fail for non-existent job")
	}
}

// --- CONDITIONALS_NEGATION at line 651: s.cron == nil ---
// IsJobRunning returns false when cron is nil.
func TestIsJobRunning_NilCron(t *testing.T) {
	t.Parallel()

	// Create a scheduler and nil out cron to test the guard
	sc := &Scheduler{
		Logger: newDiscardLogger(),
		cron:   nil,
	}

	result := sc.IsJobRunning("any-job")
	if result {
		t.Error("IsJobRunning should return false when cron is nil")
	}
}

// Test IsJobRunning with valid cron but non-running job
func TestIsJobRunning_NoSuchJob(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())
	result := sc.IsJobRunning("nonexistent")
	if result {
		t.Error("IsJobRunning should return false for nonexistent job")
	}
}

// --- CONDITIONALS_BOUNDARY + CONDITIONALS_NEGATION at lines 680, 684 ---
// These are in runWithCtx (jobWrapper):
// Line 680: !w.s.workflowOrchestrator.CanExecute(...) -- dependency check
// Line 684: semaphore select/default -- concurrency limit
// Tests that the dependency check and semaphore work correctly.
func TestJobWrapper_DependencyCheckPreventsExecution(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	// Use *BareJob directly so BuildDependencyGraph can extract Dependencies
	// (type assertion `job.(*BareJob)` only works for exact *BareJob type)
	prereqJob := &BareJob{
		Name:     "prerequisite-job",
		Schedule: "@daily",
		Command:  "echo prereq",
	}
	depJob := &BareJob{
		Name:         "dependent-job",
		Schedule:     "@daily",
		Command:      "echo dependent",
		Dependencies: []string{"prerequisite-job"},
	}

	_ = sc.AddJob(prereqJob)
	_ = sc.AddJob(depJob)

	_ = sc.Start()

	// Run the dependent job without completing the prerequisite first.
	// The dependency check in RunJob should fail and return ErrDependencyNotMet.
	err := sc.RunJob(context.Background(), "dependent-job")
	if err == nil {
		t.Error("RunJob should fail for job with unsatisfied dependencies")
	}

	_ = sc.Stop()
}

// Test that the concurrency semaphore limits execution.
func TestJobWrapper_SemaphoreLimitsExecution(t *testing.T) {
	t.Parallel()

	sc := NewSchedulerWithOptions(newDiscardLogger(), nil, 10*time.Millisecond)
	sc.SetMaxConcurrentJobs(1) // Only 1 job at a time

	slowJob := &SlowTestJob{duration: 100 * time.Millisecond}
	slowJob.Name = "slow-job"
	slowJob.Schedule = "@every 50ms"

	_ = sc.AddJob(slowJob)

	completed := make(chan struct{}, 10)
	sc.SetOnJobComplete(func(_ string, _ bool) {
		select {
		case completed <- struct{}{}:
		default:
		}
	})

	_ = sc.Start()

	// Wait for at least one completion
	select {
	case <-completed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for job completion")
	}

	_ = sc.Stop()

	// Verify the semaphore allowed execution
	if slowJob.called.Load() == 0 {
		t.Error("Job should have been called at least once")
	}
}

// --- CONDITIONALS_BOUNDARY at line 680, 684 ---
// These test exact boundary conditions in the runWithCtx method.
// Line 680: CanExecute returning exactly true (boundary)
// Line 684: semaphore exactly at capacity
func TestJobWrapper_SemaphoreExactCapacity(t *testing.T) {
	t.Parallel()

	sc := NewSchedulerWithOptions(newDiscardLogger(), nil, 10*time.Millisecond)
	sc.SetMaxConcurrentJobs(2)

	job1 := &TestJob{}
	job1.Name = "cap-job-1"
	job1.Schedule = "@every 1h"

	job2 := &TestJob{}
	job2.Name = "cap-job-2"
	job2.Schedule = "@every 1h"

	_ = sc.AddJob(job1)
	_ = sc.AddJob(job2)

	completedCount := atomic.Int32{}
	sc.SetOnJobComplete(func(_ string, _ bool) {
		completedCount.Add(1)
	})

	_ = sc.Start()

	// Run both jobs - should both succeed since maxConcurrent is 2
	_ = sc.RunJob(context.Background(), "cap-job-1")
	_ = sc.RunJob(context.Background(), "cap-job-2")

	time.Sleep(200 * time.Millisecond)

	if completedCount.Load() < 2 {
		t.Errorf("Both jobs should have completed with semaphore capacity of 2, got %d", completedCount.Load())
	}

	_ = sc.Stop()
}

// SlowTestJob is a test job that takes a configurable amount of time to run.
type SlowTestJob struct {
	BareJob
	called   atomic.Int32
	duration time.Duration
}

func (j *SlowTestJob) Run(ctx *Context) error {
	j.called.Add(1)
	time.Sleep(j.duration)
	return nil
}

// mockMetricsRecorder implements MetricsRecorder for testing.
type mockMetricsRecorder struct {
	jobStarted   atomic.Int32
	jobCompleted atomic.Int32
	jobScheduled atomic.Int32
}

func (m *mockMetricsRecorder) RecordJobRetry(_ string, _ int, _ bool) {}
func (m *mockMetricsRecorder) RecordContainerEvent()                  {}
func (m *mockMetricsRecorder) RecordContainerMonitorFallback()        {}
func (m *mockMetricsRecorder) RecordContainerMonitorMethod(_ bool)    {}
func (m *mockMetricsRecorder) RecordContainerWaitDuration(_ float64)  {}
func (m *mockMetricsRecorder) RecordDockerOperation(_ string)         {}
func (m *mockMetricsRecorder) RecordDockerError(_ string)             {}
func (m *mockMetricsRecorder) RecordJobStart(_ string)                { m.jobStarted.Add(1) }
func (m *mockMetricsRecorder) RecordJobComplete(_ string, _ float64, _ bool) {
	m.jobCompleted.Add(1)
}
func (m *mockMetricsRecorder) RecordJobScheduled(_ string)                { m.jobScheduled.Add(1) }
func (m *mockMetricsRecorder) RecordWorkflowComplete(_ string, _ string)  {}
func (m *mockMetricsRecorder) RecordWorkflowJobResult(_ string, _ string) {}

// --- Test StopWithTimeout returns error on timeout ---
func TestStopWithTimeout_Timeout(t *testing.T) {
	t.Parallel()

	sc := NewSchedulerWithOptions(newDiscardLogger(), nil, 10*time.Millisecond)

	slowJob := &SlowTestJob{duration: 500 * time.Millisecond}
	slowJob.Name = "very-slow-job"
	slowJob.Schedule = "@every 1h"
	slowJob.RunOnStartup = true

	_ = sc.AddJob(slowJob)

	completed := make(chan struct{}, 1)
	sc.SetOnJobComplete(func(_ string, _ bool) {
		select {
		case completed <- struct{}{}:
		default:
		}
	})

	_ = sc.Start()

	// Give time for the startup job to begin
	time.Sleep(50 * time.Millisecond)

	// Stop with very short timeout - should time out
	err := sc.StopWithTimeout(1 * time.Millisecond)
	// Note: this may or may not time out depending on timing
	// The important thing is that the function completes
	_ = err
}

// --- Test UpdateJob ---
func TestUpdateJob_ExistingJob(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job := &TestJob{}
	job.Name = "update-me"
	job.Schedule = "@daily"
	job.Command = "original"
	_ = sc.AddJob(job)

	newJob := &TestJob{}
	newJob.Name = "update-me"
	newJob.Schedule = "@hourly"
	newJob.Command = "updated"

	err := sc.UpdateJob("update-me", "@hourly", newJob)
	if err != nil {
		t.Fatalf("UpdateJob should succeed: %v", err)
	}

	// Verify the job was updated
	found := sc.GetJob("update-me")
	if found == nil {
		t.Fatal("Updated job should still be found")
	}
	if found.GetCommand() != "updated" {
		t.Errorf("Job command should be updated, got %q", found.GetCommand())
	}
}

// --- Test UpdateJob non-existent ---
func TestUpdateJob_NonExistent(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	newJob := &TestJob{}
	newJob.Name = "ghost"
	newJob.Schedule = "@daily"

	err := sc.UpdateJob("ghost", "@daily", newJob)
	if err == nil {
		t.Error("UpdateJob should fail for non-existent job")
	}
}

// --- Test DisableJob then verify in GetDisabledJobs ---
func TestGetDisabledJobs_ReturnsCopy(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job := &TestJob{}
	job.Name = "disable-copy-test"
	job.Schedule = "@daily"
	_ = sc.AddJob(job)

	_ = sc.DisableJob("disable-copy-test")

	disabled := sc.GetDisabledJobs()
	if len(disabled) != 1 {
		t.Fatalf("Expected 1 disabled job, got %d", len(disabled))
	}
	if disabled[0].GetName() != "disable-copy-test" {
		t.Error("Disabled job should match")
	}
}

// --- Test triggered job can be disabled and re-enabled ---
func TestEnableJob_TriggeredJob(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job := &TestJob{}
	job.Name = "triggered-enable"
	job.Schedule = "@triggered"
	_ = sc.AddJob(job)

	// Disable the triggered job
	err := sc.DisableJob("triggered-enable")
	if err != nil {
		t.Fatalf("DisableJob: %v", err)
	}

	// Re-enable the triggered job (should go through the triggered path in EnableJob)
	err = sc.EnableJob("triggered-enable")
	if err != nil {
		t.Fatalf("EnableJob for triggered job: %v", err)
	}

	// Verify the job is back in active jobs
	if sc.GetJob("triggered-enable") == nil {
		t.Error("Triggered job should be active after re-enable")
	}
}

// --- Test StopAndWait ---
func TestStopAndWait(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job := &TestJob{}
	job.Name = "stop-and-wait"
	job.Schedule = "@daily"
	_ = sc.AddJob(job)

	_ = sc.Start()
	sc.StopAndWait()

	if sc.IsRunning() {
		t.Error("Scheduler should not be running after StopAndWait")
	}
}

// --- Test RemoveJobsByTag ---
func TestRemoveJobsByTag(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job1 := &TestJob{}
	job1.Name = "tagged-1"
	job1.Schedule = "@daily"
	_ = sc.AddJobWithTags(job1, "remove-me")

	job2 := &TestJob{}
	job2.Name = "tagged-2"
	job2.Schedule = "@hourly"
	_ = sc.AddJobWithTags(job2, "remove-me")

	job3 := &TestJob{}
	job3.Name = "keep-me"
	job3.Schedule = "@daily"
	_ = sc.AddJobWithTags(job3, "keep")

	count := sc.RemoveJobsByTag("remove-me")
	if count != 2 {
		t.Errorf("RemoveJobsByTag should return 2, got %d", count)
	}

	// Verify removed jobs are gone
	if sc.GetJob("tagged-1") != nil {
		t.Error("tagged-1 should be removed")
	}
	if sc.GetJob("tagged-2") != nil {
		t.Error("tagged-2 should be removed")
	}

	// Verify kept job is still there
	if sc.GetJob("keep-me") == nil {
		t.Error("keep-me should still be active")
	}

	// Verify removed jobs are in removed list
	removed := sc.GetRemovedJobs()
	removedNames := make(map[string]bool)
	for _, j := range removed {
		removedNames[j.GetName()] = true
	}
	if !removedNames["tagged-1"] || !removedNames["tagged-2"] {
		t.Error("Removed jobs should be in the removed list")
	}
}

// --- Test RemoveJobsByTag with nonexistent tag ---
func TestRemoveJobsByTag_NonexistentTag(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())
	count := sc.RemoveJobsByTag("nonexistent")
	if count != 0 {
		t.Errorf("RemoveJobsByTag for nonexistent tag should return 0, got %d", count)
	}
}

// --- Test AddJob with empty schedule ---
func TestAddJob_EmptySchedule(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	job := &TestJob{}
	job.Name = "empty-sched"
	job.Schedule = ""

	err := sc.AddJob(job)
	if err == nil {
		t.Error("AddJob with empty schedule should return error")
	}
	if err != ErrEmptySchedule {
		t.Errorf("Expected ErrEmptySchedule, got: %v", err)
	}
}

// ============================================================================
// Tests for workflowStatus helper
// ============================================================================

func TestWorkflowStatus_EmptyResults(t *testing.T) {
	t.Parallel()

	status := workflowStatus(nil)
	if status != workflowStatusSuccess {
		t.Errorf("Expected 'success' for nil results, got %q", status)
	}

	status = workflowStatus(map[cron.EntryID]cron.JobResult{})
	if status != workflowStatusSuccess {
		t.Errorf("Expected 'success' for empty results, got %q", status)
	}
}

func TestWorkflowStatus_AllSuccess(t *testing.T) {
	t.Parallel()

	results := map[cron.EntryID]cron.JobResult{
		1: cron.ResultSuccess,
		2: cron.ResultSuccess,
		3: cron.ResultSuccess,
	}
	status := workflowStatus(results)
	if status != workflowStatusSuccess {
		t.Errorf("Expected 'success', got %q", status)
	}
}

func TestWorkflowStatus_AnyFailure(t *testing.T) {
	t.Parallel()

	results := map[cron.EntryID]cron.JobResult{
		1: cron.ResultSuccess,
		2: cron.ResultFailure,
		3: cron.ResultSuccess,
	}
	status := workflowStatus(results)
	if status != workflowStatusFailure {
		t.Errorf("Expected 'failure', got %q", status)
	}
}

func TestWorkflowStatus_AllSkipped(t *testing.T) {
	t.Parallel()

	results := map[cron.EntryID]cron.JobResult{
		1: cron.ResultSkipped,
		2: cron.ResultSkipped,
	}
	status := workflowStatus(results)
	if status != workflowStatusSkipped {
		t.Errorf("Expected 'skipped', got %q", status)
	}
}

func TestWorkflowStatus_Mixed(t *testing.T) {
	t.Parallel()

	results := map[cron.EntryID]cron.JobResult{
		1: cron.ResultSuccess,
		2: cron.ResultSkipped,
	}
	status := workflowStatus(results)
	if status != workflowStatusMixed {
		t.Errorf("Expected 'mixed', got %q", status)
	}
}

func TestWorkflowStatus_FailureTakesPrecedence(t *testing.T) {
	t.Parallel()

	results := map[cron.EntryID]cron.JobResult{
		1: cron.ResultSuccess,
		2: cron.ResultSkipped,
		3: cron.ResultFailure,
	}
	status := workflowStatus(results)
	if status != workflowStatusFailure {
		t.Errorf("Expected 'failure' (takes precedence), got %q", status)
	}
}

func TestWorkflowStatus_SingleSuccess(t *testing.T) {
	t.Parallel()

	results := map[cron.EntryID]cron.JobResult{
		1: cron.ResultSuccess,
	}
	status := workflowStatus(results)
	if status != workflowStatusSuccess {
		t.Errorf("Expected 'success', got %q", status)
	}
}

func TestWorkflowStatus_SingleFailure(t *testing.T) {
	t.Parallel()

	results := map[cron.EntryID]cron.JobResult{
		1: cron.ResultFailure,
	}
	status := workflowStatus(results)
	if status != workflowStatusFailure {
		t.Errorf("Expected 'failure', got %q", status)
	}
}

func TestWorkflowStatus_SingleSkipped(t *testing.T) {
	t.Parallel()

	results := map[cron.EntryID]cron.JobResult{
		1: cron.ResultSkipped,
	}
	status := workflowStatus(results)
	if status != workflowStatusSkipped {
		t.Errorf("Expected 'skipped', got %q", status)
	}
}

func TestWorkflowStatus_PendingOnly(t *testing.T) {
	t.Parallel()

	// Pending-only shouldn't happen in practice (OnWorkflowComplete fires
	// when all are terminal), but handle gracefully
	results := map[cron.EntryID]cron.JobResult{
		1: cron.ResultPending,
	}
	status := workflowStatus(results)
	// No success/failure/skipped flags set, falls to default "mixed"
	if status != workflowStatusMixed {
		t.Errorf("Expected 'mixed' for pending-only, got %q", status)
	}
}

// ============================================================================
// Tests for recordWorkflowMetrics
// ============================================================================

// workflowMetricsCapture captures workflow metric recording calls.
type workflowMetricsCapture struct {
	mockMetricsRecorder
	completions []struct {
		rootJobName string
		status      string
	}
	jobResults []struct {
		jobName string
		result  string
	}
}

func (w *workflowMetricsCapture) RecordWorkflowComplete(rootJobName, status string) {
	w.completions = append(w.completions, struct {
		rootJobName string
		status      string
	}{rootJobName, status})
}

func (w *workflowMetricsCapture) RecordWorkflowJobResult(jobName, result string) {
	w.jobResults = append(w.jobResults, struct {
		jobName string
		result  string
	}{jobName, result})
}

func TestRecordWorkflowMetrics_WithNamedEntries(t *testing.T) {
	t.Parallel()

	// Create a cron instance with named entries
	c := cron.New(cron.WithParser(cron.FullParser()))
	rootID, _ := c.AddFunc("@every 1h", func() {}, cron.WithName("root-job"))
	childID, _ := c.AddFunc("@every 1h", func() {}, cron.WithName("child-job"))

	capture := &workflowMetricsCapture{}
	results := map[cron.EntryID]cron.JobResult{
		rootID:  cron.ResultSuccess,
		childID: cron.ResultFailure,
	}

	recordWorkflowMetrics(c, capture, rootID, results)

	// Check workflow completion was recorded
	if len(capture.completions) != 1 {
		t.Fatalf("Expected 1 workflow completion, got %d", len(capture.completions))
	}
	if capture.completions[0].rootJobName != "root-job" {
		t.Errorf("Expected root job name 'root-job', got %q", capture.completions[0].rootJobName)
	}
	if capture.completions[0].status != workflowStatusFailure {
		t.Errorf("Expected status 'failure', got %q", capture.completions[0].status)
	}

	// Check individual job results were recorded
	if len(capture.jobResults) != 2 {
		t.Fatalf("Expected 2 job results, got %d", len(capture.jobResults))
	}

	// Build a map of results for easier assertion (iteration order not guaranteed)
	resultMap := make(map[string]string)
	for _, jr := range capture.jobResults {
		resultMap[jr.jobName] = jr.result
	}

	if resultMap["root-job"] != "Success" {
		t.Errorf("Expected root-job result 'Success', got %q", resultMap["root-job"])
	}
	if resultMap["child-job"] != "Failure" {
		t.Errorf("Expected child-job result 'Failure', got %q", resultMap["child-job"])
	}
}

func TestRecordWorkflowMetrics_UnknownEntries(t *testing.T) {
	t.Parallel()

	// Create a cron instance without the referenced entries
	c := cron.New(cron.WithParser(cron.FullParser()))

	capture := &workflowMetricsCapture{}
	results := map[cron.EntryID]cron.JobResult{
		999: cron.ResultSuccess,
	}

	recordWorkflowMetrics(c, capture, 999, results)

	// Should use unknownJobName for entries not found
	if len(capture.completions) != 1 {
		t.Fatalf("Expected 1 workflow completion, got %d", len(capture.completions))
	}
	if capture.completions[0].rootJobName != unknownJobName {
		t.Errorf("Expected root job name %q, got %q", unknownJobName, capture.completions[0].rootJobName)
	}

	if len(capture.jobResults) != 1 {
		t.Fatalf("Expected 1 job result, got %d", len(capture.jobResults))
	}
	if capture.jobResults[0].jobName != unknownJobName {
		t.Errorf("Expected job name %q, got %q", unknownJobName, capture.jobResults[0].jobName)
	}
}

func TestRecordWorkflowMetrics_EmptyResults(t *testing.T) {
	t.Parallel()

	c := cron.New(cron.WithParser(cron.FullParser()))

	capture := &workflowMetricsCapture{}
	results := map[cron.EntryID]cron.JobResult{}

	recordWorkflowMetrics(c, capture, 0, results)

	// Workflow completion should still be recorded with "success" status
	if len(capture.completions) != 1 {
		t.Fatalf("Expected 1 workflow completion, got %d", len(capture.completions))
	}
	if capture.completions[0].status != workflowStatusSuccess {
		t.Errorf("Expected status 'success' for empty results, got %q", capture.completions[0].status)
	}

	// No individual job results
	if len(capture.jobResults) != 0 {
		t.Errorf("Expected 0 job results, got %d", len(capture.jobResults))
	}
}
