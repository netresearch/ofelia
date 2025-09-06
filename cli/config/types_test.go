package config

import (
	"testing"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner
func TestConfig(t *testing.T) { TestingT(t) }

type TypesSuite struct{}

var _ = Suite(&TypesSuite{})

func (s *TypesSuite) TestNewUnifiedJobConfig(c *C) {
	testCases := []struct {
		jobType      JobType
		expectedType string
	}{
		{JobTypeExec, "exec"},
		{JobTypeRun, "run"},
		{JobTypeService, "service-run"},
		{JobTypeLocal, "local"},
		{JobTypeCompose, "compose"},
	}

	for _, tc := range testCases {
		job := NewUnifiedJobConfig(tc.jobType)
		c.Assert(job, NotNil)
		c.Assert(string(job.Type), Equals, expectedType)
		c.Assert(job.GetCoreJob(), NotNil)
	}
}

func (s *TypesSuite) TestUnifiedJobConfigGetCoreJob(c *C) {
	// Test exec job
	execJob := NewUnifiedJobConfig(JobTypeExec)
	coreJob := execJob.GetCoreJob()
	c.Assert(coreJob, NotNil)
	_, ok := coreJob.(*core.ExecJob)
	c.Assert(ok, Equals, true)

	// Test run job
	runJob := NewUnifiedJobConfig(JobTypeRun)
	coreJob = runJob.GetCoreJob()
	c.Assert(coreJob, NotNil)
	_, ok = coreJob.(*core.RunJob)
	c.Assert(ok, Equals, true)

	// Test service job
	serviceJob := NewUnifiedJobConfig(JobTypeService)
	coreJob = serviceJob.GetCoreJob()
	c.Assert(coreJob, NotNil)
	_, ok = coreJob.(*core.RunServiceJob)
	c.Assert(ok, Equals, true)

	// Test local job
	localJob := NewUnifiedJobConfig(JobTypeLocal)
	coreJob = localJob.GetCoreJob()
	c.Assert(coreJob, NotNil)
	_, ok = coreJob.(*core.LocalJob)
	c.Assert(ok, Equals, true)

	// Test compose job
	composeJob := NewUnifiedJobConfig(JobTypeCompose)
	coreJob = composeJob.GetCoreJob()
	c.Assert(coreJob, NotNil)
	_, ok = coreJob.(*core.ComposeJob)
	c.Assert(ok, Equals, true)
}

func (s *TypesSuite) TestUnifiedJobConfigJobSource(c *C) {
	job := NewUnifiedJobConfig(JobTypeExec)
	
	// Test initial source
	c.Assert(job.GetJobSource(), Equals, JobSource(""))
	
	// Test setting source
	job.SetJobSource(JobSourceINI)
	c.Assert(job.GetJobSource(), Equals, JobSourceINI)
	
	job.SetJobSource(JobSourceLabel)
	c.Assert(job.GetJobSource(), Equals, JobSourceLabel)
}

func (s *TypesSuite) TestUnifiedJobConfigBuildMiddlewares(c *C) {
	job := NewUnifiedJobConfig(JobTypeExec)
	
	// Set up middleware configuration
	job.MiddlewareConfig.OverlapConfig.NoOverlap = true
	job.MiddlewareConfig.SlackConfig.SlackWebhook = "http://example.com/webhook"
	
	// Build middlewares
	job.buildMiddlewares()
	
	// Verify middlewares were applied
	middlewares := job.Middlewares()
	c.Assert(len(middlewares), Equals, 4) // overlap, slack, save, mail
}

func (s *TypesSuite) TestUnifiedJobConfigGetters(c *C) {
	job := NewUnifiedJobConfig(JobTypeExec)
	
	// Set values on the core job
	job.ExecJob.Name = "test-job"
	job.ExecJob.Schedule = "@every 5s"
	job.ExecJob.Command = "echo test"
	
	// Test getters
	c.Assert(job.GetName(), Equals, "test-job")
	c.Assert(job.GetSchedule(), Equals, "@every 5s")
	c.Assert(job.GetCommand(), Equals, "echo test")
}

func (s *TypesSuite) TestUnifiedJobConfigHash(c *C) {
	job1 := NewUnifiedJobConfig(JobTypeExec)
	job1.ExecJob.Name = "test"
	job1.ExecJob.Schedule = "@every 5s"
	
	job2 := NewUnifiedJobConfig(JobTypeExec)
	job2.ExecJob.Name = "test"
	job2.ExecJob.Schedule = "@every 5s"
	
	job3 := NewUnifiedJobConfig(JobTypeExec)
	job3.ExecJob.Name = "test"
	job3.ExecJob.Schedule = "@every 10s" // Different schedule
	
	hash1, err1 := job1.Hash()
	hash2, err2 := job2.Hash()
	hash3, err3 := job3.Hash()
	
	c.Assert(err1, IsNil)
	c.Assert(err2, IsNil)
	c.Assert(err3, IsNil)
	
	// Same configuration should produce same hash
	c.Assert(hash1, Equals, hash2)
	
	// Different configuration should produce different hash
	c.Assert(hash1, Not(Equals), hash3)
}

func (s *TypesSuite) TestMiddlewareConfig(c *C) {
	config := &MiddlewareConfig{
		OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
		SlackConfig:   middlewares.SlackConfig{SlackWebhook: "http://example.com"},
		SaveConfig:    middlewares.SaveConfig{SaveFolder: "/tmp"},
		MailConfig:    middlewares.MailConfig{EmailTo: "test@example.com"},
	}
	
	c.Assert(config.OverlapConfig.NoOverlap, Equals, true)
	c.Assert(config.SlackConfig.SlackWebhook, Equals, "http://example.com")
	c.Assert(config.SaveConfig.SaveFolder, Equals, "/tmp")
	c.Assert(config.MailConfig.EmailTo, Equals, "test@example.com")
}

func (s *TypesSuite) TestJobTypeConstants(c *C) {
	c.Assert(string(JobTypeExec), Equals, "exec")
	c.Assert(string(JobTypeRun), Equals, "run")
	c.Assert(string(JobTypeService), Equals, "service-run")
	c.Assert(string(JobTypeLocal), Equals, "local")
	c.Assert(string(JobTypeCompose), Equals, "compose")
}

func (s *TypesSuite) TestJobSourceConstants(c *C) {
	c.Assert(string(JobSourceINI), Equals, "ini")
	c.Assert(string(JobSourceLabel), Equals, "label")
}

func (s *TypesSuite) TestUnifiedJobConfigRun(c *C) {
	// Create a mock context
	logger := &test.Logger{}
	scheduler := core.NewScheduler(logger)
	execution := &core.Execution{}
	ctx := &core.Context{
		Logger:    logger,
		Scheduler: scheduler,
		Execution: execution,
	}

	// Test with exec job
	execJob := NewUnifiedJobConfig(JobTypeExec)
	execJob.ExecJob.Name = "test-exec"
	execJob.ExecJob.Command = "echo test"

	// Since we don't have a real Docker client, the Run will fail
	// but we can verify the method delegation works
	err := execJob.Run(ctx)
	c.Assert(err, NotNil) // Expected to fail without proper setup

	// Test with nil core job (invalid state)
	invalidJob := &UnifiedJobConfig{Type: JobType("invalid")}
	err = invalidJob.Run(ctx)
	c.Assert(err, Equals, core.ErrUnexpected)
}

func (s *TypesSuite) TestUnifiedJobConfigMiddlewareOperations(c *C) {
	job := NewUnifiedJobConfig(JobTypeExec)
	
	// Test Use method
	testMiddleware := &mockMiddleware{}
	job.Use(testMiddleware)
	
	// Verify middleware was added
	middlewares := job.Middlewares()
	c.Assert(len(middlewares), Equals, 1)
	c.Assert(middlewares[0], Equals, testMiddleware)
}

// Mock middleware for testing
type mockMiddleware struct{}

func (m *mockMiddleware) Run(ctx *core.Context) error {
	return ctx.Next()
}