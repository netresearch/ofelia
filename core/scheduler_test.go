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
	const every1s = "@every 1s"
	job.Schedule = every1s

	sc := NewScheduler(&TestLogger{})
	err := sc.AddJob(job)
	c.Assert(err, IsNil)

	_ = sc.Start()
	c.Assert(sc.IsRunning(), Equals, true)

	time.Sleep(time.Second * 2)

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

	_ = sc.Start()
	time.Sleep(time.Second * 2)
	_ = sc.Stop()

	lr := job.GetLastRun()
	c.Assert(lr, NotNil)
	c.Assert(lr.Duration > 0, Equals, true)
}

func (s *SuiteScheduler) TestWorkflowOrchestratorInit(c *C) {
	sc := NewScheduler(&TestLogger{})

	// Initialize workflow orchestrator
	sc.workflowOrchestrator = NewWorkflowOrchestrator(sc, &TestLogger{})
	c.Assert(sc.workflowOrchestrator, NotNil)

	// Test that executions map is initialized
	c.Assert(sc.workflowOrchestrator.executions, NotNil)

	// Test creating a workflow execution
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
	sc := NewScheduler(&TestLogger{})

	// Test that cleanup ticker can be initialized
	sc.cleanupTicker = time.NewTicker(1 * time.Hour)
	c.Assert(sc.cleanupTicker, NotNil)

	// Test that cleanup stop channel can be initialized
	sc.cleanupStop = make(chan struct{})
	c.Assert(sc.cleanupStop, NotNil)

	// Clean up
	sc.cleanupTicker.Stop()
	close(sc.cleanupStop)
}
