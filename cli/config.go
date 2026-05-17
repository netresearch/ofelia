// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	defaults "github.com/creasty/defaults"
	ini "gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/config"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
)

const (
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
		middlewares.SlackConfig         `mapstructure:",squash"`
		middlewares.SaveConfig          `mapstructure:",squash"`
		middlewares.MailConfig          `mapstructure:",squash"`
		middlewares.WebhookGlobalConfig `mapstructure:",squash"`
		LogLevel                        string        `gcfg:"log-level" mapstructure:"log-level" validate:"omitempty,oneof=debug info notice trace warn warning error fatal panic critical"` //nolint:revive
		EnableWeb                       bool          `gcfg:"enable-web" mapstructure:"enable-web" default:"false"`
		WebAddr                         string        `gcfg:"web-address" mapstructure:"web-address" default:":8081"`
		WebAuthEnabled                  bool          `gcfg:"web-auth-enabled" mapstructure:"web-auth-enabled" default:"false"`
		WebUsername                     string        `gcfg:"web-username" mapstructure:"web-username"`
		WebPasswordHash                 string        `gcfg:"web-password-hash" mapstructure:"web-password-hash" json:"-"`
		WebSecretKey                    string        `gcfg:"web-secret-key" mapstructure:"web-secret-key" json:"-"`
		WebTokenExpiry                  int           `gcfg:"web-token-expiry" mapstructure:"web-token-expiry" validate:"gte=1,lte=8760" default:"24"`           //nolint:revive
		WebMaxLoginAttempts             int           `gcfg:"web-max-login-attempts" mapstructure:"web-max-login-attempts" validate:"gte=1,lte=100" default:"5"` //nolint:revive
		WebTrustedProxies               []string      `gcfg:"web-trusted-proxies" mapstructure:"web-trusted-proxies,"`
		EnablePprof                     bool          `gcfg:"enable-pprof" mapstructure:"enable-pprof" default:"false"`
		PprofAddr                       string        `gcfg:"pprof-address" mapstructure:"pprof-address" default:"127.0.0.1:8080"`
		MaxRuntime                      time.Duration `gcfg:"max-runtime" mapstructure:"max-runtime" validate:"gte=0" default:"24h"`
		AllowHostJobsFromLabels         bool          `gcfg:"allow-host-jobs-from-labels" mapstructure:"allow-host-jobs-from-labels" default:"false"` //nolint:revive
		EnableStrictValidation          bool          `gcfg:"enable-strict-validation" mapstructure:"enable-strict-validation" default:"false"`       //nolint:revive
		// DefaultUser sets the default user for exec/run/service jobs when not specified per-job.
		// Set to empty string "" to use the container's default user.
		// Default: "nobody" (secure unprivileged user)
		DefaultUser string `gcfg:"default-user" mapstructure:"default-user" default:"nobody"`
		// NotificationCooldown sets the minimum time between duplicate error notifications.
		// When a job fails with the same error, notifications (Slack, email, save) will be
		// suppressed until this cooldown period expires. Set to 0 to disable deduplication.
		// Default: 0 (disabled - all notifications sent)
		NotificationCooldown time.Duration `gcfg:"notification-cooldown" mapstructure:"notification-cooldown" validate:"gte=0" default:"0"`
	}
	ExecJobs          map[string]*ExecJobConfig    `gcfg:"job-exec" mapstructure:"job-exec,squash"`
	RunJobs           map[string]*RunJobConfig     `gcfg:"job-run" mapstructure:"job-run,squash"`
	ServiceJobs       map[string]*RunServiceConfig `gcfg:"job-service-run" mapstructure:"job-service-run,squash"`
	LocalJobs         map[string]*LocalJobConfig   `gcfg:"job-local" mapstructure:"job-local,squash"`
	ComposeJobs       map[string]*ComposeJobConfig `gcfg:"job-compose" mapstructure:"job-compose,squash"`
	Docker            DockerConfig
	mu                sync.RWMutex // protects job map access
	configPath        string
	configFiles       []string
	configModTime     time.Time
	sh                *core.Scheduler
	dockerHandler     *DockerHandler
	logger            *slog.Logger
	levelVar          *slog.LevelVar
	notificationDedup *middlewares.NotificationDedup
	WebhookConfigs    *WebhookConfigs
}

func NewConfig(logger *slog.Logger) *Config {
	c := &Config{
		ExecJobs:       make(map[string]*ExecJobConfig),
		RunJobs:        make(map[string]*RunJobConfig),
		ServiceJobs:    make(map[string]*RunServiceConfig),
		LocalJobs:      make(map[string]*LocalJobConfig),
		ComposeJobs:    make(map[string]*ComposeJobConfig),
		WebhookConfigs: NewWebhookConfigs(),
		logger:         logger,
	}

	_ = defaults.Set(c)
	// Seed the embedded WebhookGlobalConfig with the same defaults that
	// NewWebhookConfigs() applied to c.WebhookConfigs.Global. This ensures
	// that keys omitted from the [global] INI section keep their defaults
	// (notably AllowedHosts="*", PresetCacheTTL=24h, PresetCacheDir).
	c.Global.WebhookGlobalConfig = *middlewares.DefaultWebhookGlobalConfig()
	// Alias c.WebhookConfigs.Global into the embedded struct so that
	// mapstructure-decoded values from the [global] INI section become
	// visible to the webhook subsystem without a hand-rolled field-by-field
	// copy. This closes the dual-store gap flagged in #620 for the INI
	// live-reload path: mutating Config.Global automatically refreshes what
	// c.WebhookConfigs.Global / WebhookManager.globalConfig dereference.
	//
	// NOTE: the Docker label sync path builds a scratch parsed Config via
	// newScratchConfig, which mirrors this same alias so mapstructure-decoded
	// label values land in the live WebhookConfigs.Global store too (#641).
	// mergeWebhookConfigs / syncWebhookConfigs then forward the
	// operator-tunable, non-SSRF globals (webhook-webhooks,
	// webhook-preset-cache-ttl) into the live config. The SSRF-sensitive
	// globals (webhook-allowed-hosts and friends) stay INI-only by design —
	// see #486, #640 and the comment above globalLabelAllowList.
	c.WebhookConfigs.Global = &c.Global.WebhookGlobalConfig
	return c
}

