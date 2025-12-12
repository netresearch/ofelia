package cli

import (
	"testing"
	"time"

	"github.com/netresearch/ofelia/middlewares"
)

func TestNewWebhookConfigs(t *testing.T) {
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
	name := parseWebhookName(`webhook "slack-alerts"`)
	if name != "slack-alerts" {
		t.Errorf("Expected 'slack-alerts', got '%s'", name)
	}
}

func TestParseWebhookName_SingleQuotes(t *testing.T) {
	name := parseWebhookName(`webhook 'discord-webhook'`)
	if name != "discord-webhook" {
		t.Errorf("Expected 'discord-webhook', got '%s'", name)
	}
}

func TestParseWebhookName_NoQuotes(t *testing.T) {
	name := parseWebhookName("webhook mywebhook")
	if name != "mywebhook" {
		t.Errorf("Expected 'mywebhook', got '%s'", name)
	}
}

func TestParseWebhookName_WithSpaces(t *testing.T) {
	name := parseWebhookName(`webhook   "spaced"   `)
	if name != "spaced" {
		t.Errorf("Expected 'spaced', got '%s'", name)
	}
}

func TestParseWebhookName_Empty(t *testing.T) {
	name := parseWebhookName("webhook")
	if name != "" {
		t.Errorf("Expected empty string, got '%s'", name)
	}
}

func TestJobWebhookConfig_GetWebhookNames_Empty(t *testing.T) {
	config := &JobWebhookConfig{Webhooks: ""}
	names := config.GetWebhookNames()

	if len(names) != 0 {
		t.Errorf("Expected empty slice, got %v", names)
	}
}

func TestJobWebhookConfig_GetWebhookNames_Single(t *testing.T) {
	config := &JobWebhookConfig{Webhooks: "slack"}
	names := config.GetWebhookNames()

	if len(names) != 1 || names[0] != "slack" {
		t.Errorf("Expected ['slack'], got %v", names)
	}
}

func TestJobWebhookConfig_GetWebhookNames_Multiple(t *testing.T) {
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
	global := middlewares.DefaultWebhookGlobalConfig()

	if global.AllowRemotePresets {
		t.Error("AllowRemotePresets should be false by default")
	}

	if global.PresetCacheTTL != 24*time.Hour {
		t.Errorf("Expected 24h TTL, got %v", global.PresetCacheTTL)
	}
}
