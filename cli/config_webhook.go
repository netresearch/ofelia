// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	ini "gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/middlewares"
)

const webhookSection = "webhook"

// Canonical and legacy Docker label key names that the webhook label reader
// consumes. Two webhook globals are exposed via labels:
//
//   - webhook-webhooks: the per-job/global selector (operationally tunable).
//   - webhook-preset-cache-ttl: cache lifetime for remote presets (operationally
//     tunable, NOT SSRF-sensitive — narrowing or widening the TTL cannot widen
//     the network egress surface).
//
// The SSRF-sensitive globals (webhook-allowed-hosts, webhook-allow-remote-presets,
// webhook-trusted-preset-sources, webhook-preset-cache-dir) remain INI-only to
// prevent containers from widening the network egress surface or redirecting
// preset loading. See docker-labels.go (globalLabelAllowList) and #486.
const (
	webhookGlobalKeyWebhooks       = "webhook-webhooks"
	webhookGlobalKeyPresetCacheTTL = "webhook-preset-cache-ttl"
	// webhookGlobalKeyDefaultPreset selects the preset used when a webhook's
	// per-instance `preset` field is empty (default: middlewares.DefaultPresetName,
	// which is "json-post"). Operator-tunable; not SSRF-sensitive — narrowing or
	// widening the default cannot reach a new network destination because every
	// webhook still goes through the same URL allow-list and preset loader.
	// See #676.
	webhookGlobalKeyDefaultPreset = "webhook-default-preset"

	// Legacy unprefixed form left behind by #618 when the INI side was
	// renamed to webhook-*. Still accepted with a one-shot deprecation
	// warning so operators have one release window to migrate.
	legacyLabelKeyWebhooks = "webhooks"
)

// WebhookConfigs holds all parsed webhook configurations
type WebhookConfigs struct {
	Global          *middlewares.WebhookGlobalConfig
	Webhooks        map[string]*middlewares.WebhookConfig
	Manager         *middlewares.WebhookManager
	iniWebhookNames map[string]struct{} // tracks names defined in INI (protected from label overwrite)
}

// NewWebhookConfigs creates a new WebhookConfigs with defaults
func NewWebhookConfigs() *WebhookConfigs {
	return &WebhookConfigs{
		Global:   middlewares.DefaultWebhookGlobalConfig(),
		Webhooks: make(map[string]*middlewares.WebhookConfig),
	}
}

// InitManager initializes the webhook manager with the parsed
// configurations. Called on first init and on every reload (Docker
// label sync, INI live-reload). The new manager adopts the prior
// manager's *http.Client cache so keep-alive connection pools survive
// the reload — without this carry-forward, every reload would drop
// the underlying transports and defeat the #674 fix. See
// (*middlewares.WebhookManager).AdoptClientCacheFrom for the contract.
func (wc *WebhookConfigs) InitManager() error {
	prior := wc.Manager
	wc.Manager = middlewares.NewWebhookManager(wc.Global)
	wc.Manager.AdoptClientCacheFrom(prior)

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
			if c.WebhookConfigs.iniWebhookNames == nil {
				c.WebhookConfigs.iniWebhookNames = make(map[string]struct{})
			}
			c.WebhookConfigs.iniWebhookNames[webhookName] = struct{}{}
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
		config.Preset = ExpandEnvVars(key.String())
	}

	if key, err := section.GetKey("id"); err == nil {
		config.ID = ExpandEnvVars(key.String())
	}

	if key, err := section.GetKey("secret"); err == nil {
		config.Secret = ExpandEnvVars(key.String())
	}

	if key, err := section.GetKey("url"); err == nil {
		config.URL = ExpandEnvVars(key.String())
	}

	if key, err := section.GetKey("trigger"); err == nil {
		config.Trigger = middlewares.TriggerType(ExpandEnvVars(key.String()))
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
		config.Link = ExpandEnvVars(key.String())
	}

	if key, err := section.GetKey("link-text"); err == nil {
		config.LinkText = ExpandEnvVars(key.String())
	}

	if key, err := section.GetKey("device"); err == nil {
		config.Device = ExpandEnvVars(key.String())
	}

	return nil
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

