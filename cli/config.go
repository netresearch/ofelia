package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	defaults "github.com/creasty/defaults"
	"github.com/mitchellh/mapstructure"
	ini "gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
)

const (
	logFormat     = "%{time} %{color} %{shortfile} ▶ %{level}%{color:reset} %{message}"
	jobExec       = "job-exec"
	jobRun        = "job-run"
	jobServiceRun = "job-service-run"
	jobLocal      = "job-local"
	jobCompose    = "job-compose"
)

// JobSource indicates where a job configuration originated from.
type JobSource string

const (
	JobSourceINI   JobSource = "ini"
	JobSourceLabel JobSource = "label"
)

// Config contains the configuration
type Config struct {
	Global struct {
		middlewares.SlackConfig `mapstructure:",squash"`
		middlewares.SaveConfig  `mapstructure:",squash"`
		middlewares.MailConfig  `mapstructure:",squash"`
		LogLevel                string        `gcfg:"log-level" mapstructure:"log-level"`
		EnableWeb               bool          `gcfg:"enable-web" mapstructure:"enable-web" default:"false"`
		WebAddr                 string        `gcfg:"web-address" mapstructure:"web-address" default:":8081"`
		EnablePprof             bool          `gcfg:"enable-pprof" mapstructure:"enable-pprof" default:"false"`
		PprofAddr               string        `gcfg:"pprof-address" mapstructure:"pprof-address" default:"127.0.0.1:8080"`
		MaxRuntime              time.Duration `gcfg:"max-runtime" mapstructure:"max-runtime" default:"24h"`
	}
	ExecJobs      map[string]*ExecJobConfig    `gcfg:"job-exec" mapstructure:"job-exec,squash"`
	RunJobs       map[string]*RunJobConfig     `gcfg:"job-run" mapstructure:"job-run,squash"`
	ServiceJobs   map[string]*RunServiceConfig `gcfg:"job-service-run" mapstructure:"job-service-run,squash"`
	LocalJobs     map[string]*LocalJobConfig   `gcfg:"job-local" mapstructure:"job-local,squash"`
	ComposeJobs   map[string]*ComposeJobConfig `gcfg:"job-compose" mapstructure:"job-compose,squash"`
	Docker        DockerConfig
	configPath    string
	configFiles   []string
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
		ComposeJobs: make(map[string]*ComposeJobConfig),
		logger:      logger,
	}

	_ = defaults.Set(c)
	return c
}

// resolveConfigFiles returns files matching the given pattern. If no file
// matches, the pattern itself is treated as a literal path.
func resolveConfigFiles(pattern string) ([]string, error) {
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}
	if len(files) == 0 {
		files = []string{pattern}
	}
	sort.Strings(files)
	return files, nil
}

// BuildFromFile builds a scheduler using the config from one or multiple files.
// The filename may include glob patterns. When multiple files are matched,
// they are parsed in lexical order and merged.
func BuildFromFile(filename string, logger core.Logger) (*Config, error) {
	files, err := resolveConfigFiles(filename)
	if err != nil {
		return nil, err
	}

	c := NewConfig(logger)
	var latest time.Time
	for _, f := range files {
		cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true}, f)
		if err != nil {
			return nil, fmt.Errorf("load ini %q: %w", f, err)
		}
		if err := parseIni(cfg, c); err != nil {
			return nil, fmt.Errorf("parse ini %q: %w", f, err)
		}
		if info, statErr := os.Stat(f); statErr == nil {
			if info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
		logger.Debugf("loaded config file %s", f)
	}
	c.configPath = filename
	c.configFiles = files
	c.configModTime = latest
	return c, nil
}

// BuildFromString builds a scheduler using the config from a string

// newDockerHandler allows overriding Docker handler creation (e.g., for testing)
var newDockerHandler = NewDockerHandler

func BuildFromString(config string, logger core.Logger) (*Config, error) {
	c := NewConfig(logger)
	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true}, []byte(config))
	if err != nil {
		return nil, fmt.Errorf("load ini from string: %w", err)
	}
	if err := parseIni(cfg, c); err != nil {
		return nil, fmt.Errorf("parse ini from string: %w", err)
	}
	return c, nil
}

// Call this only once at app init
func (c *Config) InitializeApp() error {
	c.sh = core.NewScheduler(c.logger)
	c.buildSchedulerMiddlewares(c.sh)

	if err := c.initDockerHandler(); err != nil {
		return err
	}
	c.mergeJobsFromDockerLabels()
	c.registerAllJobs()
	return nil
}

func (c *Config) initDockerHandler() error {
	var err error
	c.dockerHandler, err = newDockerHandler(context.Background(), c, c.logger, &c.Docker, nil)
	return err
}

