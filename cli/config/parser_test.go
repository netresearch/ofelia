package config

import (
	"github.com/netresearch/ofelia/test"
	ini "gopkg.in/ini.v1"
	. "gopkg.in/check.v1"
)

type ParserSuite struct{}

var _ = Suite(&ParserSuite{})

func (s *ParserSuite) TestNewConfigurationParser(c *C) {
	logger := &test.Logger{}
	parser := NewConfigurationParser(logger)
	c.Assert(parser, NotNil)
	c.Assert(parser.logger, Equals, logger)
}

func (s *ParserSuite) TestParseINI(c *C) {
	logger := &test.Logger{}
	parser := NewConfigurationParser(logger)
	
	iniContent := `
[job-exec "test-exec"]
schedule = @every 10s
command = echo "test exec"
container = test-container
no-overlap = true

[job-run "test-run"]
schedule = @every 5s
command = echo "test run"
image = busybox:latest
slack-webhook = http://example.com/webhook

[job-service-run "test-service"]
schedule = @every 15s
command = echo "test service"
save-folder = /tmp/logs

[job-local "test-local"]
schedule = @every 20s
command = echo "test local"
email-to = admin@example.com

[job-compose "test-compose"]
schedule = @every 30s
command = docker-compose up
save-only-on-error = true
`
	
	cfg, err := ini.LoadSources(ini.LoadOptions{}, []byte(iniContent))
	c.Assert(err, IsNil)
	
	jobs, err := parser.ParseINI(cfg)
	c.Assert(err, IsNil)
	c.Assert(len(jobs), Equals, 5)
	
	// Test exec job
	execJob, exists := jobs["test-exec"]
	c.Assert(exists, Equals, true)
	c.Assert(execJob.Type, Equals, JobTypeExec)
	c.Assert(execJob.JobSource, Equals, JobSourceINI)
	c.Assert(execJob.ExecJob.Schedule, Equals, "@every 10s")
	c.Assert(execJob.ExecJob.Command, Equals, "echo \"test exec\"")
	c.Assert(execJob.ExecJob.Container, Equals, "test-container")
	c.Assert(execJob.MiddlewareConfig.OverlapConfig.NoOverlap, Equals, true)
	
	// Test run job
	runJob, exists := jobs["test-run"]
	c.Assert(exists, Equals, true)
	c.Assert(runJob.Type, Equals, JobTypeRun)
	c.Assert(runJob.RunJob.Schedule, Equals, "@every 5s")
	c.Assert(runJob.RunJob.Command, Equals, "echo \"test run\"")
	c.Assert(runJob.RunJob.Image, Equals, "busybox:latest")
	c.Assert(runJob.MiddlewareConfig.SlackConfig.SlackWebhook, Equals, "http://example.com/webhook")
	
	// Test service job
	serviceJob, exists := jobs["test-service"]
	c.Assert(exists, Equals, true)
	c.Assert(serviceJob.Type, Equals, JobTypeService)
	c.Assert(serviceJob.RunServiceJob.Schedule, Equals, "@every 15s")
	c.Assert(serviceJob.MiddlewareConfig.SaveConfig.SaveFolder, Equals, "/tmp/logs")
	
	// Test local job
	localJob, exists := jobs["test-local"]
	c.Assert(exists, Equals, true)
	c.Assert(localJob.Type, Equals, JobTypeLocal)
	c.Assert(localJob.LocalJob.Schedule, Equals, "@every 20s")
	c.Assert(localJob.MiddlewareConfig.MailConfig.EmailTo, Equals, "admin@example.com")
	
	// Test compose job
	composeJob, exists := jobs["test-compose"]
	c.Assert(exists, Equals, true)
	c.Assert(composeJob.Type, Equals, JobTypeCompose)
	c.Assert(composeJob.ComposeJob.Schedule, Equals, "@every 30s")
	c.Assert(composeJob.MiddlewareConfig.SaveConfig.SaveOnlyOnError, Equals, true)
}

