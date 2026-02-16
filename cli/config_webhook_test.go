package cli

import (
	"testing"
	"time"

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
}

func TestApplyGlobalWebhookLabels(t *testing.T) {
	t.Parallel()
	c := NewConfig(nil)

	globals := map[string]any{
		"webhooks":              "slack-alerts,discord-notify",
		"allow-remote-presets":  "true",
		"webhook-allowed-hosts": "hooks.slack.com",
	}

	applyGlobalWebhookLabels(c, globals)

	if c.WebhookConfigs.Global.Webhooks != "slack-alerts,discord-notify" {
		t.Errorf("Expected webhooks 'slack-alerts,discord-notify', got %q", c.WebhookConfigs.Global.Webhooks)
	}
	if !c.WebhookConfigs.Global.AllowRemotePresets {
		t.Error("Expected AllowRemotePresets to be true")
	}
	if c.WebhookConfigs.Global.AllowedHosts != "hooks.slack.com" {
		t.Errorf("Expected allowed hosts 'hooks.slack.com', got %q", c.WebhookConfigs.Global.AllowedHosts)
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
