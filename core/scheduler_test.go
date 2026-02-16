package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulerAddJob(t *testing.T) {
	t.Parallel()

	job := &TestJob{}
	job.Schedule = "@hourly"

	sc := NewScheduler(newDiscardLogger())
	err := sc.AddJob(job)
	require.NoError(t, err)

	e := sc.cron.Entries()
	assert.Len(t, e, 1)
	assert.Equal(t, job, e[0].Job.(*jobWrapper).j)
}

func TestSchedulerStartStop(t *testing.T) {
	t.Parallel()

	job := &TestJob{}
	job.Schedule = "@every 50ms"

	sc := NewSchedulerWithOptions(newDiscardLogger(), nil, 10*time.Millisecond)
	err := sc.AddJob(job)
	require.NoError(t, err)

	jobCompleted := make(chan struct{}, 1)
	sc.SetOnJobComplete(func(_ string, _ bool) {
		select {
		case jobCompleted <- struct{}{}:
		default:
		}
	})

	_ = sc.Start()
	assert.True(t, sc.IsRunning())

	select {
	case <-jobCompleted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for job to complete")
	}

	_ = sc.Stop()
	assert.False(t, sc.IsRunning())
}

func TestSchedulerMergeMiddlewaresSame(t *testing.T) {
	t.Parallel()

	mA, mB, mC := &TestMiddleware{}, &TestMiddleware{}, &TestMiddleware{}

	job := &TestJob{}
	job.Schedule = "@every 1s"
	job.Use(mB, mC)

	sc := NewScheduler(newDiscardLogger())
	sc.Use(mA)
	_ = sc.AddJob(job)

	m := job.Middlewares()
	assert.Len(t, m, 1)
	assert.Equal(t, mB, m[0])
}

func TestSchedulerLastRunRecorded(t *testing.T) {
	t.Parallel()

	job := &TestJob{}
	job.Schedule = "@every 50ms"

	sc := NewSchedulerWithOptions(newDiscardLogger(), nil, 10*time.Millisecond)
	err := sc.AddJob(job)
	require.NoError(t, err)

	jobCompleted := make(chan struct{}, 1)
	sc.SetOnJobComplete(func(_ string, _ bool) {
		select {
		case jobCompleted <- struct{}{}:
		default:
		}
	})

	_ = sc.Start()

	select {
	case <-jobCompleted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for job to complete")
	}

	_ = sc.Stop()

	lr := job.GetLastRun()
	assert.NotNil(t, lr)
	assert.Greater(t, lr.Duration, time.Duration(0))
}

func TestSchedulerWorkflowOrchestratorInit(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())

	sc.workflowOrchestrator = NewWorkflowOrchestrator(sc, newDiscardLogger())
	assert.NotNil(t, sc.workflowOrchestrator)
	assert.NotNil(t, sc.workflowOrchestrator.executions)

	exec := &WorkflowExecution{
		ID:            "test-exec",
		StartTime:     time.Now(),
		CompletedJobs: make(map[string]bool),
		FailedJobs:    make(map[string]bool),
		RunningJobs:   make(map[string]bool),
	}

	sc.workflowOrchestrator.executions["test-exec"] = exec
	assert.Equal(t, exec, sc.workflowOrchestrator.executions["test-exec"])
}

func TestSchedulerCleanupTicker(t *testing.T) {
	t.Parallel()

	fakeClock := NewFakeClock(time.Now())
	sc := NewScheduler(newDiscardLogger())
	sc.SetClock(fakeClock)

	assert.Equal(t, fakeClock, sc.clock)
	assert.NotNil(t, sc.cleanupStop)
}

func TestSchedulerSetClock(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())
	fakeClock := NewFakeClock(time.Now())

	sc.SetClock(fakeClock)
	assert.Equal(t, fakeClock, sc.clock)
}

func TestSchedulerSetOnJobComplete(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(newDiscardLogger())
	called := false

	sc.SetOnJobComplete(func(_ string, _ bool) {
		called = true
	})

	assert.NotNil(t, sc.onJobComplete)
	sc.onJobComplete("test", true)
	assert.True(t, called)
}

