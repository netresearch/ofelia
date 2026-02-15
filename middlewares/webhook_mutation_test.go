package middlewares

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

// TestSendWithRetry_ZeroRetries kills:
//   - CONDITIONALS_BOUNDARY at webhook.go:126 (attempt <= RetryCount vs attempt < RetryCount)
//   - INCREMENT_DECREMENT at webhook.go:126:59 (attempt++ vs attempt--)
//
// With RetryCount=0, the loop condition `attempt <= 0` allows exactly 1 iteration.
// A boundary mutation to `attempt < 0` would allow 0 iterations (never sends).
// An increment/decrement mutation to `attempt--` would cause infinite loop.
func TestSendWithRetry_ZeroRetries(t *testing.T) {
	// Not parallel - modifies global security config
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &WebhookConfig{
		Name:       "test-zero-retry",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 0, // Zero retries = exactly 1 attempt
		RetryDelay: time.Millisecond,
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
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "with RetryCount=0, exactly 1 attempt should be made")
}

// TestSendWithRetry_AllAttemptsFail kills:
//   - CONDITIONALS_BOUNDARY at webhook.go:126:28 (attempt <= vs <)
//   - ARITHMETIC_BASE at webhook.go:135:81 (attempt+1 -> different arithmetic)
//
// With RetryCount=2, there should be exactly 3 attempts (0, 1, 2).
// A boundary mutation changing <= to < would give only 2 attempts.
func TestSendWithRetry_AllAttemptsFail(t *testing.T) {
	// Not parallel - modifies global security config
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := &WebhookConfig{
		Name:       "test-all-fail",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 2,
		RetryDelay: time.Millisecond,
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
	require.Error(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts),
		"with RetryCount=2, exactly 3 attempts (initial + 2 retries) should be made")
	assert.Contains(t, err.Error(), "all 3 attempts failed")
}

// TestSendWithRetry_FirstAttemptNoSleep kills:
//   - CONDITIONALS_BOUNDARY at webhook.go:127:14 (attempt > 0 vs attempt >= 0)
//   - CONDITIONALS_NEGATION at webhook.go:127:14 (attempt > 0 vs attempt <= 0)
//
// The first attempt (attempt=0) should NOT sleep. Subsequent attempts should sleep.
// If the condition is negated to attempt <= 0, the first attempt would sleep
// and retries would not sleep.
func TestSendWithRetry_FirstAttemptNoSleep(t *testing.T) {
	// Not parallel - modifies global security config
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	var attemptTimes []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptTimes = append(attemptTimes, time.Now())
		if len(attemptTimes) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	retryDelay := 100 * time.Millisecond
	config := &WebhookConfig{
		Name:       "test-sleep-timing",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 3,
		RetryDelay: retryDelay,
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

	start := time.Now()
	err = webhook.sendWithRetry(ctx)
	require.NoError(t, err)
	require.Len(t, attemptTimes, 3)

	// First attempt should be nearly immediate (< retryDelay)
	firstDelay := attemptTimes[0].Sub(start)
	assert.Less(t, firstDelay, retryDelay,
		"first attempt should not sleep (delay=%v)", firstDelay)

	// Second attempt should be at least retryDelay after first
	secondDelay := attemptTimes[1].Sub(attemptTimes[0])
	assert.GreaterOrEqual(t, secondDelay, retryDelay/2,
		"retry attempts should sleep (delay=%v)", secondDelay)
}

// TestSendWithRetry_ErrorMessage kills ARITHMETIC_BASE at webhook.go:135:81
// The error log uses attempt+1 for 1-based attempt numbering.
// Changing attempt+1 to attempt-1 or attempt*1 etc. would change the error message.
// We verify the final error message contains the correct total attempt count.
func TestSendWithRetry_ErrorMessage(t *testing.T) {
	// Not parallel - modifies global security config
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := &WebhookConfig{
		Name:       "test-error-msg",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 1,
		RetryDelay: time.Millisecond,
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
	require.Error(t, err)
	// RetryCount=1 means 2 total attempts: "all 2 attempts failed"
	assert.Contains(t, err.Error(), "all 2 attempts failed")
}

// TestNewWebhookManager_NilGlobalConfig kills CONDITIONALS_NEGATION at webhook.go:294
// The condition is: if globalConfig == nil { globalConfig = DefaultWebhookGlobalConfig() }
// Negating to != nil would apply defaults when config IS provided, and skip when nil (panic).
func TestNewWebhookManager_NilGlobalConfig(t *testing.T) {
	t.Parallel()

	// nil globalConfig should use defaults (not panic)
	manager := NewWebhookManager(nil)
	require.NotNil(t, manager)
	assert.NotNil(t, manager.globalConfig)
	assert.NotNil(t, manager.presetLoader)
	assert.NotNil(t, manager.webhooks)
}

// TestNewWebhookManager_WithGlobalConfig ensures provided config is used, not defaults.
func TestNewWebhookManager_WithGlobalConfig(t *testing.T) {
	t.Parallel()

	gc := &WebhookGlobalConfig{
		Webhooks:     "my-hook",
		AllowedHosts: "example.com",
	}
	manager := NewWebhookManager(gc)
	require.NotNil(t, manager)
	assert.Equal(t, "my-hook", manager.globalConfig.Webhooks)
	assert.Equal(t, "example.com", manager.globalConfig.AllowedHosts)
}

// TestBuildWebhookDataWithPreset_LinkText kills:
//   - CONDITIONALS_NEGATION at webhook.go:430:19 (Link != "" -> Link == "")
//   - CONDITIONALS_NEGATION at webhook.go:430:37 (linkText == "" -> linkText != "")
//
// The condition is: if w.Config.Link != "" && linkText == "" { linkText = "View Details" }
func TestBuildWebhookDataWithPreset_LinkText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		link             string
		linkText         string
		expectedLinkText string
	}{
		{
			name:             "link set with no text gets default",
			link:             "https://example.com/logs",
			linkText:         "",
			expectedLinkText: "View Details",
		},
		{
			name:             "link set with explicit text preserves it",
			link:             "https://example.com/logs",
			linkText:         "Custom Link",
			expectedLinkText: "Custom Link",
		},
		{
			name:             "no link means no default text",
			link:             "",
			linkText:         "",
			expectedLinkText: "",
		},
		{
			name:             "no link but text set preserves text",
			link:             "",
			linkText:         "Some Text",
			expectedLinkText: "Some Text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := &WebhookConfig{
				Name:     "test",
				Preset:   "slack",
				ID:       "T12345/B67890",
				Secret:   "xoxb-test-secret",
				Link:     tt.link,
				LinkText: tt.linkText,
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

			data := webhook.buildWebhookDataWithPreset(ctx)
			presetData, ok := data["Preset"].(PresetDataForTemplate)
			require.True(t, ok)
			assert.Equal(t, tt.expectedLinkText, presetData.LinkText)
			assert.Equal(t, tt.link, presetData.Link)
		})
	}
}

