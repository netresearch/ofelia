package middlewares

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/netresearch/ofelia/core"
)

type SuiteWebhook struct {
	BaseSuite
}

var _ = Suite(&SuiteWebhook{})

func (s *SuiteWebhook) TestNewWebhook_WithConfig(c *C) {
	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
		ID:     "T12345/B67890",
		Secret: "xoxb-test-secret",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	c.Assert(err, IsNil)
	c.Assert(middleware, NotNil)
}

func (s *SuiteWebhook) TestNewWebhook_NilConfig(c *C) {
	loader := NewPresetLoader(nil)
	middleware, err := NewWebhook(nil, loader)

	// NewWebhook returns (nil, nil) for nil config - this is valid behavior
	c.Assert(err, IsNil)
	c.Assert(middleware, IsNil)
}

func (s *SuiteWebhook) TestNewWebhook_InvalidPreset(c *C) {
	config := &WebhookConfig{
		Name:   "test",
		Preset: "nonexistent-preset",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	c.Assert(err, NotNil)
	c.Assert(middleware, IsNil)
}

func (s *SuiteWebhook) TestWebhook_ContinueOnStop(c *C) {
	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
		ID:     "T12345/B67890",
		Secret: "xoxb-test-secret",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	c.Assert(err, IsNil)

	webhook, ok := middleware.(*Webhook)
	c.Assert(ok, Equals, true)
	c.Assert(webhook.ContinueOnStop(), Equals, true)
}

func (s *SuiteWebhook) TestWebhookManager_Creation(c *C) {
	globalConfig := DefaultWebhookGlobalConfig()
	manager := NewWebhookManager(globalConfig)

	c.Assert(manager, NotNil)
	c.Assert(manager.webhooks, NotNil)
}

func (s *SuiteWebhook) TestWebhookManager_Register(c *C) {
	manager := NewWebhookManager(DefaultWebhookGlobalConfig())

	config := &WebhookConfig{
		Name:   "test-webhook",
		Preset: "slack",
	}

	err := manager.Register(config)
	c.Assert(err, IsNil)
}

func (s *SuiteWebhook) TestWebhookManager_RegisterEmptyName(c *C) {
	manager := NewWebhookManager(DefaultWebhookGlobalConfig())

	// Register validates that name is not empty
	config := &WebhookConfig{
		Name:   "",
		Preset: "slack",
	}

	err := manager.Register(config)
	c.Assert(err, NotNil)
}

func (s *SuiteWebhook) TestWebhookManager_Get(c *C) {
	manager := NewWebhookManager(DefaultWebhookGlobalConfig())

	config := &WebhookConfig{
		Name:   "slack-alerts",
		Preset: "slack",
	}

	err := manager.Register(config)
	c.Assert(err, IsNil)

	webhook, ok := manager.Get("slack-alerts")
	c.Assert(ok, Equals, true)
	c.Assert(webhook, NotNil)
}

func (s *SuiteWebhook) TestWebhookManager_GetNonExistent(c *C) {
	manager := NewWebhookManager(DefaultWebhookGlobalConfig())

	webhook, ok := manager.Get("nonexistent")
	c.Assert(ok, Equals, false)
	c.Assert(webhook, IsNil)
}

func (s *SuiteWebhook) TestWebhookConfig_ShouldNotify(c *C) {
	// Test error trigger
	config := &WebhookConfig{Trigger: TriggerError}
	c.Assert(config.ShouldNotify(true, false), Equals, true)
	c.Assert(config.ShouldNotify(false, false), Equals, false)

	// Test success trigger
	config = &WebhookConfig{Trigger: TriggerSuccess}
	c.Assert(config.ShouldNotify(false, false), Equals, true)
	c.Assert(config.ShouldNotify(true, false), Equals, false)

	// Test always trigger
	config = &WebhookConfig{Trigger: TriggerAlways}
	c.Assert(config.ShouldNotify(true, false), Equals, true)
	c.Assert(config.ShouldNotify(false, false), Equals, true)
}

// Standard Go testing for HTTP integration tests

func TestWebhook_SendsRequest(t *testing.T) {
	// Temporarily disable SSRF validation for testing with localhost
	originalValidator := ValidateWebhookURL
	ValidateWebhookURL = func(rawURL string) error { return nil }
	defer func() { ValidateWebhookURL = originalValidator }()

	// Temporarily use default transport (bypass DNS rebinding protection)
	originalTransport := TransportFactory
	TransportFactory = func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() }
	defer func() { TransportFactory = originalTransport }()

	// Create a test server that records requests
	var receivedBody string
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create webhook with test server URL
	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 0,
	}

	loader := NewPresetLoader(nil)
	middleware, err := NewWebhook(config, loader)
	if err != nil {
		t.Fatalf("Failed to create webhook: %v", err)
	}

	webhook := middleware.(*Webhook)

	// Create test context
	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	// Send the webhook
	err = webhook.send(ctx)
	if err != nil {
		t.Errorf("Failed to send webhook: %v", err)
	}

	// Verify request was received
	if receivedBody == "" {
		t.Error("No body received by server")
	}

	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Unexpected Content-Type: %s", receivedHeaders.Get("Content-Type"))
	}
}

