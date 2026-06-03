// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

// TestJobWebhookConfig_EmbeddedInExecJobConfig verifies JobWebhookConfig is embedded
func TestJobWebhookConfig_EmbeddedInExecJobConfig(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	config := &RunJobConfig{}
	config.Webhooks = "teams"

	names := config.GetWebhookNames()
	if len(names) != 1 {
		t.Errorf("Expected 1 webhook name, got %d", len(names))
	}
}

// TestJobWebhookConfig_EmbeddedInLocalJobConfig verifies JobWebhookConfig is embedded
func TestJobWebhookConfig_EmbeddedInLocalJobConfig(t *testing.T) {
	t.Parallel()
	config := &LocalJobConfig{}
	config.Webhooks = "ntfy, pushover"

	names := config.GetWebhookNames()
	if len(names) != 2 {
		t.Errorf("Expected 2 webhook names, got %d", len(names))
	}
}

// TestJobWebhookConfig_EmbeddedInRunServiceConfig verifies JobWebhookConfig is embedded
func TestJobWebhookConfig_EmbeddedInRunServiceConfig(t *testing.T) {
	t.Parallel()
	config := &RunServiceConfig{}
	config.Webhooks = "pagerduty"

	names := config.GetWebhookNames()
	if len(names) != 1 {
		t.Errorf("Expected 1 webhook name, got %d", len(names))
	}
}

// TestJobWebhookConfig_EmbeddedInComposeJobConfig verifies JobWebhookConfig is embedded
func TestJobWebhookConfig_EmbeddedInComposeJobConfig(t *testing.T) {
	t.Parallel()
	config := &ComposeJobConfig{}
	config.Webhooks = "gotify"

	names := config.GetWebhookNames()
	if len(names) != 1 {
		t.Errorf("Expected 1 webhook name, got %d", len(names))
	}
}

