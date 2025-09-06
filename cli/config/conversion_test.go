package config

import (
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	. "gopkg.in/check.v1"
)

type ConversionSuite struct{}

var _ = Suite(&ConversionSuite{})

func (s *ConversionSuite) TestConvertFromExecJobConfig(c *C) {
	legacy := &ExecJobConfigLegacy{
		ExecJob: core.ExecJob{
			BareJob: core.BareJob{
				Name:     "test-exec",
				Schedule: "@every 5s",
				Command:  "echo test",
			},
			Container: "test-container",
		},
		OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
		SlackConfig:   middlewares.SlackConfig{SlackWebhook: "http://example.com"},
		JobSource:     JobSourceINI,
	}

	unified := ConvertFromExecJobConfig(legacy)
	
	c.Assert(unified, NotNil)
	c.Assert(unified.Type, Equals, JobTypeExec)
	c.Assert(unified.JobSource, Equals, JobSourceINI)
	c.Assert(unified.ExecJob.Name, Equals, "test-exec")
	c.Assert(unified.ExecJob.Schedule, Equals, "@every 5s")
	c.Assert(unified.ExecJob.Command, Equals, "echo test")
	c.Assert(unified.ExecJob.Container, Equals, "test-container")
	c.Assert(unified.MiddlewareConfig.OverlapConfig.NoOverlap, Equals, true)
	c.Assert(unified.MiddlewareConfig.SlackConfig.SlackWebhook, Equals, "http://example.com")
}

func (s *ConversionSuite) TestConvertFromRunJobConfig(c *C) {
	legacy := &RunJobConfigLegacy{
		RunJob: core.RunJob{
			BareJob: core.BareJob{
				Name:     "test-run",
				Schedule: "@every 10s",
				Command:  "echo run test",
			},
			Image: "busybox:latest",
		},
		SaveConfig: middlewares.SaveConfig{SaveFolder: "/tmp/logs"},
		JobSource:  JobSourceLabel,
	}

	unified := ConvertFromRunJobConfig(legacy)
	
	c.Assert(unified, NotNil)
	c.Assert(unified.Type, Equals, JobTypeRun)
	c.Assert(unified.JobSource, Equals, JobSourceLabel)
	c.Assert(unified.RunJob.Name, Equals, "test-run")
	c.Assert(unified.RunJob.Schedule, Equals, "@every 10s")
	c.Assert(unified.RunJob.Command, Equals, "echo run test")
	c.Assert(unified.RunJob.Image, Equals, "busybox:latest")
	c.Assert(unified.MiddlewareConfig.SaveConfig.SaveFolder, Equals, "/tmp/logs")
}

func (s *ConversionSuite) TestConvertFromRunServiceConfig(c *C) {
	legacy := &RunServiceConfigLegacy{
		RunServiceJob: core.RunServiceJob{
			BareJob: core.BareJob{
				Name:     "test-service",
				Schedule: "@every 15s",
				Command:  "echo service test",
			},
		},
		MailConfig: middlewares.MailConfig{EmailTo: "admin@example.com"},
		JobSource:  JobSourceINI,
	}

	unified := ConvertFromRunServiceConfig(legacy)
	
	c.Assert(unified, NotNil)
	c.Assert(unified.Type, Equals, JobTypeService)
	c.Assert(unified.JobSource, Equals, JobSourceINI)
	c.Assert(unified.RunServiceJob.Name, Equals, "test-service")
	c.Assert(unified.RunServiceJob.Schedule, Equals, "@every 15s")
	c.Assert(unified.RunServiceJob.Command, Equals, "echo service test")
	c.Assert(unified.MiddlewareConfig.MailConfig.EmailTo, Equals, "admin@example.com")
}

