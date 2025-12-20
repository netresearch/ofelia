package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	defaults "github.com/creasty/defaults"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

const (
	iniFoo = "[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\n"
	iniBar = "[job-run \"bar\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo bar\n"
)

// Keep unused constants minimal; remove if not used to satisfy unused linter.

// Test error path of BuildFromString with invalid INI string
func TestBuildFromStringInvalidIni(t *testing.T) {
	t.Parallel()
	_, err := BuildFromString("this is not ini", test.NewTestLogger())
	assert.NotNil(t, err)
}

// Test error path of BuildFromFile for non-existent or invalid file
func TestBuildFromFileError(t *testing.T) {
	t.Parallel()
	// Non-existent file
	_, err := BuildFromFile("nonexistent_file.ini", test.NewTestLogger())
	assert.NotNil(t, err)

	// Invalid content
	tmpFile, err := os.CreateTemp("", "config_test")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, _ = tmpFile.WriteString("invalid content")
	tmpFile.Close()

	_, err = BuildFromFile(tmpFile.Name(), test.NewTestLogger())
	assert.NotNil(t, err)
}

// Test InitializeApp returns error when Docker handler factory fails
func TestInitializeAppErrorDockerHandler(t *testing.T) {
	t.Parallel()
	// Override newDockerHandler to simulate factory error
	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("factory error")
	}

	cfg := NewConfig(test.NewTestLogger())
	err := cfg.InitializeApp()
	require.NotNil(t, err)
	assert.Equal(t, "factory error", err.Error())
}

// Test dynamic updates via dockerLabelsUpdate for ExecJobs: additions, schedule changes, removals
func TestDockerLabelsUpdateExecJobs(t *testing.T) {
	t.Parallel()
	// Prepare initial Config
	cfg := NewConfig(test.NewTestLogger())
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.ExecJobs = make(map[string]*ExecJobConfig)

	// 1) Addition of new job
	labelsAdd := map[string]map[string]string{
		"container1": {
			labelPrefix + ".job-exec.foo.schedule": "@every 5s",
			labelPrefix + ".job-exec.foo.command":  "echo foo",
		},
	}
	cfg.dockerLabelsUpdate(labelsAdd)
	assert.Equal(t, 1, len(cfg.ExecJobs))
	j := cfg.ExecJobs["container1.foo"]
	assert.Equal(t, JobSourceLabel, j.JobSource)
	// Verify schedule and command set
	assert.Equal(t, "@every 5s", j.GetSchedule())
	assert.Equal(t, "echo foo", j.GetCommand())

	// Inspect cron entries count
	entries := cfg.sh.Entries()
	assert.Equal(t, 1, len(entries))

	// 2) Change schedule (should restart job)
	labelsChange := map[string]map[string]string{
		"container1": {
			labelPrefix + ".job-exec.foo.schedule": "@every 10s",
			labelPrefix + ".job-exec.foo.command":  "echo foo",
		},
	}
	cfg.dockerLabelsUpdate(labelsChange)
	assert.Equal(t, 1, len(cfg.ExecJobs))
	j2 := cfg.ExecJobs["container1.foo"]
	assert.Equal(t, "@every 10s", j2.GetSchedule())
	entries = cfg.sh.Entries()
	assert.Equal(t, 1, len(entries))

	// 3) Removal of job
	cfg.dockerLabelsUpdate(map[string]map[string]string{})
	assert.Equal(t, 0, len(cfg.ExecJobs))
	entries = cfg.sh.Entries()
	assert.Equal(t, 0, len(entries))
}

// Test dockerLabelsUpdate blocks host jobs when security policy is disabled.
func TestDockerLabelsSecurityPolicyViolation(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.logger = logger
	cfg.Global.AllowHostJobsFromLabels = false // Security policy: block host jobs from labels
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.LocalJobs = make(map[string]*LocalJobConfig)
	cfg.ComposeJobs = make(map[string]*ComposeJobConfig)

	// Attempt to create local and compose jobs via labels
	labels := map[string]map[string]string{
		"cont1": {
			requiredLabel:                           "true",
			serviceLabel:                            "true",
			labelPrefix + ".job-local.l.schedule":   "@daily",
			labelPrefix + ".job-local.l.command":    "echo dangerous",
			labelPrefix + ".job-compose.c.schedule": "@hourly",
			labelPrefix + ".job-compose.c.command":  "restart",
		},
	}
	cfg.dockerLabelsUpdate(labels)

	// Verify security policy blocked the jobs
	assert.Len(t, cfg.LocalJobs, 0, "Local jobs should be blocked by security policy")
	assert.Len(t, cfg.ComposeJobs, 0, "Compose jobs should be blocked by security policy")

	// Verify error logs were generated
	assert.Equal(t, 2, logger.ErrorCount(), "Expected 2 error logs (1 for local, 1 for compose)")
	assert.True(t, logger.HasError("SECURITY POLICY VIOLATION"),
		"Error log should contain SECURITY POLICY VIOLATION")
	assert.True(t, logger.HasError("local jobs"),
		"Error log should mention local jobs")
	assert.True(t, logger.HasError("compose jobs"),
		"Error log should mention compose jobs")
	assert.True(t, logger.HasError("privilege escalation"),
		"Error log should explain privilege escalation risk")
}

