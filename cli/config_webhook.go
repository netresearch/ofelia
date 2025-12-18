package cli

import (
	"fmt"
	"strings"

	ini "gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/middlewares"
)

const webhookSection = "webhook"

// WebhookConfigs holds all parsed webhook configurations
type WebhookConfigs struct {
	Global   *middlewares.WebhookGlobalConfig
	Webhooks map[string]*middlewares.WebhookConfig
	Manager  *middlewares.WebhookManager
}

// NewWebhookConfigs creates a new WebhookConfigs with defaults
func NewWebhookConfigs() *WebhookConfigs {
	return &WebhookConfigs{
		Global:   middlewares.DefaultWebhookGlobalConfig(),
		Webhooks: make(map[string]*middlewares.WebhookConfig),
	}
}

// InitManager initializes the webhook manager with the parsed configurations
func (wc *WebhookConfigs) InitManager() error {
	wc.Manager = middlewares.NewWebhookManager(wc.Global)

	for name, config := range wc.Webhooks {
		config.Name = name
		if err := wc.Manager.Register(config); err != nil {
			return fmt.Errorf("register webhook %q: %w", name, err)
		}
	}

	return nil
}

// parseWebhookSections parses [webhook "name"] sections from INI config
func parseWebhookSections(cfg *ini.File, c *Config) error {
	if c.WebhookConfigs == nil {
		c.WebhookConfigs = NewWebhookConfigs()
	}

	for _, section := range cfg.Sections() {
		name := strings.TrimSpace(section.Name())

		// Parse [webhook "name"] sections
		if strings.HasPrefix(name, webhookSection) {
			webhookName := parseWebhookName(name)
			if webhookName == "" {
				return fmt.Errorf("webhook section must have a name: [webhook \"name\"]")
			}

			config := middlewares.DefaultWebhookConfig()
			config.Name = webhookName

			if err := parseWebhookConfig(section, config); err != nil {
				return fmt.Errorf("parse webhook %q: %w", webhookName, err)
			}

			c.WebhookConfigs.Webhooks[webhookName] = config
		}
	}

	return nil
}

// parseWebhookName extracts the webhook name from section name
// e.g., "webhook \"slack-alerts\"" -> "slack-alerts"
func parseWebhookName(sectionName string) string {
	// Format: webhook "name" or webhook 'name'
	sectionName = strings.TrimPrefix(sectionName, webhookSection)
	sectionName = strings.TrimSpace(sectionName)

	// Remove quotes
	if len(sectionName) >= 2 {
		if (sectionName[0] == '"' && sectionName[len(sectionName)-1] == '"') ||
			(sectionName[0] == '\'' && sectionName[len(sectionName)-1] == '\'') {
			return sectionName[1 : len(sectionName)-1]
		}
	}

	return sectionName
}

// parseWebhookConfig parses webhook configuration from an INI section.
// Currently always returns nil as all fields are optional with defaults.
//
//nolint:unparam // error return kept for future validation additions
func parseWebhookConfig(section *ini.Section, config *middlewares.WebhookConfig) error {
	if key, err := section.GetKey("preset"); err == nil {
		config.Preset = key.String()
	}

	if key, err := section.GetKey("id"); err == nil {
		config.ID = key.String()
	}

	if key, err := section.GetKey("secret"); err == nil {
		config.Secret = key.String()
	}

	if key, err := section.GetKey("url"); err == nil {
		config.URL = key.String()
	}

	if key, err := section.GetKey("trigger"); err == nil {
		config.Trigger = middlewares.TriggerType(key.String())
	}

	if key, err := section.GetKey("timeout"); err == nil {
		if d, err := key.Duration(); err == nil {
			config.Timeout = d
		}
	}

	if key, err := section.GetKey("retry-count"); err == nil {
		if n, err := key.Int(); err == nil {
			config.RetryCount = n
		}
	}

	if key, err := section.GetKey("retry-delay"); err == nil {
		if d, err := key.Duration(); err == nil {
			config.RetryDelay = d
		}
	}

	if key, err := section.GetKey("link"); err == nil {
		config.Link = key.String()
	}

	if key, err := section.GetKey("link-text"); err == nil {
		config.LinkText = key.String()
	}

	return nil
}

// parseGlobalWebhookConfig parses global webhook configuration from [global] section
func parseGlobalWebhookConfig(section *ini.Section, c *Config) {
	if c.WebhookConfigs == nil {
		c.WebhookConfigs = NewWebhookConfigs()
	}

	if key, err := section.GetKey("webhooks"); err == nil {
		c.WebhookConfigs.Global.Webhooks = key.String()
	}

	if key, err := section.GetKey("allow-remote-presets"); err == nil {
		c.WebhookConfigs.Global.AllowRemotePresets, _ = key.Bool()
	}

	if key, err := section.GetKey("trusted-preset-sources"); err == nil {
		c.WebhookConfigs.Global.TrustedPresetSources = key.String()
	}

	if key, err := section.GetKey("preset-cache-ttl"); err == nil {
		if d, err := key.Duration(); err == nil {
			c.WebhookConfigs.Global.PresetCacheTTL = d
		}
	}

	if key, err := section.GetKey("preset-cache-dir"); err == nil {
		c.WebhookConfigs.Global.PresetCacheDir = key.String()
	}

	// Host whitelist: "*" = allow all (default), specific list = whitelist mode
	if key, err := section.GetKey("webhook-allowed-hosts"); err == nil {
		c.WebhookConfigs.Global.AllowedHosts = key.String()
	}
}

// JobWebhookConfig holds per-job webhook configuration
type JobWebhookConfig struct {
	// Webhooks is a comma-separated list of webhook names for this job
	Webhooks string `gcfg:"webhooks" mapstructure:"webhooks"`
}

// GetWebhookNames returns the list of webhook names for a job
func (c *JobWebhookConfig) GetWebhookNames() []string {
	return middlewares.ParseWebhookNames(c.Webhooks)
}
