package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	defaults "github.com/creasty/defaults"
	"github.com/sirupsen/logrus"
	. "gopkg.in/check.v1"

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
func (s *SuiteConfig) TestBuildFromStringInvalidIni(c *C) {
	_, err := BuildFromString("this is not ini", test.NewTestLogger())
	c.Assert(err, NotNil)
}

// Test error path of BuildFromFile for non-existent or invalid file
func (s *SuiteConfig) TestBuildFromFileError(c *C) {
	// Non-existent file
	_, err := BuildFromFile("nonexistent_file.ini", test.NewTestLogger())
	c.Assert(err, NotNil)

	// Invalid content
	tmpFile, err := os.CreateTemp("", "config_test")
	c.Assert(err, IsNil)
	defer os.Remove(tmpFile.Name())

	_, _ = tmpFile.WriteString("invalid content")
	tmpFile.Close()

	_, err = BuildFromFile(tmpFile.Name(), test.NewTestLogger())
	c.Assert(err, NotNil)
}

// Test InitializeApp returns error when Docker handler factory fails
func (s *SuiteConfig) TestInitializeAppErrorDockerHandler(c *C) {
	// Override newDockerHandler to simulate factory error
	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		return nil, errors.New("factory error")
	}

	cfg := NewConfig(test.NewTestLogger())
	err := cfg.InitializeApp()
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "factory error")
}

// Test dynamic updates via dockerLabelsUpdate for ExecJobs: additions, schedule changes, removals
func (s *SuiteConfig) TestDockerLabelsUpdateExecJobs(c *C) {
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
	c.Assert(len(cfg.ExecJobs), Equals, 1)
	j := cfg.ExecJobs["container1.foo"]
	c.Assert(j.JobSource, Equals, JobSourceLabel)
	// Verify schedule and command set
	c.Assert(j.GetSchedule(), Equals, "@every 5s")
	c.Assert(j.GetCommand(), Equals, "echo foo")

	// Inspect cron entries count
	entries := cfg.sh.Entries()
	c.Assert(len(entries), Equals, 1)

	// 2) Change schedule (should restart job)
	labelsChange := map[string]map[string]string{
		"container1": {
			labelPrefix + ".job-exec.foo.schedule": "@every 10s",
			labelPrefix + ".job-exec.foo.command":  "echo foo",
		},
	}
	cfg.dockerLabelsUpdate(labelsChange)
	c.Assert(len(cfg.ExecJobs), Equals, 1)
	j2 := cfg.ExecJobs["container1.foo"]
	c.Assert(j2.GetSchedule(), Equals, "@every 10s")
	entries = cfg.sh.Entries()
	c.Assert(len(entries), Equals, 1)

	// 3) Removal of job
	cfg.dockerLabelsUpdate(map[string]map[string]string{})
	c.Assert(len(cfg.ExecJobs), Equals, 0)
	entries = cfg.sh.Entries()
	c.Assert(len(entries), Equals, 0)
}

// Test dockerLabelsUpdate blocks host jobs when security policy is disabled.
func (s *SuiteConfig) TestDockerLabelsSecurityPolicyViolation(c *C) {
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
	c.Assert(cfg.LocalJobs, HasLen, 0, Commentf("Local jobs should be blocked by security policy"))
	c.Assert(cfg.ComposeJobs, HasLen, 0, Commentf("Compose jobs should be blocked by security policy"))

	// Verify error logs were generated
	c.Assert(logger.ErrorCount(), Equals, 2, Commentf("Expected 2 error logs (1 for local, 1 for compose)"))
	c.Assert(logger.HasError("SECURITY POLICY VIOLATION"), Equals, true,
		Commentf("Error log should contain SECURITY POLICY VIOLATION"))
	c.Assert(logger.HasError("local jobs"), Equals, true,
		Commentf("Error log should mention local jobs"))
	c.Assert(logger.HasError("compose jobs"), Equals, true,
		Commentf("Error log should mention compose jobs"))
	c.Assert(logger.HasError("privilege escalation"), Equals, true,
		Commentf("Error log should explain privilege escalation risk"))
}

