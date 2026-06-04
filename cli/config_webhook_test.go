// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"reflect"
	"testing"
	"time"

	ini "gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

func TestNewWebhookConfigs(t *testing.T) {
	t.Parallel()
	wc := NewWebhookConfigs()

	if wc == nil {
		t.Fatal("NewWebhookConfigs returned nil")
	}

	if wc.Global == nil {
		t.Error("Global config should not be nil")
	}

	if wc.Webhooks == nil {
		t.Error("Webhooks map should not be nil")
	}
}

func TestParseWebhookName_DoubleQuotes(t *testing.T) {
	t.Parallel()
	name := parseWebhookName(`webhook "slack-alerts"`)
	if name != "slack-alerts" {
		t.Errorf("Expected 'slack-alerts', got '%s'", name)
	}
}

func TestParseWebhookName_SingleQuotes(t *testing.T) {
	t.Parallel()
	name := parseWebhookName(`webhook 'discord-webhook'`)
	if name != "discord-webhook" {
		t.Errorf("Expected 'discord-webhook', got '%s'", name)
	}
}

func TestParseWebhookName_NoQuotes(t *testing.T) {
	t.Parallel()
	name := parseWebhookName("webhook mywebhook")
	if name != "mywebhook" {
		t.Errorf("Expected 'mywebhook', got '%s'", name)
	}
}

func TestParseWebhookName_WithSpaces(t *testing.T) {
	t.Parallel()
	name := parseWebhookName(`webhook   "spaced"   `)
	if name != "spaced" {
		t.Errorf("Expected 'spaced', got '%s'", name)
	}
}

func TestParseWebhookName_Empty(t *testing.T) {
	t.Parallel()
	name := parseWebhookName("webhook")
	if name != "" {
		t.Errorf("Expected empty string, got '%s'", name)
	}
}

func TestJobWebhookConfig_GetWebhookNames_Empty(t *testing.T) {
	t.Parallel()
	config := &JobWebhookConfig{Webhooks: ""}
	names := config.GetWebhookNames()

	if len(names) != 0 {
		t.Errorf("Expected empty slice, got %v", names)
	}
}

func TestJobWebhookConfig_GetWebhookNames_Single(t *testing.T) {
	t.Parallel()
	config := &JobWebhookConfig{Webhooks: "slack"}
	names := config.GetWebhookNames()

	if len(names) != 1 || names[0] != "slack" {
		t.Errorf("Expected ['slack'], got %v", names)
	}
}

func TestJobWebhookConfig_GetWebhookNames_Multiple(t *testing.T) {
	t.Parallel()
	config := &JobWebhookConfig{Webhooks: "slack, discord, teams"}
	names := config.GetWebhookNames()

	expected := []string{"slack", "discord", "teams"}
	if len(names) != len(expected) {
		t.Errorf("Expected %d names, got %d", len(expected), len(names))
		return
	}

	for i, name := range expected {
		if names[i] != name {
			t.Errorf("Expected %s at position %d, got %s", name, i, names[i])
		}
	}
}

func TestWebhookConfigs_InitManager(t *testing.T) {
	t.Parallel()
	wc := NewWebhookConfigs()

	// Add a webhook config
	wc.Webhooks["test-slack"] = &middlewares.WebhookConfig{
		Preset:  "slack",
		Trigger: middlewares.TriggerError,
	}

	err := wc.InitManager()
	if err != nil {
		t.Errorf("InitManager failed: %v", err)
	}

	if wc.Manager == nil {
		t.Error("Manager should be initialized")
	}
}

func TestWebhookConfigs_InitManager_EmptyName(t *testing.T) {
	t.Parallel()
	wc := NewWebhookConfigs()

	// Add a webhook config with empty name (which Register validates)
	wc.Webhooks[""] = &middlewares.WebhookConfig{
		Preset:  "slack",
		Trigger: middlewares.TriggerError,
	}

	err := wc.InitManager()
	if err == nil {
		t.Error("InitManager should fail with empty webhook name")
	}
}

