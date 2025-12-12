package cli

import (
	"strings"
	"testing"

	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

// TestJobWebhookConfig_EmbeddedInExecJobConfig verifies JobWebhookConfig is embedded
func TestJobWebhookConfig_EmbeddedInExecJobConfig(t *testing.T) {
	config := &ExecJobConfig{}
	// This will fail to compile if JobWebhookConfig is not embedded
	config.Webhooks = "slack-alerts, discord-notify"

	names := config.GetWebhookNames()
	if len(names) != 2 {
		t.Errorf("Expected 2 webhook names, got %d", len(names))
	}
	if names[0] != "slack-alerts" {
		t.Errorf("Expected first webhook 'slack-alerts', got %q", names[0])
	}
}

// TestJobWebhookConfig_EmbeddedInRunJobConfig verifies JobWebhookConfig is embedded
func TestJobWebhookConfig_EmbeddedInRunJobConfig(t *testing.T) {
	config := &RunJobConfig{}
	config.Webhooks = "teams"

	names := config.GetWebhookNames()
	if len(names) != 1 {
		t.Errorf("Expected 1 webhook name, got %d", len(names))
	}
}

// TestJobWebhookConfig_EmbeddedInLocalJobConfig verifies JobWebhookConfig is embedded
func TestJobWebhookConfig_EmbeddedInLocalJobConfig(t *testing.T) {
	config := &LocalJobConfig{}
	config.Webhooks = "ntfy, pushover"

	names := config.GetWebhookNames()
	if len(names) != 2 {
		t.Errorf("Expected 2 webhook names, got %d", len(names))
	}
}

// TestJobWebhookConfig_EmbeddedInRunServiceConfig verifies JobWebhookConfig is embedded
func TestJobWebhookConfig_EmbeddedInRunServiceConfig(t *testing.T) {
	config := &RunServiceConfig{}
	config.Webhooks = "pagerduty"

	names := config.GetWebhookNames()
	if len(names) != 1 {
		t.Errorf("Expected 1 webhook name, got %d", len(names))
	}
}

// TestJobWebhookConfig_EmbeddedInComposeJobConfig verifies JobWebhookConfig is embedded
func TestJobWebhookConfig_EmbeddedInComposeJobConfig(t *testing.T) {
	config := &ComposeJobConfig{}
	config.Webhooks = "gotify"

	names := config.GetWebhookNames()
	if len(names) != 1 {
		t.Errorf("Expected 1 webhook name, got %d", len(names))
	}
}

// TestWebhookConfig_ParsedFromINI verifies webhooks field is parsed from INI
func TestWebhookConfig_ParsedFromINI(t *testing.T) {
	iniContent := `
[global]
webhooks = global-slack

[webhook "slack-alerts"]
preset = slack
id = T123/B456
secret = xoxb-secret
trigger = error

[webhook "discord-notify"]
preset = discord
id = 123456789
secret = abcdef123456
trigger = always

[job-exec "test-job"]
schedule = @daily
container = test
command = echo hello
webhooks = slack-alerts, discord-notify
`
	config, err := BuildFromString(iniContent, test.NewTestLogger())
	if err != nil {
		t.Fatalf("Failed to parse INI: %v", err)
	}

	// Verify webhooks were parsed
	if config.WebhookConfigs == nil {
		t.Fatal("WebhookConfigs is nil")
	}

	if len(config.WebhookConfigs.Webhooks) != 2 {
		t.Errorf("Expected 2 webhooks, got %d", len(config.WebhookConfigs.Webhooks))
	}

	slackConfig, ok := config.WebhookConfigs.Webhooks["slack-alerts"]
	if !ok {
		t.Error("slack-alerts webhook not found")
	} else {
		if slackConfig.Preset != "slack" {
			t.Errorf("Expected preset 'slack', got %q", slackConfig.Preset)
		}
		if slackConfig.Trigger != middlewares.TriggerError {
			t.Errorf("Expected trigger 'error', got %q", slackConfig.Trigger)
		}
	}

	// Verify job webhook assignment was parsed
	jobConfig, ok := config.ExecJobs["test-job"]
	if !ok {
		t.Fatal("test-job not found")
	}

	webhookNames := jobConfig.GetWebhookNames()
	if len(webhookNames) != 2 {
		t.Errorf("Expected 2 webhook names on job, got %d", len(webhookNames))
	}
}

// TestGlobalWebhooks_ParsedFromINI verifies global webhooks are parsed
func TestGlobalWebhooks_ParsedFromINI(t *testing.T) {
	iniContent := `
[global]
webhooks = slack-alerts

[webhook "slack-alerts"]
preset = slack
id = T123/B456
secret = xoxb-secret
`
	config, err := BuildFromString(iniContent, test.NewTestLogger())
	if err != nil {
		t.Fatalf("Failed to parse INI: %v", err)
	}

	if config.WebhookConfigs.Global.Webhooks != "slack-alerts" {
		t.Errorf("Expected global webhooks 'slack-alerts', got %q", config.WebhookConfigs.Global.Webhooks)
	}
}

// TestWebhookManager_InitializedOnStartup verifies manager is initialized
func TestWebhookManager_InitializedOnStartup(t *testing.T) {
	iniContent := `
[webhook "test-webhook"]
preset = slack
id = T123/B456
secret = xoxb-secret
`
	config, err := BuildFromString(iniContent, test.NewTestLogger())
	if err != nil {
		t.Fatalf("Failed to parse INI: %v", err)
	}

	// Initialize the app (this should initialize the webhook manager)
	// Note: This will fail without Docker, so we just test the webhook initialization
	if config.WebhookConfigs == nil {
		t.Fatal("WebhookConfigs is nil")
	}

	err = config.WebhookConfigs.InitManager()
	if err != nil {
		t.Fatalf("Failed to initialize webhook manager: %v", err)
	}

	if config.WebhookConfigs.Manager == nil {
		t.Error("WebhookManager is nil after initialization")
	}

	// Verify webhook is registered
	wh, ok := config.WebhookConfigs.Manager.Get("test-webhook")
	if !ok {
		t.Error("test-webhook not found in manager")
	}
	if wh.Preset != "slack" {
		t.Errorf("Expected preset 'slack', got %q", wh.Preset)
	}
}

// TestBuildMiddlewares_IncludesWebhooks verifies webhook middleware is attached
func TestBuildMiddlewares_IncludesWebhooks(t *testing.T) {
	// Create a webhook manager with a test webhook
	globalConfig := middlewares.DefaultWebhookGlobalConfig()
	manager := middlewares.NewWebhookManager(globalConfig)

	testWebhook := &middlewares.WebhookConfig{
		Name:    "test-slack",
		Preset:  "slack",
		ID:      "T123/B456",
		Secret:  "xoxb-secret",
		Trigger: middlewares.TriggerAlways,
	}
	err := manager.Register(testWebhook)
	if err != nil {
		t.Fatalf("Failed to register webhook: %v", err)
	}

	// Create job config with webhook
	jobConfig := &ExecJobConfig{}
	jobConfig.Name = "test-job"
	jobConfig.Schedule = "@daily"
	jobConfig.Webhooks = "test-slack"

	// Build middlewares with manager
	jobConfig.buildMiddlewares(manager)

	// Verify middleware count increased (overlap + slack + save + mail + webhook)
	// The job should have at least one middleware from the webhook
	middlewareCount := len(jobConfig.ExecJob.Middlewares())
	if middlewareCount < 1 {
		t.Errorf("Expected at least 1 middleware, got %d", middlewareCount)
	}
}

// TestGlobalWebhooks_AttachedToScheduler verifies global webhooks are attached
func TestGlobalWebhooks_AttachedToScheduler(t *testing.T) {
	iniContent := `
[global]
webhooks = global-slack

[webhook "global-slack"]
preset = slack
id = T123/B456
secret = xoxb-secret
trigger = error
`
	config, err := BuildFromString(iniContent, test.NewTestLogger())
	if err != nil {
		t.Fatalf("Failed to parse INI: %v", err)
	}

	// Initialize webhook manager
	err = config.WebhookConfigs.InitManager()
	if err != nil {
		t.Fatalf("Failed to initialize webhook manager: %v", err)
	}

	// Get global middlewares
	globalMiddlewares, err := config.WebhookConfigs.Manager.GetGlobalMiddlewares()
	if err != nil {
		t.Fatalf("Failed to get global middlewares: %v", err)
	}

	if len(globalMiddlewares) != 1 {
		t.Errorf("Expected 1 global middleware, got %d", len(globalMiddlewares))
	}
}

// TestWebhookLinkFields_ParsedCorrectly verifies link and link-text are parsed
func TestWebhookLinkFields_ParsedCorrectly(t *testing.T) {
	iniContent := `
[webhook "matrix-alerts"]
preset = matrix
url = https://matrix.example.com/hookshot/webhook/123
link = https://logs.example.com/ofelia
link-text = View Logs
trigger = error
`
	config, err := BuildFromString(iniContent, test.NewTestLogger())
	if err != nil {
		t.Fatalf("Failed to parse INI: %v", err)
	}

	webhook, ok := config.WebhookConfigs.Webhooks["matrix-alerts"]
	if !ok {
		t.Fatal("matrix-alerts webhook not found")
	}

	if webhook.Link != "https://logs.example.com/ofelia" {
		t.Errorf("Expected link 'https://logs.example.com/ofelia', got %q", webhook.Link)
	}

	if webhook.LinkText != "View Logs" {
		t.Errorf("Expected link-text 'View Logs', got %q", webhook.LinkText)
	}
}

// Helper to check if a string contains another
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if strings.TrimSpace(item) == s {
			return true
		}
	}
	return false
}
