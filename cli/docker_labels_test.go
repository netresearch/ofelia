package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

func TestMergeJobMaps(t *testing.T) {
	t.Parallel()

	t.Run("left empty right has keys", func(t *testing.T) {
		t.Parallel()
		right := map[string]string{"a": "right"}
		got := mergeJobMaps(nil, right, true)
		assert.Equal(t, right, got)
		got = mergeJobMaps(nil, right, false)
		assert.Equal(t, right, got)
	})

	t.Run("same key useRightIfExists false keeps left", func(t *testing.T) {
		t.Parallel()
		left := map[string]string{"a": "left"}
		right := map[string]string{"a": "right"}
		got := mergeJobMaps(left, right, false)
		assert.Equal(t, "left", got["a"])
	})

	t.Run("same key useRightIfExists true uses right", func(t *testing.T) {
		t.Parallel()
		left := map[string]string{"a": "left"}
		right := map[string]string{"a": "right"}
		got := mergeJobMaps(left, right, true)
		assert.Equal(t, "right", got["a"])
	})

	t.Run("disjoint keys both present", func(t *testing.T) {
		t.Parallel()
		left := map[string]string{"a": "left"}
		right := map[string]string{"b": "right"}
		got := mergeJobMaps(left, right, false)
		assert.Len(t, got, 2)
		assert.Equal(t, "left", got["a"])
		assert.Equal(t, "right", got["b"])
		got = mergeJobMaps(left, right, true)
		assert.Len(t, got, 2)
		assert.Equal(t, "left", got["a"])
		assert.Equal(t, "right", got["b"])
	})

	t.Run("both empty", func(t *testing.T) {
		t.Parallel()
		got := mergeJobMaps(map[string]string{}, map[string]string{}, true)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})
}

func TestCanRunServiceJob(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("job-local on non-service returns false", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnServiceContainer(jobLocal, "myjob", "c1", false, logger)
		assert.False(t, got)
	})

	t.Run("job-service-run on non-service returns false", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnServiceContainer(jobServiceRun, "myjob", "c1", false, logger)
		assert.False(t, got)
	})

	t.Run("job-local on service returns true", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnServiceContainer(jobLocal, "myjob", "c1", true, logger)
		assert.True(t, got)
	})

	t.Run("job-exec on non-service returns true", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnServiceContainer(jobExec, "myjob", "c1", false, logger)
		assert.True(t, got)
	})

	t.Run("job-run on non-service returns true", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnStoppedContainer(jobRun, "myjob", "c1", false, logger)
		assert.True(t, got)
	})

	t.Run("job-compose on non-service returns true", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnServiceContainer(jobCompose, "myjob", "c1", false, logger)
		assert.True(t, got)
	})

	t.Run("unknown job type returns false", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnServiceContainer("job-unknown", "myjob", "c1", false, logger)
		assert.False(t, got)
	})
}

func TestCanRunJobInStoppedContainer(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	t.Run("job-exec on stopped returns false", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnStoppedContainer(jobExec, "myjob", "c1", false, logger)
		assert.False(t, got)
	})

	t.Run("job-exec on running returns true", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnStoppedContainer(jobExec, "myjob", "c1", true, logger)
		assert.True(t, got)
	})

	t.Run("job-run on stopped returns true", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnStoppedContainer(jobRun, "myjob", "c1", false, logger)
		assert.True(t, got)
	})

	t.Run("job-run on running returns true", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnStoppedContainer(jobRun, "myjob", "c1", true, logger)
		assert.True(t, got)
	})

	t.Run("unknown job type returns false", func(t *testing.T) {
		t.Parallel()
		got := checkJobTypeAllowedOnStoppedContainer("job-unknown", "myjob", "c1", false, logger)
		assert.False(t, got)
	})
}

