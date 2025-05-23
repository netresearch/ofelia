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
	ExecJobs      map[string]*ExecJobConfig    `gcfg:"job-exec" mapstructure:"job-exec,squash"`
	RunJobs       map[string]*RunJobConfig     `gcfg:"job-run" mapstructure:"job-run,squash"`
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
		ExecJobs:    make(map[string]*ExecJobConfig),
		RunJobs:     make(map[string]*RunJobConfig),
		ServiceJobs: make(map[string]*RunServiceConfig),
		LocalJobs:   make(map[string]*LocalJobConfig),
		logger:      logger,
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
		for name, j := range parsedLabelConfig.RunJobs {
			c.RunJobs[name] = j
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

	for name, j := range c.RunJobs {
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

func (c *Config) dockerLabelsUpdate(labels map[string]map[string]string) {
	// log start of func
	c.logger.Debugf("dockerLabelsUpdate started")

	// Get the current labels
	var parsedLabelConfig Config
	parsedLabelConfig.buildFromDockerLabels(labels)
	c.logger.Debugf("dockerLabelsUpdate labels: %v", labels)

	var hasIterated bool

	// Calculate the delta execJobs
	hasIterated = false
	for name, j := range c.ExecJobs {
		hasIterated = true
		c.logger.Debugf("checking exec job %s for changes", name)
		found := false
		for newJobsName, newJob := range parsedLabelConfig.ExecJobs {
			c.logger.Debugf("checking exec job %s vs %s", name, newJobsName)
			// Check if the schedule has changed
			if name == newJobsName {
				found = true
				// There is a slight race condition were a job can be canceled / restarted with different params
				// so, lets take care of it by simply restarting
				// For the hash to work properly, we must fill the fields before calling it
				defaults.Set(newJob)
				newJob.Client = c.dockerHandler.GetInternalDockerClient()
				newJob.Name = newJobsName
				newHash, err1 := newJob.Hash()
				if err1 != nil {
					c.logger.Errorf("hash calculation failed: %v", err1)
					break
				}
				oldHash, err2 := j.Hash()
				if err2 != nil {
					c.logger.Errorf("hash calculation failed: %v", err2)
					break
				}
				if newHash != oldHash {
					// Remove from the scheduler
					c.sh.RemoveJob(j)
					// Add the job back to the scheduler
					newJob.buildMiddlewares()
					c.sh.AddJob(newJob)
					// Update the job config
					c.ExecJobs[name] = newJob
				}
				break
			}
		}
		if !found {
			// Remove from the scheduler
			c.sh.RemoveJob(j)
			// Remove the job from the ExecJobs map
			delete(c.ExecJobs, name)
			c.logger.Debugf("removing exec job %s", name)
		}
	}
	if !hasIterated {
		c.logger.Debugf("no exec jobs to update")
	}

	// Check for additions
	hasIterated = false
	for newJobsName, newJob := range parsedLabelConfig.ExecJobs {
		hasIterated = true
		c.logger.Debugf("checking exec job %s if new", newJobsName)
		found := false
		for name := range c.ExecJobs {
			if name == newJobsName {
				found = true
				break
			}
		}
		if !found {
			defaults.Set(newJob)
			newJob.Client = c.dockerHandler.GetInternalDockerClient()
			newJob.Name = newJobsName
			newJob.buildMiddlewares()
			c.sh.AddJob(newJob)
			c.ExecJobs[newJobsName] = newJob
		}
	}
	if !hasIterated {
		c.logger.Debugf("no new exec jobs")
	}

	hasIterated = false
	for name, j := range c.RunJobs {
		hasIterated = true
		c.logger.Debugf("checking run job %s for changes", name)
		found := false
		for newJobsName, newJob := range parsedLabelConfig.RunJobs {
			// Check if the schedule has changed
			if name == newJobsName {
				found = true
				// There is a slight race condition were a job can be canceled / restarted with different params
				// so, lets take care of it by simply restarting
				// For the hash to work properly, we must fill the fields before calling it
				defaults.Set(newJob)
				newJob.Client = c.dockerHandler.GetInternalDockerClient()
				newJob.Name = newJobsName
				newHash, err1 := newJob.Hash()
				if err1 != nil {
					c.logger.Errorf("hash calculation failed: %v", err1)
					break
				}
				oldHash, err2 := j.Hash()
				if err2 != nil {
					c.logger.Errorf("hash calculation failed: %v", err2)
					break
				}
				if newHash != oldHash {
					// Remove from the scheduler
					c.sh.RemoveJob(j)
					// Add the job back to the scheduler
					newJob.buildMiddlewares()
					c.sh.AddJob(newJob)
					// Update the job config
					c.RunJobs[name] = newJob
				}
				break
			}
		}
		if !found {
			// Remove from the scheduler
			c.sh.RemoveJob(j)
			// Remove the job from the RunJobs map
			delete(c.RunJobs, name)
			c.logger.Debugf("removing run job %s", name)
		}
	}
	if !hasIterated {
		c.logger.Debugf("no run jobs to update")
	}

	// Check for additions
	hasIterated = false
	for newJobsName, newJob := range parsedLabelConfig.RunJobs {
		hasIterated = true
		c.logger.Debugf("checking run job %s if new", newJobsName)
		found := false
		for name := range c.RunJobs {
			if name == newJobsName {
				found = true
				break
			}
		}
		if !found {
			defaults.Set(newJob)
			newJob.Client = c.dockerHandler.GetInternalDockerClient()
			newJob.Name = newJobsName
			newJob.buildMiddlewares()
			c.sh.AddJob(newJob)
			c.RunJobs[newJobsName] = newJob
		}
	}
	if !hasIterated {
		c.logger.Debugf("no new run jobs")
	}
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

	// Exec jobs
	for name, j := range c.ExecJobs {
		newJob, ok := parsed.ExecJobs[name]
		if !ok {
			c.sh.RemoveJob(j)
			delete(c.ExecJobs, name)
			continue
		}
		defaults.Set(newJob)
		newJob.Client = c.dockerHandler.GetInternalDockerClient()
		newJob.Name = name
		newHash, err1 := newJob.Hash()
		if err1 != nil {
			return err1
		}
		oldHash, err2 := j.Hash()
		if err2 != nil {
			return err2
		}
		if newHash != oldHash {
			c.sh.RemoveJob(j)
			newJob.buildMiddlewares()
			c.sh.AddJob(newJob)
			c.ExecJobs[name] = newJob
		}
	}

	for name, j := range parsed.ExecJobs {
		if _, ok := c.ExecJobs[name]; !ok {
			defaults.Set(j)
			j.Client = c.dockerHandler.GetInternalDockerClient()
			j.Name = name
			j.buildMiddlewares()
			c.sh.AddJob(j)
			c.ExecJobs[name] = j
		}
	}

	// Run jobs
	for name, j := range c.RunJobs {
		newJob, ok := parsed.RunJobs[name]
		if !ok {
			c.sh.RemoveJob(j)
			delete(c.RunJobs, name)
			continue
		}
		defaults.Set(newJob)
		newJob.Client = c.dockerHandler.GetInternalDockerClient()
		newJob.Name = name
		newHash, err1 := newJob.Hash()
		if err1 != nil {
			return err1
		}
		oldHash, err2 := j.Hash()
		if err2 != nil {
			return err2
		}
		if newHash != oldHash {
			c.sh.RemoveJob(j)
			newJob.buildMiddlewares()
			c.sh.AddJob(newJob)
			c.RunJobs[name] = newJob
		}
	}

	for name, j := range parsed.RunJobs {
		if _, ok := c.RunJobs[name]; !ok {
			defaults.Set(j)
			j.Client = c.dockerHandler.GetInternalDockerClient()
			j.Name = name
			j.buildMiddlewares()
			c.sh.AddJob(j)
			c.RunJobs[name] = j
		}
	}

	// Local jobs
	for name, j := range c.LocalJobs {
		newJob, ok := parsed.LocalJobs[name]
		if !ok {
			c.sh.RemoveJob(j)
			delete(c.LocalJobs, name)
			continue
		}
		defaults.Set(newJob)
		newJob.Name = name
		newHash, err1 := newJob.Hash()
		if err1 != nil {
			return err1
		}
		oldHash, err2 := j.Hash()
		if err2 != nil {
			return err2
		}
		if newHash != oldHash {
			c.sh.RemoveJob(j)
			newJob.buildMiddlewares()
			c.sh.AddJob(newJob)
			c.LocalJobs[name] = newJob
		}
	}

	for name, j := range parsed.LocalJobs {
		if _, ok := c.LocalJobs[name]; !ok {
			defaults.Set(j)
			j.Name = name
			j.buildMiddlewares()
			c.sh.AddJob(j)
			c.LocalJobs[name] = j
		}
	}

	// Service jobs
	for name, j := range c.ServiceJobs {
		newJob, ok := parsed.ServiceJobs[name]
		if !ok {
			c.sh.RemoveJob(j)
			delete(c.ServiceJobs, name)
			continue
		}
		defaults.Set(newJob)
		newJob.Client = c.dockerHandler.GetInternalDockerClient()
		newJob.Name = name
		newHash, err1 := newJob.Hash()
		if err1 != nil {
			return err1
		}
		oldHash, err2 := j.Hash()
		if err2 != nil {
			return err2
		}
		if newHash != oldHash {
			c.sh.RemoveJob(j)
			newJob.buildMiddlewares()
			c.sh.AddJob(newJob)
			c.ServiceJobs[name] = newJob
		}
	}

	for name, j := range parsed.ServiceJobs {
		if _, ok := c.ServiceJobs[name]; !ok {
			defaults.Set(j)
			j.Client = c.dockerHandler.GetInternalDockerClient()
			j.Name = name
			j.buildMiddlewares()
			c.sh.AddJob(j)
			c.ServiceJobs[name] = j
		}
	}

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