// newScratchConfig builds a transient Config used by the Docker label sync
// path. It carries the live config's logger and Global settings (so gating
// flags like AllowHostJobsFromLabels keep applying) and re-establishes the
// same WebhookConfigs.Global pointer alias that NewConfig sets up for the
// live Config — without this, mapstructure decoding into Global.WebhookGlobalConfig
// from container labels would never reach WebhookConfigs.Global, silently
// dropping any future label-forwarded fields (#641, prerequisite for #640).
//
// The scratch instance intentionally allocates fresh job maps via the
// zero-value Config: dockerContainersUpdate / mergeJobsFromDockerContainers
// merge those back into the live config under c.mu.
func newScratchConfig(c *Config) *Config {
	scratch := &Config{
		logger:         c.logger,
		Global:         c.Global, // value-copy: scratch mutations do not bleed into c
		WebhookConfigs: NewWebhookConfigs(),
	}
	// Mirror the NewConfig invariant so decodeWithMetadata writes into the
	// embedded WebhookGlobalConfig become visible to the webhook subsystem
	// (which always reads through WebhookConfigs.Global).
	scratch.WebhookConfigs.Global = &scratch.Global.WebhookGlobalConfig
	return scratch
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
func BuildFromFile(filename string, logger *slog.Logger) (*Config, error) {
	files, err := resolveConfigFiles(filename)
	if err != nil {
		return nil, err
	}

	c := NewConfig(logger)
	var latest time.Time
	allUsedKeys := make(map[string]bool)

	for _, f := range files {
		data, err := os.ReadFile(f) // #nosec G304 -- file path from user config flag, not external input
		if err != nil {
			//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
			return nil, fmt.Errorf("failed to load config file %q: %w\n  → Check file exists and is readable: ls -l %q\n  → Verify file path is correct\n  → Check file permissions (should be readable)", f, err, f)
		}
		cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true}, data)
		if err != nil {
			//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
			return nil, fmt.Errorf("failed to parse config file %q: %w\n  → Check INI syntax is valid\n  → Verify environment variable substitutions resolve correctly", f, err)
		}
		parseRes, parseErr := parseIni(cfg, c)
		if parseErr != nil {
			//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
			return nil, fmt.Errorf("failed to parse config file %q: %w\n  → Check INI syntax is valid (sections in [brackets], key=value pairs)\n  → Look for syntax errors near line mentioned in error\n  → Use 'ofelia validate --config=%q' to validate syntax", f, parseErr, f)
		}

		// Merge used keys from this file
		if parseRes != nil {
			for k, v := range parseRes.usedKeys {
				if v {
					allUsedKeys[k] = true
				}
			}

			// Log warnings for unknown keys
			logUnknownKeyWarnings(logger, f, parseRes)
		}

		if info, statErr := os.Stat(f); statErr == nil {
			if info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
		logger.Debug("loaded config file", "file", f)
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

	// Handle deprecated configuration options: migrate then warn (using key presence)
	ResetDeprecationWarnings()
	deprecationRegistry.SetLogger(logger)
	ApplyDeprecationMigrationsWithKeys(c, allUsedKeys)
	CheckDeprecationsWithKeys(c, allUsedKeys)

	return c, nil
}

// logUnknownKeyWarnings logs warnings for unknown configuration keys
func logUnknownKeyWarnings(logger *slog.Logger, filename string, res *parseResult) {
	if res == nil {
		return
	}

	if len(res.unknownGlobal) > 0 {
		logSectionUnknownKeyWarnings(logger, "global", res.unknownGlobal, globalKnownKeys(), filename)
	}
	if len(res.unknownDocker) > 0 {
		logSectionUnknownKeyWarnings(logger, "docker", res.unknownDocker, dockerKnownKeys(), filename)
	}

	// Log warnings for unknown keys in job sections
	logJobUnknownKeyWarnings(logger, res.unknownJobs, filename)
}

// logSectionUnknownKeyWarnings emits a "Unknown configuration key … in [section]"
// warning for each key, with a "did you mean?" suggestion when a close match is
// found in knownKeys. Used by [global] and [docker] sections so the suggestion
// behavior is at parity with the job-section path (issue #678).
func logSectionUnknownKeyWarnings(logger *slog.Logger, section string, unknownKeys, knownKeys []string, filename string) {
	for _, key := range unknownKeys {
		suggestion := findClosestMatch(key, knownKeys)
		var msg string
		switch {
		case suggestion != "" && filename != "":
			msg = fmt.Sprintf("Unknown configuration key '%s' in [%s] section of %s (did you mean '%s'?)",
				key, section, filename, suggestion)
		case suggestion != "":
			msg = fmt.Sprintf("Unknown configuration key '%s' in [%s] section (did you mean '%s'?)", key, section, suggestion)
		case filename != "":
			msg = fmt.Sprintf("Unknown configuration key '%s' in [%s] section of %s (typo?)", key, section, filename)
		default:
			msg = fmt.Sprintf("Unknown configuration key '%s' in [%s] section (typo?)", key, section)
		}
		// Drop the "file" structured attr for string-based configs (filename
		// is empty) so JSON logs don't carry a noisy file="" field for the
		// BuildFromString path.
		if filename != "" {
			logger.Warn(msg, "key", key, "file", filename)
		} else {
			logger.Warn(msg, "key", key)
		}
	}
}

// globalKnownKeys returns the list of valid mapstructure keys for the [global]
// INI section. Derived from Config{}.Global so the suggestion list cannot
// drift from the actual decoded struct.
func globalKnownKeys() []string {
	return extractMapstructureKeys(Config{}.Global)
}

// dockerKnownKeys returns the list of valid mapstructure keys for the [docker]
// INI section. Derived from DockerConfig{} so the suggestion list cannot drift
// from the actual decoded struct.
func dockerKnownKeys() []string {
	return extractMapstructureKeys(DockerConfig{})
}

// logJobUnknownKeyWarnings logs warnings for unknown keys in job sections with
// "did you mean?" suggestions. If filename is non-empty, it is included in the message.
func logJobUnknownKeyWarnings(logger *slog.Logger, unknownJobs []jobUnknownKeys, filename string) {
	for _, job := range unknownJobs {
		knownKeys := getKnownKeysForJobType(job.JobType)
		for _, key := range job.UnknownKeys {
			suggestion := findClosestMatch(key, knownKeys)
			if filename != "" {
				if suggestion != "" {
					logger.Warn(fmt.Sprintf("Unknown configuration key '%s' in [%s \"%s\"] of %s (did you mean '%s'?)",
						key, job.JobType, job.JobName, filename, suggestion))
				} else {
					logger.Warn(fmt.Sprintf("Unknown configuration key '%s' in [%s \"%s\"] of %s (typo?)",
						key, job.JobType, job.JobName, filename))
				}
			} else {
				if suggestion != "" {
					logger.Warn(fmt.Sprintf("Unknown configuration key '%s' in [%s \"%s\"] (did you mean '%s'?)",
						key, job.JobType, job.JobName, suggestion))
				} else {
					logger.Warn(fmt.Sprintf("Unknown configuration key '%s' in [%s \"%s\"] (typo?)",
						key, job.JobType, job.JobName))
				}
			}
		}
	}
}

// getKnownKeysForJobType returns the list of valid configuration keys for a given job type.
// It extracts all mapstructure keys from the corresponding job config struct.
func getKnownKeysForJobType(jobType string) []string {
	switch jobType {
	case jobExec:
		return extractMapstructureKeys(ExecJobConfig{})
	case jobRun:
		return extractMapstructureKeys(RunJobConfig{})
	case jobServiceRun:
		return extractMapstructureKeys(RunServiceConfig{})
	case jobLocal:
		return extractMapstructureKeys(LocalJobConfig{})
	case jobCompose:
		return extractMapstructureKeys(ComposeJobConfig{})
	default:
		return nil
	}
}

// BuildFromString builds a scheduler using the config from a string

// newDockerHandler allows overriding Docker handler creation (e.g., for testing)
var newDockerHandler = NewDockerHandler

func BuildFromString(configStr string, logger *slog.Logger) (*Config, error) {
	c := NewConfig(logger)
	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true}, []byte(configStr))
	if err != nil {
		return nil, fmt.Errorf("load ini from string: %w", err)
	}
	parseRes, parseErr := parseIni(cfg, c)
	if parseErr != nil {
		return nil, fmt.Errorf("parse ini from string: %w", parseErr)
	}

	// Collect used keys for deprecation detection
	usedKeys := make(map[string]bool)
	if parseRes != nil {
		usedKeys = parseRes.usedKeys

		// Log warnings for unknown keys (empty filename for string-based config)
		if len(parseRes.unknownGlobal) > 0 {
			logSectionUnknownKeyWarnings(logger, "global", parseRes.unknownGlobal, globalKnownKeys(), "")
		}
		if len(parseRes.unknownDocker) > 0 {
			logSectionUnknownKeyWarnings(logger, "docker", parseRes.unknownDocker, dockerKnownKeys(), "")
		}

		// Log warnings for unknown keys in job sections (empty filename for string-based config)
		logJobUnknownKeyWarnings(logger, parseRes.unknownJobs, "")
	}

	// Validate the loaded configuration (if enabled)
	if c.Global.EnableStrictValidation {
		validator := config.NewConfigValidator(c)
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("configuration validation failed: %w", err)
		}
	}

	// Handle deprecated configuration options: migrate then warn (using key presence)
	ResetDeprecationWarnings()
	deprecationRegistry.SetLogger(logger)
	ApplyDeprecationMigrationsWithKeys(c, usedKeys)
	CheckDeprecationsWithKeys(c, usedKeys)

	return c, nil
}

