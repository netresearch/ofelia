// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/netresearch/ofelia/core"
)

// Version is set during build and used in webhook templates
var Version = "dev"

// Webhook middleware sends HTTP webhook notifications after job execution
type Webhook struct {
	Config       *WebhookConfig
	Preset       *Preset
	PresetLoader *PresetLoader
	Client       *http.Client
}

// NewWebhook creates a new Webhook middleware from configuration.
// Returns (nil, nil) when config is nil, indicating no middleware should be created.
func NewWebhook(config *WebhookConfig, loader *PresetLoader) (core.Middleware, error) {
	if config == nil {
		return nil, nil //nolint:nilnil // nil config means no middleware needed, not an error
	}

	// Apply defaults
	config.ApplyDefaults()

	if loader == nil {
		return nil, fmt.Errorf("webhook %q: preset loader is required", config.Name)
	}

	// Fall back to the global default preset (e.g. "json-post", the bundled
	// JSON POST preset) when the per-webhook config omits `preset`. Lets
	// operators define a webhook with just a `url` and get a sensible
	// JSON POST without authoring a custom preset. See #676.
	if config.Preset == "" {
		config.Preset = loader.DefaultPreset()
	}

	// If preset is STILL empty after the fallback, the operator has
	// explicitly opted out of the json-post default (set
	// [global] webhook-default-preset to an empty string) and not
	// declared `preset` on this webhook. Surface that with a message
	// that names webhook-default-preset so operators can grep their way
	// to the docs. Runs before Validate so the resulting error mentions
	// the actual knob, not a generic "either preset or url" message
	// that doesn't hint at why the previously-working url-only config
	// stopped working. See #676.
	if config.Preset == "" {
		return nil, fmt.Errorf(
			"webhook %q: preset is required when [global] webhook-default-preset "+
				"is set to an empty string (opt-out). Declare `preset` on this "+
				"webhook, or unset webhook-default-preset to restore the "+
				"bundled %q fallback for url-only configs (see #676)",
			config.Name, DefaultPresetName,
		)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Load preset
	preset, err := loader.Load(config.Preset)
	if err != nil {
		return nil, fmt.Errorf("webhook %q: load preset %q: %w", config.Name, config.Preset, err)
	}

	// Validate required variables
	if err := validatePresetVariables(preset, config); err != nil {
		return nil, fmt.Errorf("webhook %q: %w", config.Name, err)
	}

	return &Webhook{
		Config:       config,
		Preset:       preset,
		PresetLoader: loader,
		Client: &http.Client{
			Timeout:   config.Timeout,
			Transport: TransportFactory(),
		},
	}, nil
}

// validatePresetVariables checks that all required variables are provided
func validatePresetVariables(preset *Preset, config *WebhookConfig) error {
	for name, variable := range preset.Variables {
		if !variable.Required {
			continue
		}

		var value string
		switch name {
		case "id":
			value = config.ID
		case "secret":
			value = config.Secret
		case "url":
			value = config.URL
		default:
			if config.CustomVars != nil {
				value = config.CustomVars[name]
			}
		}

		if value == "" {
			return fmt.Errorf("required variable %q not provided (description: %s)", name, variable.Description)
		}
	}
	return nil
}

// ContinueOnStop returns true because we want to report final execution status
func (w *Webhook) ContinueOnStop() bool {
	return true
}

// Key returns the per-instance dedup key used by core.middlewareContainer
// so multiple Webhook instances with distinct Config.Name can coexist in a
// single job's middleware chain. Pre-#672, the container deduped by
// reflect.TypeOf(m).String() and silently dropped any second *Webhook;
// PR #671 worked around this with the WebhookMiddleware composite. With
// the Key() opt-in, two distinct *Webhook instances (e.g. "wh-success"
// and "wh-error") survive insertion into the chain by themselves.
//
// Returning Config.Name (instead of, say, a UUID) preserves the existing
// "first wins" dedup contract for the scheduler-to-job propagation path
// at core/scheduler.go:315 / config_webhook.go:289: if the scheduler-level
// webhook with the same Config.Name is re-propagated to a job that
// already has it, the second insertion is correctly skipped.
//
// See https://github.com/netresearch/ofelia/issues/672.
func (w *Webhook) Key() string {
	return w.Config.Name
}

// Run executes the webhook notification
func (w *Webhook) Run(ctx *core.Context) error {
	err := ctx.Next()
	ctx.Stop(err)

	// Check if we should notify based on trigger configuration
	if !w.Config.ShouldNotify(ctx.Execution.Failed, ctx.Execution.Skipped) {
		return err
	}

	// Check deduplication - suppress duplicate error notifications
	if w.Config.Dedup != nil && ctx.Execution.Failed && !w.Config.Dedup.ShouldNotify(ctx) {
		ctx.Logger.Debug(fmt.Sprintf("Webhook %q notification suppressed (duplicate within cooldown)", w.Config.Name))
		return err
	}

	// Send webhook with retry logic
	if webhookErr := w.sendWithRetry(ctx); webhookErr != nil {
		ctx.Logger.Error("Webhook error", "webhook", w.Config.Name, "error", webhookErr)
	}

	return err
}

// sendWithRetry sends the webhook with configurable retry logic.
//
// The inter-attempt backoff observes ctx cancellation so that scheduler
// shutdown or per-job deadline drains promptly even when a wedged endpoint
// has pushed the retry budget to its limit. Without this, SIGTERM on a
// daemon mid-retry blocked for up to RetryDelay × RetryCount waiting for
// time.Sleep to return — keeping a goroutine pinned past Scheduler.Stop.
// See https://github.com/netresearch/ofelia/issues/673.
func (w *Webhook) sendWithRetry(ctx *core.Context) error {
	var lastErr error

	for attempt := 0; attempt <= w.Config.RetryCount; attempt++ {
		if attempt > 0 {
			ctx.Logger.Debug(fmt.Sprintf("Webhook %q: retry attempt %d/%d after %v",
				w.Config.Name, attempt, w.Config.RetryCount, w.Config.RetryDelay))
			runCtx := ctx.RunContext()
			select {
			case <-time.After(w.Config.RetryDelay):
			case <-runCtx.Done():
				return fmt.Errorf("webhook %q retry interrupted: %w", w.Config.Name, runCtx.Err())
			}
		}

		if err := w.send(ctx); err != nil {
			lastErr = err
			ctx.Logger.Debug(fmt.Sprintf("Webhook %q: attempt %d failed: %v", w.Config.Name, attempt+1, err))
			continue
		}

		// Success
		return nil
	}

	return fmt.Errorf("all %d attempts failed, last error: %w", w.Config.RetryCount+1, lastErr)
}

// send performs the actual HTTP request
func (w *Webhook) send(ctx *core.Context) error {
	// Build webhook data with preset config for templates that need ID/Secret
	data := w.buildWebhookDataWithPreset(ctx)

	// Build URL
	targetURL, err := w.Preset.BuildURL(w.Config)
	if err != nil {
		return fmt.Errorf("build URL: %w", err)
	}

	// Validate URL for SSRF protection
	if err := ValidateWebhookURL(targetURL); err != nil {
		return fmt.Errorf("URL validation: %w", err)
	}

	// Render body template with preset data
	body, err := w.Preset.RenderBodyWithPreset(data)
	if err != nil {
		return fmt.Errorf("render body: %w", err)
	}

	// Create request with a context that observes BOTH the per-request
	// timeout AND scheduler / job cancellation. Deriving from
	// ctx.RunContext() (not context.Background()) means an in-flight HTTP
	// write is also drained on SIGTERM / job-level deadline expiry — not
	// just the inter-attempt backoff in sendWithRetry. Without this,
	// shutdown still blocked for up to w.Config.Timeout per in-flight
	// request. See https://github.com/netresearch/ofelia/issues/673.
	reqCtx, cancel := context.WithTimeout(ctx.RunContext(), w.Config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, w.Preset.Method, targetURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Set headers from preset
	for key, value := range w.Preset.Headers {
		// Substitute variables in header values
		value = w.substituteVariables(value)
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := w.Client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error reporting
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	ctx.Logger.Debug(fmt.Sprintf("Webhook %q sent successfully to %s", w.Config.Name, targetURL))
	return nil
}

// substituteVariables replaces variable placeholders in a string
func (w *Webhook) substituteVariables(s string) string {
	s = strings.ReplaceAll(s, "{id}", w.Config.ID)
	s = strings.ReplaceAll(s, "{secret}", w.Config.Secret)
	s = strings.ReplaceAll(s, "{url}", w.Config.URL)

	for k, v := range w.Config.CustomVars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}

	return s
}

// buildWebhookData constructs the data structure for template rendering
func (w *Webhook) buildWebhookData(ctx *core.Context) *WebhookData {
	hostname, _ := os.Hostname()

	data := &WebhookData{
		Job: WebhookJobData{
			Name:     ctx.Job.GetName(),
			Command:  ctx.Job.GetCommand(),
			Schedule: ctx.Job.GetSchedule(),
			Type:     getJobType(ctx.Job),
		},
		Execution: WebhookExecutionData{
			ID:        ctx.Execution.ID,
			Status:    getExecutionStatus(ctx.Execution),
			Failed:    ctx.Execution.Failed,
			Skipped:   ctx.Execution.Skipped,
			Duration:  ctx.Execution.Duration,
			StartTime: ctx.Execution.Date,
			EndTime:   ctx.Execution.Date.Add(ctx.Execution.Duration),
		},
		Host: WebhookHostData{
			Hostname:  hostname,
			Timestamp: time.Now(),
		},
		Ofelia: WebhookOfeliaData{
			Version: Version,
		},
	}

	// Set error message if present
	if ctx.Execution.Error != nil {
		data.Execution.Error = ctx.Execution.Error.Error()
	}

	// Set output streams
	data.Execution.Output = ctx.Execution.GetStdout()
	data.Execution.Stderr = ctx.Execution.GetStderr()

	return data
}

// getJobType returns the job type string
func getJobType(job core.Job) string {
	switch job.(type) {
	case *core.ExecJob:
		return "exec"
	case *core.RunJob:
		return "run"
	case *core.LocalJob:
		return "local"
	case *core.RunServiceJob:
		return "run-service"
	default:
		return "unknown"
	}
}

// getExecutionStatus returns a human-readable status string
func getExecutionStatus(e *core.Execution) string {
	switch {
	case e.Failed:
		return statusFailed
	case e.Skipped:
		return statusSkipped
	default:
		return statusSuccessful
	}
}

// WebhookManager manages multiple webhook configurations
type WebhookManager struct {
	webhooks     map[string]*WebhookConfig
	presetLoader *PresetLoader
	globalConfig *WebhookGlobalConfig
}

// NewWebhookManager creates a new webhook manager
func NewWebhookManager(globalConfig *WebhookGlobalConfig) *WebhookManager {
	if globalConfig == nil {
		globalConfig = DefaultWebhookGlobalConfig()
	}

	// Configure global security settings based on the webhook global config
	// This affects URL validation and DNS rebinding protection
	securityConfig := SecurityConfigFromGlobal(globalConfig)
	SetGlobalSecurityConfig(securityConfig)

	return &WebhookManager{
		webhooks:     make(map[string]*WebhookConfig),
		presetLoader: NewPresetLoader(globalConfig),
		globalConfig: globalConfig,
	}
}

// Register adds a webhook configuration
func (m *WebhookManager) Register(config *WebhookConfig) error {
	if config.Name == "" {
		return fmt.Errorf("webhook name cannot be empty")
	}
	m.webhooks[config.Name] = config
	return nil
}

// Get returns a webhook configuration by name
func (m *WebhookManager) Get(name string) (*WebhookConfig, bool) {
	config, ok := m.webhooks[name]
	return config, ok
}

// GetMiddlewares returns middlewares for the specified webhook names
func (m *WebhookManager) GetMiddlewares(names []string) ([]core.Middleware, error) {
	var middlewares []core.Middleware

	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		config, ok := m.webhooks[name]
		if !ok {
			return nil, fmt.Errorf("webhook %q not found", name)
		}

		middleware, err := NewWebhook(config, m.presetLoader)
		if err != nil {
			return nil, fmt.Errorf("create webhook %q: %w", name, err)
		}

		if middleware != nil {
			middlewares = append(middlewares, middleware)
		}
	}

	return middlewares, nil
}

// GetGlobalMiddlewares returns middlewares for globally configured webhooks
func (m *WebhookManager) GetGlobalMiddlewares() ([]core.Middleware, error) {
	return m.GetMiddlewares(m.GlobalWebhookNames())
}

// GlobalWebhookNames returns the parsed names of globally configured webhooks
// (the `[global] webhook-webhooks = ...` selector). Returns nil when no globals
// are configured.
//
// Used by the per-job attach path so each job can union global + per-job
// webhook names into a single composite — propagating globals via the
// scheduler's middleware chain doesn't work because core.middlewareContainer
// would dedup the scheduler's *WebhookMiddleware against the job's own. See
// https://github.com/netresearch/ofelia/issues/670.
func (m *WebhookManager) GlobalWebhookNames() []string {
	return ParseWebhookNames(m.globalConfig.Webhooks)
}

// ParseWebhookNames parses a comma-separated list of webhook names
func ParseWebhookNames(s string) []string {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			names = append(names, p)
		}
	}
	return names
}

