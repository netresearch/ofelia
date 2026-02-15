package middlewares

import (
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