// Call this only once at app init
func (c *Config) InitializeApp() error {
	c.sh = core.NewScheduler(c.logger)

	// Initialize notification deduplication if cooldown is set
	c.initNotificationDedup()

	// Init Docker and merge labels BEFORE webhook manager, so label-defined
	// webhooks are collected into WebhookConfigs before InitManager runs.
	if err := c.initDockerHandler(); err != nil {
		return err
	}
	c.mergeJobsFromDockerContainers()

	// Initialize webhook manager after all sources (INI + labels) are collected
	if c.WebhookConfigs != nil && len(c.WebhookConfigs.Webhooks) > 0 {
		if err := c.WebhookConfigs.InitManager(); err != nil {
			return fmt.Errorf("initialize webhook manager: %w", err)
		}
		c.logger.Info("Webhook notification system initialized", "count", len(c.WebhookConfigs.Webhooks))
	}

	c.buildSchedulerMiddlewares(c.sh)
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

	c.logger.Info(fmt.Sprintf("Notification deduplication enabled with cooldown: %s", c.Global.NotificationCooldown))
}

// getWebhookManager returns the webhook manager if initialized, nil otherwise
func (c *Config) getWebhookManager() *middlewares.WebhookManager {
	if c.WebhookConfigs != nil {
		return c.WebhookConfigs.Manager
	}
	return nil
}

// refreshWebhookManagerOnGlobalChange re-initializes the webhook manager so that
// snapshotted state (URL validator wired up via SetGlobalSecurityConfig, preset
// loader) picks up new values from the embedded WebhookGlobalConfig. The data
// store itself is already live thanks to the c.WebhookConfigs.Global pointer
// alias set up in NewConfig() (#620). Skipped when the manager was never
// initialized (no webhooks configured at startup) — nothing to refresh.
func (c *Config) refreshWebhookManagerOnGlobalChange() {
	if c.WebhookConfigs == nil || c.WebhookConfigs.Manager == nil {
		return
	}
	if err := c.WebhookConfigs.InitManager(); err != nil {
		c.logger.Error("Failed to re-initialize webhook manager during live-reload", "error", err)
	}
}