func TestGlobalWebhookConfig_Defaults(t *testing.T) {
	t.Parallel()
	global := middlewares.DefaultWebhookGlobalConfig()

	if global.AllowRemotePresets {
		t.Error("AllowRemotePresets should be false by default")
	}

	if global.PresetCacheTTL != 24*time.Hour {
		t.Errorf("Expected 24h TTL, got %v", global.PresetCacheTTL)
	}
}

func TestApplyWebhookLabelParams(t *testing.T) {
	t.Parallel()
	config := middlewares.DefaultWebhookConfig()

	params := map[string]string{
		"preset":      "slack",
		"id":          "T123/B456",
		"secret":      "xoxb-secret",
		"url":         "https://hooks.slack.com/custom",
		"trigger":     "always",
		"timeout":     "30s",
		"retry-count": "5",
		"retry-delay": "10s",
		"link":        "https://logs.example.com",
		"link-text":   "View Logs",
		"device":      "iphone,desktop",
	}

	applyWebhookLabelParams(config, params)

	if config.Preset != "slack" {
		t.Errorf("Expected preset 'slack', got %q", config.Preset)
	}
	if config.ID != "T123/B456" {
		t.Errorf("Expected ID 'T123/B456', got %q", config.ID)
	}
	if config.Secret != "xoxb-secret" {
		t.Errorf("Expected Secret 'xoxb-secret', got %q", config.Secret)
	}
	if config.URL != "https://hooks.slack.com/custom" {
		t.Errorf("Expected URL, got %q", config.URL)
	}
	if config.Trigger != middlewares.TriggerAlways {
		t.Errorf("Expected trigger 'always', got %q", config.Trigger)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", config.Timeout)
	}
	if config.RetryCount != 5 {
		t.Errorf("Expected retry-count 5, got %d", config.RetryCount)
	}
	if config.RetryDelay != 10*time.Second {
		t.Errorf("Expected retry-delay 10s, got %v", config.RetryDelay)
	}
	if config.Link != "https://logs.example.com" {
		t.Errorf("Expected link, got %q", config.Link)
	}
	if config.LinkText != "View Logs" {
		t.Errorf("Expected link-text 'View Logs', got %q", config.LinkText)
	}
	if config.Device != "iphone,desktop" {
		t.Errorf("Expected device 'iphone,desktop', got %q", config.Device)
	}
}

func TestApplyGlobalWebhookLabels(t *testing.T) {
	t.Parallel()
	c := NewConfig(nil)

	globals := map[string]any{
		"webhooks":              "slack-alerts,discord-notify",
		"allow-remote-presets":  "true",            // SSRF-sensitive — must be ignored (#486)
		"webhook-allowed-hosts": "hooks.slack.com", // SSRF-sensitive — must be ignored (#486)
	}

	applyGlobalWebhookLabels(c, globals)

	if c.WebhookConfigs.Global.Webhooks != "slack-alerts,discord-notify" {
		t.Errorf("Expected webhooks 'slack-alerts,discord-notify', got %q", c.WebhookConfigs.Global.Webhooks)
	}
	if c.WebhookConfigs.Global.AllowRemotePresets {
		t.Error("Expected AllowRemotePresets to remain false — labels must not change SSRF-sensitive globals (#486)")
	}
	if c.WebhookConfigs.Global.AllowedHosts != "*" {
		t.Errorf("Expected AllowedHosts to remain default '*' — labels must not change SSRF-sensitive globals (#486), got %q", c.WebhookConfigs.Global.AllowedHosts)
	}
}

func TestSyncWebhookConfigs_NewWebhookDetected(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	c.sh = core.NewScheduler(logger)

	// Existing webhook in config
	c.WebhookConfigs.Webhooks["existing"] = &middlewares.WebhookConfig{
		Name:   "existing",
		Preset: "slack",
		ID:     "T123",
	}
	// Initialize manager so syncWebhookConfigs can re-init
	_ = c.WebhookConfigs.InitManager()

	// Parsed labels have existing + new webhook
	parsed := NewWebhookConfigs()
	parsed.Webhooks["existing"] = &middlewares.WebhookConfig{
		Name:   "existing",
		Preset: "slack",
		ID:     "T123",
	}
	parsed.Webhooks["new-discord"] = &middlewares.WebhookConfig{
		Name:   "new-discord",
		Preset: "discord",
		URL:    "https://discord.example.com/webhook",
	}

	c.syncWebhookConfigs(parsed)

	// New webhook should be added
	if _, ok := c.WebhookConfigs.Webhooks["new-discord"]; !ok {
		t.Error("Expected new-discord webhook to be added via sync")
	}
	// Manager should be re-initialized (not nil)
	if c.WebhookConfigs.Manager == nil {
		t.Error("Expected webhook manager to be re-initialized after change")
	}
}