// syncWebhookConfigs detects changes in label-defined webhooks and re-initializes
// the webhook manager if needed (called during container update events).
func (c *Config) syncWebhookConfigs(parsed *WebhookConfigs) {
	if parsed == nil {
		return
	}

	if !c.applyWebhookChanges(parsed) {
		return
	}

	// Re-initialize webhook manager with updated configs
	if err := c.WebhookConfigs.InitManager(); err != nil {
		c.logger.Error(fmt.Sprintf("Failed to re-initialize webhook manager: %v", err))
		return
	}
	c.logger.Info("Webhook configuration updated from container labels", "count", len(c.WebhookConfigs.Webhooks))

	c.rebuildAllMiddlewares()
}

// applyWebhookChanges merges parsed webhook configs into the current config,
// returning true if any changes were detected. INI-defined webhooks are never
// overwritten by labels (security: prevents container label hijacking).
//
// Also forwards operator-tunable webhook globals (Webhooks selector,
// AllowedHosts, PresetCacheTTL) via mergeWebhookGlobals so that runtime
// label edits on a service container actually update the live config —
// without this, the merge only happened once at startup via
// mergeWebhookConfigs and subsequent label changes were silently dropped
// (Code/DRY review of #650).
func (c *Config) applyWebhookChanges(parsed *WebhookConfigs) bool {
	changed := false

	for name, wh := range parsed.Webhooks {
		// Never overwrite INI-defined webhooks from labels
		if _, isINI := c.WebhookConfigs.iniWebhookNames[name]; isINI {
			continue
		}
		existing, exists := c.WebhookConfigs.Webhooks[name]
		if !exists {
			c.WebhookConfigs.Webhooks[name] = wh
			changed = true
			continue
		}
		if webhookConfigChanged(existing, wh) {
			c.WebhookConfigs.Webhooks[name] = wh
			changed = true
		}
	}

	// Remove label-defined webhooks no longer present in parsed labels
	for name := range c.WebhookConfigs.Webhooks {
		if _, isINI := c.WebhookConfigs.iniWebhookNames[name]; isINI {
			continue
		}
		if _, stillPresent := parsed.Webhooks[name]; !stillPresent {
			delete(c.WebhookConfigs.Webhooks, name)
			changed = true
		}
	}

	// Forward operator-tunable webhook globals from a fresh label parse so
	// that a container that changes its ofelia.webhook-webhooks /
	// webhook-preset-cache-ttl labels takes effect without restart.
	if mergeWebhookGlobals(c.WebhookConfigs.Global, parsed.Global) {
		changed = true
	}

	return changed
}

// webhookConfigChanged returns true if any configurable field differs between two webhook configs.
func webhookConfigChanged(a, b *middlewares.WebhookConfig) bool {
	return a.Preset != b.Preset || a.URL != b.URL ||
		a.ID != b.ID || a.Secret != b.Secret ||
		a.Trigger != b.Trigger || a.Timeout != b.Timeout ||
		a.RetryCount != b.RetryCount || a.RetryDelay != b.RetryDelay ||
		a.Link != b.Link || a.LinkText != b.LinkText ||
		a.Device != b.Device
}

// rebuildAllMiddlewares resets and rebuilds scheduler and job middlewares.
// Used after webhook config changes to re-attach updated webhook middlewares.
func (c *Config) rebuildAllMiddlewares() {
	c.sh.ResetMiddlewares()
	c.buildSchedulerMiddlewares(c.sh)
	wm := c.getWebhookManager()
	// All jobs (including disabled/paused) remain in Jobs, so one loop suffices.
	for _, j := range c.sh.Jobs {
		if jc, ok := j.(jobConfig); ok {
			jc.ResetMiddlewares()
			jc.buildMiddlewares(c.logger, wm)
			j.Use(c.sh.Middlewares()...)
		}
	}
}

// mergeWebhookConfigs merges label-defined webhooks into the main config.
// INI-defined webhooks take precedence: label webhooks are added only if no
// INI webhook with the same name exists.
//
// The early-return is intentionally split: per-webhook merge gates on
// len(parsed.Webhooks) > 0, but the global-selector / cache-TTL merge runs
// regardless. An operator who sets only `ofelia.webhook-webhooks=slack-alerts`
// on a service container (referencing INI-defined webhooks) must still see
// that selector applied — see #640.
func mergeWebhookConfigs(c *Config, parsed *WebhookConfigs) {
	if parsed == nil {
		return
	}
	if c.WebhookConfigs == nil {
		c.WebhookConfigs = NewWebhookConfigs()
	}

	for name, wh := range parsed.Webhooks {
		if _, exists := c.WebhookConfigs.Webhooks[name]; exists {
			c.logger.Warn(fmt.Sprintf("ignoring label-defined webhook %q because an INI webhook with the same name exists", name))
			continue
		}
		c.WebhookConfigs.Webhooks[name] = wh
	}

	mergeWebhookGlobals(c.WebhookConfigs.Global, parsed.Global)
}