// WebhookMiddleware is a composite middleware that dispatches to multiple webhooks.
//
// A composite wrapper is required because core.middlewareContainer.Use()
// deduplicates middlewares by their reflect type — adding two *Webhook
// instances directly would silently drop the second one. See #670.
type WebhookMiddleware struct {
	webhooks []core.Middleware
}

// NewWebhookMiddleware creates a composite middleware from multiple webhook
// middlewares. Returns nil for an empty slice and the single webhook directly
// when there is only one — the composite is only needed to bypass the
// core.middlewareContainer.Use() type dedup that strikes when a second
// *Webhook joins the same chain.
func NewWebhookMiddleware(webhooks []core.Middleware) core.Middleware {
	switch len(webhooks) {
	case 0:
		return nil
	case 1:
		return webhooks[0]
	default:
		return &WebhookMiddleware{webhooks: webhooks}
	}
}

// Webhooks returns a shallow copy of the inner webhook middlewares.
//
// Exposed for tests that need to verify multi-webhook attachment without
// reaching into unexported fields. The returned slice header is copied so
// callers cannot append, reorder, or replace the composite's stored list —
// but each element aliases the composite's stored *Webhook, so callers must
// not mutate Webhook.Config (URL/Secret/Trigger) on the returned entries.
// In practice send() re-validates URL on every dispatch, so this is
// documentation-grade rather than security-grade.
func (w *WebhookMiddleware) Webhooks() []core.Middleware {
	out := make([]core.Middleware, len(w.webhooks))
	copy(out, w.webhooks)
	return out
}

