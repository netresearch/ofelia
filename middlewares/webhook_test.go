// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestWebhook_Key pins the contract used by core.middlewareContainer to
// dedup webhooks per Config.Name instead of by reflect type. See
// https://github.com/netresearch/ofelia/issues/672. Without this, two
// *Webhook instances with distinct names collapse into the first at
// j.Use() time and the second one silently never fires — the failure
// mode tracked in #670 / fixed at the webhook layer in PR #671.
func TestWebhook_Key(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:   "wh-success",
		Preset: "slack",
		ID:     "T12345/B67890",
		Secret: "xoxb-test-secret",
	}
	loader := NewPresetLoader(nil)

	middleware, err := NewWebhook(config, loader)
	require.NoError(t, err)

	webhook, ok := middleware.(*Webhook)
	require.True(t, ok)
	assert.Equal(t, "wh-success", webhook.Key(),
		"Webhook.Key() must return Config.Name so core.middlewareContainer can dedup per-instance instead of per-type")
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

	sh := core.NewScheduler(newDiscardLogger())
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

	sh := core.NewScheduler(newDiscardLogger())
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

	sh := core.NewScheduler(newDiscardLogger())
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

	sh := core.NewScheduler(newDiscardLogger())
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

func TestWebhookManager_GetMiddlewares_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		register    []string
		lookup      []string
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name:      "known webhook returns middleware",
			register:  []string{"alert"},
			lookup:    []string{"alert"},
			wantCount: 1,
		},
		{
			name:        "unknown webhook returns error",
			register:    []string{},
			lookup:      []string{"missing"},
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:      "empty name in lookup is skipped",
			register:  []string{"alert"},
			lookup:    []string{"", "alert", "  "},
			wantCount: 1,
		},
		{
			name:      "multiple known webhooks",
			register:  []string{"slack-alert", "discord-alert"},
			lookup:    []string{"slack-alert", "discord-alert"},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			manager := NewWebhookManager(DefaultWebhookGlobalConfig())

			for _, name := range tt.register {
				err := manager.Register(&WebhookConfig{
					Name:   name,
					Preset: "slack",
					ID:     "T00000/B00000",
					Secret: "xoxb-secret",
				})
				require.NoError(t, err)
			}

			mws, err := manager.GetMiddlewares(tt.lookup)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.Len(t, mws, tt.wantCount)
		})
	}
}

func TestWebhookManager_GetGlobalMiddlewares_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		webhooks  string
		register  []string
		wantCount int
		wantNil   bool
		wantErr   bool
	}{
		{name: "empty webhooks string returns nil", wantNil: true},
		{name: "with names resolves", webhooks: "a1,a2", register: []string{"a1", "a2"}, wantCount: 2},
		{name: "unknown name errors", webhooks: "nonexistent", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gc := DefaultWebhookGlobalConfig()
			gc.Webhooks = tt.webhooks
			manager := NewWebhookManager(gc)

			for _, name := range tt.register {
				_ = manager.Register(&WebhookConfig{Name: name, Preset: "slack", ID: "T0/B0", Secret: "s"})
			}

			mws, err := manager.GetGlobalMiddlewares()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, mws)
				return
			}
			assert.Len(t, mws, tt.wantCount)
		})
	}
}

func TestNewWebhookMiddleware_Variants(t *testing.T) {
	t.Parallel()

	assert.Nil(t, NewWebhookMiddleware(nil), "nil input should return nil")
	assert.Nil(t, NewWebhookMiddleware([]core.Middleware{}), "empty input should return nil")
	assert.NotNil(t, NewWebhookMiddleware([]core.Middleware{&stubMW{}}), "non-empty should return middleware")
}

