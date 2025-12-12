package middlewares

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"
)

type SuiteWebhookConfig struct {
	BaseSuite
}

var _ = Suite(&SuiteWebhookConfig{})

func (s *SuiteWebhookConfig) TestDefaultWebhookConfig(c *C) {
	config := DefaultWebhookConfig()

	c.Assert(config, NotNil)
	c.Assert(config.Trigger, Equals, TriggerError)
	c.Assert(config.Timeout, Equals, 10*time.Second)
	c.Assert(config.RetryCount, Equals, 3)
	c.Assert(config.RetryDelay, Equals, 5*time.Second)
}

func (s *SuiteWebhookConfig) TestDefaultWebhookGlobalConfig(c *C) {
	config := DefaultWebhookGlobalConfig()

	c.Assert(config, NotNil)
	c.Assert(config.AllowRemotePresets, Equals, false)
	c.Assert(config.PresetCacheTTL, Equals, 24*time.Hour)
}

func (s *SuiteWebhookConfig) TestTriggerType_Constants(c *C) {
	c.Assert(TriggerAlways, Equals, TriggerType("always"))
	c.Assert(TriggerSuccess, Equals, TriggerType("success"))
	c.Assert(TriggerError, Equals, TriggerType("error"))
}

func (s *SuiteWebhookConfig) TestParseWebhookNames_Empty(c *C) {
	names := ParseWebhookNames("")
	c.Assert(names, HasLen, 0)
}

func (s *SuiteWebhookConfig) TestParseWebhookNames_Single(c *C) {
	names := ParseWebhookNames("slack")
	c.Assert(names, HasLen, 1)
	c.Assert(names[0], Equals, "slack")
}

func (s *SuiteWebhookConfig) TestParseWebhookNames_Multiple(c *C) {
	names := ParseWebhookNames("slack,discord,teams")
	c.Assert(names, HasLen, 3)
	c.Assert(names[0], Equals, "slack")
	c.Assert(names[1], Equals, "discord")
	c.Assert(names[2], Equals, "teams")
}

func (s *SuiteWebhookConfig) TestParseWebhookNames_WithSpaces(c *C) {
	names := ParseWebhookNames("slack , discord , teams")
	c.Assert(names, HasLen, 3)
	c.Assert(names[0], Equals, "slack")
	c.Assert(names[1], Equals, "discord")
	c.Assert(names[2], Equals, "teams")
}

func (s *SuiteWebhookConfig) TestParseWebhookNames_EmptyElements(c *C) {
	names := ParseWebhookNames("slack,,discord")
	c.Assert(names, HasLen, 2)
	c.Assert(names[0], Equals, "slack")
	c.Assert(names[1], Equals, "discord")
}

func (s *SuiteWebhookConfig) TestWebhookConfig_Validate_Valid(c *C) {
	config := &WebhookConfig{
		Name:   "test",
		Preset: "slack",
	}

	err := config.Validate()
	c.Assert(err, IsNil)
}

func (s *SuiteWebhookConfig) TestWebhookConfig_Validate_NoPresetOrURL(c *C) {
	config := &WebhookConfig{
		Name: "test",
	}

	err := config.Validate()
	c.Assert(err, NotNil)
}

func (s *SuiteWebhookConfig) TestWebhookConfig_Validate_InvalidTrigger(c *C) {
	config := &WebhookConfig{
		Name:    "test",
		Preset:  "slack",
		Trigger: TriggerType("invalid"),
	}

	err := config.Validate()
	c.Assert(err, NotNil)
}

func (s *SuiteWebhookConfig) TestWebhookConfig_ShouldNotify_Error(c *C) {
	config := &WebhookConfig{Trigger: TriggerError}

	c.Assert(config.ShouldNotify(true, false), Equals, true)   // Failed
	c.Assert(config.ShouldNotify(false, false), Equals, false) // Success
	c.Assert(config.ShouldNotify(false, true), Equals, false)  // Skipped
}

func (s *SuiteWebhookConfig) TestWebhookConfig_ShouldNotify_Success(c *C) {
	config := &WebhookConfig{Trigger: TriggerSuccess}

	c.Assert(config.ShouldNotify(true, false), Equals, false) // Failed
	c.Assert(config.ShouldNotify(false, false), Equals, true) // Success
	c.Assert(config.ShouldNotify(false, true), Equals, false) // Skipped
}

func (s *SuiteWebhookConfig) TestWebhookConfig_ShouldNotify_Always(c *C) {
	config := &WebhookConfig{Trigger: TriggerAlways}

	c.Assert(config.ShouldNotify(true, false), Equals, true)  // Failed
	c.Assert(config.ShouldNotify(false, false), Equals, true) // Success
	c.Assert(config.ShouldNotify(false, true), Equals, true)  // Skipped
}

func (s *SuiteWebhookConfig) TestWebhookConfig_ApplyDefaults(c *C) {
	config := &WebhookConfig{Name: "test", Preset: "slack"}
	config.ApplyDefaults()

	c.Assert(config.Trigger, Equals, TriggerError)
	c.Assert(config.Timeout, Equals, 10*time.Second)
	c.Assert(config.RetryCount, Equals, 3)
	c.Assert(config.RetryDelay, Equals, 5*time.Second)
}

// Standard Go testing integration tests
func TestWebhookConfig_Integration(t *testing.T) {
	config := DefaultWebhookConfig()
	config.Name = "test-webhook"
	config.Preset = "slack"
	config.ID = "T12345/B67890"
	config.Secret = "xoxb-secret"

	if config.Name != "test-webhook" {
		t.Errorf("Expected name 'test-webhook', got %s", config.Name)
	}

	if config.Preset != "slack" {
		t.Errorf("Expected preset 'slack', got %s", config.Preset)
	}
}

func TestWebhookData_Construction(t *testing.T) {
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

	if data.Job.Name != "test-job" {
		t.Errorf("Expected job name 'test-job', got %s", data.Job.Name)
	}

	if data.Execution.Status != "success" {
		t.Errorf("Expected status 'success', got %s", data.Execution.Status)
	}
}
