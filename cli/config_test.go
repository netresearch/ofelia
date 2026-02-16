package cli

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	defaults "github.com/creasty/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

func TestBuildFromString(t *testing.T) {
	t.Parallel()

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
  `, test.NewTestLogger())

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
			conf, err := BuildFromString(tc.Ini, test.NewTestLogger())
			require.NoError(t, err)

			expectedWithDefaults := NewConfig(test.NewTestLogger())
			expectedWithDefaults.logger = nil
			conf.logger = nil

			maps.Copy(expectedWithDefaults.ExecJobs, tc.ExpectedConfig.ExecJobs)
			maps.Copy(expectedWithDefaults.RunJobs, tc.ExpectedConfig.RunJobs)
			maps.Copy(expectedWithDefaults.ServiceJobs, tc.ExpectedConfig.ServiceJobs)
			maps.Copy(expectedWithDefaults.LocalJobs, tc.ExpectedConfig.LocalJobs)
			setJobSource(expectedWithDefaults, JobSourceINI)

			assert.Equal(t, expectedWithDefaults, conf, "Test %q failed", tc.Comment)
		})
	}
}

func buildSingleContainer(container DockerContainerInfo, labels map[string]string) DockerContainerInfo {
	result := DockerContainerInfo{
		Name:    container.Name,
		Created: container.Created,
		State:   container.State,
		Labels:  labels,
	}
	return result
}

func buildContainers(container DockerContainerInfo, labels map[string]string) []DockerContainerInfo {
	result := []DockerContainerInfo{buildSingleContainer(container, labels)}
	return result
}

func buildTwoContainers(container1 DockerContainerInfo, labels1 map[string]string, container2 DockerContainerInfo, labels2 map[string]string) []DockerContainerInfo {
	result := []DockerContainerInfo{buildSingleContainer(container1, labels1), buildSingleContainer(container2, labels2)}
	return result
}

func TestLabelsConfig(t *testing.T) {
	t.Parallel()

	someContainerInfo := DockerContainerInfo{
		Name:  "some",
		State: domain.ContainerState{Running: true},
	}
	otherContainerInfo := DockerContainerInfo{
		Name:  "other",
		State: domain.ContainerState{Running: true},
	}
	notRunningContainerInfo := DockerContainerInfo{
		Name:    "not-running",
		Created: time.Now().Add(-time.Minute * 2),
		State:   domain.ContainerState{Running: false},
	}
	otherNotRunningContainerInfo := DockerContainerInfo{
		Name:    "other-not-running",
		Created: time.Now().Add(-time.Minute * 3), // Older than notRunningContainerInfo
		State:   domain.ContainerState{Running: false},
	}

	testcases := []struct {
		Containers     []DockerContainerInfo
		ExpectedConfig Config
		Comment        string
	}{
		{
			Containers:     []DockerContainerInfo{},
			ExpectedConfig: Config{},
			Comment:        "No containers, no config",
		},
		{
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					"label1": "1",
					"label2": "2",
				},
			),
			ExpectedConfig: Config{},
			Comment:        "No required label, no config",
		},
		{
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					"label2":      "2",
				},
			),
			ExpectedConfig: Config{},
			Comment:        "No prefixed labels, no config",
		},
		{
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "false",
					labelPrefix + "." + jobLocal + ".job1.schedule": "everyday! yey!",
				},
			),
			ExpectedConfig: Config{},
			Comment:        "With prefixed labels, but without required label still no config",
		},
		{
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobLocal + ".job1.schedule": "everyday! yey!",
					labelPrefix + "." + jobLocal + ".job1.command":  "rm -rf *test*",
					labelPrefix + "." + jobLocal + ".job2.schedule": "everynanosecond! yey!",
					labelPrefix + "." + jobLocal + ".job2.command":  "ls -al *test*",
				},
			),
			ExpectedConfig: Config{},
			Comment:        "No service label, no 'local' jobs",
		},
		{
			Containers: buildTwoContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobLocal + ".job1.schedule":      "schedule1",
					labelPrefix + "." + jobLocal + ".job1.command":       "command1",
					labelPrefix + "." + jobRun + ".job2.schedule":        "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":         "command2",
					labelPrefix + "." + jobServiceRun + ".job3.schedule": "schedule3",
					labelPrefix + "." + jobServiceRun + ".job3.command":  "command3",
				},
				otherContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobLocal + ".job4.schedule":      "schedule4",
					labelPrefix + "." + jobLocal + ".job4.command":       "command4",
					labelPrefix + "." + jobRun + ".job5.schedule":        "schedule5",
					labelPrefix + "." + jobRun + ".job5.command":         "command5",
					labelPrefix + "." + jobServiceRun + ".job6.schedule": "schedule6",
					labelPrefix + "." + jobServiceRun + ".job6.command":  "command6",
				},
			),
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
					}, Container: "other"}},
				},
				ServiceJobs: map[string]*RunServiceConfig{
					"job3": {RunServiceJob: core.RunServiceJob{BareJob: core.BareJob{
						Schedule: "schedule3",
						Command:  "command3",
					}}},
				},
			},
			Comment: "Local/Service jobs from non-service container ignored",
		},
		{
			Containers: buildTwoContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobExec + ".job1.schedule": "schedule1",
					labelPrefix + "." + jobExec + ".job1.command":  "command1",
				},
				otherContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobExec + ".job2.schedule": "schedule2",
					labelPrefix + "." + jobExec + ".job2.command":  "command2",
				},
			),
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
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobExec + ".job1.schedule":   "schedule1",
					labelPrefix + "." + jobExec + ".job1.command":    "command1",
					labelPrefix + "." + jobExec + ".job1.no-overlap": "true",
				},
			),
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
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobRun + ".job1.schedule": "schedule1",
					labelPrefix + "." + jobRun + ".job1.command":  "command1",
					labelPrefix + "." + jobRun + ".job1.volume":   "/test/tmp:/test/tmp:ro",
					labelPrefix + "." + jobRun + ".job2.schedule": "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":  "command2",
					labelPrefix + "." + jobRun + ".job2.volume":   `["/test/tmp:/test/tmp:ro", "/test/tmp:/test/tmp:rw"]`,
				},
			),
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
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobRun + ".job1.schedule":    "schedule1",
					labelPrefix + "." + jobRun + ".job1.command":     "command1",
					labelPrefix + "." + jobRun + ".job1.environment": "KEY1=value1",
					labelPrefix + "." + jobRun + ".job2.schedule":    "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":     "command2",
					labelPrefix + "." + jobRun + ".job2.environment": `["KEY1=value1", "KEY2=value2"]`,
				},
			),
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
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobRun + ".job1.schedule":     "schedule1",
					labelPrefix + "." + jobRun + ".job1.command":      "command1",
					labelPrefix + "." + jobRun + ".job1.volumes-from": "test123",
					labelPrefix + "." + jobRun + ".job2.schedule":     "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":      "command2",
					labelPrefix + "." + jobRun + ".job2.volumes-from": `["test321", "test456"]`,
				},
			),
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
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobRun + ".job1.schedule":   "schedule1",
					labelPrefix + "." + jobRun + ".job1.entrypoint": "",
				},
			),
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
		{
			Containers: buildContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobRun + ".job1.schedule": "schedule1",
					labelPrefix + "." + jobRun + ".job1.command":  "command1",
				},
			),
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"job1": {RunJob: core.RunJob{BareJob: core.BareJob{
						Schedule: "schedule1",
						Command:  "command1",
					}, Container: someContainerInfo.Name}},
				},
			},
			Comment: "Run jobs from non-service container are not ignored",
		},
		{
			Containers: buildTwoContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobRun + ".job1.schedule":  "schedule1",
					labelPrefix + "." + jobRun + ".job1.command":   "command1",
					labelPrefix + "." + jobRun + ".job1.container": "not-some-container",
				},
				otherContainerInfo, map[string]string{
					requiredLabel: "true",
					serviceLabel:  "true",
					labelPrefix + "." + jobRun + ".job2.schedule":  "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":   "command2",
					labelPrefix + "." + jobRun + ".job2.container": "another-one",
				},
			),
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"job1": {RunJob: core.RunJob{BareJob: core.BareJob{
						Schedule: "schedule1",
						Command:  "command1",
					}, Container: "not-some-container"}},
					"job2": {RunJob: core.RunJob{BareJob: core.BareJob{
						Schedule: "schedule2",
						Command:  "command2",
					}, Container: "another-one"}},
				},
			},
			Comment: "Run jobs from non-service container respect the specified container name",
		},
		{
			Containers: buildContainers(
				notRunningContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobExec + ".job1.schedule":        "schedule1",
					labelPrefix + "." + jobExec + ".job1.command":         "command1",
					labelPrefix + "." + jobExec + ".job1.container":       "not-some-container",
					labelPrefix + "." + jobRun + ".job2.schedule":         "schedule2",
					labelPrefix + "." + jobRun + ".job2.command":          "command2",
					labelPrefix + "." + jobServiceRun + ".job3.schedule":  "schedule3",
					labelPrefix + "." + jobServiceRun + ".job3.command":   "command3",
					labelPrefix + "." + jobServiceRun + ".job3.container": "another-one",
					labelPrefix + "." + jobLocal + ".job4.schedule":       "schedule4",
					labelPrefix + "." + jobLocal + ".job4.command":        "command4",
					labelPrefix + "." + jobLocal + ".job4.container":      "another-one",
				},
			),
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"job2": {RunJob: core.RunJob{BareJob: core.BareJob{
						Schedule: "schedule2",
						Command:  "command2",
					}, Container: "not-running"}},
				},
			},
			Comment: "Only run jobs are allowed on non-running containers",
		},
		{
			Containers: buildTwoContainers(
				someContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobRun + ".job1.schedule": "running-schedule",
					labelPrefix + "." + jobRun + ".job1.command":  "running-command",
				},
				notRunningContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobRun + ".job1.schedule": "stopped-schedule",
					labelPrefix + "." + jobRun + ".job1.command":  "stopped-command",
				},
			),
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"job1": {RunJob: core.RunJob{BareJob: core.BareJob{
						Schedule: "running-schedule",
						Command:  "running-command",
					}, Container: someContainerInfo.Name}},
				},
			},
			Comment: "Run jobs from running container take precedence over jobs from stopped container",
		},
		{
			Containers: buildTwoContainers(
				otherNotRunningContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobRun + ".job1.schedule": "stopped-schedule2",
					labelPrefix + "." + jobRun + ".job1.command":  "stopped-command2",
					labelPrefix + "." + jobRun + ".job2.schedule": "stopped-schedule3",
					labelPrefix + "." + jobRun + ".job2.command":  "stopped-command3",
				},
				notRunningContainerInfo, map[string]string{
					requiredLabel: "true",
					labelPrefix + "." + jobRun + ".job1.schedule": "stopped-schedule1",
					labelPrefix + "." + jobRun + ".job1.command":  "stopped-command1",
				},
			),
			ExpectedConfig: Config{
				RunJobs: map[string]*RunJobConfig{
					"job1": {RunJob: core.RunJob{BareJob: core.BareJob{
						Schedule: "stopped-schedule1",
						Command:  "stopped-command1",
					}, Container: notRunningContainerInfo.Name}},
					"job2": {RunJob: core.RunJob{BareJob: core.BareJob{
						Schedule: "stopped-schedule3",
						Command:  "stopped-command3",
					}, Container: otherNotRunningContainerInfo.Name}},
				},
			},
			Comment: "Only one run job with a unique name is allowed from stopped containers. The job from the newer container takes precedence.",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.Comment, func(t *testing.T) {
			conf := Config{}
			conf.logger = test.NewTestLogger()
			conf.Global.AllowHostJobsFromLabels = true
			err := conf.buildFromDockerContainers(tc.Containers)
			require.NoError(t, err)
			setJobSource(&conf, JobSourceLabel)
			setJobSource(&tc.ExpectedConfig, JobSourceLabel)

			conf.logger = nil
			conf.WebhookConfigs = nil
			tc.ExpectedConfig.logger = nil
			tc.ExpectedConfig.WebhookConfigs = nil
			tc.ExpectedConfig.Global.AllowHostJobsFromLabels = true

			assert.Equal(t, tc.ExpectedConfig, conf, "Test %q failed", tc.Comment)
		})
	}
}

func TestBuildFromStringError(t *testing.T) {
	t.Parallel()

	_, err := BuildFromString("[invalid", test.NewTestLogger())
	assert.Error(t, err)
}

func TestBuildFromFile(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.ini")
	content := `
[ job-run "foo" ]
schedule = @every 5s
image = alpine
command = echo test123
`
	err := os.WriteFile(configFile, []byte(content), 0o644)
	require.NoError(t, err)

	conf, err := BuildFromFile(configFile, test.NewTestLogger())
	require.NoError(t, err)
	assert.Len(t, conf.RunJobs, 1)
	job, ok := conf.RunJobs["foo"]
	assert.True(t, ok)
	assert.Equal(t, "@every 5s", job.Schedule)
	assert.Equal(t, "echo test123", job.Command)
}

func TestBuildFromFileGlob(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	file1 := filepath.Join(dir, "a.ini")
	err := os.WriteFile(file1, []byte("[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\n"), 0o644)
	require.NoError(t, err)

	file2 := filepath.Join(dir, "b.ini")
	err = os.WriteFile(file2, []byte("[job-exec \"bar\"]\nschedule = @every 10s\ncommand = echo bar\n"), 0o644)
	require.NoError(t, err)

	conf, err := BuildFromFile(filepath.Join(dir, "*.ini"), test.NewTestLogger())
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

	cfg := NewConfig(test.NewTestLogger())
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

	sh := core.NewScheduler(test.NewTestLogger())
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

	logger := test.NewTestLogger()

	cfg, err := BuildFromString(`
		[job-exec "test"]
		schedule = @every 10s
		container = test-container
		command = echo test
	`, logger)
	require.NoError(t, err)
	assert.Equal(t, "nobody", cfg.Global.DefaultUser)

	cfg, err = BuildFromString(`
		[global]
		default-user = root

		[job-exec "test"]
		schedule = @every 10s
		container = test-container
		command = echo test
	`, logger)
	require.NoError(t, err)
	assert.Equal(t, "root", cfg.Global.DefaultUser)

	cfg, err = BuildFromString(`
		[global]
		default-user =

		[job-exec "test"]
		schedule = @every 10s
		container = test-container
		command = echo test
	`, logger)
	require.NoError(t, err)
	assert.Empty(t, cfg.Global.DefaultUser)
}

func TestApplyDefaultUser(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(test.NewTestLogger())
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

	cfg := NewConfig(test.NewTestLogger())
	cfg.Global.MailConfig.SMTPHost = "smtp.example.com"
	cfg.Global.MailConfig.SMTPPort = 587
	cfg.Global.MailConfig.SMTPUser = "globaluser"
	cfg.Global.MailConfig.SMTPPassword = "globalpwd"
	cfg.Global.MailConfig.SMTPTLSSkipVerify = true
	cfg.Global.MailConfig.EmailTo = "global@example.com"
	cfg.Global.MailConfig.EmailFrom = "sender@example.com"
	cfg.Global.MailConfig.MailOnlyOnError = new(true)

	jobMail := middlewares.MailConfig{
		// MailOnlyOnError not set (nil) — should inherit global=true
	}
	cfg.mergeMailDefaults(&jobMail)

	assert.Equal(t, "smtp.example.com", jobMail.SMTPHost)
	assert.Equal(t, 587, jobMail.SMTPPort)
	assert.Equal(t, "globaluser", jobMail.SMTPUser)
	assert.Equal(t, "globalpwd", jobMail.SMTPPassword)
	assert.True(t, jobMail.SMTPTLSSkipVerify)
	assert.Equal(t, "global@example.com", jobMail.EmailTo)
	assert.Equal(t, "sender@example.com", jobMail.EmailFrom)
	require.NotNil(t, jobMail.MailOnlyOnError, "Global mail-only-on-error=true should propagate to job")
	assert.True(t, *jobMail.MailOnlyOnError)

	jobMail2 := middlewares.MailConfig{
		SMTPHost:        "job-smtp.example.com",
		SMTPPort:        465,
		EmailTo:         "job@example.com",
		MailOnlyOnError: new(true),
	}
	cfg.mergeMailDefaults(&jobMail2)

	assert.Equal(t, "job-smtp.example.com", jobMail2.SMTPHost)
	assert.Equal(t, 465, jobMail2.SMTPPort)
	assert.Equal(t, "globaluser", jobMail2.SMTPUser)
	assert.Equal(t, "globalpwd", jobMail2.SMTPPassword)
	assert.Equal(t, "job@example.com", jobMail2.EmailTo)
	assert.Equal(t, "sender@example.com", jobMail2.EmailFrom)

	cfgEmpty := NewConfig(test.NewTestLogger())
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

	cfg := NewConfig(test.NewTestLogger())
	cfg.Global.SlackConfig.SlackWebhook = "https://hooks.slack.com/services/global"
	cfg.Global.SlackConfig.SlackOnlyOnError = new(true)

	jobSlack := middlewares.SlackConfig{
		SlackOnlyOnError: new(true),
	}
	cfg.mergeSlackDefaults(&jobSlack)

	assert.Equal(t, "https://hooks.slack.com/services/global", jobSlack.SlackWebhook)
	require.NotNil(t, jobSlack.SlackOnlyOnError)
	assert.True(t, *jobSlack.SlackOnlyOnError)

	// Job without SlackOnlyOnError (nil) should inherit global=true
	jobSlack2 := middlewares.SlackConfig{
		SlackWebhook: "https://hooks.slack.com/services/job-specific",
	}
	cfg.mergeSlackDefaults(&jobSlack2)

	assert.Equal(t, "https://hooks.slack.com/services/job-specific", jobSlack2.SlackWebhook)
	require.NotNil(t, jobSlack2.SlackOnlyOnError, "Global slack-only-on-error=true should propagate to job")
	assert.True(t, *jobSlack2.SlackOnlyOnError)

	// Global=false should not override job=true
	cfg3 := NewConfig(test.NewTestLogger())
	cfg3.Global.SlackConfig.SlackWebhook = "https://hooks.slack.com/services/global"
	cfg3.Global.SlackConfig.SlackOnlyOnError = new(false)
	jobSlack3 := middlewares.SlackConfig{
		SlackOnlyOnError: new(true),
	}
	cfg3.mergeSlackDefaults(&jobSlack3)
	require.NotNil(t, jobSlack3.SlackOnlyOnError)
	assert.True(t, *jobSlack3.SlackOnlyOnError, "Job slack-only-on-error=true should NOT be overridden by global false")
}

func TestMergeMailDefaultsBoolFieldLimitation(t *testing.T) {
	t.Parallel()

	cfg1 := NewConfig(test.NewTestLogger())
	cfg1.Global.MailConfig.SMTPTLSSkipVerify = true
	jobMail1 := middlewares.MailConfig{SMTPHost: "mail.example.com"}
	cfg1.mergeMailDefaults(&jobMail1)
	assert.True(t, jobMail1.SMTPTLSSkipVerify, "Global skip-verify=true should propagate to job")

	cfg2 := NewConfig(test.NewTestLogger())
	cfg2.Global.MailConfig.SMTPTLSSkipVerify = false
	jobMail2 := middlewares.MailConfig{
		SMTPHost:          "mail.example.com",
		SMTPTLSSkipVerify: true,
	}
	cfg2.mergeMailDefaults(&jobMail2)
	assert.True(t, jobMail2.SMTPTLSSkipVerify, "Job skip-verify=true should NOT be overridden by global false")

	cfg3 := NewConfig(test.NewTestLogger())
	cfg3.Global.MailConfig.SMTPTLSSkipVerify = false
	jobMail3 := middlewares.MailConfig{SMTPHost: "mail.example.com"}
	cfg3.mergeMailDefaults(&jobMail3)
	assert.False(t, jobMail3.SMTPTLSSkipVerify, "Both false - secure default should be maintained")

	cfg4 := NewConfig(test.NewTestLogger())
	cfg4.Global.MailConfig.SMTPTLSSkipVerify = true
	jobMail4 := middlewares.MailConfig{
		SMTPHost:          "mail.example.com",
		SMTPTLSSkipVerify: true,
	}
	cfg4.mergeMailDefaults(&jobMail4)
	assert.True(t, jobMail4.SMTPTLSSkipVerify, "Both true - insecure setting should be preserved")
}

func TestMergeMailDefaultsOnlyOnErrorInheritance(t *testing.T) {
	t.Parallel()

	// Global=true, Job=nil (not set) → Job inherits true
	cfg1 := NewConfig(test.NewTestLogger())
	cfg1.Global.MailConfig.MailOnlyOnError = new(true)
	jobMail1 := middlewares.MailConfig{SMTPHost: "mail.example.com"}
	cfg1.mergeMailDefaults(&jobMail1)
	require.NotNil(t, jobMail1.MailOnlyOnError, "Global mail-only-on-error=true should propagate to job")
	assert.True(t, *jobMail1.MailOnlyOnError)

	// Global=false, Job=true → Job keeps true
	cfg2 := NewConfig(test.NewTestLogger())
	cfg2.Global.MailConfig.MailOnlyOnError = new(false)
	jobMail2 := middlewares.MailConfig{
		SMTPHost:        "mail.example.com",
		MailOnlyOnError: new(true),
	}
	cfg2.mergeMailDefaults(&jobMail2)
	require.NotNil(t, jobMail2.MailOnlyOnError)
	assert.True(t, *jobMail2.MailOnlyOnError, "Job mail-only-on-error=true should NOT be overridden by global false")

	// Global=true, Job=false (explicitly) → Job keeps false (explicit override)
	cfg3 := NewConfig(test.NewTestLogger())
	cfg3.Global.MailConfig.MailOnlyOnError = new(true)
	jobMail3 := middlewares.MailConfig{
		SMTPHost:        "mail.example.com",
		MailOnlyOnError: new(false),
	}
	cfg3.mergeMailDefaults(&jobMail3)
	require.NotNil(t, jobMail3.MailOnlyOnError)
	assert.False(t, *jobMail3.MailOnlyOnError, "Job explicit false should NOT be overridden by global true")

	// Global=nil, Job=nil → Job stays nil (default: send all)
	cfg4 := NewConfig(test.NewTestLogger())
	jobMail4 := middlewares.MailConfig{SMTPHost: "mail.example.com"}
	cfg4.mergeMailDefaults(&jobMail4)
	assert.Nil(t, jobMail4.MailOnlyOnError, "Both nil - should stay nil (send all)")

	// Global=true, Job=true → Job stays true
	cfg5 := NewConfig(test.NewTestLogger())
	cfg5.Global.MailConfig.MailOnlyOnError = new(true)
	jobMail5 := middlewares.MailConfig{
		SMTPHost:        "mail.example.com",
		MailOnlyOnError: new(true),
	}
	cfg5.mergeMailDefaults(&jobMail5)
	require.NotNil(t, jobMail5.MailOnlyOnError)
	assert.True(t, *jobMail5.MailOnlyOnError, "Both true - error-only setting should be preserved")
}

func TestMergeMailDefaultsEmailSubjectInheritance(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(test.NewTestLogger())
	cfg.Global.MailConfig.EmailSubject = "[{{status .Execution}}] {{.Job.GetName}}"

	// Job without subject inherits from global
	jobMail := middlewares.MailConfig{SMTPHost: "mail.example.com"}
	cfg.mergeMailDefaults(&jobMail)
	assert.Equal(t, "[{{status .Execution}}] {{.Job.GetName}}", jobMail.EmailSubject)

	// Job with explicit subject keeps its own
	jobMail2 := middlewares.MailConfig{
		SMTPHost:     "mail.example.com",
		EmailSubject: "Custom: {{.Job.GetName}}",
	}
	cfg.mergeMailDefaults(&jobMail2)
	assert.Equal(t, "Custom: {{.Job.GetName}}", jobMail2.EmailSubject)
}

func TestMergeSaveDefaults(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(test.NewTestLogger())
	cfg.Global.SaveConfig.SaveFolder = "/var/log/ofelia"
	cfg.Global.SaveConfig.SaveOnlyOnError = new(true)

	// Job without save settings inherits from global
	jobSave := middlewares.SaveConfig{}
	cfg.mergeSaveDefaults(&jobSave)
	assert.Equal(t, "/var/log/ofelia", jobSave.SaveFolder)
	require.NotNil(t, jobSave.SaveOnlyOnError, "Global save-only-on-error=true should propagate to job")
	assert.True(t, *jobSave.SaveOnlyOnError)

	// Job with explicit folder keeps its own
	jobSave2 := middlewares.SaveConfig{SaveFolder: "/custom/logs"}
	cfg.mergeSaveDefaults(&jobSave2)
	assert.Equal(t, "/custom/logs", jobSave2.SaveFolder)
	require.NotNil(t, jobSave2.SaveOnlyOnError)
	assert.True(t, *jobSave2.SaveOnlyOnError, "SaveOnlyOnError should still inherit")

	// Job can explicitly override to false
	jobSave3 := middlewares.SaveConfig{SaveOnlyOnError: new(false)}
	cfg.mergeSaveDefaults(&jobSave3)
	require.NotNil(t, jobSave3.SaveOnlyOnError)
	assert.False(t, *jobSave3.SaveOnlyOnError, "Job explicit false should NOT be overridden by global true")

	// Empty global: nothing inherited
	cfgEmpty := NewConfig(test.NewTestLogger())
	jobSave4 := middlewares.SaveConfig{}
	cfgEmpty.mergeSaveDefaults(&jobSave4)
	assert.Empty(t, jobSave4.SaveFolder)
	assert.Nil(t, jobSave4.SaveOnlyOnError)
}
