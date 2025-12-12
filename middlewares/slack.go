package middlewares

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/netresearch/ofelia/core"
)

var (
	slackUsername   = "Ofelia"
	slackAvatarURL  = "https://raw.githubusercontent.com/netresearch/ofelia/master/static/avatar.png"
	slackPayloadVar = "payload"

	// slackDeprecationOnce ensures deprecation warning is only shown once
	slackDeprecationOnce sync.Once
)

// SlackConfig configuration for the Slack middleware
type SlackConfig struct {
	SlackWebhook     string `gcfg:"slack-webhook" mapstructure:"slack-webhook" json:"-"`
	SlackOnlyOnError bool   `gcfg:"slack-only-on-error" mapstructure:"slack-only-on-error"`
	// Dedup is the notification deduplicator (set by config loader, not INI)
	Dedup *NotificationDedup `mapstructure:"-" json:"-"`
}

// NewSlack returns a Slack middleware if the given configuration is not empty
//
// Deprecated: The Slack middleware is deprecated and will be removed in v0.8.0.
// Please migrate to the generic webhook notification system with the "slack" preset:
//
//	[webhook "slack-alerts"]
//	preset = slack
//	id = T00000000/B00000000
//	secret = XXXXXXXXXXXXXXXXXXXXXXXX
//	trigger = error
//
// The new webhook system provides retry logic, multiple webhooks, and support
// for other services (Discord, Teams, ntfy, Pushover, PagerDuty, Gotify, etc.)
func NewSlack(c *SlackConfig) core.Middleware {
	var m core.Middleware
	if !IsEmpty(c) {
		// Show deprecation warning once
		slackDeprecationOnce.Do(func() {
			fmt.Fprintln(os.Stderr, "DEPRECATION WARNING: The 'slack-webhook' configuration is deprecated and will be removed in v0.8.0.")
			fmt.Fprintln(os.Stderr, "Please migrate to the new webhook notification system:")
			fmt.Fprintln(os.Stderr, "  [webhook \"slack\"]")
			fmt.Fprintln(os.Stderr, "  preset = slack")
			fmt.Fprintln(os.Stderr, "  id = T.../B...")
			fmt.Fprintln(os.Stderr, "  secret = XXXX...")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "See documentation for migration guide: https://github.com/netresearch/ofelia#webhook-notifications")
		})

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

	shouldNotify := ctx.Execution.Failed || !m.SlackOnlyOnError
	if shouldNotify {
		// Check deduplication - suppress duplicate error notifications
		if m.Dedup != nil && ctx.Execution.Failed && !m.Dedup.ShouldNotify(ctx) {
			ctx.Logger.Debugf("Slack notification suppressed (duplicate within cooldown)")
			return err
		}
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

	// Build request with context and validate URL
	u, err := url.Parse(m.SlackWebhook)
	if err != nil || u.Scheme == "" || u.Host == "" {
		ctx.Logger.Errorf("Slack webhook URL is invalid: %q", m.SlackWebhook)
		return
	}
	ctxReq, cancel := context.WithTimeout(context.Background(), m.Client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctxReq, http.MethodPost, u.String(), strings.NewReader(values.Encode()))
	if err != nil {
		ctx.Logger.Errorf("Slack request build error: %q", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r, err := m.Client.Do(req)
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
