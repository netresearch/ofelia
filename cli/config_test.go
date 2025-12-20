package cli

import (
	"os"
	"path/filepath"
	"testing"

	defaults "github.com/creasty/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

type TestLogger = test.Logger

func TestBuildFromString(t *testing.T) {
	t.Parallel()

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

	require.NoError(t, err)
}

func TestJobDefaultsSet(t *testing.T) {
	t.Parallel()

	j := &RunJobConfig{}
	j.Pull = "false"

	_ = defaults.Set(j)

	assert.Equal(t, "false", j.Pull)
}

func TestJobDefaultsNotSet(t *testing.T) {
	t.Parallel()

	j := &RunJobConfig{}

	_ = defaults.Set(j)

	assert.Equal(t, "true", j.Pull)
}

func TestExecJobBuildEmpty(t *testing.T) {
	t.Parallel()

	j := &ExecJobConfig{}

	assert.Empty(t, j.Middlewares())
}

func TestExecJobBuild(t *testing.T) {
	t.Parallel()

	j := &ExecJobConfig{}
	j.OverlapConfig.NoOverlap = true
	j.buildMiddlewares(nil)

	assert.Len(t, j.Middlewares(), 1)
}

func TestConfigIni(t *testing.T) {
	t.Parallel()

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

	for _, tc := range testcases {
		t.Run(tc.Comment, func(t *testing.T) {
			conf, err := BuildFromString(tc.Ini, &TestLogger{})
			require.NoError(t, err)

			expectedWithDefaults := NewConfig(&TestLogger{})
			expectedWithDefaults.logger = nil
			conf.logger = nil

			for name, job := range tc.ExpectedConfig.ExecJobs {
				expectedWithDefaults.ExecJobs[name] = job
			}
			for name, job := range tc.ExpectedConfig.RunJobs {
				expectedWithDefaults.RunJobs[name] = job
			}
			for name, job := range tc.ExpectedConfig.ServiceJobs {
				expectedWithDefaults.ServiceJobs[name] = job
			}
			for name, job := range tc.ExpectedConfig.LocalJobs {
				expectedWithDefaults.LocalJobs[name] = job
			}
			setJobSource(expectedWithDefaults, JobSourceINI)

			assert.Equal(t, expectedWithDefaults, conf, "Test %q failed", tc.Comment)
		})
	}
}

func TestLabelsConfig(t *testing.T) {
	t.Parallel()

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

	for _, tc := range testcases {
		t.Run(tc.Comment, func(t *testing.T) {
			conf := Config{}
			conf.logger = test.NewTestLogger()
			conf.Global.AllowHostJobsFromLabels = true
			err := conf.buildFromDockerLabels(tc.Labels)
			require.NoError(t, err)
			setJobSource(&conf, JobSourceLabel)
			setJobSource(&tc.ExpectedConfig, JobSourceLabel)

			conf.logger = nil
			tc.ExpectedConfig.logger = nil
			tc.ExpectedConfig.Global.AllowHostJobsFromLabels = true

			assert.Equal(t, tc.ExpectedConfig, conf, "Test %q failed", tc.Comment)
		})
	}
}

func TestBuildFromStringError(t *testing.T) {
	t.Parallel()

	_, err := BuildFromString("[invalid", &TestLogger{})
	assert.Error(t, err)
}

func TestBuildFromFile(t *testing.T) {
	t.Parallel()

	tmpFile, err := os.CreateTemp("", "ofelia_test_*.ini")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	content := `
[ job-run "foo" ]
schedule = @every 5s
image = alpine
command = echo test123
`
	_, _ = tmpFile.WriteString(content)
	err = tmpFile.Close()
	require.NoError(t, err)

	conf, err := BuildFromFile(tmpFile.Name(), &TestLogger{})
	require.NoError(t, err)
	assert.Len(t, conf.RunJobs, 1)
	job, ok := conf.RunJobs["foo"]
	assert.True(t, ok)
	assert.Equal(t, "@every 5s", job.Schedule)
	assert.Equal(t, "echo test123", job.Command)
}

func TestBuildFromFileGlob(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "ofelia_glob")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	file1 := filepath.Join(dir, "a.ini")
	err = os.WriteFile(file1, []byte("[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\n"), 0o644)
	require.NoError(t, err)

	file2 := filepath.Join(dir, "b.ini")
	err = os.WriteFile(file2, []byte("[job-exec \"bar\"]\nschedule = @every 10s\ncommand = echo bar\n"), 0o644)
	require.NoError(t, err)

	conf, err := BuildFromFile(filepath.Join(dir, "*.ini"), &TestLogger{})
	require.NoError(t, err)
	assert.Len(t, conf.RunJobs, 1)
	_, ok := conf.RunJobs["foo"]
	assert.True(t, ok)
	assert.Len(t, conf.ExecJobs, 1)
	_, ok = conf.ExecJobs["bar"]
	assert.True(t, ok)
}

