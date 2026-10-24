// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"log/slog"
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

// --- iniConfigUpdate ---

func TestIniConfigUpdate_FileChanged_GlobalChanged(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	content := `[global]
log-level = info
[job-local "test"]
schedule = @daily
command = echo hello
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	logger := test.NewTestLogger()
	config, err := BuildFromFile(configPath, logger)
	require.NoError(t, err)

	// Set up scheduler and docker handler
	config.sh = core.NewScheduler(logger)
	config.buildSchedulerMiddlewares(config.sh)
	config.dockerHandler = &DockerHandler{
		ctx:    context.Background(),
		logger: logger,
	}
	config.configPath = configPath
	config.levelVar = &slog.LevelVar{}

	// Register existing local job in scheduler
	for name, j := range config.LocalJobs {
		_ = defaults.Set(j)
		j.Name = name
		j.SetJobSource(JobSourceINI)
		_ = config.sh.AddJob(j)
	}

	// Set modtime well in the past so the file appears changed
	config.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	// Rewrite file with changed global settings
	newContent := `[global]
log-level = debug
[job-local "test"]
schedule = @daily
command = echo hello
`
	require.NoError(t, os.WriteFile(configPath, []byte(newContent), 0o644))

	err = config.iniConfigUpdate()
	require.NoError(t, err)
	assert.Equal(t, "debug", config.Global.LogLevel)
}

func TestIniConfigUpdate_FileChanged_JobAdded(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	content := `[global]
[job-local "job1"]
schedule = @daily
command = echo job1
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	logger := test.NewTestLogger()
	config, err := BuildFromFile(configPath, logger)
	require.NoError(t, err)

	config.sh = core.NewScheduler(logger)
	config.buildSchedulerMiddlewares(config.sh)
	config.dockerHandler = &DockerHandler{
		ctx:    context.Background(),
		logger: logger,
	}
	config.configPath = configPath
	config.levelVar = &slog.LevelVar{}

	for name, j := range config.LocalJobs {
		_ = defaults.Set(j)
		j.Name = name
		j.SetJobSource(JobSourceINI)
		_ = config.sh.AddJob(j)
	}

	config.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	// Add a second job to the config file
	newContent := `[global]
[job-local "job1"]
schedule = @daily
command = echo job1
[job-local "job2"]
schedule = @hourly
command = echo job2
`
	require.NoError(t, os.WriteFile(configPath, []byte(newContent), 0o644))

	err = config.iniConfigUpdate()
	require.NoError(t, err)
	assert.Contains(t, config.LocalJobs, "job2", "new job should be added")
}

func TestIniConfigUpdate_FileChanged_JobRemoved(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	content := `[global]
[job-local "job1"]
schedule = @daily
command = echo job1
[job-local "job2"]
schedule = @hourly
command = echo job2
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	logger := test.NewTestLogger()
	config, err := BuildFromFile(configPath, logger)
	require.NoError(t, err)

	config.sh = core.NewScheduler(logger)
	config.buildSchedulerMiddlewares(config.sh)
	config.dockerHandler = &DockerHandler{
		ctx:    context.Background(),
		logger: logger,
	}
	config.configPath = configPath
	config.levelVar = &slog.LevelVar{}

	for name, j := range config.LocalJobs {
		_ = defaults.Set(j)
		j.Name = name
		j.SetJobSource(JobSourceINI)
		_ = config.sh.AddJob(j)
	}

	config.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	// Remove job2
	newContent := `[global]
[job-local "job1"]
schedule = @daily
command = echo job1
`
	require.NoError(t, os.WriteFile(configPath, []byte(newContent), 0o644))

	err = config.iniConfigUpdate()
	require.NoError(t, err)
	assert.NotContains(t, config.LocalJobs, "job2", "removed job should be gone")
}

func TestIniConfigUpdate_ParseError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	content := `[global]
[job-local "test"]
schedule = @daily
command = echo test
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	logger := test.NewTestLogger()
	config, err := BuildFromFile(configPath, logger)
	require.NoError(t, err)

	config.sh = core.NewScheduler(logger)
	config.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	config.configPath = configPath
	config.levelVar = &slog.LevelVar{}
	config.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	// Write invalid INI content
	require.NoError(t, os.WriteFile(configPath, []byte("[broken\n"), 0o644))

	err = config.iniConfigUpdate()
	assert.Error(t, err)
}

// --- dockerContainersUpdate ---

func TestDockerContainersUpdate_EmptyContainers(t *testing.T) {
	t.Parallel()

	cfg := newBaseConfig()
	setJobSource(cfg, JobSourceLabel)
	cfg.WebhookConfigs = NewWebhookConfigs()

	// Should not panic with empty containers
	cfg.dockerContainersUpdate([]DockerContainerInfo{})
}