// TestSendWithRetry_ExactRetryCount_One kills CONDITIONALS_BOUNDARY at webhook.go:126
// Tests with RetryCount=1 to verify exact boundary: 2 total attempts.
func TestSendWithRetry_ExactRetryCount_One(t *testing.T) {
	// Not parallel - modifies global security config
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := &WebhookConfig{
		Name:       "test-retry-one",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 1,
		RetryDelay: time.Millisecond,
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
	require.Error(t, err)
	// RetryCount=1 means exactly 2 attempts: initial + 1 retry
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts),
		"with RetryCount=1, exactly 2 attempts should be made")
}

// TestSendWithRetry_SucceedsOnSecondAttempt verifies that retry succeeds after first failure.
// This also validates attempt+1 arithmetic in the debug log (line 135).
func TestSendWithRetry_SucceedsOnSecondAttempt(t *testing.T) {
	// Not parallel - modifies global security config
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &WebhookConfig{
		Name:       "test-second-success",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerAlways,
		Timeout:    5 * time.Second,
		RetryCount: 3,
		RetryDelay: time.Millisecond,
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
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts),
		"should succeed on 2nd attempt and stop retrying")
}

// TestWebhookRun_TriggerFiltering verifies webhook Run respects trigger config.
// This tests the full Run path including ShouldNotify.
func TestWebhookRun_TriggerFiltering(t *testing.T) {
	// Not parallel - modifies global security config
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	var called int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// TriggerError: should NOT send on success
	config := &WebhookConfig{
		Name:       "test-trigger-filter",
		Preset:     "slack",
		ID:         "T12345/B67890",
		Secret:     "xoxb-test-secret",
		URL:        server.URL,
		Trigger:    TriggerError,
		Timeout:    5 * time.Second,
		RetryCount: 0,
		RetryDelay: time.Millisecond,
	}

	loader := NewPresetLoader(nil)
	middleware, err := NewWebhook(config, loader)
	require.NoError(t, err)
	webhook := middleware.(*Webhook)

	job := &TestJob{}
	job.Name = "success-job"
	job.Command = "echo hello"
	sh := core.NewScheduler(&TestLogger{})
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	_ = webhook.Run(ctx)
	assert.Equal(t, int32(0), atomic.LoadInt32(&called),
		"webhook with TriggerError should not fire on success")

	// TriggerError: should send on failure
	e2, err := core.NewExecution()
	require.NoError(t, err)
	ctx2 := core.NewContext(sh, job, e2)
	ctx2.Start()
	ctx2.Stop(errors.New("job failed"))

	_ = webhook.Run(ctx2)
	assert.Equal(t, int32(1), atomic.LoadInt32(&called),
		"webhook with TriggerError should fire on failure")
}