func (s *ParserSuite) TestParseINIInvalidSection(c *C) {
	logger := &test.Logger{}
	parser := NewConfigurationParser(logger)
	
	iniContent := `
[global]
log-level = debug

[docker]
poll-interval = 5s

[job-exec "test"]
schedule = @every 10s
command = echo test
`
	
	cfg, err := ini.LoadSources(ini.LoadOptions{}, []byte(iniContent))
	c.Assert(err, IsNil)
	
	jobs, err := parser.ParseINI(cfg)
	c.Assert(err, IsNil)
	c.Assert(len(jobs), Equals, 1) // Only job-exec should be parsed
	
	execJob, exists := jobs["test"]
	c.Assert(exists, Equals, true)
	c.Assert(execJob.Type, Equals, JobTypeExec)
}

func (s *ParserSuite) TestParseINIWithQuotedJobName(c *C) {
	logger := &test.Logger{}
	parser := NewConfigurationParser(logger)
	
	iniContent := `
[job-exec "quoted job name"]
schedule = @every 10s
command = echo test
`
	
	cfg, err := ini.LoadSources(ini.LoadOptions{}, []byte(iniContent))
	c.Assert(err, IsNil)
	
	jobs, err := parser.ParseINI(cfg)
	c.Assert(err, IsNil)
	c.Assert(len(jobs), Equals, 1)
	
	_, exists := jobs["quoted job name"]
	c.Assert(exists, Equals, true)
}

func (s *ParserSuite) TestParseDockerLabels(c *C) {
	logger := &test.Logger{}
	parser := NewConfigurationParser(logger)
	
	labels := map[string]map[string]string{
		"test-container": {
			"ofelia.enabled":                            "true",
			"ofelia.service":                            "true",
			"ofelia.job-exec.test-exec.schedule":        "@every 10s",
			"ofelia.job-exec.test-exec.command":         "echo test",
			"ofelia.job-run.test-run.schedule":          "@every 5s",
			"ofelia.job-run.test-run.command":           "echo run",
			"ofelia.job-run.test-run.image":             "busybox:latest",
			"ofelia.job-local.test-local.schedule":      "@every 20s",
			"ofelia.job-local.test-local.command":       "echo local",
			"ofelia.job-service-run.test-service.schedule": "@every 15s",
			"ofelia.job-service-run.test-service.command":  "echo service",
			"ofelia.job-compose.test-compose.schedule":     "@every 30s",
			"ofelia.job-compose.test-compose.command":      "docker-compose up",
		},
	}
	
	jobs, err := parser.ParseDockerLabels(labels, true) // Allow host jobs
	c.Assert(err, IsNil)
	c.Assert(len(jobs), Equals, 5) // All job types should be present
	
	// Test exec job (with container scope)
	execJob, exists := jobs["test-container.test-exec"]
	c.Assert(exists, Equals, true)
	c.Assert(execJob.Type, Equals, JobTypeExec)
	c.Assert(execJob.JobSource, Equals, JobSourceLabel)
	c.Assert(execJob.ExecJob.Schedule, Equals, "@every 10s")
	c.Assert(execJob.ExecJob.Command, Equals, "echo test")
	
	// Test run job
	runJob, exists := jobs["test-run"]
	c.Assert(exists, Equals, true)
	c.Assert(runJob.Type, Equals, JobTypeRun)
	c.Assert(runJob.RunJob.Schedule, Equals, "@every 5s")
	c.Assert(runJob.RunJob.Command, Equals, "echo run")
	
	// Test local job
	localJob, exists := jobs["test-local"]
	c.Assert(exists, Equals, true)
	c.Assert(localJob.Type, Equals, JobTypeLocal)
	
	// Test service job
	serviceJob, exists := jobs["test-service"]
	c.Assert(exists, Equals, true)
	c.Assert(serviceJob.Type, Equals, JobTypeService)
	
	// Test compose job
	composeJob, exists := jobs["test-compose"]
	c.Assert(exists, Equals, true)
	c.Assert(composeJob.Type, Equals, JobTypeCompose)
}

