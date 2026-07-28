// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	mail "github.com/go-mail/mail/v2"

	"github.com/netresearch/ofelia/core"
)

// SMTPTLSPolicy controls the STARTTLS posture of the outbound SMTP dialer.
// It maps 1:1 onto go-mail's StartTLSPolicy enum and is exposed as a string
// in the INI config (`smtp-tls-policy`) so operators don't have to know the
// upstream library's integer values.
//
// Default (empty string) resolves to MandatoryStartTLS — the upstream
// library's own recommendation for any modern SMTP server. The previous
// behavior (OpportunisticStartTLS) silently sent credentials and message
// body in cleartext when the server did not advertise STARTTLS, even when
// `smtp-tls-skip-verify` was off, which violated the operator's intent.
// See https://github.com/netresearch/ofelia/issues/653.
type SMTPTLSPolicy string

// SMTPTLSPolicy constants. The empty string is also accepted (and treated
// as `mandatory`) so operators upgrading do not have to touch their config.
const (
	// SMTPTLSPolicyMandatory requires STARTTLS; the dialer aborts with an
	// error if the server does not advertise it. This is the default.
	SMTPTLSPolicyMandatory SMTPTLSPolicy = "mandatory"

	// SMTPTLSPolicyOpportunistic tries STARTTLS when offered but silently
	// falls back to plaintext if it is not. This is the upstream
	// go-mail/mail/v2 default and the previous Ofelia behavior. Use only
	// when sending to a legacy server that cannot offer STARTTLS but the
	// network path is otherwise trusted (e.g. localhost-only relay).
	SMTPTLSPolicyOpportunistic SMTPTLSPolicy = "opportunistic"

	// SMTPTLSPolicyNone disables STARTTLS entirely; messages and credentials
	// are sent in cleartext. Required for some test fixtures (MailHog,
	// emersion/go-smtp without TLS) and intentionally insecure.
	SMTPTLSPolicyNone SMTPTLSPolicy = "none"
)

// ErrInvalidSMTPTLSPolicy is the sentinel returned by Validate when an
// unknown `smtp-tls-policy` value is encountered. Production callers that
// want a hard-fail on misconfiguration (rather than the soft-fail-with-warn
// of resolveSMTPTLSPolicy) can branch on this via errors.Is.
//
// resolveSMTPTLSPolicy intentionally does NOT return an error — it
// normalizes unknown values to the safe default (mandatory) and emits an
// slog.Warn so a typo cannot weaken transport security at runtime.
var ErrInvalidSMTPTLSPolicy = errors.New("invalid smtp-tls-policy")

// Valid reports whether p is one of the documented values (or empty,
// meaning "use default"). Unknown values are rejected so config validation
// surfaces operator typos that would otherwise be silently normalized.
func (p SMTPTLSPolicy) Valid() bool {
	switch p {
	case "", SMTPTLSPolicyMandatory, SMTPTLSPolicyOpportunistic, SMTPTLSPolicyNone:
		return true
	default:
		return false
	}
}

// Validate returns ErrInvalidSMTPTLSPolicy (wrapped with the offending
// value for diagnostics) when p is not one of the documented values.
// Returns nil for the empty string (treated as "mandatory") and the three
// documented constants. Callers that want a hard-fail on misconfiguration
// at config-load time should use this; callers that should never break
// existing deployments on a typo should use resolveSMTPTLSPolicy instead.
func (p SMTPTLSPolicy) Validate() error {
	if p.Valid() {
		return nil
	}
	return fmt.Errorf("%w: %q (expected mandatory, opportunistic, or none)", ErrInvalidSMTPTLSPolicy, string(p))
}

// loggedInvalidSMTPTLSPolicy is the one-shot gate for the unknown-policy
// warning. resolveSMTPTLSPolicy is invoked on every sendMail; without this
// gate a single typo would emit a warning per delivery (gemini noted this
// in PR #660 review). The gate keys on the unknown value so different
// typos still surface independently.
var loggedInvalidSMTPTLSPolicy sync.Map // map[SMTPTLSPolicy]struct{}

// resolveSMTPTLSPolicy maps the INI-string policy onto go-mail's
// StartTLSPolicy enum. Unknown / invalid input deliberately falls through
// to MandatoryStartTLS (the safe default) and emits an slog.Warn (one-shot
// per unknown value) so a typo cannot silently weaken transport security.
func resolveSMTPTLSPolicy(p SMTPTLSPolicy) SMTPTLSPolicy {
	switch p {
	case "", SMTPTLSPolicyMandatory:
		return SMTPTLSPolicyMandatory
	case SMTPTLSPolicyOpportunistic, SMTPTLSPolicyNone:
		return p
	default:
		if _, loaded := loggedInvalidSMTPTLSPolicy.LoadOrStore(p, struct{}{}); !loaded {
			slog.Default().Warn(
				"unknown smtp-tls-policy value; falling back to mandatory STARTTLS",
				"value", string(p),
				"hint", "valid values: mandatory (default), opportunistic, none",
			)
		}
		return SMTPTLSPolicyMandatory
	}
}