func TestSyncWebhookConfigs_ChangedWebhookDetected(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	c.sh = core.NewScheduler(logger)

	c.WebhookConfigs.Webhooks["slack"] = &middlewares.WebhookConfig{
		Name:    "slack",
		Preset:  "slack",
		Trigger: middlewares.TriggerError,
	}
	_ = c.WebhookConfigs.InitManager()

	// Same webhook name but changed trigger
	parsed := NewWebhookConfigs()
	parsed.Webhooks["slack"] = &middlewares.WebhookConfig{
		Name:    "slack",
		Preset:  "slack",
		Trigger: middlewares.TriggerAlways,
	}

	c.syncWebhookConfigs(parsed)

	if c.WebhookConfigs.Webhooks["slack"].Trigger != middlewares.TriggerAlways {
		t.Errorf("Expected trigger to be updated to 'always', got %q", c.WebhookConfigs.Webhooks["slack"].Trigger)
	}
}

func TestSyncWebhookConfigs_NoChangeNoReinit(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	c.sh = core.NewScheduler(logger)

	c.WebhookConfigs.Webhooks["slack"] = &middlewares.WebhookConfig{
		Name:    "slack",
		Preset:  "slack",
		ID:      "T123",
		Trigger: middlewares.TriggerError,
	}
	_ = c.WebhookConfigs.InitManager()
	originalManager := c.WebhookConfigs.Manager

	// Identical parsed config — no change
	parsed := NewWebhookConfigs()
	parsed.Webhooks["slack"] = &middlewares.WebhookConfig{
		Name:    "slack",
		Preset:  "slack",
		ID:      "T123",
		Trigger: middlewares.TriggerError,
	}

	c.syncWebhookConfigs(parsed)

	// Manager should be the same pointer (not re-initialized)
	if c.WebhookConfigs.Manager != originalManager {
		t.Error("Expected webhook manager to NOT be re-initialized when nothing changed")
	}
}

func TestSyncWebhookConfigs_INIWebhookNotOverwritten(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	c.sh = core.NewScheduler(logger)

	// Mark "slack-alerts" as INI-defined
	c.WebhookConfigs.Webhooks["slack-alerts"] = &middlewares.WebhookConfig{
		Name:   "slack-alerts",
		Preset: "slack",
		ID:     "ini-original-id",
		Secret: "ini-secret",
	}
	c.WebhookConfigs.iniWebhookNames = map[string]struct{}{
		"slack-alerts": {},
	}
	_ = c.WebhookConfigs.InitManager()

	// Container labels try to overwrite the INI webhook
	parsed := NewWebhookConfigs()
	parsed.Webhooks["slack-alerts"] = &middlewares.WebhookConfig{
		Name:   "slack-alerts",
		Preset: "slack",
		ID:     "attacker-id",
		Secret: "attacker-secret",
		URL:    "https://evil.example.com/webhook",
	}

	c.syncWebhookConfigs(parsed)

	// INI webhook must NOT be overwritten
	wh := c.WebhookConfigs.Webhooks["slack-alerts"]
	if wh.ID != "ini-original-id" {
		t.Errorf("INI webhook ID was overwritten: got %q, want %q", wh.ID, "ini-original-id")
	}
	if wh.Secret != "ini-secret" {
		t.Errorf("INI webhook Secret was overwritten: got %q, want %q", wh.Secret, "ini-secret")
	}
	if wh.URL != "" {
		t.Errorf("INI webhook URL was overwritten: got %q, want empty", wh.URL)
	}
}