// TestWebhookConfig_ParsedFromINI verifies webhooks field is parsed from INI
func TestWebhookConfig_ParsedFromINI(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	iniContent := `
[global]
webhook-webhooks = slack-alerts

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
	t.Parallel()
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
	t.Parallel()
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
	jobConfig.buildMiddlewares(nil, manager)

	// Verify middleware count increased (overlap + slack + save + mail + webhook)
	// The job should have at least one middleware from the webhook
	middlewareCount := len(jobConfig.ExecJob.Middlewares())
	if middlewareCount < 1 {
		t.Errorf("Expected at least 1 middleware, got %d", middlewareCount)
	}
}

// TestBuildMiddlewares_MultipleWebhooks_AllAttached verifies that multiple
// webhooks attached to a single job are ALL retained — regression test for
// https://github.com/netresearch/ofelia/issues/670 where
// core.middlewareContainer.Use() deduplicated by reflect type, silently
// dropping every webhook after the first when a job referenced more than one.
func TestBuildMiddlewares_MultipleWebhooks_AllAttached(t *testing.T) {
	t.Parallel()
	globalConfig := middlewares.DefaultWebhookGlobalConfig()
	manager := middlewares.NewWebhookManager(globalConfig)

	successHook := &middlewares.WebhookConfig{
		Name:    "wh-success",
		Preset:  "slack",
		ID:      "T123/B456",
		Secret:  "xoxb-secret",
		Trigger: middlewares.TriggerSuccess,
	}
	errorHook := &middlewares.WebhookConfig{
		Name:    "wh-error",
		Preset:  "slack",
		ID:      "T123/B456",
		Secret:  "xoxb-secret",
		Trigger: middlewares.TriggerError,
	}
	if err := manager.Register(successHook); err != nil {
		t.Fatalf("register success hook: %v", err)
	}
	if err := manager.Register(errorHook); err != nil {
		t.Fatalf("register error hook: %v", err)
	}

	jobConfig := &ExecJobConfig{}
	jobConfig.Name = "test-job"
	jobConfig.Schedule = "@daily"
	jobConfig.Webhooks = "wh-success, wh-error"

	jobConfig.buildMiddlewares(nil, manager)

	var (
		individual []string
		composite  *middlewares.WebhookMiddleware
	)
	for _, mw := range jobConfig.ExecJob.Middlewares() {
		switch w := mw.(type) {
		case *middlewares.Webhook:
			individual = append(individual, w.Config.Name)
		case *middlewares.WebhookMiddleware:
			composite = w
		}
	}

	if composite == nil {
		t.Fatalf("expected a *middlewares.WebhookMiddleware composite attached; only got individual %v", individual)
	}
	if len(individual) > 0 {
		t.Errorf("expected webhooks to be wrapped in composite, but found loose *middlewares.Webhook entries: %v", individual)
	}

	names := make([]string, 0, len(composite.Webhooks()))
	for _, mw := range composite.Webhooks() {
		w, ok := mw.(*middlewares.Webhook)
		if !ok {
			t.Errorf("composite contains non-Webhook middleware: %T", mw)
			continue
		}
		names = append(names, w.Config.Name)
	}
	if len(names) != 2 {
		t.Fatalf("expected composite to contain 2 webhooks, got %d: %v", len(names), names)
	}
	wantNames := map[string]bool{"wh-success": true, "wh-error": true}
	for _, n := range names {
		if !wantNames[n] {
			t.Errorf("unexpected webhook in composite: %q", n)
		}
		delete(wantNames, n)
	}
	for n := range wantNames {
		t.Errorf("composite missing webhook %q", n)
	}
}

// TestBuildMiddlewares_WebhookAttachFailureLogged verifies that an error from
// WebhookManager.GetMiddlewares (e.g., unknown webhook name) is logged and the
// job is left without webhook middleware, instead of the previous silent
// swallow that hid misconfigurations from operators.
//
// Uses a per-test slog.Logger passed via buildMiddlewares' logger parameter
// so the assertion is hermetic — other parallel tests' egress warnings
// don't contaminate the buffer and we don't have to mutate slog.Default().
//
// See https://github.com/netresearch/ofelia/issues/670.
func TestBuildMiddlewares_WebhookAttachFailureLogged(t *testing.T) {
	t.Parallel()
	globalConfig := middlewares.DefaultWebhookGlobalConfig()
	manager := middlewares.NewWebhookManager(globalConfig)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	jobConfig := &ExecJobConfig{}
	jobConfig.Name = "test-job"
	jobConfig.Schedule = "@daily"
	jobConfig.Webhooks = "missing-webhook"

	jobConfig.buildMiddlewares(logger, manager)

	for _, mw := range jobConfig.ExecJob.Middlewares() {
		if _, ok := mw.(*middlewares.Webhook); ok {
			t.Errorf("expected no *middlewares.Webhook attached when lookup fails")
		}
		if _, ok := mw.(*middlewares.WebhookMiddleware); ok {
			t.Errorf("expected no *middlewares.WebhookMiddleware attached when lookup fails")
		}
	}

	logged := buf.String()
	if !strings.Contains(logged, "webhook middleware attach failed") {
		t.Errorf("expected attach-failure error log, got: %q", logged)
	}
	if !strings.Contains(logged, "missing-webhook") {
		t.Errorf("expected log to mention the missing webhook name, got: %q", logged)
	}
	if !strings.Contains(logged, "job=test-job") {
		t.Errorf("expected log to scope by job name, got: %q", logged)
	}
}

// TestBuildMiddlewares_GlobalAndPerJobWebhooks_BothAttached verifies that
// global webhooks (from `[global] webhook-webhooks = ...`) are unioned with
// per-job webhook names so jobs that declare their own webhooks ALSO fire
// the global ones. Previously the scheduler's *WebhookMiddleware shadowed
// the per-job composite during scheduler→job propagation (same type, dropped
// by core.middlewareContainer.Use's reflect-type dedup), so a job that
// listed any webhook at all silently lost the global notifications.
// See https://github.com/netresearch/ofelia/issues/670.
func TestBuildMiddlewares_GlobalAndPerJobWebhooks_BothAttached(t *testing.T) {
	t.Parallel()
	globalConfig := middlewares.DefaultWebhookGlobalConfig()
	globalConfig.Webhooks = "global-hook"
	manager := middlewares.NewWebhookManager(globalConfig)

	for _, name := range []string{"global-hook", "job-hook"} {
		err := manager.Register(&middlewares.WebhookConfig{
			Name:    name,
			Preset:  "slack",
			ID:      "T123/B456",
			Secret:  "xoxb-secret",
			Trigger: middlewares.TriggerAlways,
		})
		if err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}

	jobConfig := &ExecJobConfig{}
	jobConfig.Name = "test-job"
	jobConfig.Schedule = "@daily"
	jobConfig.Webhooks = "job-hook"

	jobConfig.buildMiddlewares(nil, manager)

	var composite *middlewares.WebhookMiddleware
	for _, mw := range jobConfig.ExecJob.Middlewares() {
		if w, ok := mw.(*middlewares.WebhookMiddleware); ok {
			composite = w
		}
	}
	if composite == nil {
		t.Fatal("expected *middlewares.WebhookMiddleware composite carrying both global and per-job webhooks")
	}

	got := make(map[string]bool)
	for _, mw := range composite.Webhooks() {
		w, ok := mw.(*middlewares.Webhook)
		if !ok {
			continue
		}
		got[w.Config.Name] = true
	}
	if !got["global-hook"] {
		t.Errorf("global-hook missing from composite — global webhooks not unioned into per-job")
	}
	if !got["job-hook"] {
		t.Errorf("job-hook missing from composite")
	}
}

// TestGlobalWebhooks_AttachedToScheduler verifies global webhooks are attached
func TestGlobalWebhooks_AttachedToScheduler(t *testing.T) {
	t.Parallel()
	iniContent := `
[global]
webhook-webhooks = global-slack

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
	t.Parallel()
	iniContent := `
[webhook "matrix-alerts"]
preset = matrix
url = https://matrix.example.com/hookshot/webhook/123
link = https://logs.example.com/ofelia
link-text = View Logs
device = iphone,desktop
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

	if webhook.Device != "iphone,desktop" {
		t.Errorf("Expected device 'iphone,desktop', got %q", webhook.Device)
	}
}