// Test dockerLabelsUpdate removes local and service jobs when containers disappear.
func (s *SuiteConfig) TestDockerLabelsUpdateStaleJobs(c *C) {
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
			labelPrefix + ".job-service-run.s.command":  "echo svc",
		},
	}
	cfg.dockerLabelsUpdate(labels)
	c.Assert(cfg.LocalJobs, HasLen, 1)
	c.Assert(cfg.ServiceJobs, HasLen, 1)

	cfg.dockerLabelsUpdate(map[string]map[string]string{})
	c.Assert(cfg.LocalJobs, HasLen, 0)
	c.Assert(cfg.ServiceJobs, HasLen, 0)
}

// Test iniConfigUpdate reloads jobs from the INI file
func (s *SuiteConfig) TestIniConfigUpdate(c *C) {
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmp.Name())

	_, _ = tmp.WriteString(iniFoo)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	c.Assert(err, IsNil)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	// register initial jobs
	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Client = cfg.dockerHandler.GetInternalDockerClient()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares()
		_ = cfg.sh.AddJob(j)
	}

	c.Assert(len(cfg.RunJobs), Equals, 1)
	c.Assert(cfg.RunJobs["foo"].GetSchedule(), Equals, "@every 5s")

	// modify ini: change schedule and add new job
	oldTime := cfg.configModTime
	content2 := strings.ReplaceAll(iniFoo, "@every 5s", "@every 10s") + iniBar
	err = os.WriteFile(tmp.Name(), []byte(content2), 0o644)
	c.Assert(err, IsNil)
	c.Assert(waitForModTimeChange(tmp.Name(), oldTime), IsNil)

	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	c.Assert(len(cfg.RunJobs), Equals, 2)
	c.Assert(cfg.RunJobs["foo"].GetSchedule(), Equals, "@every 10s")

	// modify ini: remove foo
	oldTime = cfg.configModTime
	content3 := iniBar
	err = os.WriteFile(tmp.Name(), []byte(content3), 0o644)
	c.Assert(err, IsNil)
	c.Assert(waitForModTimeChange(tmp.Name(), oldTime), IsNil)

	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	c.Assert(len(cfg.RunJobs), Equals, 1)
	_, ok := cfg.RunJobs["foo"]
	c.Assert(ok, Equals, false)
}

// TestIniConfigUpdateEnvChange verifies environment changes are applied on reload.
func (s *SuiteConfig) TestIniConfigUpdateEnvChange(c *C) {
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmp.Name())

	content1 := "[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\nenvironment = FOO=bar\n"
	_, err = tmp.WriteString(content1)
	c.Assert(err, IsNil)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	c.Assert(err, IsNil)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Client = cfg.dockerHandler.GetInternalDockerClient()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares()
		_ = cfg.sh.AddJob(j)
	}

	c.Assert(cfg.RunJobs["foo"].Environment[0], Equals, "FOO=bar")

	oldTime := cfg.configModTime
	content2 := "[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\nenvironment = FOO=baz\n"
	err = os.WriteFile(tmp.Name(), []byte(content2), 0o644)
	c.Assert(err, IsNil)
	c.Assert(waitForModTimeChange(tmp.Name(), oldTime), IsNil)

	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	c.Assert(cfg.RunJobs["foo"].Environment[0], Equals, "FOO=baz")
}

// Test iniConfigUpdate does nothing when the INI file did not change
func (s *SuiteConfig) TestIniConfigUpdateNoReload(c *C) {
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmp.Name())

	_, err = tmp.WriteString(iniFoo)
	c.Assert(err, IsNil)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	c.Assert(err, IsNil)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Client = cfg.dockerHandler.GetInternalDockerClient()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares()
		_ = cfg.sh.AddJob(j)
	}

	// call iniConfigUpdate without modifying the file
	oldTime := cfg.configModTime
	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	c.Assert(cfg.configModTime, Equals, oldTime)
	c.Assert(len(cfg.RunJobs), Equals, 1)
}

// TestIniConfigUpdateLabelConflict verifies INI jobs override label jobs on reload.
func (s *SuiteConfig) TestIniConfigUpdateLabelConflict(c *C) {
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmp.Name())

	_, err = tmp.WriteString("")
	c.Assert(err, IsNil)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	c.Assert(err, IsNil)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	cfg.RunJobs["foo"] = &RunJobConfig{RunJob: core.RunJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo lbl"}}, JobSource: JobSourceLabel}
	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Client = cfg.dockerHandler.GetInternalDockerClient()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares()
		_ = cfg.sh.AddJob(j)
	}

	oldTime := cfg.configModTime
	iniStr := "[job-run \"foo\"]\nschedule = @daily\nimage = busybox\ncommand = echo ini\n"
	err = os.WriteFile(tmp.Name(), []byte(iniStr), 0o644)
	c.Assert(err, IsNil)
	c.Assert(waitForModTimeChange(tmp.Name(), oldTime), IsNil)

	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	j, ok := cfg.RunJobs["foo"]
	c.Assert(ok, Equals, true)
	c.Assert(j.JobSource, Equals, JobSourceINI)
	c.Assert(j.Command, Equals, "echo ini")
}