// Test dockerLabelsUpdate removes local and service jobs when containers disappear.
func TestDockerLabelsUpdateStaleJobs(t *testing.T) {
	t.Parallel()
	cfg := NewConfig(test.NewTestLogger())
	cfg.logger = test.NewTestLogger()
	cfg.Global.AllowHostJobsFromLabels = true // Enable local jobs from labels for testing
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.LocalJobs = make(map[string]*LocalJobConfig)
	cfg.ServiceJobs = make(map[string]*RunServiceConfig)

	labels := map[string]map[string]string{
		"cont1": {
			requiredLabel:                               "true",
			serviceLabel:                                "true",
			labelPrefix + ".job-local.l.schedule":       "@daily",
			labelPrefix + ".job-local.l.command":        "echo loc",
			labelPrefix + ".job-service-run.s.schedule": "@hourly",
			labelPrefix + ".job-service-run.s.image":    "nginx",
			labelPrefix + ".job-service-run.s.command":  "echo svc",
		},
	}
	cfg.dockerLabelsUpdate(labels)
	assert.Len(t, cfg.LocalJobs, 1)
	assert.Len(t, cfg.ServiceJobs, 1)

	cfg.dockerLabelsUpdate(map[string]map[string]string{})
	assert.Len(t, cfg.LocalJobs, 0)
	assert.Len(t, cfg.ServiceJobs, 0)
}

// Test iniConfigUpdate reloads jobs from the INI file
func TestIniConfigUpdate(t *testing.T) {
	t.Parallel()
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	_, _ = tmp.WriteString(iniFoo)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	require.NoError(t, err)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	// register initial jobs
	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Provider = cfg.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares(nil)
		_ = cfg.sh.AddJob(j)
	}

	assert.Equal(t, 1, len(cfg.RunJobs))
	assert.Equal(t, "@every 5s", cfg.RunJobs["foo"].GetSchedule())

	// modify ini: change schedule and add new job
	oldTime := cfg.configModTime
	content2 := strings.ReplaceAll(iniFoo, "@every 5s", "@every 10s") + iniBar
	err = os.WriteFile(tmp.Name(), []byte(content2), 0o644)
	require.NoError(t, err)
	require.NoError(t, waitForModTimeChange(tmp.Name(), oldTime))

	err = cfg.iniConfigUpdate()
	require.NoError(t, err)
	assert.Equal(t, 2, len(cfg.RunJobs))
	assert.Equal(t, "@every 10s", cfg.RunJobs["foo"].GetSchedule())

	// modify ini: remove foo
	oldTime = cfg.configModTime
	content3 := iniBar
	err = os.WriteFile(tmp.Name(), []byte(content3), 0o644)
	require.NoError(t, err)
	require.NoError(t, waitForModTimeChange(tmp.Name(), oldTime))

	err = cfg.iniConfigUpdate()
	require.NoError(t, err)
	assert.Equal(t, 1, len(cfg.RunJobs))
	_, ok := cfg.RunJobs["foo"]
	assert.False(t, ok)
}

// TestIniConfigUpdateEnvChange verifies environment changes are applied on reload.
func TestIniConfigUpdateEnvChange(t *testing.T) {
	t.Parallel()
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	content1 := "[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\nenvironment = FOO=bar\n"
	_, err = tmp.WriteString(content1)
	require.NoError(t, err)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	require.NoError(t, err)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Provider = cfg.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares(nil)
		_ = cfg.sh.AddJob(j)
	}

	assert.Equal(t, "FOO=bar", cfg.RunJobs["foo"].Environment[0])

	oldTime := cfg.configModTime
	content2 := "[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\nenvironment = FOO=baz\n"
	err = os.WriteFile(tmp.Name(), []byte(content2), 0o644)
	require.NoError(t, err)
	require.NoError(t, waitForModTimeChange(tmp.Name(), oldTime))

	err = cfg.iniConfigUpdate()
	require.NoError(t, err)
	assert.Equal(t, "FOO=baz", cfg.RunJobs["foo"].Environment[0])
}

// Test iniConfigUpdate does nothing when the INI file did not change
func TestIniConfigUpdateNoReload(t *testing.T) {
	t.Parallel()
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	_, err = tmp.WriteString(iniFoo)
	require.NoError(t, err)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	require.NoError(t, err)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Provider = cfg.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares(nil)
		_ = cfg.sh.AddJob(j)
	}

	// call iniConfigUpdate without modifying the file
	oldTime := cfg.configModTime
	err = cfg.iniConfigUpdate()
	require.NoError(t, err)
	assert.Equal(t, oldTime, cfg.configModTime)
	assert.Equal(t, 1, len(cfg.RunJobs))
}