// TestNewWebhookMiddleware_SingleWebhookFastPath pins the documented
// fast-path: when only one webhook is provided, the bare middleware is
// returned directly (no composite wrapper). The composite only exists to
// bypass core.middlewareContainer.Use() type dedup for multi-webhook jobs;
// a single-webhook job has no dedup conflict and should pay no indirection
// cost. Without this assertion a future refactor wrapping singletons in a
// composite would silently re-introduce the dedup hazard (the wrapped
// singleton's type collides with a global composite's type).
func TestNewWebhookMiddleware_SingleWebhookFastPath(t *testing.T) {
	t.Parallel()
	stub := &stubMW{}
	mw := NewWebhookMiddleware([]core.Middleware{stub})
	assert.Same(t, core.Middleware(stub), mw,
		"single-webhook input should return the bare middleware, not a composite")
	_, isComposite := mw.(*WebhookMiddleware)
	assert.False(t, isComposite, "single-webhook input must not be wrapped in *WebhookMiddleware")
}

// TestNewWebhookMiddleware_ThreeWebhooks_Boundary pins behavior with more
// than the minimal multi-webhook case. Two-webhook tests can mask off-by-one
// bugs (range bounds, copy/append handling in WebhookMiddleware.Webhooks(),
// switch boundaries in NewWebhookMiddleware).
func TestNewWebhookMiddleware_ThreeWebhooks_Boundary(t *testing.T) {
	t.Parallel()
	s1, s2, s3 := &stubMW{}, &stubMW{}, &stubMW{}
	mw := NewWebhookMiddleware([]core.Middleware{s1, s2, s3})
	composite, ok := mw.(*WebhookMiddleware)
	require.True(t, ok, "three-webhook input must produce *WebhookMiddleware composite")

	inner := composite.Webhooks()
	require.Len(t, inner, 3)
	assert.Same(t, core.Middleware(s1), inner[0], "order preserved [0]")
	assert.Same(t, core.Middleware(s2), inner[1], "order preserved [1]")
	assert.Same(t, core.Middleware(s3), inner[2], "order preserved [2]")
}

// TestWebhookMiddleware_Webhooks_ReturnsCopy verifies the defensive copy in
// (*WebhookMiddleware).Webhooks(). Without the copy, a test mutating the
// returned slice would corrupt the composite's stored webhook list.
func TestWebhookMiddleware_Webhooks_ReturnsCopy(t *testing.T) {
	t.Parallel()
	s1, s2 := &stubMW{}, &stubMW{}
	wm := &WebhookMiddleware{webhooks: []core.Middleware{s1, s2}}

	got := wm.Webhooks()
	got[0] = nil

	again := wm.Webhooks()
	assert.Same(t, core.Middleware(s1), again[0],
		"mutating returned slice must not affect composite's internal state")
	assert.Same(t, core.Middleware(s2), again[1])
}

// TestWebhookMiddleware_Run_PreservesOuterError pins that Run returns the
// error produced by ctx.Next() (the job's error), not nil. Inner webhook
// failures are intentionally discarded — each webhook handles its own
// retry/logging — but the outer return must reflect the underlying job
// result so callers up the chain still see job failures.
func TestWebhookMiddleware_Run_PreservesOuterError(t *testing.T) {
	t.Parallel()
	wm := &WebhookMiddleware{webhooks: []core.Middleware{&stubMW{}}}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	// Stop with an error before Run, so the upstream ctx.Next() inside Run
	// sees IsRunning=false and returns nil — but Execution.Error survives.
	// The actual return-value protection lives in webhook_mutation_test.go's
	// realistic-chain test; here we exercise the trivial path.
	err := wm.Run(ctx)
	// With IsRunning=true and no middlewares after composite, ctx.Next() runs
	// the job which is a no-op TestJob → returns nil. The contract is that
	// Run propagates whatever ctx.Next() returned (currently always nil per
	// Context.Next's signature) — pin that by asserting no surprise error.
	assert.NoError(t, err)
}

func TestWebhookMiddleware_ContinueOnStop(t *testing.T) {
	t.Parallel()
	wm := &WebhookMiddleware{webhooks: []core.Middleware{&stubMW{}}}
	assert.True(t, wm.ContinueOnStop())
}

