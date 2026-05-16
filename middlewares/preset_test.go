// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPresetLoader_Creation(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	assert.NotNil(t, loader)
}

func TestPresetLoader_LoadBundledPreset_Slack(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("slack")

	require.NoError(t, err)
	assert.NotNil(t, preset)
	assert.Equal(t, "slack", preset.Name)
	assert.Equal(t, "POST", preset.Method)
	assert.NotEmpty(t, preset.URLScheme)
}

func TestPresetLoader_LoadBundledPreset_Discord(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("discord")

	require.NoError(t, err)
	assert.NotNil(t, preset)
	assert.Equal(t, "discord", preset.Name)
}

func TestPresetLoader_LoadBundledPreset_Teams(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("teams")

	require.NoError(t, err)
	assert.NotNil(t, preset)
	assert.Equal(t, "teams", preset.Name)
}

func TestPresetLoader_LoadBundledPreset_Ntfy(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("ntfy")

	require.NoError(t, err)
	assert.NotNil(t, preset)
	assert.Equal(t, "ntfy", preset.Name)
	_, hasAuth := preset.Headers["Authorization"]
	assert.False(t, hasAuth)
}

func TestPresetLoader_LoadBundledPreset_NtfyToken(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("ntfy-token")

	require.NoError(t, err)
	assert.NotNil(t, preset)
	assert.Equal(t, "ntfy-token", preset.Name)
	assert.Equal(t, "Bearer {secret}", preset.Headers["Authorization"])
	assert.True(t, preset.Variables["secret"].Required)
}

func TestPresetLoader_LoadBundledPreset_Pushover(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("pushover")

	require.NoError(t, err)
	assert.NotNil(t, preset)
	assert.Equal(t, "pushover", preset.Name)
}

func TestPresetLoader_LoadBundledPreset_PagerDuty(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("pagerduty")

	require.NoError(t, err)
	assert.NotNil(t, preset)
	assert.Equal(t, "pagerduty", preset.Name)
}

func TestPresetLoader_LoadBundledPreset_Gotify(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("gotify")

	require.NoError(t, err)
	assert.NotNil(t, preset)
	assert.Equal(t, "gotify", preset.Name)
}

func TestPresetLoader_LoadNonExistent(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	preset, err := loader.Load("nonexistent")

	require.Error(t, err)
	assert.Nil(t, preset)
}

func TestPreset_BuildURL_WithIDAndSecret(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Name:      "test",
		URLScheme: "https://hooks.example.com/{id}/{secret}",
	}

	config := &WebhookConfig{
		ID:     "test-id",
		Secret: "test-secret",
	}

	url, err := preset.BuildURL(config)
	require.NoError(t, err)
	assert.Equal(t, "https://hooks.example.com/test-id/test-secret", url)
}

func TestPreset_BuildURL_WithCustomURL(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Name:      "test",
		URLScheme: "https://default.example.com",
	}

	config := &WebhookConfig{
		URL: "https://custom.example.com/webhook",
	}

	url, err := preset.BuildURL(config)
	require.NoError(t, err)
	assert.Equal(t, "https://custom.example.com/webhook", url)
}

func TestPreset_RenderBody_Simple(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Name: "test",
		Body: `{"message": "Job {{.Job.Name}} finished"}`,
	}

	data := &WebhookData{
		Job: WebhookJobData{
			Name: "test-job",
		},
	}

	body, err := preset.RenderBody(data)
	require.NoError(t, err)
	assert.JSONEq(t, `{"message": "Job test-job finished"}`, body)
}

func TestPreset_RenderBody_WithStatus(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Name: "test",
		Body: `{"status": "{{.Execution.Status}}"}`,
	}

	data := &WebhookData{
		Execution: WebhookExecutionData{
			Status: "success",
		},
	}

	body, err := preset.RenderBody(data)
	require.NoError(t, err)
	assert.JSONEq(t, `{"status": "success"}`, body)
}

func TestPreset_RenderBody_WithDuration(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Name: "test",
		Body: `Duration: {{.Execution.Duration}}`,
	}

	data := &WebhookData{
		Execution: WebhookExecutionData{
			Duration: 5*time.Second + 230*time.Millisecond,
		},
	}

	body, err := preset.RenderBody(data)
	require.NoError(t, err)
	assert.Equal(t, `Duration: 5.23s`, body)
}

func TestPreset_RenderBody_EmptyTemplate(t *testing.T) {
	t.Parallel()

	preset := &Preset{
		Name: "test",
		Body: "",
	}

	data := &WebhookData{}

	body, err := preset.RenderBody(data)
	require.NoError(t, err)
	assert.Empty(t, body)
}

