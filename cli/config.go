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

	"github.com/netresearch/ofelia/config"
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
		AllowHostJobsFromLabels bool          `gcfg:"allow-host-jobs-from-labels" mapstructure:"allow-host-jobs-from-labels" default:"false"` //nolint:revive
		EnableStrictValidation  bool          `gcfg:"enable-strict-validation" mapstructure:"enable-strict-validation" default:"false"`
		// DefaultUser sets the default user for exec/run/service jobs when not specified per-job.
		// Set to empty string "" to use the container's default user.
		// Default: "nobody" (secure unprivileged user)
		DefaultUser string `gcfg:"default-user" mapstructure:"default-user" default:"nobody"`
		// NotificationCooldown sets the minimum time between duplicate error notifications.
		// When a job fails with the same error, notifications (Slack, email, save) will be
		// suppressed until this cooldown period expires. Set to 0 to disable deduplication.
		// Default: 0 (disabled - all notifications sent)
		NotificationCooldown time.Duration `gcfg:"notification-cooldown" mapstructure:"notification-cooldown" default:"0"`
	}
	ExecJobs          map[string]*ExecJobConfig    `gcfg:"job-exec" mapstructure:"job-exec,squash"`
	RunJobs           map[string]*RunJobConfig     `gcfg:"job-run" mapstructure:"job-run,squash"`
	ServiceJobs       map[string]*RunServiceConfig `gcfg:"job-service-run" mapstructure:"job-service-run,squash"`
	LocalJobs         map[string]*LocalJobConfig   `gcfg:"job-local" mapstructure:"job-local,squash"`
	ComposeJobs       map[string]*ComposeJobConfig `gcfg:"job-compose" mapstructure:"job-compose,squash"`
	Docker            DockerConfig
	configPath        string
	configFiles       []string
	configModTime     time.Time
	sh                *core.Scheduler
	dockerHandler     *DockerHandler
	logger            core.Logger
	notificationDedup *middlewares.NotificationDedup
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
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return nil, fmt.Errorf("invalid glob pattern %q: %w\n  → Check pattern syntax (wildcards: *, ?, [abc])\n  → Example valid patterns: '/etc/ofelia/*.ini', '/etc/ofelia/config-*.ini'\n  → Escape special characters if using literal brackets\n  → Verify directory path exists before the pattern", pattern, err)
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
			//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
			return nil, fmt.Errorf("failed to load config file %q: %w\n  → Check file exists and is readable: ls -l %q\n  → Verify file path is correct\n  → Check file permissions (should be readable)", f, err, f)
		}
		if err := parseIni(cfg, c); err != nil {
			//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
			return nil, fmt.Errorf("failed to parse config file %q: %w\n  → Check INI syntax is valid (sections in [brackets], key=value pairs)\n  → Look for syntax errors near line mentioned in error\n  → Use 'ofelia validate --config=%q' to validate syntax", f, err, f)
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

	// Validate the loaded configuration (if enabled)
	if c.Global.EnableStrictValidation {
		validator := config.NewConfigValidator(c)
		if err := validator.Validate(); err != nil {
			//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
			return nil, fmt.Errorf("configuration validation failed: %w\n  → Review validation errors above for specific issues\n  → Check job schedules are valid cron expressions\n  → Verify required fields are set for all jobs\n  → Use 'ofelia validate --config=%q' for detailed validation", err, filename)
		}
	}

	return c, nil
}

// BuildFromString builds a scheduler using the config from a string

// newDockerHandler allows overriding Docker handler creation (e.g., for testing)
var newDockerHandler = NewDockerHandler

func BuildFromString(configStr string, logger core.Logger) (*Config, error) {
	c := NewConfig(logger)
	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true}, []byte(configStr))
	if err != nil {
		return nil, fmt.Errorf("load ini from string: %w", err)
	}
	if err := parseIni(cfg, c); err != nil {
		return nil, fmt.Errorf("parse ini from string: %w", err)
	}

	// Validate the loaded configuration (if enabled)
	if c.Global.EnableStrictValidation {
		validator := config.NewConfigValidator(c)
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("configuration validation failed: %w", err)
		}
	}

	return c, nil
}