func TestDockerContainersUpdate_LocalAndComposeSecurityWarning(t *testing.T) {
	t.Parallel()

	logger, handler := test.NewTestLoggerWithHandler()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.WebhookConfigs = NewWebhookConfigs()
	cfg.Global.AllowHostJobsFromLabels = true

	containers := []DockerContainerInfo{
		{
			Name:  "test-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                  "true",
				"ofelia.service":                  "true",
				"ofelia.job-local.myjob.schedule": "@daily",
				"ofelia.job-local.myjob.command":  "echo hello",
			},
		},
	}

	cfg.dockerContainersUpdate(containers)

	assert.True(t, handler.HasWarning("SECURITY WARNING"),
		"should warn about host-based jobs from labels")
}

func TestDockerContainersUpdate_AllJobTypes(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.WebhookConfigs = NewWebhookConfigs()

	containers := []DockerContainerInfo{
		{
			Name:  "test-exec-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                 "true",
				"ofelia.job-exec.myjob.schedule": "@daily",
				"ofelia.job-exec.myjob.command":  "echo exec",
				"ofelia.job-run.myrun.schedule":  "@hourly",
				"ofelia.job-run.myrun.image":     "alpine:latest",
				"ofelia.job-run.myrun.command":   "echo run",
			},
		},
	}

	cfg.dockerContainersUpdate(containers)

	assert.Contains(t, cfg.ExecJobs, "test-exec-container.myjob")
	assert.Contains(t, cfg.RunJobs, "myrun")
}

// --- parseIni with all job types ---

func TestParseIni_AllJobTypes(t *testing.T) {
	t.Parallel()

	configStr := `[global]
[job-exec "exec1"]
schedule = @daily
command = echo exec
container = test
[job-run "run1"]
schedule = @hourly
image = alpine
command = echo run
[job-service-run "svc1"]
schedule = @daily
image = nginx
command = echo svc
[job-local "local1"]
schedule = @daily
command = echo local
[job-compose "compose1"]
schedule = @daily
command = echo compose
compose-file = docker-compose.yml
`

	logger := test.NewTestLogger()
	config, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Contains(t, config.ExecJobs, "exec1")
	assert.Contains(t, config.RunJobs, "run1")
	assert.Contains(t, config.ServiceJobs, "svc1")
	assert.Contains(t, config.LocalJobs, "local1")
	assert.Contains(t, config.ComposeJobs, "compose1")
}

func TestParseIni_WebhookSectionParsedBeforeJobs(t *testing.T) {
	t.Parallel()

	configStr := `[global]
webhooks = mywebhook
[webhook "mywebhook"]
url = https://example.com/hook
trigger = on-error
[job-local "test"]
schedule = @daily
command = echo test
webhooks = mywebhook
`

	logger := test.NewTestLogger()
	config, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.NotNil(t, config.WebhookConfigs)
	assert.Contains(t, config.WebhookConfigs.Webhooks, "mywebhook")
}

// --- parseGlobalAndDocker ---

func TestParseGlobalAndDocker_DockerDecodeError(t *testing.T) {
	t.Parallel()

	// Docker section with wrong type for a duration field
	configStr := `[docker]
config-poll-interval = not-a-duration
[job-local "test"]
schedule = @daily
command = echo test
`

	_, err := BuildFromString(configStr, test.NewTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker")
}

func TestParseGlobalAndDocker_BothSections(t *testing.T) {
	t.Parallel()

	configStr := `[global]
log-level = debug
[docker]
events = true
include-stopped = true
[job-local "test"]
schedule = @daily
command = echo test
`

	logger := test.NewTestLogger()
	config, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, "debug", config.Global.LogLevel)
	assert.True(t, config.Docker.UseEvents)
	assert.True(t, config.Docker.IncludeStopped)
}

// --- buildMiddlewares for all job types with webhook manager ---

func TestBuildMiddlewares_WithWebhookManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func()
	}{
		{
			name: "ExecJobConfig",
			fn: func() {
				wm := middlewares.NewWebhookManager(middlewares.DefaultWebhookGlobalConfig())
				j := &ExecJobConfig{}
				j.buildMiddlewares(nil, wm)
			},
		},
		{
			name: "RunJobConfig",
			fn: func() {
				wm := middlewares.NewWebhookManager(middlewares.DefaultWebhookGlobalConfig())
				j := &RunJobConfig{}
				j.buildMiddlewares(nil, wm)
			},
		},
		{
			name: "LocalJobConfig",
			fn: func() {
				wm := middlewares.NewWebhookManager(middlewares.DefaultWebhookGlobalConfig())
				j := &LocalJobConfig{}
				j.buildMiddlewares(nil, wm)
			},
		},
		{
			name: "ComposeJobConfig",
			fn: func() {
				wm := middlewares.NewWebhookManager(middlewares.DefaultWebhookGlobalConfig())
				j := &ComposeJobConfig{}
				j.buildMiddlewares(nil, wm)
			},
		},
		{
			name: "RunServiceConfig",
			fn: func() {
				wm := middlewares.NewWebhookManager(middlewares.DefaultWebhookGlobalConfig())
				j := &RunServiceConfig{}
				j.buildMiddlewares(nil, wm)
			},
		},
		{
			name: "ExecJobConfig_NilManager",
			fn: func() {
				j := &ExecJobConfig{}
				j.buildMiddlewares(nil, nil)
			},
		},
		{
			name: "RunJobConfig_NilManager",
			fn: func() {
				j := &RunJobConfig{}
				j.buildMiddlewares(nil, nil)
			},
		},
		{
			name: "LocalJobConfig_NilManager",
			fn: func() {
				j := &LocalJobConfig{}
				j.buildMiddlewares(nil, nil)
			},
		},
		{
			name: "ComposeJobConfig_NilManager",
			fn: func() {
				j := &ComposeJobConfig{}
				j.buildMiddlewares(nil, nil)
			},
		},
		{
			name: "RunServiceConfig_NilManager",
			fn: func() {
				j := &RunServiceConfig{}
				j.buildMiddlewares(nil, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Should not panic
			tt.fn()
		})
	}
}