// ContinueOnStop returns true because we want to report final status
func (w *WebhookMiddleware) ContinueOnStop() bool {
	return true
}

// Run dispatches to each inner webhook in order, after the chain below the
// composite (typically just the job itself) has executed.
//
// Re-entrancy invariant: ctx.Next() runs the rest of the chain (and the job)
// then we call ctx.Stop(err) and loop calling each inner webhook.Run(ctx).
// Each inner Webhook.Run also calls ctx.Next() + ctx.Stop. This is safe NOT
// because the composite happens to be the last middleware, but because
// core.Context.doNext() short-circuits on !Execution.IsRunning before
// re-running the job (core/common.go:136), and ctx.Stop is idempotent under
// the same flag (core/common.go:157). The IsRunning gate — not composite
// position — enforces single-job-execution, so the invariant survives any
// future middleware appended after the composite. Be aware, however, that
// any post-composite middleware whose ContinueOnStop()==true will run N
// times (once per inner webhook) on the same Context.
//
// Errors from individual webhook.Run calls are intentionally discarded —
// Webhook.Run returns the underlying *job* error (not a notification error)
// and logs its own delivery failures internally via ctx.Logger.Error. The
// outer return value preserves the job's error so callers up the middleware
// chain still see job failures.
func (w *WebhookMiddleware) Run(ctx *core.Context) error {
	err := ctx.Next()
	ctx.Stop(err)

	for _, webhook := range w.webhooks {
		// Intentional discard — see Run godoc above for rationale.
		_ = webhook.Run(ctx)
	}

	return err
}

