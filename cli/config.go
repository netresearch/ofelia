package cli

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"

	defaults "github.com/creasty/defaults"
	"github.com/mitchellh/mapstructure"
	ini "gopkg.in/ini.v1"
)

const (
	logFormat     = "%{time} %{color} %{shortfile} ▶ %{level}%{color:reset} %{message}"
	jobExec       = "job-exec"
	jobRun        = "job-run"
	jobServiceRun = "job-service-run"
	jobLocal      = "job-local"
)

// Config contains the configuration
type Config struct {
	Global struct {
		middlewares.SlackConfig `mapstructure:",squash"`
		middlewares.SaveConfig  `mapstructure:",squash"`
		middlewares.MailConfig  `mapstructure:",squash"`
		LogLevel                string `gcfg:"log-level" mapstructure:"log-level"`
		EnableWeb               bool   `gcfg:"enable-web" mapstructure:"enable-web" default:"false"`
		WebAddr                 string `gcfg:"web-address" mapstructure:"web-address" default:":8081"`
		EnablePprof             bool   `gcfg:"enable-pprof" mapstructure:"enable-pprof" default:"false"`
		PprofAddr               string `gcfg:"pprof-address" mapstructure:"pprof-address" default:"127.0.0.1:8080"`
	}
	ExecJobs      map[string]*ExecJobConfig `gcfg:"job-exec" mapstructure:"job-exec,squash"`
	LabelExecJobs map[string]*ExecJobConfig
	RunJobs       map[string]*RunJobConfig `gcfg:"job-run" mapstructure:"job-run,squash"`
	LabelRunJobs  map[string]*RunJobConfig
	ServiceJobs   map[string]*RunServiceConfig `gcfg:"job-service-run" mapstructure:"job-service-run,squash"`
	LocalJobs     map[string]*LocalJobConfig   `gcfg:"job-local" mapstructure:"job-local,squash"`
	Docker        DockerConfig
	configPath    string
	configModTime time.Time
	sh            *core.Scheduler
	dockerHandler *DockerHandler
	logger        core.Logger
}

func NewConfig(logger core.Logger) *Config {
	c := &Config{
		ExecJobs:      make(map[string]*ExecJobConfig),
		LabelExecJobs: make(map[string]*ExecJobConfig),
		RunJobs:       make(map[string]*RunJobConfig),
		LabelRunJobs:  make(map[string]*RunJobConfig),
		ServiceJobs:   make(map[string]*RunServiceConfig),
		LocalJobs:     make(map[string]*LocalJobConfig),
		logger:        logger,
	}

	defaults.Set(c)
	return c
}

// BuildFromFile builds a scheduler using the config from a file
func BuildFromFile(filename string, logger core.Logger) (*Config, error) {
	c := NewConfig(logger)
	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true}, filename)
	if err != nil {
		return nil, err
	}
	if err := parseIni(cfg, c); err != nil {
		return nil, err
	}
	c.configPath = filename
	if info, statErr := os.Stat(filename); statErr == nil {
		c.configModTime = info.ModTime()
	}
	logger.Debugf("loaded config file %s", filename)
	return c, nil
}

// BuildFromString builds a scheduler using the config from a string

// newDockerHandler allows overriding Docker handler creation (e.g., for testing)
var newDockerHandler = NewDockerHandler