func TestWebhookMiddleware_Run_DispatchesToAll(t *testing.T) {
	t.Parallel()

	var called []string
	s1 := &stubMW{onRun: func() { called = append(called, "s1") }}
	s2 := &stubMW{onRun: func() { called = append(called, "s2") }}
	wm := &WebhookMiddleware{webhooks: []core.Middleware{s1, s2}}

	ctx, _ := setupTestContext(t)
	ctx.Start()
	err := wm.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s2"}, called)
}

// TestBundledPreset_JSONPostExists verifies the bundled json-post preset
// loaded into the binary and is the documented default fallback name.
// Regression for https://github.com/netresearch/ofelia/issues/676.
func TestBundledPreset_JSONPostExists(t *testing.T) {
	t.Parallel()
	loader := NewPresetLoader(nil)
	preset, err := loader.Load(DefaultPresetName)
	require.NoError(t, err, "bundled %q preset must load", DefaultPresetName)
	assert.Equal(t, DefaultPresetName, preset.Name)
	assert.NotEmpty(t, preset.Body, "json-post preset must define a body")
	assert.Equal(t, http.MethodPost, preset.Method)
	require.Contains(t, preset.Variables, "url")
	assert.True(t, preset.Variables["url"].Required, "json-post.url must be required")
}

// TestEffectiveDefaultPreset_ThreeIntents pins the *string semantics on
// WebhookGlobalConfig.DefaultPreset: nil resolves to the bundled name,
// non-nil empty is the explicit opt-out, non-nil non-empty is the
// operator's chosen fallback. See #676.
func TestEffectiveDefaultPreset_ThreeIntents(t *testing.T) {
	t.Parallel()
	empty := ""
	custom := "my-custom"

	tests := []struct {
		name string
		dp   *string
		want string
	}{
		{"nil → bundled fallback", nil, DefaultPresetName},
		{"non-nil empty → opt-out (empty)", &empty, ""},
		{"non-nil custom → operator choice", &custom, "my-custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &WebhookGlobalConfig{DefaultPreset: tt.dp}
			assert.Equal(t, tt.want, cfg.EffectiveDefaultPreset())
		})
	}

	// nil receiver — defensive, since callers may pass globalConfig=nil.
	var nilCfg *WebhookGlobalConfig
	assert.Equal(t, DefaultPresetName, nilCfg.EffectiveDefaultPreset(),
		"nil receiver must still return the bundled fallback")
}

// TestNewWebhook_URLOnly_UsesJSONPostFallback is the headline end-to-end
// regression for #676: a webhook config with only `url` set (no preset)
// must attach successfully against an HTTP test server and dispatch a
// JSON POST whose body parses as valid JSON.
func TestNewWebhook_URLOnly_UsesJSONPostFallback(t *testing.T) {
	// Not parallel — overrides global URL validator / transport factory.
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	var (
		gotContentType string
		gotBody        []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	loader := NewPresetLoader(DefaultWebhookGlobalConfig())
	cfg := &WebhookConfig{
		Name:    "url-only",
		URL:     srv.URL,
		Trigger: TriggerAlways,
		Timeout: 5 * time.Second,
	}
	mw, err := NewWebhook(cfg, loader)
	require.NoError(t, err, "url-only config must attach via the json-post fallback")
	require.NotNil(t, mw)
	assert.Equal(t, DefaultPresetName, cfg.Preset,
		"NewWebhook must fill the missing preset from the loader's effective default")

	job := &TestJob{}
	job.Name = "url-only-job"
	job.Command = "echo hi"
	sh := core.NewScheduler(newDiscardLogger())
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	ctx.Stop(nil)

	_ = mw.Run(ctx)

	require.NotEmpty(t, gotBody, "test server must have received a request body")
	assert.Equal(t, "application/json; charset=utf-8", gotContentType)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload),
		"json-post body must parse as valid JSON; got: %s", string(gotBody))

	// Field-VALUE assertions, not just key-presence. Without these a template
	// refactor that hard-codes any field (e.g. `"name": "ofelia"`) would
	// still pass — the receiver-facing contract is broken silently.
	jobMap, ok := payload["job"].(map[string]any)
	require.True(t, ok, "job must be an object")
	assert.Equal(t, "url-only-job", jobMap["name"])
	assert.Equal(t, "echo hi", jobMap["command"])

	execMap, ok := payload["execution"].(map[string]any)
	require.True(t, ok, "execution must be an object")
	assert.Equal(t, "successful", execMap["status"])
	assert.False(t, execMap["failed"].(bool))
	assert.False(t, execMap["skipped"].(bool))

	hostMap, ok := payload["host"].(map[string]any)
	require.True(t, ok, "host must be an object")
	assert.NotEmpty(t, hostMap["hostname"], "host.hostname must be populated")

	_, hasLink := payload["link"]
	assert.False(t, hasLink, "no link configured → JSON must omit the optional link key")
}

