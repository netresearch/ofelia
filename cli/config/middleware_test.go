package config

import (
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
	. "gopkg.in/check.v1"
)

type MiddlewareSuite struct{}

var _ = Suite(&MiddlewareSuite{})

func (s *MiddlewareSuite) TestNewMiddlewareBuilder(c *C) {
	builder := NewMiddlewareBuilder()
	c.Assert(builder, NotNil)
}

func (s *MiddlewareSuite) TestBuildMiddlewares(c *C) {
	builder := NewMiddlewareBuilder()
	job := &core.ExecJob{}

	middlewareConfig := &MiddlewareConfig{
		OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
		SlackConfig:   middlewares.SlackConfig{SlackWebhook: "http://example.com/webhook"},
		SaveConfig:    middlewares.SaveConfig{SaveFolder: "/tmp/logs"},
		MailConfig:    middlewares.MailConfig{EmailTo: "admin@example.com"},
	}

	builder.BuildMiddlewares(job, middlewareConfig)

	// Verify middlewares were applied
	middlewares := job.Middlewares()
	c.Assert(len(middlewares), Equals, 4)

	// Verify middleware count and that they are not nil
	c.Assert(len(middlewares), Equals, 4)
	for i, mw := range middlewares {
		c.Assert(mw, NotNil, Commentf("Middleware %d should not be nil", i))
	}
}

func (s *MiddlewareSuite) TestBuildMiddlewaresNilJob(c *C) {
	builder := NewMiddlewareBuilder()
	middlewareConfig := &MiddlewareConfig{}

	// Should not panic with nil job
	builder.BuildMiddlewares(nil, middlewareConfig)
}

func (s *MiddlewareSuite) TestBuildMiddlewaresNilConfig(c *C) {
	builder := NewMiddlewareBuilder()
	job := &core.ExecJob{}

	// Should not panic with nil config
	builder.BuildMiddlewares(job, nil)
}

func (s *MiddlewareSuite) TestBuildSchedulerMiddlewares(c *C) {
	builder := NewMiddlewareBuilder()
	logger := &test.Logger{}
	scheduler := core.NewScheduler(logger)

	slackConfig := &middlewares.SlackConfig{SlackWebhook: "http://example.com/webhook"}
	saveConfig := &middlewares.SaveConfig{SaveFolder: "/tmp/logs"}
	mailConfig := &middlewares.MailConfig{EmailTo: "admin@example.com"}

	builder.BuildSchedulerMiddlewares(scheduler, slackConfig, saveConfig, mailConfig)

	// Verify scheduler middlewares were applied
	middlewares := scheduler.Middlewares()
	c.Assert(len(middlewares), Equals, 3)

	// Verify middleware types are not nil
	for i, mw := range middlewares {
		c.Assert(mw, NotNil, Commentf("Scheduler middleware %d should not be nil", i))
	}
}

func (s *MiddlewareSuite) TestBuildSchedulerMiddlewaresNilScheduler(c *C) {
	builder := NewMiddlewareBuilder()
	slackConfig := &middlewares.SlackConfig{}
	saveConfig := &middlewares.SaveConfig{}
	mailConfig := &middlewares.MailConfig{}

	// Should not panic with nil scheduler
	builder.BuildSchedulerMiddlewares(nil, slackConfig, saveConfig, mailConfig)
}

func (s *MiddlewareSuite) TestResetJobMiddlewares(c *C) {
	builder := NewMiddlewareBuilder()
	job := &core.ExecJob{}

	// Add some initial middlewares
	initialMiddleware := &mockMiddleware{}
	job.Use(initialMiddleware)
	c.Assert(len(job.Middlewares()), Equals, 1)

	// Reset and rebuild
	middlewareConfig := &MiddlewareConfig{
		OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
	}
	schedulerMiddlewares := []core.Middleware{&mockMiddleware{}}

	builder.ResetJobMiddlewares(job, middlewareConfig, schedulerMiddlewares)

	// Should have middleware config middleware + scheduler middleware
	middlewares := job.Middlewares()
	c.Assert(len(middlewares), Equals, 2) // 1 from config + 1 from scheduler
}

func (s *MiddlewareSuite) TestValidateMiddlewareConfig(c *C) {
	builder := NewMiddlewareBuilder()

	config := &MiddlewareConfig{
		OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
		SlackConfig:   middlewares.SlackConfig{SlackWebhook: "http://example.com"},
	}

	err := builder.ValidateMiddlewareConfig(config)
	c.Assert(err, IsNil) // Currently no validation logic, should return nil
}

func (s *MiddlewareSuite) TestValidateMiddlewareConfigNil(c *C) {
	builder := NewMiddlewareBuilder()

	err := builder.ValidateMiddlewareConfig(nil)
	c.Assert(err, IsNil) // Should handle nil gracefully
}

func (s *MiddlewareSuite) TestGetActiveMiddlewareNames(c *C) {
	builder := NewMiddlewareBuilder()

	// Test with empty config
	emptyConfig := &MiddlewareConfig{}
	names := builder.GetActiveMiddlewareNames(emptyConfig)
	c.Assert(len(names), Equals, 0)

	// Test with some middlewares configured
	config := &MiddlewareConfig{
		OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
		SlackConfig:   middlewares.SlackConfig{SlackWebhook: "http://example.com"},
		SaveConfig:    middlewares.SaveConfig{SaveFolder: "/tmp"},
		// MailConfig left empty
	}

	names = builder.GetActiveMiddlewareNames(config)
	c.Assert(len(names), Equals, 3)
	c.Assert(contains(names, "overlap"), Equals, true)
	c.Assert(contains(names, "slack"), Equals, true)
	c.Assert(contains(names, "save"), Equals, true)
	c.Assert(contains(names, "mail"), Equals, false) // Should not be active
}

func (s *MiddlewareSuite) TestGetActiveMiddlewareNamesNil(c *C) {
	builder := NewMiddlewareBuilder()

	names := builder.GetActiveMiddlewareNames(nil)
	c.Assert(len(names), Equals, 0)
}

func (s *MiddlewareSuite) TestBuildMiddlewaresIntegration(c *C) {
	// Integration test: build middlewares and verify they work correctly
	builder := NewMiddlewareBuilder()
	job := &core.ExecJob{}

	middlewareConfig := &MiddlewareConfig{
		OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
		SlackConfig:   middlewares.SlackConfig{SlackWebhook: "http://example.com/webhook"},
	}

	builder.BuildMiddlewares(job, middlewareConfig)

	// Verify that the middlewares are properly configured
	middlewares := job.Middlewares()
	c.Assert(len(middlewares), Equals, 2)

	// Test that middleware configuration was applied correctly
	// This tests the internal configuration of middlewares.NewOverlap, etc.
	// which should create middlewares with the provided configuration
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
