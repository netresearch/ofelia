package cli

import (
	"errors"
	"io/ioutil"
	"os"

	defaults "github.com/creasty/defaults"
	"github.com/netresearch/ofelia/core"

	. "gopkg.in/check.v1"
)

// Test error path of BuildFromString with invalid INI string
func (s *SuiteConfig) TestBuildFromStringInvalidIni(c *C) {
	_, err := BuildFromString("this is not ini", &TestLogger{})
	c.Assert(err, NotNil)
}

// Test error path of BuildFromFile for non-existent or invalid file
func (s *SuiteConfig) TestBuildFromFileError(c *C) {
	// Non-existent file
	_, err := BuildFromFile("nonexistent_file.ini", &TestLogger{})
	c.Assert(err, NotNil)

	// Invalid content
	tmpFile, err := ioutil.TempFile("", "config_test")
	c.Assert(err, IsNil)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("invalid content")
	c.Assert(err, IsNil)
	tmpFile.Close()

	_, err = BuildFromFile(tmpFile.Name(), &TestLogger{})
	c.Assert(err, NotNil)
}

// Test InitializeApp returns error when Docker handler factory fails
func (s *SuiteConfig) TestInitializeAppErrorDockerHandler(c *C) {
	// Override newDockerHandler to simulate factory error
	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		return nil, errors.New("factory error")
	}

	cfg := NewConfig(&TestLogger{})
	err := cfg.InitializeApp()
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "factory error")
}

// Test dynamic updates via dockerLabelsUpdate for ExecJobs: additions, schedule changes, removals
func (s *SuiteConfig) TestDockerLabelsUpdateExecJobs(c *C) {
	// Prepare initial Config
	cfg := NewConfig(&TestLogger{})
	cfg.logger = &TestLogger{}
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(&TestLogger{})
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

// Test iniConfigUpdate reloads jobs from the INI file
func (s *SuiteConfig) TestIniConfigUpdate(c *C) {
	tmp, err := ioutil.TempFile("", "ofelia_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmp.Name())

	content1 := "[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\n"
	_, err = tmp.WriteString(content1)
	c.Assert(err, IsNil)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), &TestLogger{})
	c.Assert(err, IsNil)
	cfg.logger = &TestLogger{}
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(&TestLogger{})
	cfg.buildSchedulerMiddlewares(cfg.sh)

	// register initial jobs
	for name, j := range cfg.RunJobs {
		defaults.Set(j)
		j.Client = cfg.dockerHandler.GetInternalDockerClient()
		j.Name = name
		j.buildMiddlewares()
		cfg.sh.AddJob(j)
	}

	c.Assert(len(cfg.RunJobs), Equals, 1)
	c.Assert(cfg.RunJobs["foo"].GetSchedule(), Equals, "@every 5s")

	// modify ini: change schedule and add new job
	content2 := "[job-run \"foo\"]\nschedule = @every 10s\nimage = busybox\ncommand = echo foo\n[job-run \"bar\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo bar\n"
	err = os.WriteFile(tmp.Name(), []byte(content2), 0o644)
	c.Assert(err, IsNil)

	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	c.Assert(len(cfg.RunJobs), Equals, 2)
	c.Assert(cfg.RunJobs["foo"].GetSchedule(), Equals, "@every 10s")

	// modify ini: remove foo
	content3 := "[job-run \"bar\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo bar\n"
	err = os.WriteFile(tmp.Name(), []byte(content3), 0o644)
	c.Assert(err, IsNil)

	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	c.Assert(len(cfg.RunJobs), Equals, 1)
	_, ok := cfg.RunJobs["foo"]
	c.Assert(ok, Equals, false)
}

// Test iniConfigUpdate skips reload when the file has not changed
func (s *SuiteConfig) TestIniConfigUpdateNoChange(c *C) {
	tmp, err := ioutil.TempFile("", "ofelia_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmp.Name())

	content := "[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo foo\n"
	_, err = tmp.WriteString(content)
	c.Assert(err, IsNil)
	tmp.Close()

	cfg, err := BuildFromFile(tmp.Name(), &TestLogger{})
	c.Assert(err, IsNil)
	cfg.logger = &TestLogger{}
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(&TestLogger{})
	cfg.buildSchedulerMiddlewares(cfg.sh)

	modTime := cfg.configModTime
	err = cfg.iniConfigUpdate()
	c.Assert(err, IsNil)
	c.Assert(cfg.configModTime, Equals, modTime)
	c.Assert(len(cfg.RunJobs), Equals, 1)
	c.Assert(cfg.RunJobs["foo"].GetSchedule(), Equals, "@every 5s")
}
