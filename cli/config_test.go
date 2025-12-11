package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	defaults "github.com/creasty/defaults"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func TestConfig(t *testing.T) { TestingT(t) }

type SuiteConfig struct{}

var _ = Suite(&SuiteConfig{})

// Use shared TestLogger from test package
type TestLogger = test.Logger

func (s *SuiteConfig) TestBuildFromString(c *C) {
	mockLogger := TestLogger{}
	_, err := BuildFromString(`
		[job-exec "foo"]
		schedule = @every 10s

		[job-exec "bar"]
		schedule = @every 10s

		[job-run "qux"]
		schedule = @every 10s
		image = alpine

		[job-local "baz"]
		schedule = @every 10s

		[job-service-run "bob"]
		schedule = @every 10s
		image = nginx
  `, &mockLogger)

	c.Assert(err, IsNil)
}

func (s *SuiteConfig) TestJobDefaultsSet(c *C) {
	j := &RunJobConfig{}
	j.Pull = "false"

	_ = defaults.Set(j)

	c.Assert(j.Pull, Equals, "false")
}

func (s *SuiteConfig) TestJobDefaultsNotSet(c *C) {
	j := &RunJobConfig{}

	_ = defaults.Set(j)

	c.Assert(j.Pull, Equals, "true")
}

func (s *SuiteConfig) TestExecJobBuildEmpty(c *C) {
	j := &ExecJobConfig{}

	c.Assert(j.Middlewares(), HasLen, 0)
}

func (s *SuiteConfig) TestExecJobBuild(c *C) {
	j := &ExecJobConfig{}
	j.OverlapConfig.NoOverlap = true
	j.buildMiddlewares()

	c.Assert(j.Middlewares(), HasLen, 1)
}

func (s *SuiteConfig) TestConfigIni(c *C) {
	testcases := []struct {
		Ini            string
		ExpectedConfig Config
		Comment        string
	}{
		{
			Ini: `
				[job-exec "foo"]
				schedule = @every 10s
				command = echo \"foo\"
				`,
			ExpectedConfig: Config{
				ExecJobs: map[string]*ExecJobConfig{
					"foo": {ExecJob: core.ExecJob{BareJob: core.BareJob{
						Schedule: "@every 10s",
						Command:  `echo \"foo\"`,
					}}},
				},
			},
			Comment: "Test job-exec",
		},
		{
			Ini: `
				[job-run "foo"]
				schedule = @every 10s
				image = alpine
				environment = "KEY1=value1"
				Environment = "KEY2=value2"
				`,
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"foo": {RunJob: core.RunJob{
						BareJob: core.BareJob{
							Schedule: "@every 10s",
						},
						Image:       "alpine",
						Environment: []string{"KEY1=value1", "KEY2=value2"},
					}},
				},
			},
			Comment: "Test job-run with Env Variables",
		},
		{
			Ini: `
                                [job-run "foo"]
                                schedule = @every 10s
                                image = alpine
                                volumes-from = "volume1"
                                volumes-from = "volume2"
                                `,
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"foo": {RunJob: core.RunJob{
						BareJob: core.BareJob{
							Schedule: "@every 10s",
						},
						Image:       "alpine",
						VolumesFrom: []string{"volume1", "volume2"},
					}},
				},
			},
			Comment: "Test job-run with Volumes",
		},
		{
			Ini: `
                                [job-run "foo"]
                                schedule = @every 10s
                                image = alpine
                                entrypoint = ""
                                `,
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"foo": {RunJob: core.RunJob{
						BareJob: core.BareJob{
							Schedule: "@every 10s",
						},
						Image:      "alpine",
						Entrypoint: func() *string { s := ""; return &s }(),
					}},
				},
			},
			Comment: "Test job-run with entrypoint",
		},
	}

	for _, t := range testcases {
		conf, err := BuildFromString(t.Ini, &TestLogger{})
		c.Assert(err, IsNil)

		// Apply defaults to expected config to match the parsed config structure
		expectedWithDefaults := NewConfig(&TestLogger{})
		// Clear both loggers for comparison
		expectedWithDefaults.logger = nil
		conf.logger = nil

		// Copy the expected job maps
		for name, job := range t.ExpectedConfig.ExecJobs {
			expectedWithDefaults.ExecJobs[name] = job
		}
		for name, job := range t.ExpectedConfig.RunJobs {
			expectedWithDefaults.RunJobs[name] = job
		}
		for name, job := range t.ExpectedConfig.ServiceJobs {
			expectedWithDefaults.ServiceJobs[name] = job
		}
		for name, job := range t.ExpectedConfig.LocalJobs {
			expectedWithDefaults.LocalJobs[name] = job
		}
		setJobSource(expectedWithDefaults, JobSourceINI)

		if !c.Check(conf, DeepEquals, expectedWithDefaults) {
			c.Errorf("Test %q\nExpected %s, but got %s", t.Comment, toJSON(expectedWithDefaults), toJSON(conf))
		}
	}
}