func (c *Config) mergeJobsFromDockerLabels() {
	dockerLabels, err := c.dockerHandler.GetDockerLabels()
	if err != nil {
		return
	}
	parsed := Config{}
	_ = parsed.buildFromDockerLabels(dockerLabels)

	mergeJobs(c, c.ExecJobs, parsed.ExecJobs, "exec")
	mergeJobs(c, c.RunJobs, parsed.RunJobs, "run")
	mergeJobs(c, c.LocalJobs, parsed.LocalJobs, "local")
	mergeJobs(c, c.ServiceJobs, parsed.ServiceJobs, "service")
	mergeJobs(c, c.ComposeJobs, parsed.ComposeJobs, "compose")
}

// mergeJobs copies jobs from src into dst while respecting INI precedence.
func mergeJobs[T jobConfig](c *Config, dst map[string]T, src map[string]T, kind string) {
	for name, j := range src {
		if existing, ok := dst[name]; ok && existing.GetJobSource() == JobSourceINI {
			c.logger.Warningf("ignoring label-defined %s job %q because an INI job with the same name exists", kind, name)
			continue
		}
		dst[name] = j
	}
}

func (c *Config) registerAllJobs() {
	client := c.dockerHandler.GetInternalDockerClient()

	for name, j := range c.ExecJobs {
		_ = defaults.Set(j)
		j.Client = client
		j.Name = name
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.RunJobs {
		_ = defaults.Set(j)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Client = client
		j.Name = name
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.LocalJobs {
		_ = defaults.Set(j)
		j.Name = name
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.ServiceJobs {
		_ = defaults.Set(j)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Name = name
		j.Client = client
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.ComposeJobs {
		_ = defaults.Set(j)
		j.Name = name
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
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
	GetJobSource() JobSource
	SetJobSource(JobSource)
	ResetMiddlewares(...core.Middleware)
}

// syncJobMap updates the scheduler and the provided job map based on the parsed
// configuration. The prep function is called on each job before comparison or
// registration to set fields such as Name or Client and apply defaults.
func syncJobMap[J jobConfig](c *Config, current map[string]J, parsed map[string]J, prep func(string, J), source JobSource, jobKind string) {
	for name, j := range current {
		if source != "" && j.GetJobSource() != source && j.GetJobSource() != "" {
			continue
		}
		newJob, ok := parsed[name]
		if !ok {
			_ = c.sh.RemoveJob(j)
			delete(current, name)
			continue
		}
		if updated := replaceIfChanged(c, name, j, newJob, prep, source); updated {
			current[name] = newJob
			continue
		}
	}

	for name, j := range parsed {
		if cur, ok := current[name]; ok {
			switch {
			case cur.GetJobSource() == source:
				continue
			case source == JobSourceINI && cur.GetJobSource() == JobSourceLabel:
				c.logger.Warningf("overriding label-defined %s job %q with INI job", jobKind, name)
				_ = c.sh.RemoveJob(cur)
			case source == JobSourceLabel && cur.GetJobSource() == JobSourceINI:
				c.logger.Warningf("ignoring label-defined %s job %q because an INI job with the same name exists", jobKind, name)
				continue
			default:
				continue
			}
		}
		addNewJob(c, name, j, prep, source, current)
	}
}

func replaceIfChanged[J jobConfig](c *Config, name string, oldJob, newJob J, prep func(string, J), source JobSource) bool {
	prep(name, newJob)
	newJob.SetJobSource(source)
	newHash, err1 := newJob.Hash()
	if err1 != nil {
		c.logger.Errorf("hash calculation failed: %v", err1)
		return false
	}
	oldHash, err2 := oldJob.Hash()
	if err2 != nil {
		c.logger.Errorf("hash calculation failed: %v", err2)
		return false
	}
	if newHash == oldHash {
		return false
	}
	_ = c.sh.RemoveJob(oldJob)
	newJob.buildMiddlewares()
	_ = c.sh.AddJob(newJob)
	// caller updates current map entry
	return true
}

func addNewJob[J jobConfig](c *Config, name string, j J, prep func(string, J), source JobSource, current map[string]J) {
	if source != "" {
		j.SetJobSource(source)
	}
	prep(name, j)
	j.buildMiddlewares()
	_ = c.sh.AddJob(j)
	current[name] = j
}

func (c *Config) dockerLabelsUpdate(labels map[string]map[string]string) {
	c.logger.Debugf("dockerLabelsUpdate started")

	var parsedLabelConfig Config
	_ = parsedLabelConfig.buildFromDockerLabels(labels)

	execPrep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.ExecJobs, parsedLabelConfig.ExecJobs, execPrep, JobSourceLabel, "exec")

	runPrep := func(name string, j *RunJobConfig) {
		_ = defaults.Set(j)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.RunJobs, parsedLabelConfig.RunJobs, runPrep, JobSourceLabel, "run")

	localPrep := func(name string, j *LocalJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}
	syncJobMap(c, c.LocalJobs, parsedLabelConfig.LocalJobs, localPrep, JobSourceLabel, "local")

	servicePrep := func(name string, j *RunServiceConfig) {
		_ = defaults.Set(j)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.ServiceJobs, parsedLabelConfig.ServiceJobs, servicePrep, JobSourceLabel, "service")

	composePrep := func(name string, j *ComposeJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}
	syncJobMap(c, c.ComposeJobs, parsedLabelConfig.ComposeJobs, composePrep, JobSourceLabel, "compose")
}

func (c *Config) iniConfigUpdate() error {
	if c.configPath == "" {
		return nil
	}

	files, err := resolveConfigFiles(c.configPath)
	if err != nil {
		return err
	}

	latest, changed, err := latestChanged(files, c.configModTime)
	if err != nil {
		return err
	}
	for _, f := range files {
		c.logger.Debugf("checking config file %s", f)
	}
	if !changed {
		c.logger.Debugf("config not changed")
		return nil
	}
	c.logger.Debugf("reloading config files from %s", strings.Join(files, ", "))

	parsed, err := BuildFromFile(c.configPath, c.logger)
	if err != nil {
		return err
	}
	globalChanged := !reflect.DeepEqual(parsed.Global, c.Global)
	c.configFiles = files
	c.configModTime = latest
	c.logger.Debugf("applied config files from %s", strings.Join(files, ", "))
	if globalChanged {
		c.Global = parsed.Global
		c.sh.ResetMiddlewares()
		c.buildSchedulerMiddlewares(c.sh)
		for _, j := range c.sh.Jobs {
			if jc, ok := j.(jobConfig); ok {
				jc.ResetMiddlewares()
				jc.buildMiddlewares()
				j.Use(c.sh.Middlewares()...)
			}
		}
		for _, j := range c.sh.Disabled {
			if jc, ok := j.(jobConfig); ok {
				jc.ResetMiddlewares()
				jc.buildMiddlewares()
				j.Use(c.sh.Middlewares()...)
			}
		}
		ApplyLogLevel(c.Global.LogLevel)
	}

	execPrep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.ExecJobs, parsed.ExecJobs, execPrep, JobSourceINI, "exec")

	runPrep := func(name string, j *RunJobConfig) {
		_ = defaults.Set(j)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.RunJobs, parsed.RunJobs, runPrep, JobSourceINI, "run")

	localPrep := func(name string, j *LocalJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}
	syncJobMap(c, c.LocalJobs, parsed.LocalJobs, localPrep, JobSourceINI, "local")

	svcPrep := func(name string, j *RunServiceConfig) {
		_ = defaults.Set(j)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Client = c.dockerHandler.GetInternalDockerClient()
		j.Name = name
	}
	syncJobMap(c, c.ServiceJobs, parsed.ServiceJobs, svcPrep, JobSourceINI, "service")

	composePrep := func(name string, j *ComposeJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}
	syncJobMap(c, c.ComposeJobs, parsed.ComposeJobs, composePrep, JobSourceINI, "compose")

	return nil
}

// ExecJobConfig contains all configuration params needed to build a ExecJob
type ExecJobConfig struct {
	core.ExecJob              `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

func (c *ExecJobConfig) buildMiddlewares() {
	c.ExecJob.Use(middlewares.NewOverlap(&c.OverlapConfig))
	c.ExecJob.Use(middlewares.NewSlack(&c.SlackConfig))
	c.ExecJob.Use(middlewares.NewSave(&c.SaveConfig))
	c.ExecJob.Use(middlewares.NewMail(&c.MailConfig))
}

func (c *ExecJobConfig) GetJobSource() JobSource  { return c.JobSource }
func (c *ExecJobConfig) SetJobSource(s JobSource) { c.JobSource = s }

// RunServiceConfig contains all configuration params needed to build a RunJob
type RunServiceConfig struct {
	core.RunServiceJob        `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

func (c *RunServiceConfig) GetJobSource() JobSource  { return c.JobSource }
func (c *RunServiceConfig) SetJobSource(s JobSource) { c.JobSource = s }

type RunJobConfig struct {
	core.RunJob               `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

func (c *RunJobConfig) buildMiddlewares() {
	c.RunJob.Use(middlewares.NewOverlap(&c.OverlapConfig))
	c.RunJob.Use(middlewares.NewSlack(&c.SlackConfig))
	c.RunJob.Use(middlewares.NewSave(&c.SaveConfig))
	c.RunJob.Use(middlewares.NewMail(&c.MailConfig))
}

func (c *RunJobConfig) GetJobSource() JobSource  { return c.JobSource }
func (c *RunJobConfig) SetJobSource(s JobSource) { c.JobSource = s }

// LocalJobConfig contains all configuration params needed to build a RunJob
type LocalJobConfig struct {
	core.LocalJob             `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

func (c *LocalJobConfig) GetJobSource() JobSource  { return c.JobSource }
func (c *LocalJobConfig) SetJobSource(s JobSource) { c.JobSource = s }

type ComposeJobConfig struct {
	core.ComposeJob           `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

func (c *ComposeJobConfig) GetJobSource() JobSource  { return c.JobSource }
func (c *ComposeJobConfig) SetJobSource(s JobSource) { c.JobSource = s }

func (c *LocalJobConfig) buildMiddlewares() {
	c.LocalJob.Use(middlewares.NewOverlap(&c.OverlapConfig))
	c.LocalJob.Use(middlewares.NewSlack(&c.SlackConfig))
	c.LocalJob.Use(middlewares.NewSave(&c.SaveConfig))
	c.LocalJob.Use(middlewares.NewMail(&c.MailConfig))
}

func (c *ComposeJobConfig) buildMiddlewares() {
	c.ComposeJob.Use(middlewares.NewOverlap(&c.OverlapConfig))
	c.ComposeJob.Use(middlewares.NewSlack(&c.SlackConfig))
	c.ComposeJob.Use(middlewares.NewSave(&c.SaveConfig))
	c.ComposeJob.Use(middlewares.NewMail(&c.MailConfig))
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
	if err := parseGlobalAndDocker(cfg, c); err != nil {
		return err
	}
	for _, section := range cfg.Sections() {
		name := strings.TrimSpace(section.Name())
		switch {
		case strings.HasPrefix(name, jobExec):
			if err := decodeJob(
				section,
				&ExecJobConfig{JobSource: JobSourceINI},
				func(n string, j *ExecJobConfig) { c.ExecJobs[n] = j },
				jobExec,
			); err != nil {
				return err
			}
		case strings.HasPrefix(name, jobRun):
			if err := decodeJob(
				section,
				&RunJobConfig{JobSource: JobSourceINI},
				func(n string, j *RunJobConfig) { c.RunJobs[n] = j },
				jobRun,
			); err != nil {
				return err
			}
		case strings.HasPrefix(name, jobServiceRun):
			if err := decodeJob(
				section,
				&RunServiceConfig{JobSource: JobSourceINI},
				func(n string, j *RunServiceConfig) { c.ServiceJobs[n] = j },
				jobServiceRun,
			); err != nil {
				return err
			}
		case strings.HasPrefix(name, jobLocal):
			if err := decodeJob(
				section,
				&LocalJobConfig{JobSource: JobSourceINI},
				func(n string, j *LocalJobConfig) { c.LocalJobs[n] = j },
				jobLocal,
			); err != nil {
				return err
			}
		case strings.HasPrefix(name, jobCompose):
			if err := decodeJob(
				section,
				&ComposeJobConfig{JobSource: JobSourceINI},
				func(n string, j *ComposeJobConfig) { c.ComposeJobs[n] = j },
				jobCompose,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func latestChanged(files []string, prev time.Time) (time.Time, bool, error) {
	var latest time.Time
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("stat %q: %w", f, err)
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest, latest.After(prev), nil
}

func parseGlobalAndDocker(cfg *ini.File, c *Config) error {
	if sec, err := cfg.GetSection("global"); err == nil {
		if err := mapstructure.WeakDecode(sectionToMap(sec), &c.Global); err != nil {
			return fmt.Errorf("decode [global]: %w", err)
		}
	}
	if sec, err := cfg.GetSection("docker"); err == nil {
		if err := mapstructure.WeakDecode(sectionToMap(sec), &c.Docker); err != nil {
			return fmt.Errorf("decode [docker]: %w", err)
		}
	}
	return nil
}

func decodeJob[T jobConfig](section *ini.Section, job T, set func(string, T), prefix string) error {
	jobName := parseJobName(strings.TrimSpace(section.Name()), prefix)
	if err := mapstructure.WeakDecode(sectionToMap(section), job); err != nil {
		return fmt.Errorf("decode job %q: %w", jobName, err)
	}
	set(jobName, job)
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
		switch {
		case len(vals) > 1:
			cp := make([]string, len(vals))
			copy(cp, vals)
			m[key.Name()] = cp
		case len(vals) == 1:
			m[key.Name()] = vals[0]
		default:
			// Handle empty values
			m[key.Name()] = ""
		}
	}
	return m
}
