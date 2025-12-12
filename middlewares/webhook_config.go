package middlewares

import (
	"fmt"
	"os"
	"time"
)

// TriggerType defines when a webhook notification should be sent
type TriggerType string

const (
	TriggerAlways  TriggerType = "always"  // Send on every execution
	TriggerError   TriggerType = "error"   // Send only on errors
	TriggerSuccess TriggerType = "success" // Send only on success
)

// WebhookConfig holds configuration for a single webhook endpoint
type WebhookConfig struct {
	// Name is the unique identifier for this webhook (from INI section name)
	Name string `gcfg:"-" mapstructure:"-"`

	// Preset specifies the preset to use (e.g., "slack", "discord", "gh:org/repo/preset.yaml@v1.0")
	Preset string `gcfg:"preset" mapstructure:"preset"`

	// ID is a generic identifier used by the preset's URL scheme (e.g., Slack workspace/bot ID)
	ID string `gcfg:"id" mapstructure:"id" json:"-"`

	// Secret is a generic secret/token used by the preset's URL scheme
	Secret string `gcfg:"secret" mapstructure:"secret" json:"-"`

	// URL overrides the preset's url_scheme entirely (useful for custom endpoints)
	URL string `gcfg:"url" mapstructure:"url" json:"-"`

	// Link is an optional URL to include in notifications (e.g., link to logs, dashboard)
	Link string `gcfg:"link" mapstructure:"link"`

	// LinkText is the display text for the link (defaults to "View Details" if link is set)
	LinkText string `gcfg:"link-text" mapstructure:"link-text"`

	// Trigger determines when to send notifications
	Trigger TriggerType `gcfg:"trigger" mapstructure:"trigger"`

	// Timeout for the HTTP request
	Timeout time.Duration `gcfg:"timeout" mapstructure:"timeout"`

	// RetryCount is the number of retry attempts on failure
	RetryCount int `gcfg:"retry-count" mapstructure:"retry-count"`

	// RetryDelay is the delay between retry attempts
	RetryDelay time.Duration `gcfg:"retry-delay" mapstructure:"retry-delay"`

	// CustomVars holds additional custom variables for template expansion
	CustomVars map[string]string `gcfg:"-" mapstructure:"-"`

	// Dedup is the notification deduplicator (set by config loader, not INI)
	Dedup *NotificationDedup `mapstructure:"-" json:"-"`
}

// WebhookGlobalConfig holds global webhook settings
type WebhookGlobalConfig struct {
	// Webhooks is a comma-separated list of webhook names to use globally
	Webhooks string `gcfg:"webhooks" mapstructure:"webhooks"`

	// AllowRemotePresets enables fetching presets from remote URLs
	AllowRemotePresets bool `gcfg:"allow-remote-presets" mapstructure:"allow-remote-presets"`

	// TrustedPresetSources is a comma-separated list of trusted remote preset sources
	// Supports glob patterns (e.g., "gh:netresearch/*", "gh:myorg/ofelia-presets/*")
	TrustedPresetSources string `gcfg:"trusted-preset-sources" mapstructure:"trusted-preset-sources"`

	// PresetCacheTTL is how long to cache remote presets
	PresetCacheTTL time.Duration `gcfg:"preset-cache-ttl" mapstructure:"preset-cache-ttl"`

	// PresetCacheDir is the directory for caching remote presets
	PresetCacheDir string `gcfg:"preset-cache-dir" mapstructure:"preset-cache-dir"`
}

// WebhookData is the data structure passed to webhook templates
type WebhookData struct {
	Job       WebhookJobData
	Execution WebhookExecutionData
	Host      WebhookHostData
	Ofelia    WebhookOfeliaData
}

// WebhookJobData contains job information for templates
type WebhookJobData struct {
	Name     string
	Command  string
	Schedule string
	Type     string
}

// WebhookExecutionData contains execution information for templates
type WebhookExecutionData struct {
	ID        string
	Status    string
	Failed    bool
	Skipped   bool
	Duration  time.Duration
	Error     string
	Output    string
	Stderr    string
	ExitCode  int
	StartTime time.Time
	EndTime   time.Time
}

// WebhookHostData contains host information for templates
type WebhookHostData struct {
	Hostname  string
	Timestamp time.Time
}

// WebhookOfeliaData contains Ofelia metadata for templates
type WebhookOfeliaData struct {
	Version string
}

// DefaultWebhookConfig returns default webhook configuration values
func DefaultWebhookConfig() *WebhookConfig {
	return &WebhookConfig{
		Trigger:    TriggerError,
		Timeout:    10 * time.Second,
		RetryCount: 3,
		RetryDelay: 5 * time.Second,
	}
}

// DefaultWebhookGlobalConfig returns default global webhook configuration
func DefaultWebhookGlobalConfig() *WebhookGlobalConfig {
	cacheDir := os.TempDir()
	if xdgCache := os.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
		cacheDir = xdgCache + "/ofelia/presets"
	}

	return &WebhookGlobalConfig{
		AllowRemotePresets:   false,
		TrustedPresetSources: "",
		PresetCacheTTL:       24 * time.Hour,
		PresetCacheDir:       cacheDir,
	}
}

// Validate checks the webhook configuration for errors
func (c *WebhookConfig) Validate() error {
	if c.Preset == "" && c.URL == "" {
		return fmt.Errorf("webhook %q: either preset or url must be specified", c.Name)
	}

	// Validate trigger type
	switch c.Trigger {
	case TriggerAlways, TriggerError, TriggerSuccess, "":
		// Valid or empty (will use default)
	default:
		return fmt.Errorf("webhook %q: invalid trigger %q (must be always, error, or success)", c.Name, c.Trigger)
	}

	if c.Timeout < 0 {
		return fmt.Errorf("webhook %q: timeout cannot be negative", c.Name)
	}

	if c.RetryCount < 0 {
		return fmt.Errorf("webhook %q: retry-count cannot be negative", c.Name)
	}

	if c.RetryDelay < 0 {
		return fmt.Errorf("webhook %q: retry-delay cannot be negative", c.Name)
	}

	return nil
}

// ApplyDefaults applies default values to empty fields
func (c *WebhookConfig) ApplyDefaults() {
	defaults := DefaultWebhookConfig()

	if c.Trigger == "" {
		c.Trigger = defaults.Trigger
	}
	if c.Timeout == 0 {
		c.Timeout = defaults.Timeout
	}
	if c.RetryCount == 0 {
		c.RetryCount = defaults.RetryCount
	}
	if c.RetryDelay == 0 {
		c.RetryDelay = defaults.RetryDelay
	}
}

// ShouldNotify determines if a notification should be sent based on trigger and execution state
func (c *WebhookConfig) ShouldNotify(failed, skipped bool) bool {
	switch c.Trigger {
	case TriggerError:
		return failed
	case TriggerSuccess:
		return !failed && !skipped
	case TriggerAlways:
		return true
	default:
		return failed // Default to error-only
	}
}