// mergeReloadedWebhookSections copies newly added [webhook "name"] sections
// from the reloaded INI's parsed config into the live config. Returns true
// when at least one new webhook was added so the caller can re-init the
// webhook manager (registering the new entries and refreshing the URL
// validator). INI-defined webhooks already present are left untouched: a
// rename of an existing section is treated as a separate add (the operator
// can clear stale entries by restarting the daemon if needed). See #640.
func (c *Config) mergeReloadedWebhookSections(parsed *Config) bool {
	if parsed == nil || parsed.WebhookConfigs == nil {
		return false
	}
	if c.WebhookConfigs == nil {
		c.WebhookConfigs = NewWebhookConfigs()
	}
	if c.WebhookConfigs.iniWebhookNames == nil {
		c.WebhookConfigs.iniWebhookNames = make(map[string]struct{})
	}
	added := false
	for name, wh := range parsed.WebhookConfigs.Webhooks {
		if _, exists := c.WebhookConfigs.Webhooks[name]; exists {
			continue
		}
		c.WebhookConfigs.Webhooks[name] = wh
		c.WebhookConfigs.iniWebhookNames[name] = struct{}{}
		added = true
	}
	return added
}

func (c *Config) initDockerHandler() error {
	var err error
	c.dockerHandler, err = newDockerHandler(context.Background(), c, c.logger, &c.Docker, nil)
	return err
}

func (c *Config) mergeJobsFromDockerContainers() {
	dockerContainers, err := c.dockerHandler.GetDockerContainers()
	if err != nil {
		return
	}
	parsed := newScratchConfig(c)
	_ = parsed.buildFromDockerContainers(dockerContainers)

	mergeJobs(c, c.ExecJobs, parsed.ExecJobs, "exec")
	mergeJobs(c, c.RunJobs, parsed.RunJobs, "run")
	mergeJobs(c, c.LocalJobs, parsed.LocalJobs, "local")
	mergeJobs(c, c.ComposeJobs, parsed.ComposeJobs, "compose")
	mergeJobs(c, c.ServiceJobs, parsed.ServiceJobs, "service")

	// Merge webhook configs from labels (INI takes precedence)
	mergeWebhookConfigs(c, parsed.WebhookConfigs)

	// Forward every other allow-listed global key (Slack, Mail, Save,
	// log-level, max-runtime, notification-cooldown, enable-strict-validation)
	// from the label-decoded scratch into the live c.Global. Boot path runs
	// before registerAllJobs, so subsequent mergeNotificationDefaults() calls
	// will inherit fresh values for every job. See #652 (sibling fix to #650).
	//
	// Snapshot LogLevel + NotificationCooldown BEFORE the merge: the daemon
	// already called ApplyLogLevel and the upcoming InitializeApp body called
	// initNotificationDedup with the INI-only values. If a label sets either,
	// we must re-run the corresponding side effect here so the daemon doesn't
	// silently ignore label-supplied values for the entire process — same
	// shape as the headline #652 regression but for the process-wide knobs
	// rather than per-job middleware fields.
	prevLogLevel := c.Global.LogLevel
	prevCooldown := c.Global.NotificationCooldown
	_ = c.applyAllowListedGlobals(parsed)
	c.refreshRuntimeKnobsAfterGlobalMerge(prevLogLevel, prevCooldown)
}

// mergeJobs copies jobs from src into dst while respecting INI precedence.
func mergeJobs[T jobConfig](c *Config, dst map[string]T, src map[string]T, kind string) {
	for name, j := range src {
		if existing, ok := dst[name]; ok && existing.GetJobSource() == JobSourceINI {
			c.logger.Warn(fmt.Sprintf("ignoring label-defined %s job %q because an INI job with the same name exists", kind, name))
			continue
		}
		dst[name] = j
	}
}