func TestSplitLabelsWebhookFromServiceContainer(t *testing.T) {
	t.Parallel()
	c := NewConfig(test.NewTestLogger())

	containers := []DockerContainerInfo{
		{
			Name:  "ofelia-service",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                       "true",
				"ofelia.service":                       "true",
				"ofelia.webhook.slack-alerts.preset":   "slack",
				"ofelia.webhook.slack-alerts.id":       "T123/B456",
				"ofelia.webhook.slack-alerts.secret":   "xoxb-secret",
				"ofelia.webhook.slack-alerts.trigger":  "error",
				"ofelia.webhook.discord-notify.preset": "discord",
				"ofelia.webhook.discord-notify.url":    "https://discord.example.com/webhook",
			},
		},
	}

	_, _, _, _, _, _, webhookConfigs := c.splitContainersLabelsIntoJobMapsByType(containers)

	require.Len(t, webhookConfigs, 2, "expected 2 webhook configs")

	slack, ok := webhookConfigs["slack-alerts"]
	require.True(t, ok, "slack-alerts webhook not found")
	assert.Equal(t, "slack", slack["preset"])
	assert.Equal(t, "T123/B456", slack["id"])
	assert.Equal(t, "xoxb-secret", slack["secret"])
	assert.Equal(t, "error", slack["trigger"])

	discord, ok := webhookConfigs["discord-notify"]
	require.True(t, ok, "discord-notify webhook not found")
	assert.Equal(t, "discord", discord["preset"])
	assert.Equal(t, "https://discord.example.com/webhook", discord["url"])
}

func TestWebhookLabelsIgnoredOnNonServiceContainer(t *testing.T) {
	t.Parallel()
	c := NewConfig(test.NewTestLogger())

	containers := []DockerContainerInfo{
		{
			Name:  "worker-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                     "true",
				"ofelia.webhook.slack-alerts.preset": "slack",
				"ofelia.webhook.slack-alerts.id":     "T123/B456",
			},
		},
	}

	_, _, _, _, _, _, webhookConfigs := c.splitContainersLabelsIntoJobMapsByType(containers)

	assert.Empty(t, webhookConfigs, "webhook configs should be empty for non-service container")
}