func (s *SuiteConfig) TestLabelsConfig(c *C) {
	testcases := []struct {
		Labels         map[string]map[string]string
		ExpectedConfig Config
		Comment        string
	}{
		{
			Labels:         map[string]map[string]string{},
			ExpectedConfig: Config{},
			Comment:        "No labels, no config",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					"label1": "1",
					"label2": "2",
				},
			},
			ExpectedConfig: Config{},
			Comment:        "No required label, no config",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "true",
					"label2":      "2",
				},
			},
			ExpectedConfig: Config{},
			Comment:        "No prefixed labels, no config",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "false",
					labelPrefix + "." + jobLocal + ".job1.schedule": "everyday! yey!",
				},
			},
			ExpectedConfig: Config{},
			Comment:        "With prefixed labels, but without required label still no config",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "true",
					labelPrefix + "." + jobLocal + ".job1.schedule": "everyday! yey!",
					labelPrefix + "." + jobLocal + ".job1.command":  "rm -rf *test*",
					labelPrefix + "." + jobLocal + ".job2.schedule": "everynanosecond! yey!",
					labelPrefix + "." + jobLocal + ".job2.command":  "ls -al *test*",
				},
			},
			ExpectedConfig: Config{},
			Comment:        "No service label, no 'local' jobs",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobLocal + ".job1.schedule":      "schedule1",
					labelPrefix + "." + jobLocal + ".job1.command":       "command1",
					labelPrefix + "." + jobRun + ".job2.schedule":        "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":         "command2",
					labelPrefix + "." + jobServiceRun + ".job3.schedule": "schedule3",
					labelPrefix + "." + jobServiceRun + ".job3.command":  "command3",
				},
				"other": {
					requiredLabel: "true",
					labelPrefix + "." + jobLocal + ".job4.schedule":      "schedule4",
					labelPrefix + "." + jobLocal + ".job4.command":       "command4",
					labelPrefix + "." + jobRun + ".job5.schedule":        "schedule5",
					labelPrefix + "." + jobRun + ".job5.command":         "command5",
					labelPrefix + "." + jobServiceRun + ".job6.schedule": "schedule6",
					labelPrefix + "." + jobServiceRun + ".job6.command":  "command6",
				},
			},
			ExpectedConfig: Config{
				LocalJobs: map[string]*LocalJobConfig{
					"job1": {LocalJob: core.LocalJob{BareJob: core.BareJob{
						Schedule: "schedule1",
						Command:  "command1",
					}}},
				},
				RunJobs: map[string]*RunJobConfig{
					"job2": {RunJob: core.RunJob{BareJob: core.BareJob{
						Schedule: "schedule2",
						Command:  "command2",
					}}},
					"job5": {RunJob: core.RunJob{BareJob: core.BareJob{
						Schedule: "schedule5",
						Command:  "command5",
					}}},
				},
				ServiceJobs: map[string]*RunServiceConfig{
					"job3": {RunServiceJob: core.RunServiceJob{BareJob: core.BareJob{
						Schedule: "schedule3",
						Command:  "command3",
					}}},
				},
			},
			Comment: "Local/Run/Service jobs from non-service container ignored",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobExec + ".job1.schedule": "schedule1",
					labelPrefix + "." + jobExec + ".job1.command":  "command1",
				},
				"other": {
					requiredLabel: "true",
					labelPrefix + "." + jobExec + ".job2.schedule": "schedule2",
					labelPrefix + "." + jobExec + ".job2.command":  "command2",
				},
			},
			ExpectedConfig: Config{
				ExecJobs: map[string]*ExecJobConfig{
					"some.job1": {ExecJob: core.ExecJob{BareJob: core.BareJob{
						Schedule: "schedule1",
						Command:  "command1",
					}}},
					"other.job2": {ExecJob: core.ExecJob{
						BareJob: core.BareJob{
							Schedule: "schedule2",
							Command:  "command2",
						},
						Container: "other",
					}},
				},
			},
			Comment: "Exec jobs from non-service container, saves container name to be able to exect to",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobExec + ".job1.schedule":   "schedule1",
					labelPrefix + "." + jobExec + ".job1.command":    "command1",
					labelPrefix + "." + jobExec + ".job1.no-overlap": "true",
				},
			},
			ExpectedConfig: Config{
				ExecJobs: map[string]*ExecJobConfig{
					"some.job1": {
						ExecJob: core.ExecJob{BareJob: core.BareJob{
							Schedule: "schedule1",
							Command:  "command1",
						}},
						OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
					},
				},
			},
			Comment: "Test job with 'no-overlap' set",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobRun + ".job1.schedule": "schedule1",
					labelPrefix + "." + jobRun + ".job1.command":  "command1",
					labelPrefix + "." + jobRun + ".job1.volume":   "/test/tmp:/test/tmp:ro",
					labelPrefix + "." + jobRun + ".job2.schedule": "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":  "command2",
					labelPrefix + "." + jobRun + ".job2.volume":   `["/test/tmp:/test/tmp:ro", "/test/tmp:/test/tmp:rw"]`,
				},
			},
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"job1": {
						RunJob: core.RunJob{
							BareJob: core.BareJob{
								Schedule: "schedule1",
								Command:  "command1",
							},
							Volume: []string{"/test/tmp:/test/tmp:ro"},
						},
					},
					"job2": {
						RunJob: core.RunJob{
							BareJob: core.BareJob{
								Schedule: "schedule2",
								Command:  "command2",
							},
							Volume: []string{"/test/tmp:/test/tmp:ro", "/test/tmp:/test/tmp:rw"},
						},
					},
				},
			},
			Comment: "Test run job with volumes",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobRun + ".job1.schedule":    "schedule1",
					labelPrefix + "." + jobRun + ".job1.command":     "command1",
					labelPrefix + "." + jobRun + ".job1.environment": "KEY1=value1",
					labelPrefix + "." + jobRun + ".job2.schedule":    "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":     "command2",
					labelPrefix + "." + jobRun + ".job2.environment": `["KEY1=value1", "KEY2=value2"]`,
				},
			},
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"job1": {
						RunJob: core.RunJob{
							BareJob: core.BareJob{
								Schedule: "schedule1",
								Command:  "command1",
							},
							Environment: []string{"KEY1=value1"},
						},
					},
					"job2": {
						RunJob: core.RunJob{
							BareJob: core.BareJob{
								Schedule: "schedule2",
								Command:  "command2",
							},
							Environment: []string{"KEY1=value1", "KEY2=value2"},
						},
					},
				},
			},
			Comment: "Test run job with environment variables",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobRun + ".job1.schedule":     "schedule1",
					labelPrefix + "." + jobRun + ".job1.command":      "command1",
					labelPrefix + "." + jobRun + ".job1.volumes-from": "test123",
					labelPrefix + "." + jobRun + ".job2.schedule":     "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":      "command2",
					labelPrefix + "." + jobRun + ".job2.volumes-from": `["test321", "test456"]`,
				},
			},
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"job1": {
						RunJob: core.RunJob{
							BareJob: core.BareJob{
								Schedule: "schedule1",
								Command:  "command1",
							},
							VolumesFrom: []string{"test123"},
						},
					},
					"job2": {
						RunJob: core.RunJob{
							BareJob: core.BareJob{
								Schedule: "schedule2",
								Command:  "command2",
							},
							VolumesFrom: []string{"test321", "test456"},
						},
					},
				},
			},
			Comment: "Test run job with volumes-from",
		},
		{
			Labels: map[string]map[string]string{
				"some": {
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobRun + ".job1.schedule":   "schedule1",
					labelPrefix + "." + jobRun + ".job1.entrypoint": "",
				},
			},
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"job1": {
						RunJob: core.RunJob{
							BareJob:    core.BareJob{Schedule: "schedule1"},
							Entrypoint: func() *string { s := ""; return &s }(),
						},
					},
				},
			},
			Comment: "Test run job with entrypoint override",
		},
	}

	for _, t := range testcases {
		conf := Config{}
		conf.logger = test.NewTestLogger()         // Initialize logger for tests
		conf.Global.AllowHostJobsFromLabels = true // Enable local jobs from labels for testing
		err := conf.buildFromDockerLabels(t.Labels)
		c.Assert(err, IsNil)
		setJobSource(&conf, JobSourceLabel)
		setJobSource(&t.ExpectedConfig, JobSourceLabel)

		// Clear logger for comparison to avoid message count mismatches
		actualLogger := conf.logger
		conf.logger = nil
		t.ExpectedConfig.logger = nil

		// Set the same security flag on expected config
		t.ExpectedConfig.Global.AllowHostJobsFromLabels = true

		if !c.Check(conf, DeepEquals, t.ExpectedConfig) {
			c.Errorf("Test %q\nExpected %s, but got %s", t.Comment, toJSON(t.ExpectedConfig), toJSON(conf))
		}

		// Restore logger
		conf.logger = actualLogger
	}
}