func TestSyncWebhookConfigs_RemovedLabelWebhookCleaned(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	c.sh = core.NewScheduler(logger)

	// Two label-defined webhooks
	c.WebhookConfigs.Webhooks["slack"] = &middlewares.WebhookConfig{
		Name:   "slack",
		Preset: "slack",
	}
	c.WebhookConfigs.Webhooks["discord"] = &middlewares.WebhookConfig{
		Name:   "discord",
		Preset: "discord",
	}
	_ = c.WebhookConfigs.InitManager()

	// Parsed labels only have "slack" — "discord" was removed
	parsed := NewWebhookConfigs()
	parsed.Webhooks["slack"] = &middlewares.WebhookConfig{
		Name:   "slack",
		Preset: "slack",
	}

	c.syncWebhookConfigs(parsed)

	if _, ok := c.WebhookConfigs.Webhooks["slack"]; !ok {
		t.Error("Expected slack webhook to remain")
	}
	if _, ok := c.WebhookConfigs.Webhooks["discord"]; ok {
		t.Error("Expected discord webhook to be removed (no longer in labels)")
	}
}

func TestSyncWebhookConfigs_INIWebhookNotRemovedWhenAbsentFromLabels(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	c.sh = core.NewScheduler(logger)

	// INI-defined webhook
	c.WebhookConfigs.Webhooks["ini-slack"] = &middlewares.WebhookConfig{
		Name:   "ini-slack",
		Preset: "slack",
	}
	c.WebhookConfigs.iniWebhookNames = map[string]struct{}{
		"ini-slack": {},
	}
	_ = c.WebhookConfigs.InitManager()

	// Parsed labels have no webhooks at all
	parsed := NewWebhookConfigs()

	c.syncWebhookConfigs(parsed)

	// INI webhook must NOT be removed
	if _, ok := c.WebhookConfigs.Webhooks["ini-slack"]; !ok {
		t.Error("INI-defined webhook should NOT be removed when absent from labels")
	}
}

func TestMergeWebhookConfigs_INITakesPrecedence(t *testing.T) {
	t.Parallel()
	logger, handler := test.NewTestLoggerWithHandler()
	c := NewConfig(logger)
	c.WebhookConfigs.Webhooks["slack-alerts"] = &middlewares.WebhookConfig{
		Name:   "slack-alerts",
		Preset: "slack",
		ID:     "ini-id",
	}

	parsed := NewWebhookConfigs()
	parsed.Webhooks["slack-alerts"] = &middlewares.WebhookConfig{
		Name:   "slack-alerts",
		Preset: "slack",
		ID:     "label-id",
	}
	parsed.Webhooks["discord-new"] = &middlewares.WebhookConfig{
		Name:   "discord-new",
		Preset: "discord",
	}

	mergeWebhookConfigs(c, parsed)

	// INI webhook should keep its original ID
	if c.WebhookConfigs.Webhooks["slack-alerts"].ID != "ini-id" {
		t.Errorf("Expected INI webhook to take precedence, got ID %q", c.WebhookConfigs.Webhooks["slack-alerts"].ID)
	}

	// Label-only webhook should be added
	if _, ok := c.WebhookConfigs.Webhooks["discord-new"]; !ok {
		t.Error("Expected label-defined discord-new webhook to be added")
	}

	// Warning should be logged
	if !handler.HasWarning("ignoring label-defined webhook") {
		t.Error("Expected warning about ignoring label-defined webhook")
	}
}