func (s *ConversionSuite) TestConvertFromLocalJobConfig(c *C) {
	legacy := &LocalJobConfigLegacy{
		LocalJob: core.LocalJob{
			BareJob: core.BareJob{
				Name:     "test-local",
				Schedule: "@every 20s",
				Command:  "echo local test",
			},
		},
		SlackConfig: middlewares.SlackConfig{SlackOnlyOnError: true},
		JobSource:   JobSourceLabel,
	}

	unified := ConvertFromLocalJobConfig(legacy)
	
	c.Assert(unified, NotNil)
	c.Assert(unified.Type, Equals, JobTypeLocal)
	c.Assert(unified.JobSource, Equals, JobSourceLabel)
	c.Assert(unified.LocalJob.Name, Equals, "test-local")
	c.Assert(unified.LocalJob.Schedule, Equals, "@every 20s")
	c.Assert(unified.LocalJob.Command, Equals, "echo local test")
	c.Assert(unified.MiddlewareConfig.SlackConfig.SlackOnlyOnError, Equals, true)
}

func (s *ConversionSuite) TestConvertFromComposeJobConfig(c *C) {
	legacy := &ComposeJobConfigLegacy{
		ComposeJob: core.ComposeJob{
			BareJob: core.BareJob{
				Name:     "test-compose",
				Schedule: "@every 30s",
				Command:  "docker-compose up",
			},
		},
		SaveConfig: middlewares.SaveConfig{SaveOnlyOnError: true},
		JobSource:  JobSourceINI,
	}

	unified := ConvertFromComposeJobConfig(legacy)
	
	c.Assert(unified, NotNil)
	c.Assert(unified.Type, Equals, JobTypeCompose)
	c.Assert(unified.JobSource, Equals, JobSourceINI)
	c.Assert(unified.ComposeJob.Name, Equals, "test-compose")
	c.Assert(unified.ComposeJob.Schedule, Equals, "@every 30s")
	c.Assert(unified.ComposeJob.Command, Equals, "docker-compose up")
	c.Assert(unified.MiddlewareConfig.SaveConfig.SaveOnlyOnError, Equals, true)
}

func (s *ConversionSuite) TestConvertToExecJobConfig(c *C) {
	unified := &UnifiedJobConfig{
		Type:      JobTypeExec,
		JobSource: JobSourceINI,
		MiddlewareConfig: MiddlewareConfig{
			OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
			SlackConfig:   middlewares.SlackConfig{SlackWebhook: "http://example.com"},
		},
		ExecJob: &core.ExecJob{
			BareJob: core.BareJob{
				Name:     "test-exec",
				Schedule: "@every 5s",
				Command:  "echo test",
			},
			Container: "test-container",
		},
	}

	legacy := ConvertToExecJobConfig(unified)
	
	c.Assert(legacy, NotNil)
	c.Assert(legacy.JobSource, Equals, JobSourceINI)
	c.Assert(legacy.ExecJob.Name, Equals, "test-exec")
	c.Assert(legacy.ExecJob.Schedule, Equals, "@every 5s")
	c.Assert(legacy.ExecJob.Command, Equals, "echo test")
	c.Assert(legacy.ExecJob.Container, Equals, "test-container")
	c.Assert(legacy.OverlapConfig.NoOverlap, Equals, true)
	c.Assert(legacy.SlackConfig.SlackWebhook, Equals, "http://example.com")
}

func (s *ConversionSuite) TestConvertToExecJobConfigWrongType(c *C) {
	unified := &UnifiedJobConfig{
		Type: JobTypeRun, // Wrong type
		RunJob: &core.RunJob{
			BareJob: core.BareJob{Name: "test-run"},
		},
	}

	legacy := ConvertToExecJobConfig(unified)
	c.Assert(legacy, IsNil) // Should return nil for wrong type
}

func (s *ConversionSuite) TestConvertToExecJobConfigNilJob(c *C) {
	unified := &UnifiedJobConfig{
		Type:    JobTypeExec,
		ExecJob: nil, // Nil job
	}

	legacy := ConvertToExecJobConfig(unified)
	c.Assert(legacy, IsNil) // Should return nil for nil job
}