func TestListBundledPresets(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	presets := loader.ListBundledPresets()

	assert.GreaterOrEqual(t, len(presets), 9)

	hasSlack := false
	hasDiscord := false
	for _, p := range presets {
		if p == "slack" {
			hasSlack = true
		}
		if p == "discord" {
			hasDiscord = true
		}
	}
	assert.True(t, hasSlack)
	assert.True(t, hasDiscord)
}

func TestPresetLoader_AllBundledPresets(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	presets := loader.ListBundledPresets()

	for _, name := range presets {
		preset, err := loader.Load(name)
		require.NoError(t, err, "Failed to load bundled preset %s", name)
		assert.NotEmpty(t, preset.Name, "Preset %s has empty name", name)
		assert.NotEmpty(t, preset.Method, "Preset %s has empty method", name)
		assert.NotEmpty(t, preset.Body, "Preset %s has empty body template", name)
	}
}

func TestPresetLoader_TemplateRendering(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	presets := loader.ListBundledPresets()

	for _, name := range presets {
		preset, err := loader.Load(name)
		require.NoError(t, err, "Failed to load preset %s", name)

		data := map[string]any{
			"Job": WebhookJobData{
				Name:    "test-job",
				Command: "echo hello",
			},
			"Execution": WebhookExecutionData{
				Status:    "successful",
				StartTime: time.Now(),
				EndTime:   time.Now().Add(time.Second),
				Duration:  time.Second,
			},
			"Host": WebhookHostData{
				Hostname:  "test-host",
				Timestamp: time.Now(),
			},
			"Ofelia": WebhookOfeliaData{
				Version: "1.0.0",
			},
			"Preset": PresetDataForTemplate{
				ID:     "test-id-123",
				Secret: "test-secret-456",
				URL:    "https://example.com/webhook",
			},
		}

		body, err := preset.RenderBodyWithPreset(data)
		require.NoError(t, err, "Failed to render body for preset %s", name)
		assert.NotEmpty(t, body, "Preset %s rendered empty body", name)
	}
}

func TestPresetLoader_AddLocalPresetDir(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	assert.Empty(t, loader.localPresetDirs)

	loader.AddLocalPresetDir("/tmp/presets")
	assert.Len(t, loader.localPresetDirs, 1)
	assert.Equal(t, "/tmp/presets", loader.localPresetDirs[0])

	loader.AddLocalPresetDir("/opt/presets")
	assert.Len(t, loader.localPresetDirs, 2)
}

// TestPresetLoader_AddLocalPresetDir_WarnsOnBundledShadow pins the fix for
// https://github.com/netresearch/ofelia/issues/679. PresetLoader.Load resolves
// bundled presets first and never falls through, so a local file at
// $dir/<bundled-name>.yaml is a silent no-op. AddLocalPresetDir now scans the
// directory and emits a slog.Warn per collision so operators learn at startup
// that their override won't take effect.
//
// Scope is restricted to *.yaml to mirror Load's resolution path: Load only
// probes "<name>.yaml" in localPresetDirs, so *.yml files would not have
// been loaded anyway and warning on them would mislead operators into
// thinking a .yml -> .yaml rename is the fix. The .yml anti-warning case
// pins that contract.
//
// Not parallel: shares slog.Default with other captureSlog tests in the
// package (see the helper's comment).
func TestPresetLoader_AddLocalPresetDir_WarnsOnBundledShadow(t *testing.T) {
	dir := t.TempDir()

	// json-post is a bundled preset (since #676) — placing a local file
	// with the same stem and .yaml extension must trigger the shadow
	// warning.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "json-post.yaml"), []byte("name: json-post\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "slack.yaml"), []byte("name: slack\n"), 0o644))
	// Non-colliding file must NOT trigger a warning.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my-custom.yaml"), []byte("name: my-custom\n"), 0o644))
	// .yml extension: Load only probes .yaml, so warning on .yml would
	// mislead operators. This file's bundled stem must NOT trigger a warning.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "discord.yml"), []byte("name: discord\n"), 0o644))
	// Non-yaml file must be ignored entirely.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "json-post.txt"), []byte("not yaml"), 0o644))

	buf := captureSlog(t)
	loader := NewPresetLoader(nil)
	loader.AddLocalPresetDir(dir)

	logs := buf.String()
	assert.Contains(t, logs, "shadows bundled preset \\\"json-post\\\"",
		"should warn that json-post.yaml shadows the bundled json-post preset")
	assert.Contains(t, logs, "shadows bundled preset \\\"slack\\\"",
		"should warn that slack.yaml shadows the bundled slack preset")
	assert.Contains(t, logs, `"level":"WARN"`,
		"shadow notices must use WARN level so operators don't have to raise log verbosity to see them")
	assert.NotContains(t, logs, "my-custom",
		"should NOT warn about non-colliding local presets")
	assert.NotContains(t, logs, "discord",
		"should NOT warn about .yml files — Load only probes .yaml, so .yml is not a shadow case")
	assert.NotContains(t, logs, "json-post.txt",
		"should NOT scan non-yaml files")
}