func TestWebhookConfigChanged_AllFields(t *testing.T) {
	t.Parallel()

	base := func() *middlewares.WebhookConfig {
		return &middlewares.WebhookConfig{
			Preset:     "slack",
			URL:        "https://hooks.example.com",
			ID:         "T123",
			Secret:     "secret",
			Trigger:    middlewares.TriggerError,
			Timeout:    10 * time.Second,
			RetryCount: 3,
			RetryDelay: 5 * time.Second,
			Link:       "https://logs.example.com",
			LinkText:   "View Logs",
		}
	}

	tests := []struct {
		name   string
		modify func(c *middlewares.WebhookConfig)
		want   bool
	}{
		{name: "identical", modify: func(_ *middlewares.WebhookConfig) {}, want: false},
		{name: "Preset changed", modify: func(c *middlewares.WebhookConfig) { c.Preset = "discord" }, want: true},
		{name: "URL changed", modify: func(c *middlewares.WebhookConfig) { c.URL = "https://other.example.com" }, want: true},
		{name: "ID changed", modify: func(c *middlewares.WebhookConfig) { c.ID = "T999" }, want: true},
		{name: "Secret changed", modify: func(c *middlewares.WebhookConfig) { c.Secret = "new-secret" }, want: true},
		{name: "Trigger changed", modify: func(c *middlewares.WebhookConfig) { c.Trigger = middlewares.TriggerAlways }, want: true},
		{name: "Timeout changed", modify: func(c *middlewares.WebhookConfig) { c.Timeout = 30 * time.Second }, want: true},
		{name: "RetryCount changed", modify: func(c *middlewares.WebhookConfig) { c.RetryCount = 10 }, want: true},
		{name: "RetryDelay changed", modify: func(c *middlewares.WebhookConfig) { c.RetryDelay = 15 * time.Second }, want: true},
		{name: "Link changed", modify: func(c *middlewares.WebhookConfig) { c.Link = "https://dashboard.example.com" }, want: true},
		{name: "LinkText changed", modify: func(c *middlewares.WebhookConfig) { c.LinkText = "Dashboard" }, want: true},
		{name: "Device changed", modify: func(c *middlewares.WebhookConfig) { c.Device = "iphone" }, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := base()
			b := base()
			tt.modify(b)
			got := webhookConfigChanged(a, b)
			if got != tt.want {
				t.Errorf("webhookConfigChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyWebhookLabelParams_InvalidDuration(t *testing.T) {
	t.Parallel()
	config := middlewares.DefaultWebhookConfig()

	params := map[string]string{
		"timeout": "not-a-duration",
	}

	applyWebhookLabelParams(config, params)

	// DefaultWebhookConfig sets Timeout to 10s; invalid duration should not change it
	if config.Timeout != 10*time.Second {
		t.Errorf("Expected timeout to remain at default 10s, got %v", config.Timeout)
	}
}

func TestApplyWebhookLabelParams_InvalidInt(t *testing.T) {
	t.Parallel()
	config := middlewares.DefaultWebhookConfig()

	params := map[string]string{
		"retry-count": "abc",
	}

	applyWebhookLabelParams(config, params)

	// DefaultWebhookConfig sets RetryCount to 3; invalid int should not change it
	if config.RetryCount != 3 {
		t.Errorf("Expected retry-count to remain at default 3, got %d", config.RetryCount)
	}
}

func TestApplyWebhookLabelParams_UnknownKeys(t *testing.T) {
	t.Parallel()
	config := middlewares.DefaultWebhookConfig()
	original := *config

	params := map[string]string{
		"foo": "bar",
	}

	applyWebhookLabelParams(config, params)

	// All fields should remain unchanged (reflect.DeepEqual catches new fields automatically)
	if !reflect.DeepEqual(original, *config) {
		t.Errorf("Unknown key should not affect any config field.\nGot:  %+v\nWant: %+v", *config, original)
	}
}

func TestApplyWebhookLabelParams_EmptyValues(t *testing.T) {
	t.Parallel()
	config := middlewares.DefaultWebhookConfig()

	params := map[string]string{
		"preset": "",
	}

	applyWebhookLabelParams(config, params)

	if config.Preset != "" {
		t.Errorf("Expected Preset to be empty, got %q", config.Preset)
	}
}

// TestApplyGlobalWebhookLabels_OnlyWebhooksKeyApplied asserts the policy
// boundary: the operator-tunable selector (webhook-webhooks, exercised here
// via its legacy unprefixed alias) reaches WebhookConfigs.Global, while the
// SSRF-sensitive globals (allowed-hosts, allow-remote-presets,
// trusted-preset-sources, preset-cache-dir) do NOT. This is a defense-in-depth
// check; the production allow-list already filters them upstream.
//
// webhook-preset-cache-ttl is now operator-tunable via labels (#640) and is
// covered by TestApplyGlobalWebhookLabels_PresetCacheTTL_Applied in the
// parity test file.
func TestApplyGlobalWebhookLabels_OnlyWebhooksKeyApplied(t *testing.T) {
	t.Parallel()
	c := NewConfig(nil)
	defaults := *c.WebhookConfigs.Global

	globals := map[string]any{
		"webhooks":               "slack-alerts,discord",
		"allow-remote-presets":   "true",
		"trusted-preset-sources": "gh:myorg/*",
		"preset-cache-dir":       "/tmp/presets",
		"webhook-allowed-hosts":  "hooks.slack.com,ntfy.internal",
	}

	applyGlobalWebhookLabels(c, globals)

	if c.WebhookConfigs.Global.Webhooks != "slack-alerts,discord" {
		t.Errorf("Expected webhooks 'slack-alerts,discord', got %q", c.WebhookConfigs.Global.Webhooks)
	}
	if c.WebhookConfigs.Global.AllowRemotePresets != defaults.AllowRemotePresets {
		t.Error("AllowRemotePresets must remain at default — labels must not change SSRF-sensitive globals (#486)")
	}
	if c.WebhookConfigs.Global.TrustedPresetSources != defaults.TrustedPresetSources {
		t.Errorf("TrustedPresetSources must remain at default — labels must not change SSRF-sensitive globals (#486), got %q", c.WebhookConfigs.Global.TrustedPresetSources)
	}
	if c.WebhookConfigs.Global.PresetCacheDir != defaults.PresetCacheDir {
		t.Errorf("PresetCacheDir must remain at default — labels must not change SSRF-sensitive globals (#486), got %q", c.WebhookConfigs.Global.PresetCacheDir)
	}
	if c.WebhookConfigs.Global.AllowedHosts != defaults.AllowedHosts {
		t.Errorf("AllowedHosts must remain at default — labels must not change SSRF-sensitive globals (#486), got %q", c.WebhookConfigs.Global.AllowedHosts)
	}
}

func TestMergeWebhookConfigs_NilParsed(t *testing.T) {
	t.Parallel()
	c := NewConfig(nil)
	c.WebhookConfigs.Webhooks["existing"] = &middlewares.WebhookConfig{
		Name:   "existing",
		Preset: "slack",
	}

	mergeWebhookConfigs(c, nil)

	// existing webhook should still be there
	if _, ok := c.WebhookConfigs.Webhooks["existing"]; !ok {
		t.Error("Expected existing webhook to remain after nil merge")
	}
}

func TestMergeWebhookConfigs_EmptyWebhooks(t *testing.T) {
	t.Parallel()
	c := NewConfig(nil)
	c.WebhookConfigs.Webhooks["existing"] = &middlewares.WebhookConfig{
		Name:   "existing",
		Preset: "slack",
	}

	parsed := NewWebhookConfigs()
	mergeWebhookConfigs(c, parsed)

	if _, ok := c.WebhookConfigs.Webhooks["existing"]; !ok {
		t.Error("Expected existing webhook to remain after empty merge")
	}
}

func TestMergeWebhookConfigs_NilWebhookConfigs(t *testing.T) {
	t.Parallel()
	c := NewConfig(nil)
	c.WebhookConfigs = nil

	parsed := NewWebhookConfigs()
	parsed.Webhooks["new-hook"] = &middlewares.WebhookConfig{
		Name:   "new-hook",
		Preset: "slack",
	}

	mergeWebhookConfigs(c, parsed)

	if c.WebhookConfigs == nil {
		t.Fatal("Expected WebhookConfigs to be created")
	}
	if _, ok := c.WebhookConfigs.Webhooks["new-hook"]; !ok {
		t.Error("Expected new-hook webhook to be added")
	}
}

func TestMergeWebhookConfigs_GlobalMerge(t *testing.T) {
	t.Parallel()
	c := NewConfig(nil)
	// c.WebhookConfigs.Global starts with defaults: Webhooks="", AllowedHosts="*"

	parsed := NewWebhookConfigs()
	parsed.Global.Webhooks = "slack-alerts"
	parsed.Global.AllowedHosts = "hooks.slack.com"
	parsed.Webhooks["slack-alerts"] = &middlewares.WebhookConfig{
		Name:   "slack-alerts",
		Preset: "slack",
	}

	mergeWebhookConfigs(c, parsed)

	// Webhooks should be merged because INI Global.Webhooks was empty
	if c.WebhookConfigs.Global.Webhooks != "slack-alerts" {
		t.Errorf("Expected global Webhooks 'slack-alerts', got %q", c.WebhookConfigs.Global.Webhooks)
	}
	// AllowedHosts should be merged because INI was "*" (default)
	if c.WebhookConfigs.Global.AllowedHosts != "hooks.slack.com" {
		t.Errorf("Expected AllowedHosts 'hooks.slack.com', got %q", c.WebhookConfigs.Global.AllowedHosts)
	}
}

func TestSyncWebhookConfigs_NilParsed(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	c.sh = core.NewScheduler(logger)

	c.WebhookConfigs.Webhooks["slack"] = &middlewares.WebhookConfig{
		Name:   "slack",
		Preset: "slack",
	}
	_ = c.WebhookConfigs.InitManager()
	originalManager := c.WebhookConfigs.Manager

	c.syncWebhookConfigs(nil)

	// Manager should remain unchanged
	if c.WebhookConfigs.Manager != originalManager {
		t.Error("Expected manager to remain unchanged after nil sync")
	}
	// Webhooks should remain
	if _, ok := c.WebhookConfigs.Webhooks["slack"]; !ok {
		t.Error("Expected slack webhook to remain after nil sync")
	}
}

func TestBuildWebhookConfigsFromLabels_Empty(t *testing.T) {
	t.Parallel()
	c := NewConfig(nil)
	originalLen := len(c.WebhookConfigs.Webhooks)

	buildWebhookConfigsFromLabels(c, map[string]map[string]string{})

	if len(c.WebhookConfigs.Webhooks) != originalLen {
		t.Error("Expected no webhooks to be added for empty labels")
	}
}

func TestBuildWebhookConfigsFromLabels_NilWebhookConfigs(t *testing.T) {
	t.Parallel()
	c := NewConfig(nil)
	c.WebhookConfigs = nil

	webhookLabels := map[string]map[string]string{
		"slack-alerts": {
			"preset": "slack",
			"id":     "T123",
		},
	}

	buildWebhookConfigsFromLabels(c, webhookLabels)

	if c.WebhookConfigs == nil {
		t.Fatal("Expected WebhookConfigs to be created")
	}
	wh, ok := c.WebhookConfigs.Webhooks["slack-alerts"]
	if !ok {
		t.Fatal("Expected slack-alerts webhook to be created")
	}
	if wh.Preset != "slack" {
		t.Errorf("Expected preset 'slack', got %q", wh.Preset)
	}
	if wh.ID != "T123" {
		t.Errorf("Expected ID 'T123', got %q", wh.ID)
	}
	if wh.Name != "slack-alerts" {
		t.Errorf("Expected name 'slack-alerts', got %q", wh.Name)
	}
}

func TestParseWebhookConfig_EnvExpansion(t *testing.T) {
	t.Setenv("WH_SECRET", "s3cr#t;val")
	t.Setenv("WH_URL", "https://hooks.example.com/abc")

	f := ini.Empty()
	sec, _ := f.NewSection(`webhook "test"`)
	sec.NewKey("secret", "${WH_SECRET}")
	sec.NewKey("url", "${WH_URL}")

	config := middlewares.DefaultWebhookConfig()
	if err := parseWebhookConfig(sec, config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Secret != "s3cr#t;val" {
		t.Errorf("Expected expanded secret, got %q", config.Secret)
	}
	if config.URL != "https://hooks.example.com/abc" {
		t.Errorf("Expected expanded URL, got %q", config.URL)
	}
}

// TestBuildFromString_WebhookGlobalKeys_Issue604 verifies that webhook-* keys
// in the [global] section are recognized (no "Unknown configuration key" warning)
// and that their values populate c.WebhookConfigs.Global.
//
// See https://github.com/netresearch/ofelia/issues/604
func TestBuildFromString_WebhookGlobalKeys_Issue604(t *testing.T) {
	t.Parallel()

	logger, handler := test.NewTestLoggerWithHandler()

	ini := `
[global]
webhook-allow-remote-presets   = true
webhook-preset-cache-ttl       = 12h
webhook-trusted-preset-sources = gh:netresearch/*, gh:myorg/ofelia-presets/*
webhook-preset-cache-dir       = /tmp/ofelia-presets
webhook-allowed-hosts          = hooks.slack.com, discord.com
`

	c, err := BuildFromString(ini, logger)
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}

	// (a) No "Unknown configuration key" warning for any of these keys.
	suspects := []string{
		"webhook-allow-remote-presets",
		"webhook-preset-cache-ttl",
		"webhook-trusted-preset-sources",
		"webhook-preset-cache-dir",
		"webhook-allowed-hosts",
	}
	for _, key := range suspects {
		if handler.HasWarning("Unknown configuration key '" + key + "'") {
			t.Errorf("got 'Unknown configuration key' warning for %q (issue #604)", key)
		}
	}

	// (b) Values are reflected in c.WebhookConfigs.Global.
	if c.WebhookConfigs == nil || c.WebhookConfigs.Global == nil {
		t.Fatal("WebhookConfigs.Global is nil after BuildFromString")
	}
	g := c.WebhookConfigs.Global
	if !g.AllowRemotePresets {
		t.Errorf("AllowRemotePresets = false, want true")
	}
	if g.PresetCacheTTL != 12*time.Hour {
		t.Errorf("PresetCacheTTL = %v, want 12h", g.PresetCacheTTL)
	}
	if g.TrustedPresetSources != "gh:netresearch/*, gh:myorg/ofelia-presets/*" {
		t.Errorf("TrustedPresetSources = %q, want %q",
			g.TrustedPresetSources, "gh:netresearch/*, gh:myorg/ofelia-presets/*")
	}
	if g.PresetCacheDir != "/tmp/ofelia-presets" {
		t.Errorf("PresetCacheDir = %q, want %q", g.PresetCacheDir, "/tmp/ofelia-presets")
	}
	if g.AllowedHosts != "hooks.slack.com, discord.com" {
		t.Errorf("AllowedHosts = %q, want %q",
			g.AllowedHosts, "hooks.slack.com, discord.com")
	}
}

// TestBuildFromString_OldUnprefixedKeysWarn verifies that the previously
// undocumented unprefixed forms (e.g. allow-remote-presets, webhooks) under
// [global] now produce "Unknown configuration key" warnings rather than
// silently being ignored or - worse - applied through some legacy path.
// Regression guard: if someone re-adds the unprefixed forms to the struct
// tags, this test fires.
func TestBuildFromString_OldUnprefixedKeysWarn(t *testing.T) {
	t.Parallel()

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(`
[global]
log-level = info
allow-remote-presets = true
trusted-preset-sources = gh:netresearch/*
preset-cache-ttl = 12h
preset-cache-dir = /tmp/old
allowed-hosts = *.example.com
`, logger)
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}

	want := []string{
		"allow-remote-presets",
		"trusted-preset-sources",
		"preset-cache-ttl",
		"preset-cache-dir",
		"allowed-hosts",
	}
	for _, key := range want {
		if !handler.HasWarning("Unknown configuration key '" + key + "'") {
			t.Errorf("expected 'Unknown configuration key' warning for unprefixed %q, did not see it", key)
		}
	}
}

// TestBuildFromString_WebhookGlobalKeys_DefaultsPreserved verifies that when
// no webhook-* global keys are configured, the defaults from
// DefaultWebhookGlobalConfig() are preserved (especially AllowedHosts="*", which
// is security-relevant).
func TestBuildFromString_WebhookGlobalKeys_DefaultsPreserved(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	// Minimal config with no webhook-* keys.
	c, err := BuildFromString(`
[global]
log-level = info
`, logger)
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}

	if c.WebhookConfigs == nil || c.WebhookConfigs.Global == nil {
		t.Fatal("WebhookConfigs.Global is nil")
	}
	g := c.WebhookConfigs.Global
	if g.AllowedHosts != "*" {
		t.Errorf("AllowedHosts default = %q, want %q (security-relevant)", g.AllowedHosts, "*")
	}
	if g.PresetCacheTTL != 24*time.Hour {
		t.Errorf("PresetCacheTTL default = %v, want 24h", g.PresetCacheTTL)
	}
	if g.AllowRemotePresets {
		t.Errorf("AllowRemotePresets default = true, want false")
	}
}