func (s *ParserSuite) TestParseDockerLabelsSecurityBlocking(c *C) {
	logger := &test.Logger{}
	parser := NewConfigurationParser(logger)
	
	labels := map[string]map[string]string{
		"test-container": {
			"ofelia.enabled":                       "true",
			"ofelia.service":                       "true",
			"ofelia.job-local.test-local.schedule": "@every 20s",
			"ofelia.job-local.test-local.command":  "rm -rf /",
			"ofelia.job-compose.test-compose.schedule": "@every 30s",
			"ofelia.job-compose.test-compose.command":  "docker-compose down",
		},
	}
	
	jobs, err := parser.ParseDockerLabels(labels, false) // Block host jobs
	c.Assert(err, IsNil)
	c.Assert(len(jobs), Equals, 0) // No jobs should be created due to security blocking
}

func (s *ParserSuite) TestParseDockerLabelsNoRequiredLabel(c *C) {
	logger := &test.Logger{}
	parser := NewConfigurationParser(logger)
	
	labels := map[string]map[string]string{
		"test-container": {
			// Missing "ofelia.enabled": "true"
			"ofelia.job-exec.test.schedule": "@every 10s",
			"ofelia.job-exec.test.command":  "echo test",
		},
	}
	
	jobs, err := parser.ParseDockerLabels(labels, true)
	c.Assert(err, IsNil)
	c.Assert(len(jobs), Equals, 0) // No jobs should be created without required label
}

func (s *ParserSuite) TestParseDockerLabelsWithJSONArray(c *C) {
	logger := &test.Logger{}
	parser := NewConfigurationParser(logger)
	
	labels := map[string]map[string]string{
		"test-container": {
			"ofelia.enabled":                     "true",
			"ofelia.service":                     "true",
			"ofelia.job-run.test.schedule":       "@every 5s",
			"ofelia.job-run.test.command":        "echo test",
			"ofelia.job-run.test.volume":         `["/tmp:/tmp:ro", "/var:/var:rw"]`,
			"ofelia.job-run.test.environment":    `["KEY1=value1", "KEY2=value2"]`,
			"ofelia.job-run.test.volumes-from":   `["container1", "container2"]`,
		},
	}
	
	jobs, err := parser.ParseDockerLabels(labels, true)
	c.Assert(err, IsNil)
	c.Assert(len(jobs), Equals, 1)
	
	runJob, exists := jobs["test"]
	c.Assert(exists, Equals, true)
	c.Assert(runJob.Type, Equals, JobTypeRun)
	
	// Note: The actual volume/environment parsing happens at the mapstructure level
	// This test mainly verifies that JSON arrays are handled in setJobParam
}

func (s *ParserSuite) TestSplitLabelsByType(c *C) {
	logger := &test.Logger{}
	parser := NewConfigurationParser(logger)
	
	labels := map[string]map[string]string{
		"container1": {
			"ofelia.enabled":                     "true",
			"ofelia.service":                     "true",
			"ofelia.job-exec.exec1.schedule":     "@every 10s",
			"ofelia.job-exec.exec1.command":      "echo exec",
			"ofelia.job-local.local1.schedule":   "@every 20s",
			"ofelia.job-local.local1.command":    "echo local",
		},
		"container2": {
			"ofelia.enabled":                       "true",
			"ofelia.job-run.run1.schedule":         "@every 5s",
			"ofelia.job-run.run1.command":          "echo run",
			"ofelia.job-service-run.svc1.schedule": "@every 15s",
			"ofelia.job-service-run.svc1.command":  "echo service",
		},
	}
	
	execJobs, localJobs, runJobs, serviceJobs, composeJobs := parser.splitLabelsByType(labels)
	
	// Check exec jobs
	c.Assert(len(execJobs), Equals, 1)
	_, exists := execJobs["container1.exec1"]
	c.Assert(exists, Equals, true)
	
	// Check local jobs (only from service containers)
	c.Assert(len(localJobs), Equals, 1)
	_, exists = localJobs["local1"]
	c.Assert(exists, Equals, true)
	
	// Check run jobs
	c.Assert(len(runJobs), Equals, 1)
	_, exists = runJobs["run1"]
	c.Assert(exists, Equals, true)
	
	// Check service jobs (only from service containers)
	c.Assert(len(serviceJobs), Equals, 0) // container2 doesn't have service label
	
	// Check compose jobs
	c.Assert(len(composeJobs), Equals, 0)
}