// TestJSONPostPreset_OptionalLinkBlock_RendersValidJSON exercises the
// `{{if .Preset.Link}}...{{end}}` conditional in json-post.yaml. The
// preset YAML places this block at the tail of the object so that the
// comma-after-ofelia / no-trailing-comma rules apply — a misplaced
// brace or comma would silently produce malformed JSON for one branch.
// This test runs BOTH branches (Link set, Link unset) end-to-end.
func TestJSONPostPreset_OptionalLinkBlock_RendersValidJSON(t *testing.T) {
	// Not parallel — overrides global URL validator / transport factory.
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	loader := NewPresetLoader(DefaultWebhookGlobalConfig())

	runOnce := func(t *testing.T, link, linkText string) map[string]any {
		t.Helper()
		gotBody = nil
		cfg := &WebhookConfig{
			Name: "with-link", URL: srv.URL,
			Link: link, LinkText: linkText,
			Trigger: TriggerAlways, Timeout: 5 * time.Second,
		}
		mw, err := NewWebhook(cfg, loader)
		require.NoError(t, err)

		job := &TestJob{}
		job.Name = "linked-job"
		job.Command = "echo"
		sh := core.NewScheduler(newDiscardLogger())
		e, err := core.NewExecution()
		require.NoError(t, err)
		ctx := core.NewContext(sh, job, e)
		ctx.Start()
		ctx.Stop(nil)

		_ = mw.Run(ctx)
		require.NotEmpty(t, gotBody, "test server must have received a request body")
		var payload map[string]any
		require.NoError(t, json.Unmarshal(gotBody, &payload),
			"body must parse as JSON in both link branches; got: %s", string(gotBody))
		return payload
	}

	// Branch 1: link configured → optional block must render with both fields.
	payload := runOnce(t, "https://logs.example.com/run/abc", "View Logs")
	linkMap, ok := payload["link"].(map[string]any)
	require.True(t, ok, "link configured → JSON must include the optional 'link' object")
	assert.Equal(t, "https://logs.example.com/run/abc", linkMap["url"])
	assert.Equal(t, "View Logs", linkMap["text"])

	// Branch 2: link absent → optional block must be omitted entirely.
	payload = runOnce(t, "", "")
	_, hasLink := payload["link"]
	assert.False(t, hasLink, "no link configured → JSON must omit the optional link key")
}

// TestNewWebhook_DefaultPresetOptOut verifies that setting
// webhook-default-preset to non-nil empty disables the fallback. The
// error message must name `webhook-default-preset` so operators can
// grep their way to the docs from a log line, and must name the
// bundled fallback so they know what they opted out of.
func TestNewWebhook_DefaultPresetOptOut(t *testing.T) {
	t.Parallel()
	empty := ""
	global := DefaultWebhookGlobalConfig()
	global.DefaultPreset = &empty
	loader := NewPresetLoader(global)

	cfg := &WebhookConfig{Name: "opt-out", Trigger: TriggerAlways}
	_, err := NewWebhook(cfg, loader)
	require.Error(t, err, "url-only with fallback disabled → must fail attach")
	assert.Contains(t, err.Error(), "webhook-default-preset",
		"error must name the operator-tunable knob so it's greppable from logs")
	assert.Contains(t, err.Error(), DefaultPresetName,
		"error must name the bundled fallback so operators know what they opted out of")
}