func TestBuildFromDockerContainersWithWebhooks(t *testing.T) {
	t.Parallel()
	c := NewConfig(test.NewTestLogger())

	containers := []DockerContainerInfo{
		{
			Name:  "ofelia-service",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                      "true",
				"ofelia.service":                      "true",
				"ofelia.webhook.slack-alerts.preset":  "slack",
				"ofelia.webhook.slack-alerts.id":      "T123/B456",
				"ofelia.webhook.slack-alerts.secret":  "xoxb-secret",
				"ofelia.webhook.slack-alerts.trigger": "error",
				"ofelia.webhook.slack-alerts.link":    "https://logs.example.com",
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	require.NotNil(t, c.WebhookConfigs)
	require.Len(t, c.WebhookConfigs.Webhooks, 1)

	wh, ok := c.WebhookConfigs.Webhooks["slack-alerts"]
	require.True(t, ok, "slack-alerts webhook not found")
	assert.Equal(t, "slack", wh.Preset)
	assert.Equal(t, "T123/B456", wh.ID)
	assert.Equal(t, "xoxb-secret", wh.Secret)
	assert.Equal(t, middlewares.TriggerError, wh.Trigger)
	assert.Equal(t, "https://logs.example.com", wh.Link)
	assert.Equal(t, "slack-alerts", wh.Name)
}

func TestBuildFromDockerContainersWithGlobalWebhookSettings(t *testing.T) {
	t.Parallel()
	c := NewConfig(test.NewTestLogger())

	containers := []DockerContainerInfo{
		{
			Name:  "ofelia-service",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":               "true",
				"ofelia.service":               "true",
				"ofelia.webhooks":              "slack-alerts",
				"ofelia.webhook-allowed-hosts": "hooks.slack.com,ntfy.internal",
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	require.NotNil(t, c.WebhookConfigs)
	assert.Equal(t, "slack-alerts", c.WebhookConfigs.Global.Webhooks)
	assert.Equal(t, "hooks.slack.com,ntfy.internal", c.WebhookConfigs.Global.AllowedHosts)
}

func TestMultipleWebhooksFromLabels(t *testing.T) {
	t.Parallel()
	c := NewConfig(test.NewTestLogger())

	containers := []DockerContainerInfo{
		{
			Name:  "ofelia-service",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                         "true",
				"ofelia.service":                         "true",
				"ofelia.webhook.slack-alerts.preset":     "slack",
				"ofelia.webhook.slack-alerts.trigger":    "error",
				"ofelia.webhook.discord-notify.preset":   "discord",
				"ofelia.webhook.discord-notify.trigger":  "always",
				"ofelia.webhook.ntfy-backup.url":         "https://ntfy.sh/my-topic",
				"ofelia.webhook.ntfy-backup.trigger":     "success",
				"ofelia.webhook.ntfy-backup.retry-count": "5",
				"ofelia.webhook.ntfy-backup.retry-delay": "10s",
				"ofelia.webhook.ntfy-backup.timeout":     "30s",
				"ofelia.webhook.ntfy-backup.link":        "https://dashboard.example.com",
				"ofelia.webhook.ntfy-backup.link-text":   "View Dashboard",
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	require.NotNil(t, c.WebhookConfigs)
	require.Len(t, c.WebhookConfigs.Webhooks, 3)

	slack := c.WebhookConfigs.Webhooks["slack-alerts"]
	require.NotNil(t, slack)
	assert.Equal(t, "slack", slack.Preset)
	assert.Equal(t, middlewares.TriggerError, slack.Trigger)

	discord := c.WebhookConfigs.Webhooks["discord-notify"]
	require.NotNil(t, discord)
	assert.Equal(t, "discord", discord.Preset)
	assert.Equal(t, middlewares.TriggerAlways, discord.Trigger)

	ntfy := c.WebhookConfigs.Webhooks["ntfy-backup"]
	require.NotNil(t, ntfy)
	assert.Equal(t, "https://ntfy.sh/my-topic", ntfy.URL)
	assert.Equal(t, middlewares.TriggerSuccess, ntfy.Trigger)
	assert.Equal(t, 5, ntfy.RetryCount)
	assert.Equal(t, "10s", ntfy.RetryDelay.String())
	assert.Equal(t, "30s", ntfy.Timeout.String())
	assert.Equal(t, "https://dashboard.example.com", ntfy.Link)
	assert.Equal(t, "View Dashboard", ntfy.LinkText)
}

func TestPerJobWebhookAssignmentViaLabels(t *testing.T) {
	t.Parallel()
	c := NewConfig(test.NewTestLogger())

	containers := []DockerContainerInfo{
		{
			Name:  "ofelia-service",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                     "true",
				"ofelia.service":                     "true",
				"ofelia.webhooks":                    "slack-alerts",
				"ofelia.webhook.slack-alerts.preset": "slack",
				"ofelia.webhook.slack-alerts.id":     "T123/B456",
				"ofelia.webhook.slack-alerts.secret": "xoxb-secret",
			},
		},
		{
			Name:  "worker-container",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                  "true",
				"ofelia.job-exec.backup.schedule": "@every 1h",
				"ofelia.job-exec.backup.command":  "pg_dump -U postgres mydb",
				"ofelia.job-exec.backup.webhooks": "slack-alerts",
			},
		},
	}

	err := c.buildFromDockerContainers(containers)
	require.NoError(t, err)

	require.Contains(t, c.ExecJobs, "worker-container.backup", "expected backup exec job")
	assert.Equal(t, "slack-alerts", c.ExecJobs["worker-container.backup"].Webhooks,
		"expected exec job webhooks field to be set from label")
}