func (s *ConversionSuite) TestConvertLegacyJobMaps(c *C) {
	// Create legacy job maps
	execJobs := map[string]*ExecJobConfigLegacy{
		"exec1": {
			ExecJob: core.ExecJob{
				BareJob: core.BareJob{Name: "exec1", Schedule: "@every 5s"},
			},
			JobSource: JobSourceINI,
		},
	}
	
	runJobs := map[string]*RunJobConfigLegacy{
		"run1": {
			RunJob: core.RunJob{
				BareJob: core.BareJob{Name: "run1", Schedule: "@every 10s"},
			},
			JobSource: JobSourceLabel,
		},
	}
	
	serviceJobs := map[string]*RunServiceConfigLegacy{
		"service1": {
			RunServiceJob: core.RunServiceJob{
				BareJob: core.BareJob{Name: "service1", Schedule: "@every 15s"},
			},
			JobSource: JobSourceINI,
		},
	}
	
	localJobs := map[string]*LocalJobConfigLegacy{
		"local1": {
			LocalJob: core.LocalJob{
				BareJob: core.BareJob{Name: "local1", Schedule: "@every 20s"},
			},
			JobSource: JobSourceLabel,
		},
	}
	
	composeJobs := map[string]*ComposeJobConfigLegacy{
		"compose1": {
			ComposeJob: core.ComposeJob{
				BareJob: core.BareJob{Name: "compose1", Schedule: "@every 25s"},
			},
			JobSource: JobSourceINI,
		},
	}

	// Convert to unified
	unified := ConvertLegacyJobMaps(execJobs, runJobs, serviceJobs, localJobs, composeJobs)
	
	c.Assert(len(unified), Equals, 5)
	
	// Verify exec job conversion
	execJob, exists := unified["exec1"]
	c.Assert(exists, Equals, true)
	c.Assert(execJob.Type, Equals, JobTypeExec)
	c.Assert(execJob.JobSource, Equals, JobSourceINI)
	c.Assert(execJob.GetName(), Equals, "exec1")
	
	// Verify run job conversion
	runJob, exists := unified["run1"]
	c.Assert(exists, Equals, true)
	c.Assert(runJob.Type, Equals, JobTypeRun)
	c.Assert(runJob.JobSource, Equals, JobSourceLabel)
	c.Assert(runJob.GetName(), Equals, "run1")
	
	// Verify service job conversion
	serviceJob, exists := unified["service1"]
	c.Assert(exists, Equals, true)
	c.Assert(serviceJob.Type, Equals, JobTypeService)
	c.Assert(serviceJob.JobSource, Equals, JobSourceINI)
	c.Assert(serviceJob.GetName(), Equals, "service1")
	
	// Verify local job conversion
	localJob, exists := unified["local1"]
	c.Assert(exists, Equals, true)
	c.Assert(localJob.Type, Equals, JobTypeLocal)
	c.Assert(localJob.JobSource, Equals, JobSourceLabel)
	c.Assert(localJob.GetName(), Equals, "local1")
	
	// Verify compose job conversion
	composeJob, exists := unified["compose1"]
	c.Assert(exists, Equals, true)
	c.Assert(composeJob.Type, Equals, JobTypeCompose)
	c.Assert(composeJob.JobSource, Equals, JobSourceINI)
	c.Assert(composeJob.GetName(), Equals, "compose1")
}

func (s *ConversionSuite) TestConvertLegacyJobMapsEmpty(c *C) {
	// Test with empty maps
	unified := ConvertLegacyJobMaps(
		make(map[string]*ExecJobConfigLegacy),
		make(map[string]*RunJobConfigLegacy),
		make(map[string]*RunServiceConfigLegacy),
		make(map[string]*LocalJobConfigLegacy),
		make(map[string]*ComposeJobConfigLegacy),
	)
	
	c.Assert(len(unified), Equals, 0)
}