func (s *ParserSuite) TestParseJobName(c *C) {
	testCases := []struct {
		section  string
		prefix   string
		expected string
	}{
		{"job-exec \"test\"", "job-exec", "test"},
		{"job-exec test", "job-exec", "test"},
		{"job-exec  test  ", "job-exec", "test"},
		{"job-run \"quoted name\"", "job-run", "quoted name"},
		{"job-local simple", "job-local", "simple"},
	}
	
	for _, tc := range testCases {
		result := parseJobName(tc.section, tc.prefix)
		c.Assert(result, Equals, tc.expected)
	}
}

func (s *ParserSuite) TestSectionToMap(c *C) {
	iniContent := `
[test]
single = value1
multi = value2
multi = value3
empty =
`
	
	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true}, []byte(iniContent))
	c.Assert(err, IsNil)
	
	section, err := cfg.GetSection("test")
	c.Assert(err, IsNil)
	
	sectionMap := sectionToMap(section)
	
	c.Assert(sectionMap["single"], Equals, "value1")
	
	// Multi-value keys should become slices
	multiValues, ok := sectionMap["multi"].([]string)
	c.Assert(ok, Equals, true)
	c.Assert(len(multiValues), Equals, 2)
	c.Assert(multiValues[0], Equals, "value2")
	c.Assert(multiValues[1], Equals, "value3")
	
	c.Assert(sectionMap["empty"], Equals, "")
}

func (s *ParserSuite) TestHasServiceLabel(c *C) {
	// Test with service label
	labels1 := map[string]string{
		"ofelia.enabled": "true",
		"ofelia.service": "true",
	}
	c.Assert(hasServiceLabel(labels1), Equals, true)
	
	// Test without service label
	labels2 := map[string]string{
		"ofelia.enabled": "true",
	}
	c.Assert(hasServiceLabel(labels2), Equals, false)
	
	// Test with service label set to false
	labels3 := map[string]string{
		"ofelia.enabled": "true",
		"ofelia.service": "false",
	}
	c.Assert(hasServiceLabel(labels3), Equals, false)
}

func (s *ParserSuite) TestSetJobParam(c *C) {
	params := make(map[string]interface{})
	
	// Test regular parameter
	setJobParam(params, "schedule", "@every 5s")
	c.Assert(params["schedule"], Equals, "@every 5s")
	
	// Test JSON array parameter
	setJobParam(params, "volume", `["/tmp:/tmp:ro", "/var:/var:rw"]`)
	volumes, ok := params["volume"].([]string)
	c.Assert(ok, Equals, true)
	c.Assert(len(volumes), Equals, 2)
	c.Assert(volumes[0], Equals, "/tmp:/tmp:ro")
	c.Assert(volumes[1], Equals, "/var:/var:rw")
	
	// Test invalid JSON (should fallback to string)
	setJobParam(params, "environment", "invalid json [")
	c.Assert(params["environment"], Equals, "invalid json [")
}

func (s *ParserSuite) TestEnsureJob(c *C) {
	jobs := make(map[string]map[string]interface{})
	
	ensureJob(jobs, "test-job")
	c.Assert(len(jobs), Equals, 1)
	
	jobMap, exists := jobs["test-job"]
	c.Assert(exists, Equals, true)
	c.Assert(jobMap, NotNil)
	
	// Calling again should not create duplicate
	ensureJob(jobs, "test-job")
	c.Assert(len(jobs), Equals, 1)
}