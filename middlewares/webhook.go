package middlewares

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		ctx.Logger.Debugf("Webhook %q notification suppressed (duplicate within cooldown)", w.Config.Name)
		return err
	}

	// Send webhook with retry logic
	if webhookErr := w.sendWithRetry(ctx); webhookErr != nil {
		ctx.Logger.Errorf("Webhook %q: %v", w.Config.Name, webhookErr)
	}

	return err
}

// sendWithRetry sends the webhook with configurable retry logic
func (w *Webhook) sendWithRetry(ctx *core.Context) error {
	var lastErr error

	for attempt := 0; attempt <= w.Config.RetryCount; attempt++ {
		if attempt > 0 {
			ctx.Logger.Debugf("Webhook %q: retry attempt %d/%d after %v",
				w.Config.Name, attempt, w.Config.RetryCount, w.Config.RetryDelay)
			time.Sleep(w.Config.RetryDelay)
		}

		if err := w.send(ctx); err != nil {
			lastErr = err
			ctx.Logger.Debugf("Webhook %q: attempt %d failed: %v", w.Config.Name, attempt+1, err)
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

	// Create request with context
	reqCtx, cancel := context.WithTimeout(context.Background(), w.Config.Timeout)
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

	ctx.Logger.Debugf("Webhook %q sent successfully to %s", w.Config.Name, targetURL)
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
		return "failed"
	case e.Skipped:
		return "skipped"
	default:
		return "successful"
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
	if m.globalConfig.Webhooks == "" {
		return nil, nil
	}

	names := strings.Split(m.globalConfig.Webhooks, ",")
	return m.GetMiddlewares(names)
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

// WebhookMiddleware is a composite middleware that dispatches to multiple webhooks
type WebhookMiddleware struct {
	webhooks []core.Middleware
}

// NewWebhookMiddleware creates a composite middleware from multiple webhook middlewares
func NewWebhookMiddleware(webhooks []core.Middleware) core.Middleware {
	if len(webhooks) == 0 {
		return nil
	}
	return &WebhookMiddleware{webhooks: webhooks}
}

// ContinueOnStop returns true because we want to report final status
func (w *WebhookMiddleware) ContinueOnStop() bool {
	return true
}

// Run executes all webhook middlewares
func (w *WebhookMiddleware) Run(ctx *core.Context) error {
	err := ctx.Next()
	ctx.Stop(err)

	// Execute all webhooks (they handle their own conditions)
	for _, webhook := range w.webhooks {
		// Create a wrapper context for the webhook
		// The webhook will check conditions and send if appropriate
		_ = webhook.Run(ctx)
	}

	return err
}

// ValidateWebhookURL validates a URL is safe for webhook requests (SSRF protection)
// This is a forward declaration - implementation in webhook_security.go
var ValidateWebhookURL func(rawURL string) error

func init() {
	// Default implementation if security module not loaded
	if ValidateWebhookURL == nil {
		ValidateWebhookURL = func(rawURL string) error {
			u, err := url.Parse(rawURL)
			if err != nil {
				return fmt.Errorf("invalid URL: %w", err)
			}
			if u.Scheme != "http" && u.Scheme != "https" {
				return fmt.Errorf("URL scheme must be http or https")
			}
			if u.Host == "" {
				return fmt.Errorf("URL must have a host")
			}
			return nil
		}
	}
}

// PresetDataForTemplate provides preset config to templates that need it
type PresetDataForTemplate struct {
	ID       string
	Secret   string
	URL      string
	Link     string
	LinkText string
}

// buildWebhookDataWithPreset adds preset data to webhook data for templates that reference it
func (w *Webhook) buildWebhookDataWithPreset(ctx *core.Context) map[string]interface{} {
	data := w.buildWebhookData(ctx)

	// Default link text if link is provided but text is not
	linkText := w.Config.LinkText
	if w.Config.Link != "" && linkText == "" {
		linkText = "View Details"
	}

	return map[string]interface{}{
		"Job":       data.Job,
		"Execution": data.Execution,
		"Host":      data.Host,
		"Ofelia":    data.Ofelia,
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
func (p *Preset) RenderBodyWithPreset(data map[string]interface{}) (string, error) {
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