// ValidateWebhookURL is defined in webhook_security.go with thread-safe access

// PresetDataForTemplate provides preset config to templates that need it
type PresetDataForTemplate struct {
	ID       string
	Secret   string
	URL      string
	Link     string
	LinkText string
}

// buildWebhookDataWithPreset adds preset data to webhook data for templates that reference it
func (w *Webhook) buildWebhookDataWithPreset(ctx *core.Context) map[string]any {
	data := w.buildWebhookData(ctx)

	// Default link text if link is provided but text is not
	linkText := w.Config.LinkText
	if w.Config.Link != "" && linkText == "" {
		linkText = "View Details"
	}

	return map[string]any{
		notificationVarJob:       data.Job,
		notificationVarExecution: data.Execution,
		"Host":                   data.Host,
		"Ofelia":                 data.Ofelia,
		"Preset": PresetDataForTemplate{
			ID:       w.Config.ID,
			Secret:   w.Config.Secret,
			URL:      w.Config.URL,
			Link:     w.Config.Link,
			LinkText: linkText,
		},
	}
}

// RenderBodyWithPreset renders the body template with both webhook data and preset config
func (p *Preset) RenderBodyWithPreset(data map[string]any) (string, error) {
	if p.Body == "" {
		return "", nil
	}

	tmpl, err := template.New("body").Funcs(webhookTemplateFuncs).Parse(p.Body)
	if err != nil {
		return "", fmt.Errorf("parse body template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute body template: %w", err)
	}

	return buf.String(), nil
}