func TestSchedulerWithCronClock(t *testing.T) {
	t.Parallel()

	cronClock := NewCronClock(time.Now())
	sc := NewSchedulerWithClock(newDiscardLogger(), cronClock)

	job := &TestJob{}
	job.Schedule = "@every 1h"

	err := sc.AddJob(job)
	require.NoError(t, err)

	jobCompleted := make(chan struct{}, 1)
	sc.SetOnJobComplete(func(_ string, _ bool) {
		select {
		case jobCompleted <- struct{}{}:
		default:
		}
	})

	_ = sc.Start()
	assert.True(t, sc.IsRunning())

	time.Sleep(10 * time.Millisecond)

	cronClock.Advance(1 * time.Hour)

	select {
	case <-jobCompleted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Job should have fired after advancing clock by 1 hour")
	}

	_ = sc.Stop()
	assert.False(t, sc.IsRunning())
}

func TestSchedulerRunOnStartup(t *testing.T) {
	t.Parallel()

	job := &TestJob{}
	job.Schedule = "@every 1h" // Long interval so it won't fire during test
	job.RunOnStartup = true

	sc := NewSchedulerWithOptions(newDiscardLogger(), nil, 10*time.Millisecond)
	err := sc.AddJob(job)
	require.NoError(t, err)

	jobCompleted := make(chan struct{}, 1)
	sc.SetOnJobComplete(func(_ string, _ bool) {
		select {
		case jobCompleted <- struct{}{}:
		default:
		}
	})

	_ = sc.Start()

	// Job should run immediately on startup
	select {
	case <-jobCompleted:
		// Success - job ran on startup
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Startup job should have run immediately")
	}

	assert.Equal(t, 1, job.Called())

	_ = sc.Stop()
}

func TestSchedulerRunOnStartupDisabled(t *testing.T) {
	t.Parallel()

	job := &TestJob{}
	job.Schedule = "@every 1h" // Long interval so it won't fire during test
	job.RunOnStartup = false   // Explicitly disabled

	sc := NewSchedulerWithOptions(newDiscardLogger(), nil, 10*time.Millisecond)
	err := sc.AddJob(job)
	require.NoError(t, err)

	_ = sc.Start()

	// Wait a bit to ensure job doesn't run
	time.Sleep(150 * time.Millisecond)

	// Job should NOT have run since RunOnStartup is false
	assert.Equal(t, 0, job.Called())

	_ = sc.Stop()
}

func TestSchedulerRunOnStartupMultipleJobs(t *testing.T) {
	t.Parallel()

	job1 := &TestJob{}
	job1.Name = "startup-job-1"
	job1.Schedule = "@every 1h"
	job1.RunOnStartup = true

	job2 := &TestJob{}
	job2.Name = "startup-job-2"
	job2.Schedule = "@every 1h"
	job2.RunOnStartup = true

	job3 := &TestJob{}
	job3.Name = "no-startup-job"
	job3.Schedule = "@every 1h"
	job3.RunOnStartup = false

	sc := NewSchedulerWithOptions(newDiscardLogger(), nil, 10*time.Millisecond)
	require.NoError(t, sc.AddJob(job1))
	require.NoError(t, sc.AddJob(job2))
	require.NoError(t, sc.AddJob(job3))

	jobsCompleted := make(chan string, 3)
	sc.SetOnJobComplete(func(jobName string, _ bool) {
		select {
		case jobsCompleted <- jobName:
		default:
		}
	})

	_ = sc.Start()

	// Wait for both startup jobs to complete
	completedCount := 0
	timeout := time.After(500 * time.Millisecond)
	for completedCount < 2 {
		select {
		case <-jobsCompleted:
			completedCount++
		case <-timeout:
			t.Fatalf("Expected 2 startup jobs to complete, got %d", completedCount)
		}
	}

	// Give a bit more time to ensure job3 didn't run
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, 1, job1.Called(), "job1 should have run once on startup")
	assert.Equal(t, 1, job2.Called(), "job2 should have run once on startup")
	assert.Equal(t, 0, job3.Called(), "job3 should not have run (no startup)")

	_ = sc.Stop()
}

func TestSchedulerRunOnStartupNonBlocking(t *testing.T) {
	t.Parallel()

	// Create a job that takes a while to run
	job := &TestJob{}
	job.Schedule = "@every 1h"
	job.RunOnStartup = true

	sc := NewSchedulerWithOptions(newDiscardLogger(), nil, 10*time.Millisecond)
	err := sc.AddJob(job)
	require.NoError(t, err)

	// Start() should return quickly even though startup job takes 50ms
	startTime := time.Now()
	_ = sc.Start()
	elapsed := time.Since(startTime)

	// Start() should complete quickly (non-blocking)
	// The startup job runs in a goroutine, so Start() returns immediately
	assert.Less(t, elapsed, 30*time.Millisecond, "Start() should be non-blocking")

	// Wait for job to complete
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 1, job.Called())

	_ = sc.Stop()
}
