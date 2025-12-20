package middlewares

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultWebhookConfig(t *testing.T) {
	t.Parallel()

	config := DefaultWebhookConfig()

	assert.NotNil(t, config)
	assert.Equal(t, TriggerError, config.Trigger)
	assert.Equal(t, 10*time.Second, config.Timeout)
	assert.Equal(t, 3, config.RetryCount)
	assert.Equal(t, 5*time.Second, config.RetryDelay)
}

func TestDefaultWebhookGlobalConfig(t *testing.T) {
	t.Parallel()

	config := DefaultWebhookGlobalConfig()

	assert.NotNil(t, config)
	assert.False(t, config.AllowRemotePresets)
	assert.Equal(t, 24*time.Hour, config.PresetCacheTTL)
}

func TestTriggerType_Constants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, TriggerType("always"), TriggerAlways)
	assert.Equal(t, TriggerType("success"), TriggerSuccess)
	assert.Equal(t, TriggerType("error"), TriggerError)
	assert.Equal(t, TriggerType("skipped"), TriggerSkipped)
}

func TestParseWebhookNames_Empty(t *testing.T) {
	t.Parallel()

	names := ParseWebhookNames("")
	assert.Empty(t, names)
}

func TestParseWebhookNames_Single(t *testing.T) {
	t.Parallel()

	names := ParseWebhookNames("slack")
	assert.Len(t, names, 1)
	assert.Equal(t, "slack", names[0])
}

func TestParseWebhookNames_Multiple(t *testing.T) {
	t.Parallel()

	names := ParseWebhookNames("slack,discord,teams")
	assert.Len(t, names, 3)
	assert.Equal(t, "slack", names[0])
	assert.Equal(t, "discord", names[1])
	assert.Equal(t, "teams", names[2])
}

func TestParseWebhookNames_WithSpaces(t *testing.T) {
	t.Parallel()

	names := ParseWebhookNames("slack , discord , teams")
	assert.Len(t, names, 3)
	assert.Equal(t, "slack", names[0])
	assert.Equal(t, "discord", names[1])
	assert.Equal(t, "teams", names[2])
}

func TestParseWebhookNames_EmptyElements(t *testing.T) {
	t.Parallel()

	names := ParseWebhookNames("slack,,discord")
	assert.Len(t, names, 2)
	assert.Equal(t, "slack", names[0])
	assert.Equal(t, "discord", names[1])
}

func TestWebhookConfig_Validate_Valid(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
	}

	err := config.Validate()
	require.NoError(t, err)
}

func TestWebhookConfig_Validate_NoPresetOrURL(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name: "test",
	}

	err := config.Validate()
	assert.Error(t, err)
}

func TestWebhookConfig_Validate_InvalidTrigger(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{
		Name:    "test",
		Preset:  "slack",
		Trigger: TriggerType("invalid"),
	}

	err := config.Validate()
	assert.Error(t, err)
}

func TestWebhookConfig_ShouldNotify_Error(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{Trigger: TriggerError}

	assert.True(t, config.ShouldNotify(true, false))
	assert.False(t, config.ShouldNotify(false, false))
	assert.False(t, config.ShouldNotify(false, true))
}

func TestWebhookConfig_ShouldNotify_Success(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{Trigger: TriggerSuccess}

	assert.False(t, config.ShouldNotify(true, false))
	assert.True(t, config.ShouldNotify(false, false))
	assert.False(t, config.ShouldNotify(false, true))
}

func TestWebhookConfig_ShouldNotify_Always(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{Trigger: TriggerAlways}

	assert.True(t, config.ShouldNotify(true, false))
	assert.True(t, config.ShouldNotify(false, false))
	assert.True(t, config.ShouldNotify(false, true))
}

func TestWebhookConfig_ShouldNotify_Skipped(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{Trigger: TriggerSkipped}

	assert.False(t, config.ShouldNotify(true, false))
	assert.False(t, config.ShouldNotify(false, false))
	assert.True(t, config.ShouldNotify(false, true))
}

func TestWebhookConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	config := &WebhookConfig{Name: "test", Preset: "slack"}
	config.ApplyDefaults()

	assert.Equal(t, TriggerError, config.Trigger)
	assert.Equal(t, 10*time.Second, config.Timeout)
	assert.Equal(t, 3, config.RetryCount)
	assert.Equal(t, 5*time.Second, config.RetryDelay)
}

func TestWebhookConfig_Integration(t *testing.T) {
	t.Parallel()

	config := DefaultWebhookConfig()
	config.Name = "test-webhook"
	config.Preset = "slack"
	config.ID = "T12345/B67890"
	config.Secret = "xoxb-secret"

	assert.Equal(t, "test-webhook", config.Name)
	assert.Equal(t, "slack", config.Preset)
}

func TestWebhookData_Construction(t *testing.T) {
	t.Parallel()

	data := &WebhookData{
		Job: WebhookJobData{
			Name:    "test-job",
			Command: "echo hello",
		},
		Execution: WebhookExecutionData{
			Status:    "success",
			StartTime: time.Now(),
			EndTime:   time.Now().Add(time.Second),
			Duration:  time.Second,
		},
		Host: WebhookHostData{
			Hostname:  "test-host",
			Timestamp: time.Now(),
		},
		Ofelia: WebhookOfeliaData{
			Version: "1.0.0",
		},
	}

	assert.Equal(t, "test-job", data.Job.Name)
	assert.Equal(t, "success", data.Execution.Status)
}