// TestNewWebhook_DefaultPresetCustom verifies that an operator-chosen
// default preset (non-nil non-empty) is used as the fallback instead of
// the bundled json-post when a webhook omits `preset`. Exercises the
// "operator's choice" arm of the three-intent semantics.
func TestNewWebhook_DefaultPresetCustom(t *testing.T) {
	t.Parallel()
	custom := "slack" // any bundled preset name works for this test
	global := DefaultWebhookGlobalConfig()
	global.DefaultPreset = &custom
	loader := NewPresetLoader(global)

	cfg := &WebhookConfig{
		Name:    "custom-default",
		ID:      "T123/B456",
		Secret:  "xoxb-secret",
		Trigger: TriggerAlways,
	}
	mw, err := NewWebhook(cfg, loader)
	require.NoError(t, err, "operator default %q should resolve and load", custom)
	require.NotNil(t, mw)
	assert.Equal(t, "slack", cfg.Preset,
		"NewWebhook must fill Preset from the operator's chosen default, not the bundled json-post")
}

// TestNewWebhook_ExplicitPresetWinsOverDefault verifies that a per-webhook
// `preset = X` overrides any global default. The default is only a
// fallback — it must never overwrite explicit operator intent.
func TestNewWebhook_ExplicitPresetWinsOverDefault(t *testing.T) {
	t.Parallel()
	custom := "slack"
	global := DefaultWebhookGlobalConfig()
	global.DefaultPreset = &custom
	loader := NewPresetLoader(global)

	cfg := &WebhookConfig{
		Name:    "explicit",
		Preset:  "discord", // operator chose this explicitly
		ID:      "channel-id",
		Secret:  "webhook-token",
		Trigger: TriggerAlways,
	}
	_, err := NewWebhook(cfg, loader)
	require.NoError(t, err)
	assert.Equal(t, "discord", cfg.Preset,
		"per-webhook preset must survive — fallback only fires when Preset is empty")
}

// TestNewWebhook_NilLoaderReturnsError verifies the contract on the new
// nil-loader guard added during review: webhook construction requires a
// preset loader, and passing nil is an immediate error (not a panic).
func TestNewWebhook_NilLoaderReturnsError(t *testing.T) {
	t.Parallel()
	_, err := NewWebhook(&WebhookConfig{Name: "x", URL: "https://example.com"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preset loader is required")
}

// TestJSONPostPreset_FailedExecutionRendersValidJSON exercises the
// json-post body template on the FAILURE path, where Execution.Error,
// Output, and Stderr can contain JSON-hostile characters (quotes,
// newlines, backslashes). The body MUST still parse as valid JSON
// because the template wraps every dynamic string through the `json`
// helper (which escapes \", \\, \n, \r, \t). Regression for any future
// refactor of the json-post YAML that forgets the escape.
func TestJSONPostPreset_FailedExecutionRendersValidJSON(t *testing.T) {
	// Not parallel — overrides global URL validator / transport factory.
	SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
	defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)
	SetTransportFactoryForTest(func() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() })
	defer SetTransportFactoryForTest(NewSafeTransport)

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	loader := NewPresetLoader(DefaultWebhookGlobalConfig())
	cfg := &WebhookConfig{
		Name:    "url-only-fail",
		URL:     srv.URL,
		Trigger: TriggerAlways,
		Timeout: 5 * time.Second,
	}
	mw, err := NewWebhook(cfg, loader)
	require.NoError(t, err)

	job := &TestJob{}
	job.Name = `job "with quotes" and \backslash`
	job.Command = "sh -c \"echo 'hi' && exit 1\""
	sh := core.NewScheduler(newDiscardLogger())
	e, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContext(sh, job, e)
	ctx.Start()
	// Failed execution with multi-line / quote-laden error message.
	ctx.Stop(errors.New("line one\nline \"two\"\nline\tthree"))

	_ = mw.Run(ctx)

	require.NotEmpty(t, gotBody)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload),
		"failed-execution body must parse as valid JSON; got: %s", string(gotBody))

	// Spot-check the escaped fields round-tripped to the expected values.
	execMap, ok := payload["execution"].(map[string]any)
	require.True(t, ok, "execution must be an object")
	assert.True(t, execMap["failed"].(bool), "failed field must be true")
	assert.Contains(t, execMap["error"], `line "two"`,
		"escaped quote must round-trip through the template")
	assert.Contains(t, execMap["error"], "\n",
		"newline must round-trip — the json helper produces \\n, JSON parse rehydrates")

	jobMap, ok := payload["job"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, `job "with quotes" and \backslash`, jobMap["name"])
}