// mergeWebhookGlobals copies operator-tunable webhook globals from a
// label-parsed scratch config into the live config. Each field uses the
// "INI is at the documented default → take label" sentinel, mirroring the
// AllowedHosts="*" pattern. Same ambiguity applies: an INI value set to
// exactly the default is indistinguishable from unset, but the alternative
// (tracking explicit-set state per field) would require parser-level changes
// disproportionate to the gain.
//
// Only non-SSRF-sensitive globals are forwarded here. See #486 / #640.
//
// Returns true when any field was overwritten so callers (notably
// applyWebhookChanges in the runtime label-reconcile path) can flip their
// `changed` flag and trigger webhook-manager re-init.
func mergeWebhookGlobals(dst, src *middlewares.WebhookGlobalConfig) bool {
	changed := false
	if dst.Webhooks == "" && src.Webhooks != "" {
		dst.Webhooks = src.Webhooks
		changed = true
	}
	if dst.AllowedHosts == "*" && src.AllowedHosts != "*" && src.AllowedHosts != "" {
		dst.AllowedHosts = src.AllowedHosts
		changed = true
	}
	defaultTTL := 24 * time.Hour
	if dst.PresetCacheTTL == defaultTTL && src.PresetCacheTTL != 0 && src.PresetCacheTTL != defaultTTL {
		dst.PresetCacheTTL = src.PresetCacheTTL
		changed = true
	}
	// DefaultPreset uses the *string nil-vs-set pattern (same shape as
	// SaveConfig.RestoreHistory / mergeSaveGlobals) instead of the
	// documented-default sentinel trick used for sibling fields above.
	// Lets operators distinguish three intents unambiguously: nil = "did
	// not set" (live fallback to DefaultPresetName), non-nil "" =
	// "explicit opt-out", non-nil "X" = "use X as fallback". Label only
	// wins when INI didn't set the key at all.
	if dst.DefaultPreset == nil && src.DefaultPreset != nil {
		v := *src.DefaultPreset
		dst.DefaultPreset = &v
		changed = true
	}
	return changed
}

// buildWebhookConfigsFromLabels creates WebhookConfig objects from label-parsed webhook params.
func buildWebhookConfigsFromLabels(c *Config, webhookLabels map[string]map[string]string) {
	if len(webhookLabels) == 0 {
		return
	}
	if c.WebhookConfigs == nil {
		c.WebhookConfigs = NewWebhookConfigs()
	}
	for name, params := range webhookLabels {
		wh := middlewares.DefaultWebhookConfig()
		wh.Name = name
		applyWebhookLabelParams(wh, params)
		c.WebhookConfigs.Webhooks[name] = wh
	}
}

// applyWebhookLabelParams applies flat label params to a WebhookConfig.
// This mirrors parseWebhookConfig but works from a string map (Docker labels)
// instead of an INI section.
func applyWebhookLabelParams(config *middlewares.WebhookConfig, params map[string]string) { //nolint:gocyclo // flat label-dispatch table
	for key, val := range params {
		switch strings.ToLower(key) {
		case "preset":
			config.Preset = val
		case "id":
			config.ID = val
		case "secret": //nolint:goconst // matches gcfg:"secret" struct tag — Go syntax requires literal in tag
			config.Secret = val
		case "url":
			config.URL = val
		case "trigger":
			config.Trigger = middlewares.TriggerType(val)
		case "timeout":
			if d, err := time.ParseDuration(val); err == nil {
				config.Timeout = d
			}
		case "retry-count":
			if n, err := strconv.Atoi(val); err == nil {
				config.RetryCount = n
			}
		case "retry-delay":
			if d, err := time.ParseDuration(val); err == nil {
				config.RetryDelay = d
			}
		case "link":
			config.Link = val
		case "link-text":
			config.LinkText = val
		case "device":
			config.Device = val
		}
	}
}

// legacyWebhookLabelAliases maps the OLD unprefixed Docker label keys to the
// canonical webhook-* form. Only the webhook-list selector survived #486's
// label allow-list, so this is now a single-entry map; it stays a map so the
// label allow-list and the deprecation gate can iterate on a uniform
// data structure if more aliases ever return.
var legacyWebhookLabelAliases = map[string]string{
	legacyLabelKeyWebhooks: webhookGlobalKeyWebhooks,
}

