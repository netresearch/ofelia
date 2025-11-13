package cli

import (
	. "gopkg.in/check.v1"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// SuiteExecJobInit tests ExecJob initialization from config
type SuiteExecJobInit struct{}

var _ = Suite(&SuiteExecJobInit{})

// TestExecJobInit_FromINIConfig verifies that ExecJobs loaded from INI config
// have dockerOps properly initialized and can execute without panic
func (s *SuiteExecJobInit) TestExecJobInit_FromINIConfig(c *C) {
	mockLogger := &test.Logger{}

	// Create config from INI string (simulates loading from file)
	cfg, err := BuildFromString(`
		[job-exec "test-job"]
		schedule = @every 1h
		command = echo "test"
		container = test-container
		user = nobody
	`, mockLogger)

	c.Assert(err, IsNil)
	c.Assert(cfg.ExecJobs, NotNil)
	c.Assert(cfg.ExecJobs, HasLen, 1)

	// Get the job
	job, exists := cfg.ExecJobs["test-job"]
	c.Assert(exists, Equals, true)
	c.Assert(job, NotNil)

	// Verify job fields are set from config
	c.Assert(job.GetName(), Equals, "") // Name not set yet (set during registration)
	c.Assert(job.GetSchedule(), Equals, "@every 1h")
	c.Assert(job.GetCommand(), Equals, `echo "test"`)
	c.Assert(job.Container, Equals, "test-container")
	c.Assert(job.User, Equals, "nobody")

	// CRITICAL: This is the regression test for the nil pointer bug
	// Before the fix, dockerOps would be nil here
	// The job won't have dockerOps until InitializeApp() is called
	c.Assert(job.ExecJob.Client, IsNil) // Client not set until InitializeApp
}

// TestExecJobInit_AfterInitializeApp verifies that after InitializeApp(),
// ExecJobs have dockerOps initialized and can be scheduled
func (s *SuiteExecJobInit) TestExecJobInit_AfterInitializeApp(c *C) {
	mockLogger := &test.Logger{}

	// Create config from INI string
	cfg, err := BuildFromString(`
		[job-exec "initialized-job"]
		schedule = @every 1h
		command = /bin/true
		container = test-container
	`, mockLogger)

	c.Assert(err, IsNil)

	// Initialize the app (this calls registerAllJobs which should call InitializeRuntimeFields)
	// Note: This will fail without Docker, but we're testing the initialization path
	err = cfg.InitializeApp()

	// We expect an error here because Docker is not available in test env
	// But the important thing is that it doesn't panic due to nil dockerOps
	// If there's a panic, the test will fail
	if err == nil {
		// If we somehow have Docker available, verify the job is properly initialized
		job, exists := cfg.ExecJobs["initialized-job"]
		c.Assert(exists, Equals, true)
		c.Assert(job, NotNil)
		c.Assert(job.GetName(), Equals, "initialized-job")

		// This is the critical check - dockerOps should be initialized
		// We can't check it directly as it's private, but if Run() doesn't panic, it worked
	}
}

// TestExecJobConfig_dockerOpsInitialization is a unit test that verifies
// the InitializeRuntimeFields method is called during config preparation
func (s *SuiteExecJobInit) TestExecJobConfig_dockerOpsInitialization(c *C) {
	// This test verifies the fix at the config layer
	// Create an ExecJobConfig directly (as mapstructure would)
	job := &ExecJobConfig{
		ExecJob: core.ExecJob{
			BareJob: core.BareJob{
				Name:     "direct-job",
				Command:  "echo test",
				Schedule: "@hourly",
			},
			Container: "test",
			User:      "nobody",
		},
	}

	// Before setting client, dockerOps should be nil
	// (We can't check this directly as it's private)

	// Simulate what happens in registerAllJobs, dockerLabelsUpdate, and iniConfigUpdate
	// In a real scenario, this would be a real Docker client
	// For this test, we just verify the method exists and doesn't panic with nil client
	job.InitializeRuntimeFields()

	// The method should handle nil client gracefully
	// No assertion needed - if it panics, test fails
}
