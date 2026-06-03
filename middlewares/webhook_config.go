// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

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
	TriggerSkipped TriggerType = "skipped" // Send only on skipped executions
)

// DefaultPresetName is the name of the bundled preset used as the
// documented fallback when [global] webhook-default-preset is unset.
// Returned by (*WebhookGlobalConfig).EffectiveDefaultPreset() when
// DefaultPreset is nil. The matching preset YAML lives at
// middlewares/presets/json-post.yaml and is embedded into the binary.
// See https://github.com/netresearch/ofelia/issues/676.
const DefaultPresetName = "json-post"

// EffectiveDefaultPreset returns the preset name to use as the fallback
// when a per-webhook config omits `preset`. Resolves the three intents
// encoded on (*string) DefaultPreset:
//
//   - nil          → fall back to DefaultPresetName ("json-post").
//   - non-nil ""   → operator explicitly opted out — no fallback.
//   - non-nil "X"  → operator's chosen fallback name.
//
// Called at webhook attach time (NewWebhook) rather than at startup, so
// late mutations to DefaultPreset via INI reload or label sync take
// effect on the next attach without restart. Mirrors the access-time
// resolution pattern of SaveConfig.RestoreHistoryEnabled.
func (g *WebhookGlobalConfig) EffectiveDefaultPreset() string {
	if g == nil || g.DefaultPreset == nil {
		return DefaultPresetName
	}
	return *g.DefaultPreset
}

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

	// Device is an optional target device used by presets that support
	// per-device delivery (currently the Pushover preset). Empty means the
	// preset's default (e.g. all of the user's devices).
	Device string `gcfg:"device" mapstructure:"device"`

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

// WebhookGlobalConfig holds global webhook settings.
//
// All keys are configured under the [global] section of the INI config file
// and use the `webhook-` prefix to avoid colliding with other [global] settings.
type WebhookGlobalConfig struct {
	// Webhooks is a comma-separated list of webhook names to use globally.
	// (Configured as `webhook-webhooks` in [global]. The per-job `webhooks = ...`
	// key lives on JobWebhookConfig and is separate.)
	Webhooks string `gcfg:"webhook-webhooks" mapstructure:"webhook-webhooks"`

	// AllowRemotePresets enables fetching presets from remote URLs
	AllowRemotePresets bool `gcfg:"webhook-allow-remote-presets" mapstructure:"webhook-allow-remote-presets"`

	// TrustedPresetSources is a comma-separated list of trusted remote preset sources
	// Supports glob patterns (e.g., "gh:netresearch/*", "gh:myorg/ofelia-presets/*")
	TrustedPresetSources string `gcfg:"webhook-trusted-preset-sources" mapstructure:"webhook-trusted-preset-sources"`

	// PresetCacheTTL is how long to cache remote presets
	PresetCacheTTL time.Duration `gcfg:"webhook-preset-cache-ttl" mapstructure:"webhook-preset-cache-ttl"`

	// PresetCacheDir is the directory for caching remote presets
	PresetCacheDir string `gcfg:"webhook-preset-cache-dir" mapstructure:"webhook-preset-cache-dir"`

	// AllowedHosts controls which hosts webhooks can target.
	// Default: "*" (allow all hosts) - consistent with local command execution trust model
	// Set to specific hosts for whitelist mode: "hooks.slack.com, ntfy.internal, 192.168.1.20"
	// Supports wildcards: "*.example.com"
	AllowedHosts string `gcfg:"webhook-allowed-hosts" mapstructure:"webhook-allowed-hosts"`

	// DefaultPreset is the preset name used when a per-webhook configuration
	// omits the `preset` field. Pointer-typed so we can distinguish three
	// operator intents (mirrors the SaveConfig.RestoreHistory pattern):
	//
	//   - nil → operator did not set the key at all; resolve at access time
	//     to DefaultPresetName ("json-post", the bundled JSON POST preset)
	//     via EffectiveDefaultPreset(). Lets a webhook with just `url = ...`
	//     work out of the box.
	//   - non-nil "" (empty string) → operator explicitly opted out of the
	//     fallback; webhooks missing `preset` fail attachment with a logged
	//     error.
	//   - non-nil non-empty → operator's chosen fallback (custom preset name).
	//
	// See https://github.com/netresearch/ofelia/issues/676.
	DefaultPreset *string `gcfg:"webhook-default-preset" mapstructure:"webhook-default-preset"`
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
		AllowedHosts:         "*", // Default: allow all hosts (consistent with local command trust model)
		// DefaultPreset intentionally left nil so unset-vs-explicit can be
		// distinguished by mergeWebhookGlobals and EffectiveDefaultPreset.
		// The effective fallback is DefaultPresetName.
		DefaultPreset: nil,
	}
}

// Validate checks the webhook configuration for errors
func (c *WebhookConfig) Validate() error {
	if c.Preset == "" && c.URL == "" {
		return fmt.Errorf("webhook %q: either preset or url must be specified", c.Name)
	}

	// Validate trigger type
	switch c.Trigger {
	case TriggerAlways, TriggerError, TriggerSuccess, TriggerSkipped, "":
		// Valid or empty (will use default)
	default:
		return fmt.Errorf("webhook %q: invalid trigger %q (must be always, error, success, or skipped)", c.Name, c.Trigger)
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
	case TriggerSkipped:
		return skipped
	case TriggerAlways:
		return true
	default:
		return failed // Default to error-only
	}
}