func (c *Config) registerAllJobs() {
	provider := c.dockerHandler.GetDockerProvider()

	wm := c.getWebhookManager()

	for name, j := range c.ExecJobs {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		j.Provider = provider
		j.InitializeRuntimeFields()
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares(c.logger, wm)
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
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares(c.logger, wm)
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.LocalJobs {
		_ = defaults.Set(j)
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares(c.logger, wm)
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
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares(c.logger, wm)
		_ = c.sh.AddJob(j)
	}
	for name, j := range c.ComposeJobs {
		_ = defaults.Set(j)
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
		j.buildMiddlewares(c.logger, wm)
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
func (c *Config) mergeNotificationDefaults(slack *middlewares.SlackConfig, mail *middlewares.MailConfig, save *middlewares.SaveConfig) {
	c.mergeSlackDefaults(slack)
	c.mergeMailDefaults(mail)
	c.mergeSaveDefaults(save)
}

// mergeSlackDefaults copies global Slack settings to job config where job has zero values
func (c *Config) mergeSlackDefaults(job *middlewares.SlackConfig) {
	global := &c.Global.SlackConfig
	if job.SlackWebhook == "" {
		job.SlackWebhook = global.SlackWebhook
	}
	// SlackOnlyOnError: inherit from global only when the job didn't explicitly set it (nil).
	if job.SlackOnlyOnError == nil && global.SlackOnlyOnError != nil {
		job.SlackOnlyOnError = new(*global.SlackOnlyOnError)
	}
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
	// SMTPTLSPolicy inherits when the job didn't explicitly set it. The
	// empty string is the documented "use default (mandatory)" sentinel,
	// so this preserves operator intent: a job that omits the key inherits
	// from [global], a job that explicitly sets it (including to "none"
	// for a test fixture) keeps that value. See #653.
	if job.SMTPTLSPolicy == "" {
		job.SMTPTLSPolicy = global.SMTPTLSPolicy
	}
	if job.EmailTo == "" {
		job.EmailTo = global.EmailTo
	}
	if job.EmailFrom == "" {
		job.EmailFrom = global.EmailFrom
	}
	if job.EmailSubject == "" {
		job.EmailSubject = global.EmailSubject
	}
	// MailOnlyOnError: inherit from global only when the job didn't explicitly set it (nil).
	if job.MailOnlyOnError == nil && global.MailOnlyOnError != nil {
		job.MailOnlyOnError = new(*global.MailOnlyOnError)
	}
}

// mergeSaveDefaults copies global Save settings to job config where job has zero values
func (c *Config) mergeSaveDefaults(job *middlewares.SaveConfig) {
	global := &c.Global.SaveConfig
	if job.SaveFolder == "" {
		job.SaveFolder = global.SaveFolder
	}
	// SaveOnlyOnError: inherit from global only when the job didn't explicitly set it (nil).
	if job.SaveOnlyOnError == nil && global.SaveOnlyOnError != nil {
		job.SaveOnlyOnError = new(*global.SaveOnlyOnError)
	}
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
	sh.Use(middlewares.NewSlack(&c.Global.SlackConfig)) //nolint:staticcheck // deprecated but kept for backwards compatibility
	sh.Use(middlewares.NewSave(&c.Global.SaveConfig))
	sh.Use(middlewares.NewMail(&c.Global.MailConfig))

	// Note: global webhooks are intentionally NOT attached to the scheduler
	// chain. core.Scheduler.AddJobWithTags() calls j.Use(s.Middlewares()...)
	// to propagate scheduler middlewares to each job, and
	// core.middlewareContainer.Use() would dedup the scheduler's
	// *WebhookMiddleware against the per-job one (same type), silently
	// dropping every global webhook. Instead, attachJobWebhookMiddlewares
	// merges the global names into each job's webhook list at attach time
	// so a single per-job composite carries both sets. See #670.
}

// attachJobWebhookMiddlewares resolves the union of global + per-job webhook
// names to middlewares and attaches them through use, wrapped in a single
// middlewares.NewWebhookMiddleware composite. Wrapping is required because
// core.middlewareContainer.Use() deduplicates by reflect type — adding
// individual *middlewares.Webhook instances would lose every webhook past
// the first. Errors from resolution (unknown name, preset load, missing
// required variable) are logged so misconfigurations are visible instead
// of silently disabling notifications.
//
// jobName is used only in the error log so operators can locate the offending
// job from a multi-job daemon.
//
// See https://github.com/netresearch/ofelia/issues/670.
func attachJobWebhookMiddlewares(
	logger *slog.Logger,
	jobName string,
	wm *middlewares.WebhookManager,
	perJobNames []string,
	use func(...core.Middleware),
) {
	if wm == nil {
		return
	}
	names := unionWebhookNames(wm.GlobalWebhookNames(), perJobNames)
	if len(names) == 0 {
		return
	}
	mws, err := wm.GetMiddlewares(names)
	if err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		const msg = "webhook middleware attach failed; webhook notifications " +
			"disabled for this job until config changes trigger a rebuild " +
			"(or daemon restart). enable log-level=debug for resolution details."
		logger.Error(msg, "job", jobName, "webhooks", names, "error", err)
		return
	}
	if composite := middlewares.NewWebhookMiddleware(mws); composite != nil {
		use(composite)
	}
}

// unionWebhookNames returns the deduplicated union of global and per-job
// webhook names, in global-first order. Duplicates (where a per-job entry
// names a webhook already in the global list) are silently dropped so each
// webhook resolves and fires exactly once per execution.
func unionWebhookNames(global, perJob []string) []string {
	if len(global) == 0 {
		return perJob
	}
	if len(perJob) == 0 {
		return global
	}
	seen := make(map[string]struct{}, len(global)+len(perJob))
	out := make([]string, 0, len(global)+len(perJob))
	for _, n := range global {
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	for _, n := range perJob {
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// jobConfig is implemented by all job configuration types that can be
// scheduled. It allows handling job maps in a generic way.
//
// The logger parameter on buildMiddlewares is used for webhook-attachment
// error logging so it reaches the daemon's configured handler/level rather
// than slog.Default(); nil falls back to slog.Default() for tests that don't
// care about log capture.
type jobConfig interface {
	core.Job
	buildMiddlewares(logger *slog.Logger, wm *middlewares.WebhookManager)
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
				c.logger.Warn(fmt.Sprintf("overriding label-defined %s job %q with INI job", jobKind, name))
				_ = c.sh.RemoveJob(cur)
			case source == JobSourceLabel && cur.GetJobSource() == JobSourceINI:
				c.logger.Warn(fmt.Sprintf("ignoring label-defined %s job %q because an INI job with the same name exists", jobKind, name))
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
			c.logger.Error("Job configuration error", "job", name, "error", err)
			return false
		}
	}

	newHash, err1 := newJob.Hash()
	if err1 != nil {
		c.logger.Error(fmt.Sprintf("hash calculation failed: %v", err1))
		return false
	}
	oldHash, err2 := oldJob.Hash()
	if err2 != nil {
		c.logger.Error(fmt.Sprintf("hash calculation failed: %v", err2))
		return false
	}
	if newHash == oldHash {
		return false
	}
	_ = c.sh.RemoveJob(oldJob)
	newJob.buildMiddlewares(c.logger, c.getWebhookManager())
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
			c.logger.Error(fmt.Sprintf("Job %q configuration error: %v", name, err))
			return
		}
	}

	j.buildMiddlewares(c.logger, c.getWebhookManager())
	_ = c.sh.AddJob(j)
	current[name] = j
}

func (c *Config) dockerContainersUpdate(containers []DockerContainerInfo) {
	c.logger.Debug("dockerContainersUpdate started")

	parsedLabelConfig := newScratchConfig(c)
	_ = parsedLabelConfig.buildFromDockerContainers(containers)

	execPrep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		j.Provider = c.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
	}
	runPrep := func(name string, j *RunJobConfig) {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = c.Global.MaxRuntime
		}
		j.Provider = c.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
	}

	localPrep := func(name string, j *LocalJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
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
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
	}

	composePrep := func(name string, j *ComposeJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
	}

	// Security: Log consolidated warning when syncing host-based jobs from container labels
	if c.Global.AllowHostJobsFromLabels {
		localCount := len(parsedLabelConfig.LocalJobs)
		composeCount := len(parsedLabelConfig.ComposeJobs)
		if localCount > 0 || composeCount > 0 {
			c.logger.Warn(fmt.Sprintf("SECURITY WARNING: Syncing host-based jobs from container labels (%d local, %d compose). "+
				"This allows containers to execute arbitrary commands on the host system.", localCount, composeCount))
		}
	}

	c.mu.Lock()
	// Forward allow-listed non-webhook globals (Slack, Mail, Save,
	// log-level, max-runtime, notification-cooldown, enable-strict-validation)
	// BEFORE syncJobMap so the prep closures above — which call
	// mergeNotificationDefaults / read c.Global.MaxRuntime — naturally pick
	// up fresh label values for any new or changed job. See #652.
	//
	// LIMITATION (documented in cli/config_global_merge.go header): jobs
	// whose own labels did not change in this reconcile pass (hash-equal in
	// replaceIfChanged) keep their previously-inherited per-job middleware
	// values until the next per-job change or daemon restart. Matches the
	// existing INI live-reload behavior. See #652 follow-up scope.
	prevLogLevel := c.Global.LogLevel
	prevCooldown := c.Global.NotificationCooldown
	_ = c.applyAllowListedGlobals(parsedLabelConfig)
	// Re-apply process-wide knobs (log level, notification deduplicator)
	// BEFORE syncJobMap so prep closures (which inject the dedup pointer
	// into per-job middlewares via c.injectDedup) see the freshly-rebuilt
	// dedup state. Otherwise a label changing notification-cooldown 0 → >0
	// rebuilds the dedup AFTER the prep closure had already snapshotted
	// the stale dedup pointer for the new job — Copilot review of #661.
	// Held under c.mu to mirror the INI live-reload path; ApplyLogLevel
	// and initNotificationDedup do not re-enter c.mu. No deadlock risk.
	c.refreshRuntimeKnobsAfterGlobalMerge(prevLogLevel, prevCooldown)
	syncJobMap(c, c.ExecJobs, parsedLabelConfig.ExecJobs, execPrep, JobSourceLabel, "exec")
	syncJobMap(c, c.RunJobs, parsedLabelConfig.RunJobs, runPrep, JobSourceLabel, "run")
	syncJobMap(c, c.LocalJobs, parsedLabelConfig.LocalJobs, localPrep, JobSourceLabel, "local")
	syncJobMap(c, c.ServiceJobs, parsedLabelConfig.ServiceJobs, servicePrep, JobSourceLabel, "service")
	syncJobMap(c, c.ComposeJobs, parsedLabelConfig.ComposeJobs, composePrep, JobSourceLabel, "compose")
	c.mu.Unlock()

	// Sync webhook configs from labels
	c.syncWebhookConfigs(parsedLabelConfig.WebhookConfigs)

	// Handle deprecated configuration options in parsed labels: migrate then warn
	ResetDeprecationWarnings()
	ApplyDeprecationMigrations(parsedLabelConfig)
	CheckDeprecations(parsedLabelConfig)
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
		c.logger.Debug(fmt.Sprintf("checking config file %s", f))
	}
	if !changed {
		c.logger.Debug("config not changed")
		return nil
	}
	c.logger.Debug(fmt.Sprintf("reloading config files from %s", strings.Join(files, ", ")))

	parsed, err := BuildFromFile(c.configPath, c.logger)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	globalChanged := !reflect.DeepEqual(parsed.Global, c.Global)
	c.configFiles = files
	c.configModTime = latest
	c.logger.Debug(fmt.Sprintf("applied config files from %s", strings.Join(files, ", ")))
	if globalChanged {
		c.Global = parsed.Global
		c.refreshWebhookManagerOnGlobalChange()
		c.sh.ResetMiddlewares()
		c.buildSchedulerMiddlewares(c.sh)
		wm := c.getWebhookManager()
		// All jobs (including disabled/paused) need middleware updates.
		// Use safe accessors that hold the scheduler's own lock to get a copy.
		allJobs := append(c.sh.GetActiveJobs(), c.sh.GetDisabledJobs()...)
		for _, j := range allJobs {
			if jc, ok := j.(jobConfig); ok {
				jc.ResetMiddlewares()
				jc.buildMiddlewares(c.logger, wm)
				j.Use(c.sh.Middlewares()...)
			}
		}
		if err := ApplyLogLevel(c.Global.LogLevel, c.levelVar); err != nil {
			c.logger.Warn(fmt.Sprintf("Failed to apply global log level (using default): %v", err))
		}
	}

	// Surface newly added [webhook "name"] sections from the reloaded INI into
	// the live config. Without this step a brand-new webhook block has no
	// effect until restart — only the embedded global fields benefit from the
	// dual-store collapse from #620. INI source wins on collisions; existing
	// entries are left untouched (label-defined entries keep their state, and
	// a webhook re-named in the INI is treated as a removal-then-add at the
	// next reconcile rather than a silent overwrite). See #640.
	if c.mergeReloadedWebhookSections(parsed) {
		c.refreshWebhookManagerOnGlobalChange()
	}

	execPrep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		c.applyDefaultUser(&j.User)
		j.Provider = c.dockerHandler.GetDockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
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
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
	}
	syncJobMap(c, c.RunJobs, parsed.RunJobs, runPrep, JobSourceINI, "run")

	localPrep := func(name string, j *LocalJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
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
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
	}
	syncJobMap(c, c.ServiceJobs, parsed.ServiceJobs, svcPrep, JobSourceINI, "service")

	composePrep := func(name string, j *ComposeJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
		c.mergeNotificationDefaults(&j.SlackConfig, &j.MailConfig, &j.SaveConfig)
		c.injectDedup(&j.SlackConfig, &j.MailConfig)
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
	JobWebhookConfig          `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

// buildJobMiddlewares attaches the standard per-job middleware stack
// (Overlap, Slack, Save, Mail) to job j, then the webhook composite resolved
// from per-job names unioned with the global selector. Extracted from five
// identical per-type buildMiddlewares methods because core.Job already
// promotes GetName() and Use(...) from the embedded BareJob, so there's no
// remaining per-type variation worth duplicating.
func buildJobMiddlewares(
	logger *slog.Logger,
	j core.Job,
	overlap *middlewares.OverlapConfig,
	slack *middlewares.SlackConfig,
	save *middlewares.SaveConfig,
	mail *middlewares.MailConfig,
	wm *middlewares.WebhookManager,
	webhookNames []string,
) {
	j.Use(middlewares.NewOverlap(overlap))
	j.Use(middlewares.NewSlack(slack)) //nolint:staticcheck // deprecated but kept for backwards compatibility
	j.Use(middlewares.NewSave(save))
	j.Use(middlewares.NewMail(mail))
	attachJobWebhookMiddlewares(logger, j.GetName(), wm, webhookNames, j.Use)
}

func (c *ExecJobConfig) buildMiddlewares(logger *slog.Logger, wm *middlewares.WebhookManager) {
	buildJobMiddlewares(logger, &c.ExecJob,
		&c.OverlapConfig, &c.SlackConfig, &c.SaveConfig, &c.MailConfig,
		wm, c.GetWebhookNames())
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
	JobWebhookConfig          `mapstructure:",squash"`
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
	JobWebhookConfig          `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

func (c *RunJobConfig) buildMiddlewares(logger *slog.Logger, wm *middlewares.WebhookManager) {
	buildJobMiddlewares(logger, &c.RunJob,
		&c.OverlapConfig, &c.SlackConfig, &c.SaveConfig, &c.MailConfig,
		wm, c.GetWebhookNames())
}

func (c *RunJobConfig) GetJobSource() JobSource  { return c.JobSource }
func (c *RunJobConfig) SetJobSource(s JobSource) { c.JobSource = s }

// Hash overrides BareJob.Hash() to include RunJob-specific fields
func (c *RunJobConfig) Hash() (string, error) {
	var hash string
	if err := core.GetHash(reflect.TypeFor[core.RunJob](), reflect.ValueOf(&c.RunJob).Elem(), &hash); err != nil {
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
	JobWebhookConfig          `mapstructure:",squash"`
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
	JobWebhookConfig          `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

func (c *ComposeJobConfig) GetJobSource() JobSource  { return c.JobSource }
func (c *ComposeJobConfig) SetJobSource(s JobSource) { c.JobSource = s }

func (c *LocalJobConfig) buildMiddlewares(logger *slog.Logger, wm *middlewares.WebhookManager) {
	buildJobMiddlewares(logger, &c.LocalJob,
		&c.OverlapConfig, &c.SlackConfig, &c.SaveConfig, &c.MailConfig,
		wm, c.GetWebhookNames())
}

func (c *ComposeJobConfig) buildMiddlewares(logger *slog.Logger, wm *middlewares.WebhookManager) {
	buildJobMiddlewares(logger, &c.ComposeJob,
		&c.OverlapConfig, &c.SlackConfig, &c.SaveConfig, &c.MailConfig,
		wm, c.GetWebhookNames())
}

func (c *RunServiceConfig) buildMiddlewares(logger *slog.Logger, wm *middlewares.WebhookManager) {
	buildJobMiddlewares(logger, &c.RunServiceJob,
		&c.OverlapConfig, &c.SlackConfig, &c.SaveConfig, &c.MailConfig,
		wm, c.GetWebhookNames())
}

type DockerConfig struct {
	Filters []string `mapstructure:"filters"`

	// IncludeStopped when true lists stopped containers when reading Docker labels (only for job-run).
	// When false, only running containers are considered. Can be set via --docker-include-stopped or OFELIA_DOCKER_INCLUDE_STOPPED.
	IncludeStopped bool `mapstructure:"include-stopped" default:"false"`

	// ConfigPollInterval controls how often to check for INI config file changes.
	// This is independent of container detection. Set to 0 to disable config file watching.
	ConfigPollInterval time.Duration `mapstructure:"config-poll-interval" validate:"gte=0" default:"10s"`

	// UseEvents enables Docker event-based container detection (recommended).
	// When enabled, Ofelia reacts immediately to container start/stop events.
	UseEvents bool `mapstructure:"events" default:"true"`

	// DockerPollInterval enables periodic polling for container changes.
	// This is a fallback for environments where Docker events don't work reliably.
	// Set to 0 (default) to disable explicit container polling.
	// WARNING: If both events and polling are enabled, this is usually wasteful.
	DockerPollInterval time.Duration `mapstructure:"docker-poll-interval" validate:"gte=0" default:"0"`

	// PollingFallback auto-enables container polling if event subscription fails.
	// This provides backwards compatibility and resilience.
	// Set to 0 to disable auto-fallback (will only log errors on event failure).
	// Default is 10s for backwards compatibility.
	PollingFallback time.Duration `mapstructure:"polling-fallback" validate:"gte=0" default:"10s"`

	// StartupRetryCount sets the number of EXTRA Docker connection attempts
	// at daemon startup beyond the initial ping. The total attempt budget is
	// StartupRetryCount + 1; 0 (the default) preserves the pre-#523 behavior
	// of a single ping that exits the daemon on failure. Useful with TCP-based
	// Docker hosts (socket proxies, remote daemons, Docker-in-Docker) where
	// the daemon may briefly be unreachable on startup before health checks
	// settle. Per attempt the call is bounded by dockerStartupPingTimeout
	// (10s) so the worst-case startup budget is
	// (count+1) × dockerStartupPingTimeout + Σ backoffs.
	// Can be set via --docker-startup-retry-count or
	// OFELIA_DOCKER_STARTUP_RETRY_COUNT. See
	// https://github.com/netresearch/ofelia/issues/523.
	StartupRetryCount int `mapstructure:"startup-retry-count" validate:"gte=0,lte=20" default:"0"`

	// StartupRetryInterval is the base interval between Docker connection
	// retry attempts at daemon startup. Backoff is exponential
	// (interval × 2^(attempt-1)) and is honored only when StartupRetryCount > 0.
	// Defaults to 1s so the issue's suggested 5-retry budget produces a
	// 1s → 2s → 4s → 8s → 16s spacing (≈31s total). Capped at 5min per
	// step to bound the daemon's startup delay on a wedged Docker host.
	StartupRetryInterval time.Duration `mapstructure:"startup-retry-interval" validate:"gte=0,lte=5m" default:"1s"`

	// Deprecated: Use ConfigPollInterval and DockerPollInterval instead.
	// If set, this value is used for both config and container polling (BC).
	PollInterval time.Duration `mapstructure:"poll-interval" validate:"gte=0"`

	// Deprecated: Use DockerPollInterval=0 instead.
	// If true, disables container polling entirely.
	DisablePolling bool `mapstructure:"no-poll" default:"false"`
}

func parseIni(cfg *ini.File, c *Config) (*parseResult, error) {
	parseRes, err := parseGlobalAndDocker(cfg, c)
	if err != nil {
		return nil, err
	}
	// Parse webhook sections
	if err := parseWebhookSections(cfg, c); err != nil {
		return nil, err
	}

	// Collector for unknown keys in job sections
	var unknownJobs []jobUnknownKeys

	for _, section := range cfg.Sections() {
		name := strings.TrimSpace(section.Name())
		switch {
		case strings.HasPrefix(name, jobExec):
			if err := decodeJob(
				section,
				&ExecJobConfig{JobSource: JobSourceINI},
				func(n string, j *ExecJobConfig) { c.ExecJobs[n] = j },
				jobExec,
				&unknownJobs,
			); err != nil {
				return nil, err
			}
		case strings.HasPrefix(name, jobRun):
			if err := decodeJob(
				section,
				&RunJobConfig{JobSource: JobSourceINI},
				func(n string, j *RunJobConfig) { c.RunJobs[n] = j },
				jobRun,
				&unknownJobs,
			); err != nil {
				return nil, err
			}
		case strings.HasPrefix(name, jobServiceRun):
			if err := decodeJob(
				section,
				&RunServiceConfig{JobSource: JobSourceINI},
				func(n string, j *RunServiceConfig) { c.ServiceJobs[n] = j },
				jobServiceRun,
				&unknownJobs,
			); err != nil {
				return nil, err
			}
		case strings.HasPrefix(name, jobLocal):
			if err := decodeJob(
				section,
				&LocalJobConfig{JobSource: JobSourceINI},
				func(n string, j *LocalJobConfig) { c.LocalJobs[n] = j },
				jobLocal,
				&unknownJobs,
			); err != nil {
				return nil, err
			}
		case strings.HasPrefix(name, jobCompose):
			if err := decodeJob(
				section,
				&ComposeJobConfig{JobSource: JobSourceINI},
				func(n string, j *ComposeJobConfig) { c.ComposeJobs[n] = j },
				jobCompose,
				&unknownJobs,
			); err != nil {
				return nil, err
			}
		}
	}

	parseRes.unknownJobs = unknownJobs
	return parseRes, nil
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

// parseResult holds the results from parsing global, docker, and job sections
type parseResult struct {
	usedKeys      map[string]bool
	unknownGlobal []string
	unknownDocker []string
	unknownJobs   []jobUnknownKeys
}

// jobUnknownKeys tracks unknown keys found in a job section
type jobUnknownKeys struct {
	JobType     string   // e.g., "job-exec", "job-run"
	JobName     string   // The job name from the section header
	UnknownKeys []string // Keys that didn't match any struct field
}

func parseGlobalAndDocker(cfg *ini.File, c *Config) (*parseResult, error) {
	result := &parseResult{
		usedKeys: make(map[string]bool),
	}

	if sec, err := cfg.GetSection("global"); err == nil {
		input := sectionToMap(sec)
		decResult, decErr := decodeWithMetadata(input, &c.Global)
		if decErr != nil {
			return nil, fmt.Errorf("decode [global]: %w", decErr)
		}
		// Track used keys for deprecation detection
		for k, v := range decResult.UsedKeys {
			if v {
				result.usedKeys[k] = true
			}
		}
		result.unknownGlobal = decResult.UnusedKeys
		// c.WebhookConfigs.Global aliases &c.Global.WebhookGlobalConfig (set in
		// NewConfig), so mapstructure-decoded webhook-* keys are visible to the
		// webhook subsystem without an explicit copy. The embedded struct was
		// pre-seeded with defaults in NewConfig(), so unset keys retain them.
	}

	if sec, err := cfg.GetSection("docker"); err == nil {
		input := sectionToMap(sec)
		decResult, decErr := decodeWithMetadata(input, &c.Docker)
		if decErr != nil {
			return nil, fmt.Errorf("decode [docker]: %w", decErr)
		}
		// Track used keys for deprecation detection
		for k, v := range decResult.UsedKeys {
			if v {
				result.usedKeys[k] = true
			}
		}
		result.unknownDocker = decResult.UnusedKeys
	}

	return result, nil
}

func decodeJob[T jobConfig](section *ini.Section, job T, set func(string, T), prefix string, unknownCollector *[]jobUnknownKeys) error {
	jobName := parseJobName(strings.TrimSpace(section.Name()), prefix)
	input := sectionToMap(section)

	result, err := decodeWithMetadata(input, job)
	if err != nil {
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return fmt.Errorf("failed to decode job %q configuration: %w\n  → Check job section syntax in config file\n  → Verify all required fields are set (schedule, command, container, etc.)\n  → Check for typos in configuration keys\n  → Use 'ofelia validate --config=<file>' to validate configuration\n  → Review job type requirements (job-exec, job-run, job-local, job-service-run)", jobName, err)
	}

	// Collect unknown keys for this job section
	if len(result.UnusedKeys) > 0 && unknownCollector != nil {
		*unknownCollector = append(*unknownCollector, jobUnknownKeys{
			JobType:     prefix,
			JobName:     jobName,
			UnknownKeys: result.UnusedKeys,
		})
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
	s = strings.Trim(s, "\"")
	return ExpandEnvVars(s)
}

func sectionToMap(section *ini.Section) map[string]any {
	m := make(map[string]any)
	for _, key := range section.Keys() {
		vals := key.ValueWithShadows()
		switch {
		case len(vals) > 1:
			cp := make([]string, len(vals))
			for i, v := range vals {
				cp[i] = ExpandEnvVars(v)
			}
			m[key.Name()] = cp
		case len(vals) == 1:
			m[key.Name()] = ExpandEnvVars(vals[0])
		default:
			// Handle empty values
			m[key.Name()] = ""
		}
	}
	return m
}