// Call this only once at app init
func (c *Config) InitializeApp() error {
	c.sh = core.NewScheduler(c.logger)

	// Initialize notification deduplication if cooldown is set
	c.initNotificationDedup()

	c.buildSchedulerMiddlewares(c.sh)

	if err := c.initDockerHandler(); err != nil {
		return err
	}
	c.mergeJobsFromDockerLabels()
	c.registerAllJobs()
	return nil
}

// initNotificationDedup initializes the deduplicator and injects it into middleware configs
func (c *Config) initNotificationDedup() {
	if c.Global.NotificationCooldown <= 0 {
		return // Dedup disabled
	}

	c.notificationDedup = middlewares.NewNotificationDedup(c.Global.NotificationCooldown)
	middlewares.InitNotificationDedup(c.Global.NotificationCooldown)

	// Inject dedup into global middleware configs
	c.Global.SlackConfig.Dedup = c.notificationDedup
	c.Global.MailConfig.Dedup = c.notificationDedup

	c.logger.Noticef("Notification deduplication enabled with cooldown: %s", c.Global.NotificationCooldown)
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
	parsed := Config{
		logger: c.logger,
		Global: c.Global, // Copy Global settings including AllowHostJobsFromLabels
	}
	_ = parsed.buildFromDockerLabels(dockerLabels)

	mergeJobs(c, c.ExecJobs, parsed.ExecJobs, "exec")
	mergeJobs(c, c.RunJobs, parsed.RunJobs, "run")
	mergeJobs(c, c.LocalJobs, parsed.LocalJobs, "local")
	mergeJobs(c, c.ComposeJobs, parsed.ComposeJobs, "compose")

	mergeJobs(c, c.ServiceJobs, parsed.ServiceJobs, "service")
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
	provider := c.dockerHandler.GetDockerProvider()

	for name, j := range c.ExecJobs {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		j.Provider = provider
		j.InitializeRuntimeFields()
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.RunJobs {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Provider = provider
		j.InitializeRuntimeFields()
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.LocalJobs {
		_ = defaults.Set(j)
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.ServiceJobs {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Provider = provider
		j.InitializeRuntimeFields()
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.ComposeJobs {
		_ = defaults.Set(j)
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares()
		_ = c.sh.AddJob(j)
	}
}

// injectDedup sets the notification deduplicator on job-level middleware configs
func (c *Config) injectDedup(slack *middlewares.SlackConfig, mail *middlewares.MailConfig) {
	if c.notificationDedup == nil {
		return
	}
	slack.Dedup = c.notificationDedup
	mail.Dedup = c.notificationDedup
}

// mergeNotificationDefaults copies global notification settings to job-level configs
// when the job-level field has its zero value. This allows partial overrides where
// a job can specify only `mail-only-on-error: true` while inheriting SMTP settings.
func (c *Config) mergeNotificationDefaults(slack *middlewares.SlackConfig, mail *middlewares.MailConfig) {
	c.mergeSlackDefaults(slack)
	c.mergeMailDefaults(mail)
}

// mergeSlackDefaults copies global Slack settings to job config where job has zero values
func (c *Config) mergeSlackDefaults(job *middlewares.SlackConfig) {
	global := &c.Global.SlackConfig
	if job.SlackWebhook == "" {
		job.SlackWebhook = global.SlackWebhook
	}
	// Note: SlackOnlyOnError is a bool - we can't distinguish "not set" from "explicitly false"
	// So we don't inherit it; job must explicitly set it if they want error-only behavior
}

// mergeMailDefaults copies global Mail settings to job config where job has zero values
func (c *Config) mergeMailDefaults(job *middlewares.MailConfig) {
	global := &c.Global.MailConfig
	if job.SMTPHost == "" {
		job.SMTPHost = global.SMTPHost
	}
	if job.SMTPPort == 0 {
		job.SMTPPort = global.SMTPPort
	}
	if job.SMTPUser == "" {
		job.SMTPUser = global.SMTPUser
	}
	if job.SMTPPassword == "" {
		job.SMTPPassword = global.SMTPPassword
	}
	// SMTPTLSSkipVerify is a bool field with inherent Go limitation:
	// We cannot distinguish "not explicitly set" (zero value) from "explicitly set to false".
	// Inheritance behavior:
	//   - Global=true,  Job=false  → Job gets true  (inherits global's insecure setting)
	//   - Global=false, Job=false  → Job stays false (secure default, no change needed)
	//   - Global=true,  Job=true   → Job stays true  (already insecure)
	//   - Global=false, Job=true   → Job stays true  (job explicitly set insecure - CANNOT override)
	// The last case means a job CANNOT be forced to use TLS verification if it was
	// explicitly configured with smtp-tls-skip-verify=true. This is acceptable since
	// per-job security settings should be explicit, not silently inherited.
	if global.SMTPTLSSkipVerify && !job.SMTPTLSSkipVerify {
		job.SMTPTLSSkipVerify = global.SMTPTLSSkipVerify
	}
	if job.EmailTo == "" {
		job.EmailTo = global.EmailTo
	}
	if job.EmailFrom == "" {
		job.EmailFrom = global.EmailFrom
	}
	// Note: MailOnlyOnError is a bool - we can't distinguish "not set" from "explicitly false"
	// So we don't inherit it; job must explicitly set it if they want error-only behavior
}

// UserContainerDefault is the sentinel value that explicitly requests the container's default user,
// overriding any global default-user setting.
const UserContainerDefault = "default"

// applyDefaultUser sets the job's User field to the global default if not explicitly configured.
// This allows per-job override while respecting the global default-user setting.
// Special value "default" explicitly uses the container's default user (empty string).
func (c *Config) applyDefaultUser(user *string) {
	if *user == "" {
		*user = c.Global.DefaultUser
	} else if *user == UserContainerDefault {
		*user = "" // Use container's default user
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

	// Validate job configuration if the job type supports it
	if v, ok := any(newJob).(validatable); ok {
		if err := v.Validate(); err != nil {
			c.logger.Errorf("Job %q configuration error: %v", name, err)
			return false
		}
	}

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

// validatable is an optional interface for jobs that support validation
type validatable interface {
	Validate() error
}

func addNewJob[J jobConfig](c *Config, name string, j J, prep func(string, J), source JobSource, current map[string]J) {
	if source != "" {
		j.SetJobSource(source)
	}
	prep(name, j)

	// Validate job configuration if the job type supports it
	if v, ok := any(j).(validatable); ok {
		if err := v.Validate(); err != nil {
			c.logger.Errorf("Job %q configuration error: %v", name, err)
			return
		}
	}

	j.buildMiddlewares()
	_ = c.sh.AddJob(j)
	current[name] = j
}

func (c *Config) dockerLabelsUpdate(labels map[string]map[string]string) {
	c.logger.Debugf("dockerLabelsUpdate started")

	parsedLabelConfig := Config{
		logger: c.logger,
		Global: c.Global, // Copy Global settings including AllowHostJobsFromLabels
	}
	_ = parsedLabelConfig.buildFromDockerLabels(labels)

	execPrep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		j.Provider = c.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
	}
	syncJobMap(c, c.ExecJobs, parsedLabelConfig.ExecJobs, execPrep, JobSourceLabel, "exec")

	runPrep := func(name string, j *RunJobConfig) {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Provider = c.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
	}
	syncJobMap(c, c.RunJobs, parsedLabelConfig.RunJobs, runPrep, JobSourceLabel, "run")

	localPrep := func(name string, j *LocalJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	servicePrep := func(name string, j *RunServiceConfig) {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Provider = c.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
	}

	composePrep := func(name string, j *ComposeJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	// Security: Log consolidated warning when syncing host-based jobs from container labels
	if c.Global.AllowHostJobsFromLabels {
		localCount := len(parsedLabelConfig.LocalJobs)
		composeCount := len(parsedLabelConfig.ComposeJobs)
		if localCount > 0 || composeCount > 0 {
			c.logger.Warningf("SECURITY WARNING: Syncing host-based jobs from container labels (%d local, %d compose). "+
				"This allows containers to execute arbitrary commands on the host system.", localCount, composeCount)
		}
	}

	syncJobMap(c, c.LocalJobs, parsedLabelConfig.LocalJobs, localPrep, JobSourceLabel, "local")
	syncJobMap(c, c.ServiceJobs, parsedLabelConfig.ServiceJobs, servicePrep, JobSourceLabel, "service")
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
		if err := ApplyLogLevel(c.Global.LogLevel); err != nil {
			c.logger.Warningf("Failed to apply global log level (using default): %v", err)
		}
	}

	execPrep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		j.Provider = c.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
	}
	syncJobMap(c, c.ExecJobs, parsed.ExecJobs, execPrep, JobSourceINI, "exec")

	runPrep := func(name string, j *RunJobConfig) {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Provider = c.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields()
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
		c.applyDefaultUser(&j.User)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Provider = c.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields()
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

// Hash overrides BareJob.Hash() to include RunJob-specific fields
func (c *RunJobConfig) Hash() (string, error) {
	var hash string
	if err := core.GetHash(reflect.TypeOf(&c.RunJob).Elem(), reflect.ValueOf(&c.RunJob).Elem(), &hash); err != nil {
		return "", fmt.Errorf("failed to generate hash for RunJob config: %w", err)
	}
	return hash, nil
}

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
	Filters []string `mapstructure:"filters"`

	// ConfigPollInterval controls how often to check for INI config file changes.
	// This is independent of container detection. Set to 0 to disable config file watching.
	ConfigPollInterval time.Duration `mapstructure:"config-poll-interval" default:"10s"`

	// UseEvents enables Docker event-based container detection (recommended).
	// When enabled, Ofelia reacts immediately to container start/stop events.
	UseEvents bool `mapstructure:"events" default:"true"`

	// DockerPollInterval enables periodic polling for container changes.
	// This is a fallback for environments where Docker events don't work reliably.
	// Set to 0 (default) to disable explicit container polling.
	// WARNING: If both events and polling are enabled, this is usually wasteful.
	DockerPollInterval time.Duration `mapstructure:"docker-poll-interval" default:"0"`

	// PollingFallback auto-enables container polling if event subscription fails.
	// This provides backwards compatibility and resilience.
	// Set to 0 to disable auto-fallback (will only log errors on event failure).
	// Default is 10s for backwards compatibility.
	PollingFallback time.Duration `mapstructure:"polling-fallback" default:"10s"`

	// Deprecated: Use ConfigPollInterval and DockerPollInterval instead.
	// If set, this value is used for both config and container polling (BC).
	PollInterval time.Duration `mapstructure:"poll-interval"`

	// Deprecated: Use DockerPollInterval=0 instead.
	// If true, disables container polling entirely.
	DisablePolling bool `mapstructure:"no-poll" default:"false"`
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
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return fmt.Errorf("failed to decode job %q configuration: %w\n  → Check job section syntax in config file\n  → Verify all required fields are set (schedule, command, container, etc.)\n  → Check for typos in configuration keys\n  → Use 'ofelia validate --config=<file>' to validate configuration\n  → Review job type requirements (job-exec, job-run, job-local, job-service-run)", jobName, err)
	}

	// Validate job configuration if the job type supports it
	if v, ok := any(job).(validatable); ok {
		if err := v.Validate(); err != nil {
			//nolint:revive // Error message intentionally verbose for UX
			return fmt.Errorf("job %q configuration error: %w\n  → Check required fields for this job type\n  → job-run requires 'image' OR 'container'\n  → job-service-run requires 'image'", jobName, err)
		}
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