// TestPresetLoader_AddLocalPresetDir_NoWarnOnMissingDir verifies that
// registering a directory that does not exist (yet) is silent. Operators
// frequently configure the directory before populating it, and the existing
// /tmp/presets fixture above relies on this behavior.
func TestPresetLoader_AddLocalPresetDir_NoWarnOnMissingDir(t *testing.T) {
	buf := captureSlog(t)
	loader := NewPresetLoader(nil)
	loader.AddLocalPresetDir(filepath.Join(t.TempDir(), "does-not-exist"))
	assert.NotContains(t, buf.String(), "shadows bundled preset",
		"missing directory must not produce a shadow warning")
}

func TestPresetLoader_LoadFromFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		path        string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid YAML file",
			content: "name: custom\ndescription: Custom preset\n" +
				"url_scheme: \"https://hooks.example.com/{id}\"\n" +
				"method: POST\nbody: '{\"text\": \"hello\"}'\n",
			wantErr: false,
		},
		{
			name:        "non-existent file",
			path:        "/tmp/nonexistent-preset-12345.yaml",
			wantErr:     true,
			errContains: "read preset file",
		},
		{
			name:        "invalid YAML content",
			content:     "{{invalid yaml: [}",
			wantErr:     true,
			errContains: "parse preset file",
		},
		{
			name:        "valid YAML but missing required fields",
			content:     "name: empty\ndescription: missing body and url_scheme\n",
			wantErr:     true,
			errContains: "must have either",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := NewPresetLoader(nil)

			path := tt.path
			if path == "" {
				dir := t.TempDir()
				path = dir + "/preset.yaml"
				require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o644))
			}

			preset, err := loader.loadFromFile(path)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, preset)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, preset)
		})
	}
}

func TestPresetLoader_LoadFromGitHub(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      *WebhookGlobalConfig
		spec        string
		wantErr     bool
		errContains string
	}{
		{
			name:        "remote disabled with nil config",
			config:      nil,
			spec:        "gh:org/repo/preset.yaml@v1",
			wantErr:     true,
			errContains: "remote presets are disabled",
		},
		{
			name:        "remote disabled explicitly",
			config:      &WebhookGlobalConfig{AllowRemotePresets: false},
			spec:        "gh:org/repo/preset.yaml@v1",
			wantErr:     true,
			errContains: "remote presets are disabled",
		},
		{
			name: "untrusted source",
			config: &WebhookGlobalConfig{
				AllowRemotePresets:   true,
				TrustedPresetSources: "gh:trusted-org/*",
			},
			spec:        "gh:untrusted-org/repo/preset.yaml@v1",
			wantErr:     true,
			errContains: "not in trusted-preset-sources",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := NewPresetLoader(tt.config)

			preset, err := loader.loadFromGitHub(tt.spec)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, preset)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, preset)
		})
	}
}

