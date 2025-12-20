package middlewares

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

func TestNewWebhook_WithConfig(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
		ID:     "T12345/B67890",
		Secret: "xoxb-test-secret",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	require.NoError(t, err)
	assert.NotNil(t, middleware)
}

func TestNewWebhook_NilConfig(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	middleware, err := NewWebhook(nil, loader)

	require.NoError(t, err)
	assert.Nil(t, middleware)
}

func TestNewWebhook_InvalidPreset(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:   "test",
		Preset: "nonexistent-preset",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	require.Error(t, err)
	assert.Nil(t, middleware)
}

func TestWebhook_ContinueOnStop(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
		ID:     "T12345/B67890",
		Secret: "xoxb-test-secret",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	require.NoError(t, err)

	webhook, ok := middleware.(*Webhook)
	assert.True(t, ok)
	assert.True(t, webhook.ContinueOnStop())
}

func TestWebhookManager_Creation(t *testing.T) {
	t.Parallel()

	globalConfig := DefaultWebhookGlobalConfig()
	manager := NewWebhookManager(globalConfig)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.webhooks)
}

func TestWebhookManager_Register(t *testing.T) {
	t.Parallel()

	manager := NewWebhookManager(DefaultWebhookGlobalConfig())

	config := &WebhookConfig{
		Name:   "test-webhook",
		Preset: "slack",
	}

	err := manager.Register(config)
	require.NoError(t, err)
}

func TestWebhookManager_RegisterEmptyName(t *testing.T) {
	t.Parallel()

	manager := NewWebhookManager(DefaultWebhookGlobalConfig())

	config := &WebhookConfig{
		Name:   "",
		Preset: "slack",
	}

	err := manager.Register(config)
	assert.Error(t, err)
}

func TestWebhookManager_Get(t *testing.T) {
	t.Parallel()

	manager := NewWebhookManager(DefaultWebhookGlobalConfig())

	config := &WebhookConfig{
		Name:   "slack-alerts",
		Preset: "slack",
	}

	err := manager.Register(config)
	require.NoError(t, err)

	webhook, ok := manager.Get("slack-alerts")
	assert.True(t, ok)
	assert.NotNil(t, webhook)
}

func TestWebhookManager_GetNonExistent(t *testing.T) {
	t.Parallel()

	manager := NewWebhookManager(DefaultWebhookGlobalConfig())

	webhook, ok := manager.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, webhook)
}

func TestWebhookConfig_ShouldNotify(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{Trigger: TriggerError}
	assert.True(t, config.ShouldNotify(true, false))
	assert.False(t, config.ShouldNotify(false, false))

	config = &WebhookConfig{Trigger: TriggerSuccess}
	assert.True(t, config.ShouldNotify(false, false))
	assert.False(t, config.ShouldNotify(true, false))

	config = &WebhookConfig{Trigger: TriggerAlways}
	assert.True(t, config.ShouldNotify(true, false))
	assert.True(t, config.ShouldNotify(false, false))
}

func TestWebhook_SendsRequest(t *testing.T) {
	// Note: Not parallel - modifies global security config
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

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
	require.NoError(t, err)

	webhook := middleware.(*Webhook)

	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)

	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	err = webhook.send(ctx)
	require.NoError(t, err)

	assert.NotEmpty(t, receivedBody)
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
}

func TestWebhook_Retry(t *testing.T) {
	// Note: Not parallel - modifies global security config
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

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
	require.NoError(t, err)

	webhook := middleware.(*Webhook)

	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)

	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	err = webhook.sendWithRetry(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestWebhook_TriggerError_OnlyOnFailure(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		URL:        "https://example.com",
		Trigger:    TriggerError,
		Timeout:    5 * time.Second,
		RetryCount: 0,
	}

	assert.False(t, config.ShouldNotify(false, false))
	assert.True(t, config.ShouldNotify(true, false))
}

func TestWebhook_TriggerSuccess_OnlyOnSuccess(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		URL:        "https://example.com/webhook",
		Trigger:    TriggerSuccess,
		Timeout:    5 * time.Second,
		RetryCount: 0,
	}

	assert.True(t, config.ShouldNotify(false, false))
	assert.False(t, config.ShouldNotify(true, false))
}

func TestWebhook_TriggerAlways(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:       "test",
		Preset:     "slack",
		URL:        "https://example.com/webhook",
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 0,
	}

	assert.True(t, config.ShouldNotify(false, false))
	assert.True(t, config.ShouldNotify(true, false))
}

func TestWebhook_BuildWebhookData_Success(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
		ID:     "T12345/B67890",
		Secret: "xoxb-test-secret",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	require.NoError(t, err)

	webhook := middleware.(*Webhook)

	job := &TestJob{}
	job.Name = "test-job"
	job.Command = "echo hello"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)

	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	data := webhook.buildWebhookData(ctx)

	assert.Equal(t, "test-job", data.Job.Name)
	assert.False(t, data.Execution.Failed)
	assert.Equal(t, "successful", data.Execution.Status)
}

func TestWebhook_BuildWebhookData_Error(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
		ID:     "T12345/B67890",
		Secret: "xoxb-test-secret",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	require.NoError(t, err)

	webhook := middleware.(*Webhook)

	job := &TestJob{}
	job.Name = "failing-job"
	job.Command = "exit 1"

	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)

	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(errors.New("command failed"))

	data := webhook.buildWebhookData(ctx)

	assert.Equal(t, "failing-job", data.Job.Name)
	assert.True(t, data.Execution.Failed)
	assert.Equal(t, "failed", data.Execution.Status)
}