// dialerStartTLSPolicy translates the resolved (already-validated) Ofelia
// policy into go-mail's StartTLSPolicy iota. Kept private so callers go
// through resolveSMTPTLSPolicy first.
func dialerStartTLSPolicy(p SMTPTLSPolicy) mail.StartTLSPolicy {
	switch p {
	case SMTPTLSPolicyMandatory, "":
		return mail.MandatoryStartTLS
	case SMTPTLSPolicyOpportunistic:
		return mail.OpportunisticStartTLS
	case SMTPTLSPolicyNone:
		return mail.NoStartTLS
	default:
		// Anything resolveSMTPTLSPolicy might miss → safe default.
		return mail.MandatoryStartTLS
	}
}

// MailConfig configuration for the Mail middleware
type MailConfig struct {
	SMTPHost          string `gcfg:"smtp-host" mapstructure:"smtp-host"`
	SMTPPort          int    `gcfg:"smtp-port" mapstructure:"smtp-port"`
	SMTPUser          string `gcfg:"smtp-user" mapstructure:"smtp-user" json:"-"`
	SMTPPassword      string `gcfg:"smtp-password" mapstructure:"smtp-password" json:"-"`
	SMTPTLSSkipVerify bool   `gcfg:"smtp-tls-skip-verify" mapstructure:"smtp-tls-skip-verify"`
	// SMTPTLSPolicy controls STARTTLS behavior. See SMTPTLSPolicy for valid
	// values and the security rationale for the mandatory-by-default change.
	// Empty string is treated as "mandatory".
	SMTPTLSPolicy   SMTPTLSPolicy `gcfg:"smtp-tls-policy" mapstructure:"smtp-tls-policy"`
	EmailTo         string        `gcfg:"email-to" mapstructure:"email-to"`
	EmailFrom       string        `gcfg:"email-from" mapstructure:"email-from"`
	EmailSubject    string        `gcfg:"email-subject" mapstructure:"email-subject"`
	MailOnlyOnError *bool         `gcfg:"mail-only-on-error" mapstructure:"mail-only-on-error"`
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
			tmpl.Funcs(map[string]any{
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

	if !ctx.Execution.Failed && boolVal(m.MailOnlyOnError) {
		return err
	}
	// Check deduplication - suppress duplicate error notifications
	if m.Dedup != nil && ctx.Execution.Failed && !m.Dedup.ShouldNotify(ctx) {
		ctx.Logger.Debug("Mail notification suppressed (duplicate within cooldown)")
		return err
	}
	if mailErr := m.sendMail(ctx); mailErr != nil {
		ctx.Logger.Error(fmt.Sprintf("Mail error: %q", mailErr))
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
		js, _ := json.MarshalIndent(map[string]any{
			notificationVarJob:       ctx.Job,
			notificationVarExecution: ctx.Execution,
		}, "", "  ")

		if _, err := w.Write(js); err != nil {
			return fmt.Errorf("write json attachment: %w", err)
		}
		return nil
	}))

	d := mail.NewDialer(m.SMTPHost, m.SMTPPort, m.SMTPUser, m.SMTPPassword)
	// Default to MandatoryStartTLS to close the silent-cleartext-fallback
	// vector tracked in #653. Operators can opt back into the legacy
	// behavior with smtp-tls-policy = opportunistic (or = none for plain
	// SMTP test fixtures). resolveSMTPTLSPolicy normalizes unknown values
	// to mandatory and warns once via slog.
	d.StartTLSPolicy = dialerStartTLSPolicy(resolveSMTPTLSPolicy(m.SMTPTLSPolicy))
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
	f := map[string]any{
		"status": executionLabel,
	}

	mailBodyTemplate = template.New("mail-body")
	mailSubjectTemplate = template.New("mail-subject")
	mailBodyTemplate.Funcs(f)
	mailSubjectTemplate.Funcs(f)

	template.Must(mailBodyTemplate.Parse(`
		<p>
			Job <b>{{.Job.GetName}}</b>,
			Execution <b>{{status .Execution}}</b> in <b>{{.Execution.Duration}}</b>,
			command: <pre>{{.Job.GetCommand}}</pre>
		</p>
  `))

	template.Must(mailSubjectTemplate.Parse(
		"[Execution {{status .Execution}}] Job {{.Job.GetName}} finished in {{.Execution.Duration}}",
	))
}

func executionLabel(e *core.Execution) string {
	status := statusSuccessful
	if e.Skipped {
		status = statusSkipped
	} else if e.Failed {
		status = statusFailed
	}

	return status
}
