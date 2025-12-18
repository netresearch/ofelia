package core

import (
	"time"

	. "gopkg.in/check.v1"
)

type SuiteScheduler struct{}

var _ = Suite(&SuiteScheduler{})

func (s *SuiteScheduler) TestAddJob(c *C) {
	job := &TestJob{}
	job.Schedule = "@hourly"

	sc := NewScheduler(&TestLogger{})
	err := sc.AddJob(job)
	c.Assert(err, IsNil)

	e := sc.cron.Entries()
	c.Assert(e, HasLen, 1)
	c.Assert(e[0].Job.(*jobWrapper).j, DeepEquals, job)
}

func (s *SuiteScheduler) TestStartStop(c *C) {
	job := &TestJob{}
	job.Schedule = "@every 1s"

	sc := NewScheduler(&TestLogger{})
	err := sc.AddJob(job)
	c.Assert(err, IsNil)

	jobCompleted := make(chan struct{}, 1)
	sc.SetOnJobComplete(func(_ string, _ bool) {
		select {
		case jobCompleted <- struct{}{}:
		default:
		}
	})

	_ = sc.Start()
	c.Assert(sc.IsRunning(), Equals, true)

	select {
	case <-jobCompleted:
	case <-time.After(2 * time.Second):
		c.Fatal("Timeout waiting for job to complete")
	}

	_ = sc.Stop()
	c.Assert(sc.IsRunning(), Equals, false)
}

func (s *SuiteScheduler) TestMergeMiddlewaresSame(c *C) {
	mA, mB, mC := &TestMiddleware{}, &TestMiddleware{}, &TestMiddleware{}

	job := &TestJob{}
	job.Schedule = "@every 1s"
	job.Use(mB, mC)

	sc := NewScheduler(&TestLogger{})
	sc.Use(mA)
	_ = sc.AddJob(job)

	m := job.Middlewares()
	c.Assert(m, HasLen, 1)
	c.Assert(m[0], Equals, mB)
}

func (s *SuiteScheduler) TestLastRunRecorded(c *C) {
	job := &TestJob{}
	job.Schedule = "@every 1s"

	sc := NewScheduler(&TestLogger{})
	err := sc.AddJob(job)
	c.Assert(err, IsNil)

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
	case <-time.After(2 * time.Second):
		c.Fatal("Timeout waiting for job to complete")
	}

	_ = sc.Stop()

	lr := job.GetLastRun()
	c.Assert(lr, NotNil)
	c.Assert(lr.Duration > 0, Equals, true)
}

func (s *SuiteScheduler) TestWorkflowOrchestratorInit(c *C) {
	sc := NewScheduler(&TestLogger{})

	sc.workflowOrchestrator = NewWorkflowOrchestrator(sc, &TestLogger{})
	c.Assert(sc.workflowOrchestrator, NotNil)
	c.Assert(sc.workflowOrchestrator.executions, NotNil)

	exec := &WorkflowExecution{
		ID:            "test-exec",
		StartTime:     time.Now(),
		CompletedJobs: make(map[string]bool),
		FailedJobs:    make(map[string]bool),
		RunningJobs:   make(map[string]bool),
	}

	sc.workflowOrchestrator.executions["test-exec"] = exec
	c.Assert(sc.workflowOrchestrator.executions["test-exec"], Equals, exec)
}

func (s *SuiteScheduler) TestSchedulerCleanupTicker(c *C) {
	fakeClock := NewFakeClock(time.Now())
	sc := NewScheduler(&TestLogger{})
	sc.SetClock(fakeClock)

	c.Assert(sc.clock, Equals, fakeClock)
	c.Assert(sc.cleanupStop, NotNil)
}

func (s *SuiteScheduler) TestSetClock(c *C) {
	sc := NewScheduler(&TestLogger{})
	fakeClock := NewFakeClock(time.Now())

	sc.SetClock(fakeClock)
	c.Assert(sc.clock, Equals, fakeClock)
}

func (s *SuiteScheduler) TestSetOnJobComplete(c *C) {
	sc := NewScheduler(&TestLogger{})
	called := false

	sc.SetOnJobComplete(func(_ string, _ bool) {
		called = true
	})

	c.Assert(sc.onJobComplete, NotNil)
	sc.onJobComplete("test", true)
	c.Assert(called, Equals, true)
}