// Test iniConfigUpdate reloads when any of the glob matched files change
func (s *SuiteConfig) TestIniConfigUpdateGlob(c *C) {
	dir, err := os.MkdirTemp("", "ofelia_glob_update")
	c.Assert(err, IsNil)
	defer os.RemoveAll(dir)

	file1 := filepath.Join(dir, "a.ini")
	err = os.WriteFile(file1, []byte(iniFoo), 0o644)
	c.Assert(err, IsNil)

	file2 := filepath.Join(dir, "b.ini")
	err = os.WriteFile(file2, []byte("[job-run \"bar\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo bar\n"), 0o644)
	c.Assert(err, IsNil)

	cfg, err := BuildFromFile(filepath.Join(dir, "*.ini"), test.NewTestLogger())
	c.Assert(err, IsNil)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Client = cfg.dockerHandler.GetInternalDockerClient()
		j.InitializeRuntimeFields() // Initialize monitor and dockerOps after client is set
		j.Name = name
		j.buildMiddlewares()
		_ = cfg.sh.AddJob(j)
	}

	c.Assert(len(cfg.RunJobs), Equals, 2)
	c.Assert(cfg.RunJobs["foo"].GetSchedule(), Equals, "@every 5s")

	oldTime := cfg.configModTime
	err = os.WriteFile(file1, []byte("[job-run \"foo\"]\nschedule = @every 10s\nimage = busybox\ncommand = echo foo\n"), 0o644)
	c.Assert(err, IsNil)
	c.Assert(waitForModTimeChange(file1, oldTime), IsNil)

	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	c.Assert(len(cfg.RunJobs), Equals, 2)
	c.Assert(cfg.RunJobs["foo"].GetSchedule(), Equals, "@every 10s")
}

// TestIniConfigUpdateGlobalChange verifies global middleware options and log
// level are reloaded.
func (s *SuiteConfig) TestIniConfigUpdateGlobalChange(c *C) {
	tmp, err := os.CreateTemp("", "ofelia_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmp.Name())

	dir := c.MkDir()
	content1 := fmt.Sprintf("[global]\nlog-level = INFO\nsave-folder = %s\n",
		dir)
	content1 += "save-only-on-error = false\n"
	content1 += iniFoo
	_, err = tmp.WriteString(content1)
	c.Assert(err, IsNil)
	tmp.Close()

	logrus.SetLevel(logrus.InfoLevel)

	cfg, err := BuildFromFile(tmp.Name(), test.NewTestLogger())
	c.Assert(err, IsNil)
	cfg.logger = test.NewTestLogger()
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	ApplyLogLevel(cfg.Global.LogLevel)
	ms := cfg.sh.Middlewares()
	c.Assert(ms, HasLen, 1)
	saveMw := ms[0].(*middlewares.Save)
	c.Assert(saveMw.SaveOnlyOnError, Equals, false)
	c.Assert(logrus.GetLevel(), Equals, logrus.InfoLevel)

	oldTime := cfg.configModTime
	content2 := fmt.Sprintf("[global]\nlog-level = DEBUG\nsave-folder = %s\nsave-only-on-error = true\n", dir)
	content2 += iniFoo
	err = os.WriteFile(tmp.Name(), []byte(content2), 0o644)
	c.Assert(err, IsNil)
	c.Assert(waitForModTimeChange(tmp.Name(), oldTime), IsNil)

	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	c.Assert(cfg.Global.LogLevel, Equals, "DEBUG")
	ms = cfg.sh.Middlewares()
	c.Assert(ms, HasLen, 1)
	saveMw = ms[0].(*middlewares.Save)
	c.Assert(saveMw.SaveOnlyOnError, Equals, true)
	c.Assert(logrus.GetLevel(), Equals, logrus.DebugLevel)
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