func BuildFromString(config string, logger core.Logger) (*Config, error) {
	c := NewConfig(logger)
	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true}, []byte(config))
	if err != nil {
		return nil, err
	}
	if err := parseIni(cfg, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Call this only once at app init
func (c *Config) InitializeApp() error {
	c.sh = core.NewScheduler(c.logger)
	c.buildSchedulerMiddlewares(c.sh)

	var err error
	c.dockerHandler, err = newDockerHandler(context.Background(), c, c.logger, &c.Docker, nil)
	if err != nil {
		return err
	}

	// In order to support non dynamic job types such as Local or Run using labels
	// lets parse the labels and merge the job lists
	dockerLabels, err := c.dockerHandler.GetDockerLabels()
	if err == nil {
		parsedLabelConfig := Config{}

		parsedLabelConfig.buildFromDockerLabels(dockerLabels)
		for name, j := range parsedLabelConfig.ExecJobs {
			c.LabelExecJobs[name] = j
		}

		for name, j := range parsedLabelConfig.RunJobs {
			c.LabelRunJobs[name] = j
		}

		for name, j := range parsedLabelConfig.LocalJobs {
			c.LocalJobs[name] = j
		}

		for name, j := range parsedLabelConfig.ServiceJobs {
			c.ServiceJobs[name] = j
		}
	}

	for name, j := range c.ExecJobs {
		defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
		j.buildMiddlewares()
		c.sh.AddJob(j)
	}

	for name, j := range c.LabelExecJobs {
		defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
		j.buildMiddlewares()
		c.sh.AddJob(j)
	}

	for name, j := range c.RunJobs {
		defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
		j.buildMiddlewares()
		c.sh.AddJob(j)
	}

	for name, j := range c.LabelRunJobs {
		defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
		j.buildMiddlewares()
		c.sh.AddJob(j)
	}

	for name, j := range c.LocalJobs {
		defaults.Set(j)
		j.Name = name
		j.buildMiddlewares()
		c.sh.AddJob(j)
	}

	for name, j := range c.ServiceJobs {
		defaults.Set(j)
		j.Name = name
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.buildMiddlewares()
		c.sh.AddJob(j)
	}

	return nil
}

func (c *Config) buildSchedulerMiddlewares(sh *core.Scheduler) {
	sh.Use(middlewares.NewSlack(&c.Global.SlackConfig))
	sh.Use(middlewares.NewSave(&c.Global.SaveConfig))
	sh.Use(middlewares.NewMail(&c.Global.MailConfig))
}

// jobConfig is implemented by all job configuration types that can be
// scheduled. It allows handling job maps in a generic way.
type jobConfig interface {
	core.Job
	buildMiddlewares()
	Hash() (string, error)
}

// syncJobMap updates the scheduler and the provided job map based on the parsed
// configuration. The prep function is called on each job before comparison or
// registration to set fields such as Name or Client and apply defaults.
func syncJobMap[J jobConfig](c *Config, current map[string]J, parsed map[string]J, prep func(string, J)) {
	for name, j := range current {
		newJob, ok := parsed[name]
		if !ok {
			c.sh.RemoveJob(j)
			delete(current, name)
			continue
		}
		prep(name, newJob)
		newHash, err1 := newJob.Hash()
		if err1 != nil {
			c.logger.Errorf("hash calculation failed: %v", err1)
			continue
		}
		oldHash, err2 := j.Hash()
		if err2 != nil {
			c.logger.Errorf("hash calculation failed: %v", err2)
			continue
		}
		if newHash != oldHash {
			c.sh.RemoveJob(j)
			newJob.buildMiddlewares()
			c.sh.AddJob(newJob)
			current[name] = newJob
		}
	}

	for name, j := range parsed {
		if _, ok := current[name]; ok {
			continue
		}
		prep(name, j)
		j.buildMiddlewares()
		c.sh.AddJob(j)
		current[name] = j
	}
}

func (c *Config) dockerLabelsUpdate(labels map[string]map[string]string) {
	c.logger.Debugf("dockerLabelsUpdate started")

	var parsedLabelConfig Config
	parsedLabelConfig.buildFromDockerLabels(labels)

	execPrep := func(name string, j *ExecJobConfig) {
		defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.LabelExecJobs, parsedLabelConfig.ExecJobs, execPrep)

	runPrep := func(name string, j *RunJobConfig) {
		defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.LabelRunJobs, parsedLabelConfig.RunJobs, runPrep)
}

func (c *Config) iniConfigUpdate() error {
	if c.configPath == "" {
		return nil
	}

	info, err := os.Stat(c.configPath)
	if err != nil {
		return err
	}
	c.logger.Debugf("checking config file %s", c.configPath)
	if info.ModTime().Equal(c.configModTime) {
		c.logger.Debugf("config not changed")
		return nil
	}

	c.logger.Debugf("reloading config from %s", c.configPath)

	parsed, err := BuildFromFile(c.configPath, c.logger)
	if err != nil {
		return err
	}
	c.configModTime = info.ModTime()
	c.logger.Debugf("applied config from %s", c.configPath)

	execPrep := func(name string, j *ExecJobConfig) {
		defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.ExecJobs, parsed.ExecJobs, execPrep)

	runPrep := func(name string, j *RunJobConfig) {
		defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.RunJobs, parsed.RunJobs, runPrep)

	localPrep := func(name string, j *LocalJobConfig) {
		defaults.Set(j)
		j.Name = name
	}
	syncJobMap(c, c.LocalJobs, parsed.LocalJobs, localPrep)

	svcPrep := func(name string, j *RunServiceConfig) {
		defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.ServiceJobs, parsed.ServiceJobs, svcPrep)

	return nil
}

// ExecJobConfig contains all configuration params needed to build a ExecJob
type ExecJobConfig struct {
	core.ExecJob              `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
}

func (c *ExecJobConfig) buildMiddlewares() {
	c.ExecJob.Use(middlewares.NewOverlap(&c.OverlapConfig))
	c.ExecJob.Use(middlewares.NewSlack(&c.SlackConfig))
	c.ExecJob.Use(middlewares.NewSave(&c.SaveConfig))
	c.ExecJob.Use(middlewares.NewMail(&c.MailConfig))
}

// RunServiceConfig contains all configuration params needed to build a RunJob
type RunServiceConfig struct {
	core.RunServiceJob        `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
}

type RunJobConfig struct {
	core.RunJob               `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
}

func (c *RunJobConfig) buildMiddlewares() {
	c.RunJob.Use(middlewares.NewOverlap(&c.OverlapConfig))
	c.RunJob.Use(middlewares.NewSlack(&c.SlackConfig))
	c.RunJob.Use(middlewares.NewSave(&c.SaveConfig))
	c.RunJob.Use(middlewares.NewMail(&c.MailConfig))
}

// LocalJobConfig contains all configuration params needed to build a RunJob
type LocalJobConfig struct {
	core.LocalJob             `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
}

func (c *LocalJobConfig) buildMiddlewares() {
	c.LocalJob.Use(middlewares.NewOverlap(&c.OverlapConfig))
	c.LocalJob.Use(middlewares.NewSlack(&c.SlackConfig))
	c.LocalJob.Use(middlewares.NewSave(&c.SaveConfig))
	c.LocalJob.Use(middlewares.NewMail(&c.MailConfig))
}

func (c *RunServiceConfig) buildMiddlewares() {
	c.RunServiceJob.Use(middlewares.NewOverlap(&c.OverlapConfig))
	c.RunServiceJob.Use(middlewares.NewSlack(&c.SlackConfig))
	c.RunServiceJob.Use(middlewares.NewSave(&c.SaveConfig))
	c.RunServiceJob.Use(middlewares.NewMail(&c.MailConfig))
}

type DockerConfig struct {
	Filters        []string      `mapstructure:"filters"`
	PollInterval   time.Duration `mapstructure:"poll-interval" default:"10s"`
	UseEvents      bool          `mapstructure:"events" default:"false"`
	DisablePolling bool          `mapstructure:"no-poll" default:"false"`
}

func parseIni(cfg *ini.File, c *Config) error {
	if sec, err := cfg.GetSection("global"); err == nil {
		if err := mapstructure.WeakDecode(sectionToMap(sec), &c.Global); err != nil {
			return err
		}
	}
	if sec, err := cfg.GetSection("docker"); err == nil {
		if err := mapstructure.WeakDecode(sectionToMap(sec), &c.Docker); err != nil {
			return err
		}
	}

	for _, section := range cfg.Sections() {
		name := strings.TrimSpace(section.Name())
		switch {
		case strings.HasPrefix(name, jobExec):
			jobName := parseJobName(name, jobExec)
			job := &ExecJobConfig{}
			if err := mapstructure.WeakDecode(sectionToMap(section), job); err != nil {
				return err
			}
			c.ExecJobs[jobName] = job
		case strings.HasPrefix(name, jobRun):
			jobName := parseJobName(name, jobRun)
			job := &RunJobConfig{}
			if err := mapstructure.WeakDecode(sectionToMap(section), job); err != nil {
				return err
			}
			c.RunJobs[jobName] = job
		case strings.HasPrefix(name, jobServiceRun):
			jobName := parseJobName(name, jobServiceRun)
			job := &RunServiceConfig{}
			if err := mapstructure.WeakDecode(sectionToMap(section), job); err != nil {
				return err
			}
			c.ServiceJobs[jobName] = job
		case strings.HasPrefix(name, jobLocal):
			jobName := parseJobName(name, jobLocal)
			job := &LocalJobConfig{}
			if err := mapstructure.WeakDecode(sectionToMap(section), job); err != nil {
				return err
			}
			c.LocalJobs[jobName] = job
		}
	}
	return nil
}

func parseJobName(section, prefix string) string {
	s := strings.TrimPrefix(section, prefix)
	s = strings.TrimSpace(s)
	return strings.Trim(s, "\"")
}

func sectionToMap(section *ini.Section) map[string]interface{} {
	m := make(map[string]interface{})
	for _, key := range section.Keys() {
		vals := key.ValueWithShadows()
		if len(vals) > 1 {
			cp := make([]string, len(vals))
			copy(cp, vals)
			m[key.Name()] = cp
		} else if len(vals) == 1 {
			m[key.Name()] = vals[0]
		} else {
			// Handle empty values
			m[key.Name()] = ""
		}
	}
	return m
}
