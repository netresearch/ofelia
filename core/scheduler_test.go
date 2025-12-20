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

	sc := NewScheduler(&TestLogger{})
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

	sc := NewSchedulerWithOptions(&TestLogger{}, nil, 10*time.Millisecond)
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

	sc := NewScheduler(&TestLogger{})
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

	sc := NewSchedulerWithOptions(&TestLogger{}, nil, 10*time.Millisecond)
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

	sc := NewScheduler(&TestLogger{})

	sc.workflowOrchestrator = NewWorkflowOrchestrator(sc, &TestLogger{})
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
	sc := NewScheduler(&TestLogger{})
	sc.SetClock(fakeClock)

	assert.Equal(t, fakeClock, sc.clock)
	assert.NotNil(t, sc.cleanupStop)
}

func TestSchedulerSetClock(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(&TestLogger{})
	fakeClock := NewFakeClock(time.Now())

	sc.SetClock(fakeClock)
	assert.Equal(t, fakeClock, sc.clock)
}

func TestSchedulerSetOnJobComplete(t *testing.T) {
	t.Parallel()

	sc := NewScheduler(&TestLogger{})
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
	sc := NewSchedulerWithClock(&TestLogger{}, cronClock)

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