// TestJSONPostPreset_OutputTruncation verifies the `truncate 4000`
// pipeline in the json-post body template caps stdout/stderr at the
// documented 4000-char limit (plus the "..." ellipsis), so a chatty job
// can't produce a megabyte payload that drowns the receiver.
func TestJSONPostPreset_OutputTruncation(t *testing.T) {
	t.Parallel()
	loader := NewPresetLoader(DefaultWebhookGlobalConfig())
	preset, err := loader.Load(DefaultPresetName)
	require.NoError(t, err)

	long := strings.Repeat("A", 5000)
	data := map[string]any{
		"Job": WebhookJobData{Name: "j", Command: "c", Schedule: "@daily", Type: "exec"},
		"Execution": WebhookExecutionData{
			ID: "x", Status: "successful",
			StartTime: time.Now(), EndTime: time.Now(),
			Output: long, Stderr: long,
		},
		"Host":   WebhookHostData{Hostname: "h", Timestamp: time.Now()},
		"Ofelia": WebhookOfeliaData{Version: "v"},
		"Preset": PresetDataForTemplate{},
	}

	body, err := preset.RenderBodyWithPreset(data)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &payload),
		"truncated body must still parse as JSON")
	execMap := payload["execution"].(map[string]any)
	output := execMap["output"].(string)
	assert.LessOrEqual(t, len(output), 4003, // 4000 + "..."
		"output must be truncated to ~4000 chars, got %d", len(output))
	assert.True(t, strings.HasSuffix(output, "..."),
		"truncated output must end with ellipsis marker")
}

func TestGetJobType_AllVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		job      core.Job
		expected string
	}{
		{"exec job", &core.ExecJob{}, "exec"},
		{"run job", &core.RunJob{}, "run"},
		{"local job", &core.LocalJob{}, "local"},
		{"run-service job", &core.RunServiceJob{}, "run-service"},
		{"unknown job", &TestJob{}, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, getJobType(tt.job))
		})
	}
}

func TestGetExecutionStatus_AllVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		exec     *core.Execution
		expected string
	}{
		{"failed", &core.Execution{Failed: true}, "failed"},
		{"skipped", &core.Execution{Skipped: true}, "skipped"},
		{"successful", &core.Execution{}, "successful"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, getExecutionStatus(tt.exec))
		})
	}
}

func TestParseWebhookNames_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty string", "", nil},
		{"single name", "slack", []string{"slack"}},
		{"multiple names", "slack,discord", []string{"slack", "discord"}},
		{"with spaces", " slack , discord , teams ", []string{"slack", "discord", "teams"}},
		{"empty segments", "slack,,discord,", []string{"slack", "discord"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ParseWebhookNames(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

type stubMW struct {
	onRun func()
}

func (s *stubMW) ContinueOnStop() bool { return true }
func (s *stubMW) Run(_ *core.Context) error {
	if s.onRun != nil {
		s.onRun()
	}
	return nil
}