// TestIniConfigUpdateLabelConflict verifies INI jobs override label jobs on reload.
func TestIniConfigUpdateLabelConflict(t *testing.T) {
	t.Parallel()
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	_, err = tmp.WriteString("")
	require.NoError(t, err)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	require.NoError(t, err)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	cfg.RunJobs["foo"] = &RunJobConfig{RunJob: core.RunJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo lbl"}}, JobSource: JobSourceLabel}
	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Provider = cfg.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares(nil)
		_ = cfg.sh.AddJob(j)
	}

	oldTime := cfg.configModTime
	iniStr := "[job-run \"foo\"]\nschedule = @daily\nimage = busybox\ncommand = echo ini\n"
	err = os.WriteFile(tmp.Name(), []byte(iniStr), 0o644)
	require.NoError(t, err)
	require.NoError(t, waitForModTimeChange(tmp.Name(), oldTime))

	err = cfg.iniConfigUpdate()
	require.NoError(t, err)
	j, ok := cfg.RunJobs["foo"]
	assert.True(t, ok)
	assert.Equal(t, JobSourceINI, j.JobSource)
	assert.Equal(t, "echo ini", j.Command)
}

// Test iniConfigUpdate reloads when any of the glob matched files change
func TestIniConfigUpdateGlob(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "ofelia_glob_update")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	file1 := filepath.Join(dir, "a.ini")
	err = os.WriteFile(file1, []byte(iniFoo), 0o644)
	require.NoError(t, err)

	file2 := filepath.Join(dir, "b.ini")
	err = os.WriteFile(file2, []byte("[job-run \"bar\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo bar\n"), 0o644)
	require.NoError(t, err)

	cfg, err := BuildFromFile(filepath.Join(dir, "*.ini"), test.NewTestLogger())
	require.NoError(t, err)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Provider = cfg.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares(nil)
		_ = cfg.sh.AddJob(j)
	}

	assert.Equal(t, 2, len(cfg.RunJobs))
	assert.Equal(t, "@every 5s", cfg.RunJobs["foo"].GetSchedule())

	oldTime := cfg.configModTime
	err = os.WriteFile(file1, []byte("[job-run \"foo\"]\nschedule = @every 10s\nimage = busybox\ncommand = echo foo\n"), 0o644)
	require.NoError(t, err)
	require.NoError(t, waitForModTimeChange(file1, oldTime))

	err = cfg.iniConfigUpdate()
	require.NoError(t, err)
	assert.Equal(t, 2, len(cfg.RunJobs))
	assert.Equal(t, "@every 10s", cfg.RunJobs["foo"].GetSchedule())
}

// TestIniConfigUpdateGlobalChange verifies global middleware options and log
// level are reloaded.
func TestIniConfigUpdateGlobalChange(t *testing.T) {
	t.Parallel()
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	dir := t.TempDir()
	content1 := fmt.Sprintf("[global]\nlog-level = INFO\nsave-folder = %s\n",
		dir)
	content1 += "save-only-on-error = false\n"
	content1 += iniFoo
	_, err = tmp.WriteString(content1)
	require.NoError(t, err)
	tmp.Close()

	logrus.SetLevel(logrus.InfoLevel)

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	require.NoError(t, err)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	_ = ApplyLogLevel(cfg.Global.LogLevel) // Ignore error in test
	ms := cfg.sh.Middlewares()
	assert.Len(t, ms, 1)
	saveMw := ms[0].(*middlewares.Save)
	assert.False(t, saveMw.SaveOnlyOnError)
	assert.Equal(t, logrus.InfoLevel, logrus.GetLevel())

	oldTime := cfg.configModTime
	content2 := fmt.Sprintf("[global]\nlog-level = DEBUG\nsave-folder = %s\nsave-only-on-error = true\n", dir)
	content2 += iniFoo
	err = os.WriteFile(tmp.Name(), []byte(content2), 0o644)
	require.NoError(t, err)
	require.NoError(t, waitForModTimeChange(tmp.Name(), oldTime))

	err = cfg.iniConfigUpdate()
	require.NoError(t, err)
	assert.Equal(t, "DEBUG", cfg.Global.LogLevel)
	ms = cfg.sh.Middlewares()
	assert.Len(t, ms, 1)
	saveMw = ms[0].(*middlewares.Save)
	assert.True(t, saveMw.SaveOnlyOnError)
	assert.Equal(t, logrus.DebugLevel, logrus.GetLevel())
}

func waitForModTimeChange(path string, after time.Time) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.ModTime().After(after) {
		return nil
	}
	newTime := after.Add(time.Second)
	return os.Chtimes(path, newTime, newTime)
}