func TestNewConfig(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(&TestLogger{})
	assert.NotNil(t, cfg.ExecJobs)
	assert.NotNil(t, cfg.RunJobs)
	assert.NotNil(t, cfg.ServiceJobs)
	assert.NotNil(t, cfg.LocalJobs)
	assert.Empty(t, cfg.ExecJobs)
	assert.Empty(t, cfg.RunJobs)
	assert.Empty(t, cfg.ServiceJobs)
	assert.Empty(t, cfg.LocalJobs)
}

func TestBuildSchedulerMiddlewares(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	cfg.Global.SlackConfig.SlackWebhook = "http://example.com/webhook"
	cfg.Global.SaveConfig.SaveFolder = "/tmp"
	cfg.Global.MailConfig.EmailTo = "to@example.com"
	cfg.Global.MailConfig.EmailFrom = "from@example.com"

	sh := core.NewScheduler(&TestLogger{})
	cfg.buildSchedulerMiddlewares(sh)
	ms := sh.Middlewares()
	assert.Len(t, ms, 3)
	_, ok := ms[0].(*middlewares.Slack)
	assert.True(t, ok)
	_, ok = ms[1].(*middlewares.Save)
	assert.True(t, ok)
	_, ok = ms[2].(*middlewares.Mail)
	assert.True(t, ok)
}

func TestDefaultUserGlobalConfig(t *testing.T) {
	t.Parallel()

	mockLogger := TestLogger{}

	cfg, err := BuildFromString(`
		[job-exec "test"]
		schedule = @every 10s
		container = test-container
		command = echo test
	`, &mockLogger)
	require.NoError(t, err)
	assert.Equal(t, "nobody", cfg.Global.DefaultUser)

	cfg, err = BuildFromString(`
		[global]
		default-user = root

		[job-exec "test"]
		schedule = @every 10s
		container = test-container
		command = echo test
	`, &mockLogger)
	require.NoError(t, err)
	assert.Equal(t, "root", cfg.Global.DefaultUser)

	cfg, err = BuildFromString(`
		[global]
		default-user =

		[job-exec "test"]
		schedule = @every 10s
		container = test-container
		command = echo test
	`, &mockLogger)
	require.NoError(t, err)
	assert.Empty(t, cfg.Global.DefaultUser)
}

func TestApplyDefaultUser(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(&TestLogger{})
	cfg.Global.DefaultUser = "testuser"

	user := ""
	cfg.applyDefaultUser(&user)
	assert.Equal(t, "testuser", user)

	user = "specificuser"
	cfg.applyDefaultUser(&user)
	assert.Equal(t, "specificuser", user)

	cfg.Global.DefaultUser = ""
	user = ""
	cfg.applyDefaultUser(&user)
	assert.Empty(t, user)

	cfg.Global.DefaultUser = "nobody"
	user = UserContainerDefault
	cfg.applyDefaultUser(&user)
	assert.Empty(t, user)
}