// loggedDeprecatedLabel guards the per-key deprecation log so reconcile-driven
// re-invocations of applyGlobalWebhookLabels don't spam the log on every
// container event. Each old name fires at most once for the lifetime of the
// process.
var loggedDeprecatedLabel sync.Map // map[string]struct{}

// resetDeprecatedLabelLogForTest clears the one-shot deprecation gate so each
// test exercising the warning starts from a clean slate. Tests register this
// via t.Cleanup; production code never calls it.
func resetDeprecatedLabelLogForTest() {
	loggedDeprecatedLabel.Range(func(k, _ any) bool {
		loggedDeprecatedLabel.Delete(k)
		return true
	})
}

// pickWebhookLabel returns the value for the canonical (webhook-prefixed) key
// if present; otherwise falls back to the legacy unprefixed form (logging a
// one-shot deprecation warning). The new form takes precedence so operators
// mid-migration don't see the legacy key clobber their explicit new-style
// value.
func pickWebhookLabel(globals map[string]any, canonical string, logger *slog.Logger) (string, bool) {
	if v, ok := globals[canonical]; ok {
		if s, ok := v.(string); ok {
			return s, true
		}
		return "", false
	}
	for legacy, target := range legacyWebhookLabelAliases {
		if target != canonical {
			continue
		}
		v, ok := globals[legacy]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			return "", false
		}
		// Only consume the one-shot gate when we can actually emit the
		// warning, so an early nil-logger pass doesn't permanently
		// suppress the warning for the rest of the process.
		if logger != nil {
			if _, loaded := loggedDeprecatedLabel.LoadOrStore(legacy, struct{}{}); !loaded {
				logger.Warn("DEPRECATED Docker label key — use the new prefixed form",
					"legacy_key", "ofelia."+legacy,
					"new_key", "ofelia."+canonical,
					"see", "https://github.com/netresearch/ofelia/issues/620")
			}
		}
		return s, true
	}
	return "", false
}

// applyGlobalWebhookLabels extracts the operator-tunable webhook globals from
// the globals map (populated from service container labels) into the Config's
// WebhookConfigs.Global. Two keys are handled:
//
//   - webhook-webhooks: the per-job/global selector. Accepts both the
//     canonical form and the legacy unprefixed `webhooks` form left behind by
//     #618 (with a one-shot deprecation warning).
//   - webhook-preset-cache-ttl: how long remote presets are cached. Not
//     SSRF-sensitive — narrowing or widening the TTL cannot widen the network
//     egress surface — and operationally tunable, so exposing it via labels is
//     a UX win without weakening the #486 boundary.
//
// SSRF-sensitive globals (webhook-allowed-hosts, webhook-allow-remote-presets,
// webhook-trusted-preset-sources, webhook-preset-cache-dir) are intentionally
// NOT applied here even when they appear in the map: the production allow-list
// filters them upstream, and skipping them at the reader is defense-in-depth
// so any future caller that hands this helper raw label data cannot re-enable
// the #486 risk.
func applyGlobalWebhookLabels(c *Config, globals map[string]any) {
	if c.WebhookConfigs == nil {
		c.WebhookConfigs = NewWebhookConfigs()
	}

	if s, ok := pickWebhookLabel(globals, webhookGlobalKeyWebhooks, c.logger); ok {
		c.WebhookConfigs.Global.Webhooks = s
	}
	if v, ok := globals[webhookGlobalKeyPresetCacheTTL]; ok {
		if s, ok := v.(string); ok {
			if d, err := time.ParseDuration(s); err == nil {
				c.WebhookConfigs.Global.PresetCacheTTL = d
			} else if c.logger != nil {
				c.logger.Warn("ignoring invalid webhook-preset-cache-ttl label",
					"value", s, "error", err.Error())
			}
		}
	}
	if v, ok := globals[webhookGlobalKeyDefaultPreset]; ok {
		// Capture even an empty string explicitly so operators can opt out of
		// the json-post fallback via `ofelia.webhook-default-preset:` with no
		// value. nil means "not set"; non-nil "" means "explicit opt-out".
		if s, ok := v.(string); ok {
			c.WebhookConfigs.Global.DefaultPreset = &s
		}
	}
}
