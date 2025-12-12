package middlewares

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"
)

type SuitePreset struct {
	BaseSuite
}

var _ = Suite(&SuitePreset{})

func (s *SuitePreset) TestPresetLoader_Creation(c *C) {
	loader := NewPresetLoader(nil)
	c.Assert(loader, NotNil)
}

func (s *SuitePreset) TestPresetLoader_LoadBundledPreset_Slack(c *C) {
	loader := NewPresetLoader(nil)
	preset, err := loader.Load("slack")

	c.Assert(err, IsNil)
	c.Assert(preset, NotNil)
	c.Assert(preset.Name, Equals, "slack")
	c.Assert(preset.Method, Equals, "POST")
	c.Assert(preset.URLScheme, Not(Equals), "")
}

func (s *SuitePreset) TestPresetLoader_LoadBundledPreset_Discord(c *C) {
	loader := NewPresetLoader(nil)
	preset, err := loader.Load("discord")

	c.Assert(err, IsNil)
	c.Assert(preset, NotNil)
	c.Assert(preset.Name, Equals, "discord")
}

func (s *SuitePreset) TestPresetLoader_LoadBundledPreset_Teams(c *C) {
	loader := NewPresetLoader(nil)
	preset, err := loader.Load("teams")

	c.Assert(err, IsNil)
	c.Assert(preset, NotNil)
	c.Assert(preset.Name, Equals, "teams")
}

func (s *SuitePreset) TestPresetLoader_LoadBundledPreset_Ntfy(c *C) {
	loader := NewPresetLoader(nil)
	preset, err := loader.Load("ntfy")

	c.Assert(err, IsNil)
	c.Assert(preset, NotNil)
	c.Assert(preset.Name, Equals, "ntfy")
}

func (s *SuitePreset) TestPresetLoader_LoadBundledPreset_Pushover(c *C) {
	loader := NewPresetLoader(nil)
	preset, err := loader.Load("pushover")

	c.Assert(err, IsNil)
	c.Assert(preset, NotNil)
	c.Assert(preset.Name, Equals, "pushover")
}

func (s *SuitePreset) TestPresetLoader_LoadBundledPreset_PagerDuty(c *C) {
	loader := NewPresetLoader(nil)
	preset, err := loader.Load("pagerduty")

	c.Assert(err, IsNil)
	c.Assert(preset, NotNil)
	c.Assert(preset.Name, Equals, "pagerduty")
}

func (s *SuitePreset) TestPresetLoader_LoadBundledPreset_Gotify(c *C) {
	loader := NewPresetLoader(nil)
	preset, err := loader.Load("gotify")

	c.Assert(err, IsNil)
	c.Assert(preset, NotNil)
	c.Assert(preset.Name, Equals, "gotify")
}

func (s *SuitePreset) TestPresetLoader_LoadNonExistent(c *C) {
	loader := NewPresetLoader(nil)
	preset, err := loader.Load("nonexistent")

	c.Assert(err, NotNil)
	c.Assert(preset, IsNil)
}

func (s *SuitePreset) TestPreset_BuildURL_WithIDAndSecret(c *C) {
	preset := &Preset{
		Name:      "test",
		URLScheme: "https://hooks.example.com/{id}/{secret}",
	}

	config := &WebhookConfig{
		ID:     "test-id",
		Secret: "test-secret",
	}

	url, err := preset.BuildURL(config)
	c.Assert(err, IsNil)
	c.Assert(url, Equals, "https://hooks.example.com/test-id/test-secret")
}

func (s *SuitePreset) TestPreset_BuildURL_WithCustomURL(c *C) {
	preset := &Preset{
		Name:      "test",
		URLScheme: "https://default.example.com",
	}

	config := &WebhookConfig{
		URL: "https://custom.example.com/webhook",
	}

	url, err := preset.BuildURL(config)
	c.Assert(err, IsNil)
	c.Assert(url, Equals, "https://custom.example.com/webhook")
}

func (s *SuitePreset) TestPreset_RenderBody_Simple(c *C) {
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
	c.Assert(err, IsNil)
	c.Assert(body, Equals, `{"message": "Job test-job finished"}`)
}

func (s *SuitePreset) TestPreset_RenderBody_WithStatus(c *C) {
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
	c.Assert(err, IsNil)
	c.Assert(body, Equals, `{"status": "success"}`)
}

func (s *SuitePreset) TestPreset_RenderBody_WithDuration(c *C) {
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
	c.Assert(err, IsNil)
	c.Assert(body, Equals, `Duration: 5.23s`)
}

func (s *SuitePreset) TestPreset_RenderBody_EmptyTemplate(c *C) {
	preset := &Preset{
		Name: "test",
		Body: "",
	}

	data := &WebhookData{}

	body, err := preset.RenderBody(data)
	c.Assert(err, IsNil)
	c.Assert(body, Equals, "")
}

func (s *SuitePreset) TestListBundledPresets(c *C) {
	loader := NewPresetLoader(nil)
	presets := loader.ListBundledPresets()

	c.Assert(len(presets) >= 7, Equals, true)

	// Check that expected presets are present
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
	c.Assert(hasSlack, Equals, true)
	c.Assert(hasDiscord, Equals, true)
}

// Standard Go testing for better IDE integration
func TestPresetLoader_AllBundledPresets(t *testing.T) {
	loader := NewPresetLoader(nil)
	presets := loader.ListBundledPresets()

	for _, name := range presets {
		preset, err := loader.Load(name)
		if err != nil {
			t.Errorf("Failed to load bundled preset %s: %v", name, err)
			continue
		}

		if preset.Name == "" {
			t.Errorf("Preset %s has empty name", name)
		}

		if preset.Method == "" {
			t.Errorf("Preset %s has empty method", name)
		}

		if preset.Body == "" {
			t.Errorf("Preset %s has empty body template", name)
		}
	}
}

func TestPresetLoader_TemplateRendering(t *testing.T) {
	loader := NewPresetLoader(nil)

	// Test that all presets can render without errors
	presets := loader.ListBundledPresets()
	for _, name := range presets {
		preset, err := loader.Load(name)
		if err != nil {
			t.Errorf("Failed to load preset %s: %v", name, err)
			continue
		}

		// Use RenderBodyWithPreset with map data that includes Preset field
		// This is how the actual webhook.go send() method calls it
		data := map[string]interface{}{
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
		if err != nil {
			t.Errorf("Failed to render body for preset %s: %v", name, err)
			continue
		}

		if body == "" {
			t.Errorf("Preset %s rendered empty body", name)
		}
	}
}
