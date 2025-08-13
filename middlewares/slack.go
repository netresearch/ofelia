package middlewares

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/netresearch/ofelia/core"
)

var (
	slackUsername   = "Ofelia"
	slackAvatarURL  = "https://raw.githubusercontent.com/netresearch/ofelia/master/static/avatar.png"
	slackPayloadVar = "payload"
)

// SlackConfig configuration for the Slack middleware
type SlackConfig struct {
	SlackWebhook     string `gcfg:"slack-webhook" mapstructure:"slack-webhook" json:"-"`
	SlackOnlyOnError bool   `gcfg:"slack-only-on-error" mapstructure:"slack-only-on-error"`
}

// NewSlack returns a Slack middleware if the given configuration is not empty
func NewSlack(c *SlackConfig) core.Middleware {
	var m core.Middleware
	if !IsEmpty(c) {
		m = &Slack{
			SlackConfig: *c,
			Client:      &http.Client{Timeout: 5 * time.Second},
		}
	}

	return m
}

// Slack middleware calls to a Slack input-hook after every execution of a job
type Slack struct {
	SlackConfig
	Client *http.Client
}

// ContinueOnStop always returns true; we always want to report the final status
func (m *Slack) ContinueOnStop() bool {
	return true
}

// Run sends a message to the Slack channel and stops the execution to
// gather metrics
func (m *Slack) Run(ctx *core.Context) error {
	err := ctx.Next()
	ctx.Stop(err)

	if ctx.Execution.Failed || !m.SlackOnlyOnError {
		m.pushMessage(ctx)
	}

	return err
}

func (m *Slack) pushMessage(ctx *core.Context) {
	values := make(url.Values, 0)
	content, _ := json.Marshal(m.buildMessage(ctx))
	values.Add(slackPayloadVar, string(content))

	if m.Client == nil {
		m.Client = &http.Client{Timeout: 5 * time.Second}
	}

	r, err := m.Client.PostForm(m.SlackWebhook, values)
	if err != nil {
		ctx.Logger.Errorf("Slack error calling %q error: %q", m.SlackWebhook, err)
	} else {
		defer r.Body.Close()
		if r.StatusCode != 200 {
			ctx.Logger.Errorf("Slack error non-200 status code calling %q", m.SlackWebhook)
		}
	}
}

func (m *Slack) buildMessage(ctx *core.Context) *slackMessage {
	msg := &slackMessage{
		Username: slackUsername,
		IconURL:  slackAvatarURL,
	}

	msg.Text = fmt.Sprintf(
		"Job *%q* finished in *%s*, command `%s`",
		ctx.Job.GetName(), ctx.Execution.Duration, ctx.Job.GetCommand(),
	)

	switch {
	case ctx.Execution.Failed:
		msg.Attachments = append(msg.Attachments, slackAttachment{
			Title: "Execution failed",
			Text:  ctx.Execution.Error.Error(),
			Color: "#F35A00",
		})
	case ctx.Execution.Skipped:
		msg.Attachments = append(msg.Attachments, slackAttachment{
			Title: "Execution skipped",
			Color: "#FFA500",
		})
	default:
		msg.Attachments = append(msg.Attachments, slackAttachment{
			Title: "Execution successful",
			Color: "#7CD197",
		})
	}

	return msg
}

type slackMessage struct {
	Text        string            `json:"text"`
	Username    string            `json:"username"`
	Attachments []slackAttachment `json:"attachments"`
	IconURL     string            `json:"icon_url"`
}

type slackAttachment struct {
	Color string `json:"color,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`
}