func TestPresetLoader_LoadFromURL(t *testing.T) {
	// Not parallel: subtests modify global URL validator via SetValidateWebhookURLForTest.
	// Running in parallel causes flaky failures when other tests concurrently call
	// SetGlobalSecurityConfig, which overwrites the test's validator bypass.

	t.Run("remote disabled", func(t *testing.T) {
		t.Parallel()
		loader := NewPresetLoader(nil)
		preset, err := loader.loadFromURL("https://example.com/preset.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote presets are disabled")
		assert.Nil(t, preset)
	})

	t.Run("SSRF blocked by invalid scheme", func(t *testing.T) {
		t.Parallel()
		gc := &WebhookGlobalConfig{AllowRemotePresets: true}
		loader := NewPresetLoader(gc)

		preset, err := loader.loadFromURL("ftp://169.254.169.254/metadata")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "URL validation failed")
		assert.Nil(t, preset)
	})

	t.Run("valid remote preset via httptest", func(t *testing.T) {
		SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
		defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

		presetYAML := "name: remote-test\ndescription: Remote test preset\n" +
			"url_scheme: \"https://hooks.example.com/{id}\"\n" +
			"method: POST\nbody: '{\"msg\": \"hello\"}'\n"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(presetYAML))
		}))
		defer server.Close()

		gc := &WebhookGlobalConfig{AllowRemotePresets: true}
		loader := NewPresetLoader(gc)

		preset, err := loader.loadFromURL(server.URL + "/preset.yaml")
		require.NoError(t, err)
		assert.NotNil(t, preset)
		assert.Equal(t, "remote-test", preset.Name)
	})

	t.Run("remote returns non-200 status", func(t *testing.T) {
		SetValidateWebhookURLForTest(func(rawURL string) error { return nil })
		defer SetValidateWebhookURLForTest(ValidateWebhookURLImpl)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		gc := &WebhookGlobalConfig{AllowRemotePresets: true}
		loader := NewPresetLoader(gc)

		preset, err := loader.loadFromURL(server.URL + "/missing.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 404")
		assert.Nil(t, preset)
	})
}

func TestIsTrustedSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  *WebhookGlobalConfig
		source  string
		trusted bool
	}{
		{"nil config returns false", nil, "gh:org/repo/preset.yaml@v1", false},
		{"empty trusted sources returns false", &WebhookGlobalConfig{TrustedPresetSources: ""}, "gh:org/repo/preset.yaml@v1", false},
		{"matching glob pattern", &WebhookGlobalConfig{TrustedPresetSources: "gh:netresearch/*"}, "gh:netresearch/ofelia-presets/slack.yaml@v1", true},
		{"non-matching glob pattern", &WebhookGlobalConfig{TrustedPresetSources: "gh:netresearch/*"}, "gh:evil-org/bad-presets/malicious.yaml@v1", false},
		{"exact match", &WebhookGlobalConfig{TrustedPresetSources: "gh:org/repo/preset.yaml@v1"}, "gh:org/repo/preset.yaml@v1", true},
		{"multiple patterns comma-separated", &WebhookGlobalConfig{TrustedPresetSources: "gh:orgA/*, gh:orgB/*"}, "gh:orgB/repo/preset.yaml@v1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := &PresetLoader{globalConfig: tt.config}
			assert.Equal(t, tt.trusted, loader.isTrustedSource(tt.source))
		})
	}
}

func TestMatchGlobPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		input   string
		matches bool
	}{
		{"exact match", "gh:org/repo", "gh:org/repo", true},
		{"exact mismatch", "gh:org/repo", "gh:org/other", false},
		{"wildcard matches prefix", "gh:org/*", "gh:org/anything/here", true},
		{"wildcard does not match different prefix", "gh:org/*", "gh:other/repo", false},
		{"empty pattern matches empty string", "", "", true},
		{"wildcard only matches everything", "*", "anything", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.matches, matchGlobPattern(tt.pattern, tt.input))
		})
	}
}

func TestPresetLoader_LoadFromLocalDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	presetContent := "name: local-custom\ndescription: A local custom preset\n" +
		"url_scheme: \"https://hooks.example.com/{id}\"\n" +
		"method: POST\nbody: '{\"text\": \"hello\"}'\n"
	require.NoError(t, os.WriteFile(dir+"/local-custom.yaml", []byte(presetContent), 0o644))

	loader := NewPresetLoader(nil)
	loader.AddLocalPresetDir(dir)

	preset, err := loader.Load("local-custom")
	require.NoError(t, err)
	assert.NotNil(t, preset)
	assert.Equal(t, "local-custom", preset.Name)
}

func TestParsePreset_Defaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		yaml        string
		wantMethod  string
		wantCT      string
		wantErr     bool
		errContains string
	}{
		{
			name:       "method defaults to POST",
			yaml:       "body: '{\"msg\":\"hi\"}'",
			wantMethod: "POST",
			wantCT:     "application/json",
		},
		{
			name:       "explicit GET method has no default CT",
			yaml:       "method: GET\nurl_scheme: 'https://example.com/{id}'",
			wantMethod: "GET",
			wantCT:     "",
		},
		{
			name:        "no body or url_scheme returns error",
			yaml:        "name: incomplete\n",
			wantErr:     true,
			errContains: "must have either",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			preset, err := ParsePreset([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantMethod, preset.Method)
			if tt.wantCT != "" {
				assert.Equal(t, tt.wantCT, preset.Headers["Content-Type"])
			}
		})
	}
}
