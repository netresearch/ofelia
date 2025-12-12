package middlewares

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"strings"

	mail "github.com/go-mail/mail/v2"

	"github.com/netresearch/ofelia/core"
)

// MailConfig configuration for the Mail middleware
type MailConfig struct {
	SMTPHost          string `gcfg:"smtp-host" mapstructure:"smtp-host"`
	SMTPPort          int    `gcfg:"smtp-port" mapstructure:"smtp-port"`
	SMTPUser          string `gcfg:"smtp-user" mapstructure:"smtp-user" json:"-"`
	SMTPPassword      string `gcfg:"smtp-password" mapstructure:"smtp-password" json:"-"`
	SMTPTLSSkipVerify bool   `gcfg:"smtp-tls-skip-verify" mapstructure:"smtp-tls-skip-verify"`
	EmailTo           string `gcfg:"email-to" mapstructure:"email-to"`
	EmailFrom         string `gcfg:"email-from" mapstructure:"email-from"`
	EmailSubject      string `gcfg:"email-subject" mapstructure:"email-subject"`
	MailOnlyOnError   bool   `gcfg:"mail-only-on-error" mapstructure:"mail-only-on-error"`
	// Dedup is the notification deduplicator (set by config loader, not INI)
	Dedup *NotificationDedup `mapstructure:"-" json:"-"`

	// subjectTemplate is parsed from EmailSubject (internal, set by NewMail)
	subjectTemplate *template.Template
}

// NewMail returns a Mail middleware if the given configuration is not empty
func NewMail(c *MailConfig) core.Middleware {
	var m core.Middleware

	if !IsEmpty(c) {
		// Parse custom subject template if provided
		if c.EmailSubject != "" {
			tmpl := template.New("custom-mail-subject")
			tmpl.Funcs(map[string]interface{}{
				"status": executionLabel,
			})
			if parsed, err := tmpl.Parse(c.EmailSubject); err == nil {
				c.subjectTemplate = parsed
			}
			// If parsing fails, fall back to default (subjectTemplate stays nil)
		}
		m = &Mail{MailConfig: *c}
	}

	return m
}

// Mail middleware delivers a email just after an execution finishes
type Mail struct {
	MailConfig
}

// ContinueOnStop always returns true; we always want to report the final status
func (m *Mail) ContinueOnStop() bool {
	return true
}

// Run sends an email with the result of the execution
func (m *Mail) Run(ctx *core.Context) error {
	err := ctx.Next()
	ctx.Stop(err)

	if !(ctx.Execution.Failed || !m.MailOnlyOnError) {
		return err
	}
	// Check deduplication - suppress duplicate error notifications
	if m.Dedup != nil && ctx.Execution.Failed && !m.Dedup.ShouldNotify(ctx) {
		ctx.Logger.Debugf("Mail notification suppressed (duplicate within cooldown)")
		return err
	}
	if mailErr := m.sendMail(ctx); mailErr != nil {
		ctx.Logger.Errorf("Mail error: %q", mailErr)
	}
	return err
}

func (m *Mail) sendMail(ctx *core.Context) error {
	msg := mail.NewMessage()
	msg.SetHeader("From", m.from())
	msg.SetHeader("To", strings.Split(m.EmailTo, ",")...)
	msg.SetHeader("Subject", m.subject(ctx))
	msg.SetBody("text/html", m.body(ctx))

	base := fmt.Sprintf("%s_%s", ctx.Job.GetName(), ctx.Execution.ID)

	// Only attach stdout if there's output (some SMTP servers reject zero-sized attachments)
	if ctx.Execution.OutputStream.TotalWritten() > 0 {
		msg.Attach(base+".stdout.log", mail.SetCopyFunc(func(w io.Writer) error {
			if _, err := w.Write(ctx.Execution.OutputStream.Bytes()); err != nil {
				return fmt.Errorf("write stdout attachment: %w", err)
			}
			return nil
		}))
	}

	// Only attach stderr if there's output (some SMTP servers reject zero-sized attachments)
	if ctx.Execution.ErrorStream.TotalWritten() > 0 {
		msg.Attach(base+".stderr.log", mail.SetCopyFunc(func(w io.Writer) error {
			if _, err := w.Write(ctx.Execution.ErrorStream.Bytes()); err != nil {
				return fmt.Errorf("write stderr attachment: %w", err)
			}
			return nil
		}))
	}

	msg.Attach(base+".stderr.json", mail.SetCopyFunc(func(w io.Writer) error {
		js, _ := json.MarshalIndent(map[string]interface{}{
			"Job":       ctx.Job,
			"Execution": ctx.Execution,
		}, "", "  ")

		if _, err := w.Write(js); err != nil {
			return fmt.Errorf("write json attachment: %w", err)
		}
		return nil
	}))

	d := mail.NewDialer(m.SMTPHost, m.SMTPPort, m.SMTPUser, m.SMTPPassword)
	// When TLSConfig.InsecureSkipVerify is true, mail server certificate authority is not validated
	if m.SMTPTLSSkipVerify {
		// #nosec G402 -- Allow explicit opt-in for development/legacy servers via config.
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	if err := d.DialAndSend(msg); err != nil {
		return fmt.Errorf("dial and send mail: %w", err)
	}
	return nil
}

func (m *Mail) from() string {
	if !strings.Contains(m.EmailFrom, "%") {
		return m.EmailFrom
	}

	hostname, _ := os.Hostname()
	return fmt.Sprintf(m.EmailFrom, hostname)
}

func (m *Mail) subject(ctx *core.Context) string {
	buf := bytes.NewBuffer(nil)

	// Use custom subject template if configured, otherwise use default
	tmpl := mailSubjectTemplate
	if m.subjectTemplate != nil {
		tmpl = m.subjectTemplate
	}
	_ = tmpl.Execute(buf, ctx)

	return buf.String()
}

func (m *Mail) body(ctx *core.Context) string {
	buf := bytes.NewBuffer(nil)
	_ = mailBodyTemplate.Execute(buf, ctx)

	return buf.String()
}

var mailBodyTemplate, mailSubjectTemplate *template.Template

func init() {
	f := map[string]interface{}{
		"status": executionLabel,
	}

	mailBodyTemplate = template.New("mail-body")
	mailSubjectTemplate = template.New("mail-subject")
	mailBodyTemplate.Funcs(f)
	mailSubjectTemplate.Funcs(f)

	template.Must(mailBodyTemplate.Parse(`
		<p>
			Job ​<b>{{.Job.GetName}}</b>,
			Execution <b>{{status .Execution}}</b> in ​<b>{{.Execution.Duration}}</b>​,
			command: ​<pre>{{.Job.GetCommand}}</pre>​
		</p>
  `))

	template.Must(mailSubjectTemplate.Parse(
		"[Execution {{status .Execution}}] Job {{.Job.GetName}} finished in {{.Execution.Duration}}",
	))
}

func executionLabel(e *core.Execution) string {
	status := "successful"
	if e.Skipped {
		status = "skipped"
	} else if e.Failed {
		status = "failed"
	}

	return status
}