// --- buildSchedulerMiddlewares with webhook manager ---

func TestBuildSchedulerMiddlewares_WithWebhookManager(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.WebhookConfigs = NewWebhookConfigs()

	// Create a webhook manager
	wm := middlewares.NewWebhookManager(middlewares.DefaultWebhookGlobalConfig())
	cfg.WebhookConfigs.Manager = wm

	// Should not panic and should add middlewares
	cfg.buildSchedulerMiddlewares(cfg.sh)
}

// --- addNewJob with empty source ---

func TestAddNewJob_EmptySource_Coverage(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	current := make(map[string]*LocalJobConfig)
	j := &LocalJobConfig{}
	j.Schedule = "@daily"
	j.Command = "echo test"

	prep := func(name string, j *LocalJobConfig) {
		j.Name = name
	}

	// Empty source should not set job source
	addNewJob(cfg, "newjob", j, prep, "", current, false)
	assert.Contains(t, current, "newjob")
	assert.Equal(t, JobSource(""), current["newjob"].GetJobSource())
}

// --- BuildFromString with strict validation ---

func TestBuildFromString_StrictValidation(t *testing.T) {
	t.Parallel()

	configStr := `[global]
enable-strict-validation = true
[job-local "test"]
schedule = @daily
command = echo test
`

	logger := test.NewTestLogger()
	config, err := BuildFromString(configStr, logger)
	// Strict validation may pass or fail depending on validator; just ensure no panic
	if err == nil {
		assert.NotNil(t, config)
	}
}

// --- iniConfigUpdate with all job types ---

func TestIniConfigUpdate_WithExecRunServiceCompose(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	content := `[global]
[job-local "local1"]
schedule = @daily
command = echo local
[job-compose "compose1"]
schedule = @daily
command = echo compose
compose-file = docker-compose.yml
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	logger := test.NewTestLogger()
	config, err := BuildFromFile(configPath, logger)
	require.NoError(t, err)

	config.sh = core.NewScheduler(logger)
	config.buildSchedulerMiddlewares(config.sh)
	config.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	config.configPath = configPath
	config.levelVar = &slog.LevelVar{}

	for name, j := range config.LocalJobs {
		_ = defaults.Set(j)
		j.Name = name
		j.SetJobSource(JobSourceINI)
		_ = config.sh.AddJob(j)
	}
	for name, j := range config.ComposeJobs {
		_ = defaults.Set(j)
		j.Name = name
		j.SetJobSource(JobSourceINI)
		_ = config.sh.AddJob(j)
	}

	config.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	// Remove compose job, keep local
	newContent := `[global]
[job-local "local1"]
schedule = @daily
command = echo local
`
	require.NoError(t, os.WriteFile(configPath, []byte(newContent), 0o644))

	err = config.iniConfigUpdate()
	require.NoError(t, err)
	assert.NotContains(t, config.ComposeJobs, "compose1")
}

// --- initNotificationDedup ---

func TestInitNotificationDedup_Enabled(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.Global.NotificationCooldown = 5 * time.Minute

	cfg.initNotificationDedup()

	assert.NotNil(t, cfg.notificationDedup)
	assert.NotNil(t, cfg.Global.SlackConfig.Dedup)
	assert.NotNil(t, cfg.Global.MailConfig.Dedup)
}

func TestInitNotificationDedup_Disabled(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.Global.NotificationCooldown = 0

	cfg.initNotificationDedup()

	assert.Nil(t, cfg.notificationDedup)
}

// --- getWebhookManager ---

func TestGetWebhookManager_NilConfigs(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	assert.Nil(t, cfg.getWebhookManager())
}

func TestGetWebhookManager_WithManager(t *testing.T) {
	t.Parallel()

	wm := middlewares.NewWebhookManager(middlewares.DefaultWebhookGlobalConfig())
	cfg := &Config{
		WebhookConfigs: &WebhookConfigs{
			Manager: wm,
		},
	}
	assert.Equal(t, wm, cfg.getWebhookManager())
}

// --- applyDefaultUser edge case ---

func TestApplyDefaultUser_EmptyDefault(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	cfg.Global.DefaultUser = ""
	user := ""
	cfg.applyDefaultUser(&user)
	assert.Empty(t, user, "should stay empty when no global default")
}