func TestMergeMailDefaults(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(&TestLogger{})
	cfg.Global.MailConfig.SMTPHost = "smtp.example.com"
	cfg.Global.MailConfig.SMTPPort = 587
	cfg.Global.MailConfig.SMTPUser = "globaluser"
	cfg.Global.MailConfig.SMTPPassword = "globalpwd"
	cfg.Global.MailConfig.SMTPTLSSkipVerify = true
	cfg.Global.MailConfig.EmailTo = "global@example.com"
	cfg.Global.MailConfig.EmailFrom = "sender@example.com"
	cfg.Global.MailConfig.MailOnlyOnError = false

	jobMail := middlewares.MailConfig{
		MailOnlyOnError: true,
	}
	cfg.mergeMailDefaults(&jobMail)

	assert.Equal(t, "smtp.example.com", jobMail.SMTPHost)
	assert.Equal(t, 587, jobMail.SMTPPort)
	assert.Equal(t, "globaluser", jobMail.SMTPUser)
	assert.Equal(t, "globalpwd", jobMail.SMTPPassword)
	assert.True(t, jobMail.SMTPTLSSkipVerify)
	assert.Equal(t, "global@example.com", jobMail.EmailTo)
	assert.Equal(t, "sender@example.com", jobMail.EmailFrom)
	assert.True(t, jobMail.MailOnlyOnError)

	jobMail2 := middlewares.MailConfig{
		SMTPHost:        "job-smtp.example.com",
		SMTPPort:        465,
		EmailTo:         "job@example.com",
		MailOnlyOnError: true,
	}
	cfg.mergeMailDefaults(&jobMail2)

	assert.Equal(t, "job-smtp.example.com", jobMail2.SMTPHost)
	assert.Equal(t, 465, jobMail2.SMTPPort)
	assert.Equal(t, "globaluser", jobMail2.SMTPUser)
	assert.Equal(t, "globalpwd", jobMail2.SMTPPassword)
	assert.Equal(t, "job@example.com", jobMail2.EmailTo)
	assert.Equal(t, "sender@example.com", jobMail2.EmailFrom)

	cfgEmpty := NewConfig(&TestLogger{})
	jobMail3 := middlewares.MailConfig{
		SMTPHost: "job-only.example.com",
		SMTPPort: 25,
	}
	cfgEmpty.mergeMailDefaults(&jobMail3)

	assert.Equal(t, "job-only.example.com", jobMail3.SMTPHost)
	assert.Equal(t, 25, jobMail3.SMTPPort)
	assert.Empty(t, jobMail3.SMTPUser)
}

func TestMergeSlackDefaults(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(&TestLogger{})
	cfg.Global.SlackConfig.SlackWebhook = "https://hooks.slack.com/services/global"
	cfg.Global.SlackConfig.SlackOnlyOnError = false

	jobSlack := middlewares.SlackConfig{
		SlackOnlyOnError: true,
	}
	cfg.mergeSlackDefaults(&jobSlack)

	assert.Equal(t, "https://hooks.slack.com/services/global", jobSlack.SlackWebhook)
	assert.True(t, jobSlack.SlackOnlyOnError)

	jobSlack2 := middlewares.SlackConfig{
		SlackWebhook: "https://hooks.slack.com/services/job-specific",
	}
	cfg.mergeSlackDefaults(&jobSlack2)

	assert.Equal(t, "https://hooks.slack.com/services/job-specific", jobSlack2.SlackWebhook)
}

func TestMergeMailDefaultsBoolFieldLimitation(t *testing.T) {
	t.Parallel()

	cfg1 := NewConfig(&TestLogger{})
	cfg1.Global.MailConfig.SMTPTLSSkipVerify = true
	jobMail1 := middlewares.MailConfig{SMTPHost: "mail.example.com"}
	cfg1.mergeMailDefaults(&jobMail1)
	assert.True(t, jobMail1.SMTPTLSSkipVerify, "Global skip-verify=true should propagate to job")

	cfg2 := NewConfig(&TestLogger{})
	cfg2.Global.MailConfig.SMTPTLSSkipVerify = false
	jobMail2 := middlewares.MailConfig{
		SMTPHost:          "mail.example.com",
		SMTPTLSSkipVerify: true,
	}
	cfg2.mergeMailDefaults(&jobMail2)
	assert.True(t, jobMail2.SMTPTLSSkipVerify, "Job skip-verify=true should NOT be overridden by global false")

	cfg3 := NewConfig(&TestLogger{})
	cfg3.Global.MailConfig.SMTPTLSSkipVerify = false
	jobMail3 := middlewares.MailConfig{SMTPHost: "mail.example.com"}
	cfg3.mergeMailDefaults(&jobMail3)
	assert.False(t, jobMail3.SMTPTLSSkipVerify, "Both false - secure default should be maintained")

	cfg4 := NewConfig(&TestLogger{})
	cfg4.Global.MailConfig.SMTPTLSSkipVerify = true
	jobMail4 := middlewares.MailConfig{
		SMTPHost:          "mail.example.com",
		SMTPTLSSkipVerify: true,
	}
	cfg4.mergeMailDefaults(&jobMail4)
	assert.True(t, jobMail4.SMTPTLSSkipVerify, "Both true - insecure setting should be preserved")
}