func TestWebhook_Retry(t *testing.T) {
	// Temporarily disable SSRF validation for testing with localhost
	originalValidator := ValidateWebhookURL
	ValidateWebhookURL = func(rawURL string) error { return nil }
	defer func() { ValidateWebhookURL = originalValidator }()

	// Temporarily use default transport (bypass DNS rebinding protection)
	originalTransport := TransportFactory
	TransportFactory = func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() }
	defer func() { TransportFactory = originalTransport }()

	// Create a server that fails first few requests
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 3,
		RetryDelay: 10 * time.Millisecond,
	}

	loader := NewPresetLoader(nil)
	middleware, err := NewWebhook(config, loader)
	if err != nil {
		t.Fatalf("Failed to create webhook: %v", err)
	}

	webhook := middleware.(*Webhook)

	// Create test context
	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	// Send with retry
	err = webhook.sendWithRetry(ctx)
	if err != nil {
		t.Errorf("Failed to send webhook after retries: %v", err)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestWebhook_TriggerError_OnlyOnFailure(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		URL:        server.URL,
		Trigger:    TriggerError,
		Timeout:    5 * time.Second,
		RetryCount: 0,
	}

	// Test using ShouldNotify method
	// Success = should NOT notify
	if config.ShouldNotify(false, false) {
		t.Error("Should not notify on success with error trigger")
	}

	// Error = should notify
	if !config.ShouldNotify(true, false) {
		t.Error("Should notify on error with error trigger")
	}
}

func TestWebhook_TriggerSuccess_OnlyOnSuccess(t *testing.T) {
	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		URL:        "https://example.com/webhook",
		Trigger:    TriggerSuccess,
		Timeout:    5 * time.Second,
		RetryCount: 0,
	}

	// Test with success
	if !config.ShouldNotify(false, false) {
		t.Error("Should notify on success with success trigger")
	}

	// Test with error
	if config.ShouldNotify(true, false) {
		t.Error("Should not notify on error with success trigger")
	}
}

func TestWebhook_TriggerAlways(t *testing.T) {
	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		URL:        "https://example.com/webhook",
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 0,
	}

	// Test with success
	if !config.ShouldNotify(false, false) {
		t.Error("Should notify on success with always trigger")
	}

	// Test with error
	if !config.ShouldNotify(true, false) {
		t.Error("Should notify on error with always trigger")
	}
}

func TestWebhook_BuildWebhookData_Success(t *testing.T) {
	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
		ID:     "T12345/B67890",
		Secret: "xoxb-test-secret",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	if err != nil {
		t.Fatalf("Failed to create webhook: %v", err)
	}

	webhook := middleware.(*Webhook)

	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	data := webhook.buildWebhookData(ctx)

	if data.Job.Name != "test-job" {
		t.Errorf("Expected job name 'test-job', got %s", data.Job.Name)
	}

	if data.Execution.Failed {
		t.Error("Expected execution not failed")
	}

	if data.Execution.Status != "successful" {
		t.Errorf("Expected status 'successful', got %s", data.Execution.Status)
	}
}

func TestWebhook_BuildWebhookData_Error(t *testing.T) {
	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
		ID:     "T12345/B67890",
		Secret: "xoxb-test-secret",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	if err != nil {
		t.Fatalf("Failed to create webhook: %v", err)
	}

	webhook := middleware.(*Webhook)

	job := &TestJob{}
	job.Name = "failing-job"
	job.Command = "exit 1"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(errors.New("command failed"))

	data := webhook.buildWebhookData(ctx)

	if data.Job.Name != "failing-job" {
		t.Errorf("Expected job name 'failing-job', got %s", data.Job.Name)
	}

	if !data.Execution.Failed {
		t.Error("Expected execution to be failed")
	}

	if data.Execution.Status != "failed" {
		t.Errorf("Expected status 'failed', got %s", data.Execution.Status)
	}
}