func toJSON(val interface{}) string {
	b, _ := json.MarshalIndent(val, "", "  ")
	return string(b)
}

// Test for BuildFromString error path
func (s *SuiteConfig) TestBuildFromStringError(c *C) {
	_, err := BuildFromString("[invalid", &TestLogger{})
	c.Assert(err, NotNil)
}

// Test for BuildFromFile success path
func (s *SuiteConfig) TestBuildFromFile(c *C) {
	// Create temporary config file
	tmpFile, err := os.CreateTemp("", "ofelia_test_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmpFile.Name())

	content := `
[ job-run "foo" ]
schedule = @every 5s
image = alpine
command = echo test123
`
	_, _ = tmpFile.WriteString(content)
	err = tmpFile.Close()
	c.Assert(err, IsNil)

	conf, err := BuildFromFile(tmpFile.Name(), &TestLogger{})
	c.Assert(err, IsNil)
	// Verify parsed values
	c.Assert(conf.RunJobs, HasLen, 1)
	job, ok := conf.RunJobs["foo"]
	c.Assert(ok, Equals, true)
	c.Assert(job.Schedule, Equals, "@every 5s")
	c.Assert(job.Command, Equals, "echo test123")
}

func (s *SuiteConfig) TestBuildFromFileGlob(c *C) {
	dir, err := os.MkdirTemp("", "ofelia_glob")
	c.Assert(err, IsNil)
	defer os.RemoveAll(dir)

	file1 := filepath.Join(dir, "a.ini")
	err = os.WriteFile(file1, []byte("[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\n"), 0o644)
	c.Assert(err, IsNil)

	file2 := filepath.Join(dir, "b.ini")
	err = os.WriteFile(file2, []byte("[job-exec \"bar\"]\nschedule = @every 10s\ncommand = echo bar\n"), 0o644)
	c.Assert(err, IsNil)

	conf, err := BuildFromFile(filepath.Join(dir, "*.ini"), &TestLogger{})
	c.Assert(err, IsNil)
	c.Assert(conf.RunJobs, HasLen, 1)
	_, ok := conf.RunJobs["foo"]
	c.Assert(ok, Equals, true)
	c.Assert(conf.ExecJobs, HasLen, 1)
	_, ok = conf.ExecJobs["bar"]
	c.Assert(ok, Equals, true)
}

// Test NewConfig initializes empty maps and applies defaults
func (s *SuiteConfig) TestNewConfig(c *C) {
	cfg := NewConfig(&TestLogger{})
	c.Assert(cfg.ExecJobs, NotNil)
	c.Assert(cfg.RunJobs, NotNil)
	c.Assert(cfg.ServiceJobs, NotNil)
	c.Assert(cfg.LocalJobs, NotNil)
	c.Assert(len(cfg.ExecJobs), Equals, 0)
	c.Assert(len(cfg.RunJobs), Equals, 0)
	c.Assert(len(cfg.ServiceJobs), Equals, 0)
	c.Assert(len(cfg.LocalJobs), Equals, 0)
}

// Test buildSchedulerMiddlewares adds only non-empty middlewares
func (s *SuiteConfig) TestBuildSchedulerMiddlewares(c *C) {
	// Prepare config with non-empty global middleware settings
	cfg := Config{}
	cfg.Global.SlackConfig.SlackWebhook = "http://example.com/webhook"
	cfg.Global.SaveConfig.SaveFolder = "/tmp"
	cfg.Global.MailConfig.EmailTo = "to@example.com"
	cfg.Global.MailConfig.EmailFrom = "from@example.com"

	sh := core.NewScheduler(&TestLogger{})
	cfg.buildSchedulerMiddlewares(sh)
	ms := sh.Middlewares()
	c.Assert(ms, HasLen, 3)
	// Assert types of middlewares
	_, ok := ms[0].(*middlewares.Slack)
	c.Assert(ok, Equals, true)
	_, ok = ms[1].(*middlewares.Save)
	c.Assert(ok, Equals, true)
	_, ok = ms[2].(*middlewares.Mail)
	c.Assert(ok, Equals, true)
}

// Test DefaultUser global configuration
func (s *SuiteConfig) TestDefaultUserGlobalConfig(c *C) {
	mockLogger := TestLogger{}

	// Test 1: Default value should be "nobody"
	cfg, err := BuildFromString(`
		[job-exec "test"]
		schedule = @every 10s
		container = test-container
		command = echo test
	`, &mockLogger)
	c.Assert(err, IsNil)
	c.Assert(cfg.Global.DefaultUser, Equals, "nobody")

	// Test 2: Global default-user can be changed
	cfg, err = BuildFromString(`
		[global]
		default-user = root

		[job-exec "test"]
		schedule = @every 10s
		container = test-container
		command = echo test
	`, &mockLogger)
	c.Assert(err, IsNil)
	c.Assert(cfg.Global.DefaultUser, Equals, "root")

	// Test 3: Empty string global default-user (use container's default)
	cfg, err = BuildFromString(`
		[global]
		default-user =

		[job-exec "test"]
		schedule = @every 10s
		container = test-container
		command = echo test
	`, &mockLogger)
	c.Assert(err, IsNil)
	c.Assert(cfg.Global.DefaultUser, Equals, "")
}

// Test applyDefaultUser function
func (s *SuiteConfig) TestApplyDefaultUser(c *C) {
	cfg := NewConfig(&TestLogger{})
	cfg.Global.DefaultUser = "testuser"

	// Test 1: Empty user gets global default
	user := ""
	cfg.applyDefaultUser(&user)
	c.Assert(user, Equals, "testuser")

	// Test 2: Explicit user is preserved
	user = "specificuser"
	cfg.applyDefaultUser(&user)
	c.Assert(user, Equals, "specificuser")

	// Test 3: When global default is empty, user stays empty
	cfg.Global.DefaultUser = ""
	user = ""
	cfg.applyDefaultUser(&user)
	c.Assert(user, Equals, "")

	// Test 4: Sentinel value "default" overrides global to use container's default
	cfg.Global.DefaultUser = "nobody"
	user = UserContainerDefault
	cfg.applyDefaultUser(&user)
	c.Assert(user, Equals, "") // Should be empty (container's default)
}

// TestMergeMailDefaults tests that job-level mail configs inherit global SMTP settings
// This tests the fix for issue #330 (upstream #124) where partial mail config overrides
// would lose SMTP connection details.
func (s *SuiteConfig) TestMergeMailDefaults(c *C) {
	cfg := NewConfig(&TestLogger{})

	// Set global mail configuration
	cfg.Global.MailConfig.SMTPHost = "smtp.example.com"
	cfg.Global.MailConfig.SMTPPort = 587
	cfg.Global.MailConfig.SMTPUser = "globaluser"
	cfg.Global.MailConfig.SMTPPassword = "globalpwd"
	cfg.Global.MailConfig.SMTPTLSSkipVerify = true
	cfg.Global.MailConfig.EmailTo = "global@example.com"
	cfg.Global.MailConfig.EmailFrom = "sender@example.com"
	cfg.Global.MailConfig.MailOnlyOnError = false

	// Test 1: Job with only mail-only-on-error inherits all SMTP settings
	jobMail := middlewares.MailConfig{
		MailOnlyOnError: true, // Only this is set by user
	}
	cfg.mergeMailDefaults(&jobMail)

	c.Assert(jobMail.SMTPHost, Equals, "smtp.example.com")
	c.Assert(jobMail.SMTPPort, Equals, 587)
	c.Assert(jobMail.SMTPUser, Equals, "globaluser")
	c.Assert(jobMail.SMTPPassword, Equals, "globalpwd")
	c.Assert(jobMail.SMTPTLSSkipVerify, Equals, true)
	c.Assert(jobMail.EmailTo, Equals, "global@example.com")
	c.Assert(jobMail.EmailFrom, Equals, "sender@example.com")
	c.Assert(jobMail.MailOnlyOnError, Equals, true) // Preserved from job

	// Test 2: Job with explicit values are preserved (no override)
	jobMail2 := middlewares.MailConfig{
		SMTPHost:        "job-smtp.example.com",
		SMTPPort:        465,
		EmailTo:         "job@example.com",
		MailOnlyOnError: true,
	}
	cfg.mergeMailDefaults(&jobMail2)

	c.Assert(jobMail2.SMTPHost, Equals, "job-smtp.example.com") // Job value preserved
	c.Assert(jobMail2.SMTPPort, Equals, 465)                    // Job value preserved
	c.Assert(jobMail2.SMTPUser, Equals, "globaluser")           // Inherited from global
	c.Assert(jobMail2.SMTPPassword, Equals, "globalpwd")        // Inherited from global
	c.Assert(jobMail2.EmailTo, Equals, "job@example.com")       // Job value preserved
	c.Assert(jobMail2.EmailFrom, Equals, "sender@example.com")  // Inherited from global

	// Test 3: Empty global config doesn't cause issues
	cfgEmpty := NewConfig(&TestLogger{})
	jobMail3 := middlewares.MailConfig{
		SMTPHost: "job-only.example.com",
		SMTPPort: 25,
	}
	cfgEmpty.mergeMailDefaults(&jobMail3)

	c.Assert(jobMail3.SMTPHost, Equals, "job-only.example.com") // Job value preserved
	c.Assert(jobMail3.SMTPPort, Equals, 25)                     // Job value preserved
	c.Assert(jobMail3.SMTPUser, Equals, "")                     // No global to inherit
}

// TestMergeSlackDefaults tests that job-level slack configs inherit global webhook
func (s *SuiteConfig) TestMergeSlackDefaults(c *C) {
	cfg := NewConfig(&TestLogger{})
	cfg.Global.SlackConfig.SlackWebhook = "https://hooks.slack.com/services/global"
	cfg.Global.SlackConfig.SlackOnlyOnError = false

	// Test 1: Job with only slack-only-on-error inherits webhook
	jobSlack := middlewares.SlackConfig{
		SlackOnlyOnError: true,
	}
	cfg.mergeSlackDefaults(&jobSlack)

	c.Assert(jobSlack.SlackWebhook, Equals, "https://hooks.slack.com/services/global")
	c.Assert(jobSlack.SlackOnlyOnError, Equals, true) // Job value preserved

	// Test 2: Job with explicit webhook preserves it
	jobSlack2 := middlewares.SlackConfig{
		SlackWebhook: "https://hooks.slack.com/services/job-specific",
	}
	cfg.mergeSlackDefaults(&jobSlack2)

	c.Assert(jobSlack2.SlackWebhook, Equals, "https://hooks.slack.com/services/job-specific")
}

// TestMergeMailDefaults_BoolFieldLimitation documents the Go bool field inheritance limitation.
// Since we cannot distinguish "not set" from "explicitly false", the behavior is asymmetric:
// - Global=true, Job=false → Job inherits true (insecure setting propagates)
// - Global=false, Job=true → Job keeps true (secure global CANNOT force secure on job)
// This is acceptable since per-job security settings should be explicit.
func (s *SuiteConfig) TestMergeMailDefaults_BoolFieldLimitation(c *C) {
	// Case 1: Global smtp-tls-skip-verify=true propagates to job with default (false)
	cfg1 := NewConfig(&TestLogger{})
	cfg1.Global.MailConfig.SMTPTLSSkipVerify = true
	jobMail1 := middlewares.MailConfig{SMTPHost: "mail.example.com"}
	cfg1.mergeMailDefaults(&jobMail1)
	c.Assert(jobMail1.SMTPTLSSkipVerify, Equals, true,
		Commentf("Global skip-verify=true should propagate to job"))

	// Case 2: Global smtp-tls-skip-verify=false does NOT override job with true
	// This is the documented limitation - we cannot force TLS verification
	cfg2 := NewConfig(&TestLogger{})
	cfg2.Global.MailConfig.SMTPTLSSkipVerify = false
	jobMail2 := middlewares.MailConfig{
		SMTPHost:          "mail.example.com",
		SMTPTLSSkipVerify: true, // Job explicitly skips verification
	}
	cfg2.mergeMailDefaults(&jobMail2)
	c.Assert(jobMail2.SMTPTLSSkipVerify, Equals, true,
		Commentf("Job skip-verify=true should NOT be overridden by global false (Go bool limitation)"))

	// Case 3: Both false - secure default maintained
	cfg3 := NewConfig(&TestLogger{})
	cfg3.Global.MailConfig.SMTPTLSSkipVerify = false
	jobMail3 := middlewares.MailConfig{SMTPHost: "mail.example.com"}
	cfg3.mergeMailDefaults(&jobMail3)
	c.Assert(jobMail3.SMTPTLSSkipVerify, Equals, false,
		Commentf("Both false - secure default should be maintained"))

	// Case 4: Both true - insecure settings preserved
	cfg4 := NewConfig(&TestLogger{})
	cfg4.Global.MailConfig.SMTPTLSSkipVerify = true
	jobMail4 := middlewares.MailConfig{
		SMTPHost:          "mail.example.com",
		SMTPTLSSkipVerify: true,
	}
	cfg4.mergeMailDefaults(&jobMail4)
	c.Assert(jobMail4.SMTPTLSSkipVerify, Equals, true,
		Commentf("Both true - insecure setting should be preserved"))
}
