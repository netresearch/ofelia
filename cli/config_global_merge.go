// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"time"

	"github.com/netresearch/ofelia/middlewares"
)

// This file forwards label-decoded values for the non-webhook global keys
// listed in cli/docker-labels.go::globalLabelAllowList into the live config.
// It is the structural sibling of mergeWebhookGlobals (config_webhook.go) —
// see issues #650 (webhook half) and #652 (this half) for context.
//
// PRECEDENCE POLICY (uniform across every helper here)
//
//   - String/int/duration with a documented zero/empty/default sentinel:
//     "INI is at default → take label" — mirrors the AllowedHosts="*" and
//     PresetCacheTTL=24h sentinels in mergeWebhookGlobals. Carries the same
//     deliberate ambiguity (an INI value set to exactly the default is
//     indistinguishable from unset). Tracking explicit-set state per field
//     would require parser-level changes disproportionate to the gain.
//
//   - *bool (SlackOnlyOnError, MailOnlyOnError, SaveOnlyOnError,
//     RestoreHistory): "INI is nil → take label". Pointer-typed by design so
//     unset can be distinguished from explicit false.
//
//   - Plain bool (SMTPTLSSkipVerify, EnableStrictValidation): inherently
//     cannot distinguish unset from false. Mirrors the
//     mergeMailDefaults/SMTPTLSSkipVerify policy in cli/config.go: a label
//     may UPGRADE an INI false→true (more strict / more insecure-but-
//     observable) but cannot downgrade. The asymmetry is documented inline
//     at every plain-bool field.
//
// SCOPE
//
// Only fields listed in globalLabelAllowList are forwarded. SSRF-sensitive
// webhook globals (webhook-allowed-hosts, webhook-allow-remote-presets,
// webhook-trusted-preset-sources, webhook-preset-cache-dir) remain INI-only
// — see config_webhook.go and #486.
//
// INDIRECT-INHERITANCE LIMITATION (documented for reviewers)
//
// mergeNotificationDefaults (cli/config.go) copies c.Global.SlackConfig /
// MailConfig / SaveConfig into per-job middleware configs at job-prep time.
// Once copied, the job's field is non-zero and will not re-inherit on a
// subsequent global label change. applyAllowListedGlobals therefore makes
// the new global values visible to (a) NEW jobs created in the same
// reconcile pass and (b) EXISTING jobs that change for any other reason.
// Already-running jobs whose own labels are unchanged keep their stale
// inherited values until the daemon restarts. This matches the existing
// behavior of the INI live-reload path (cli/config.go::iniConfigUpdate
// "globalChanged" branch) — closing the per-job inheritance gap is out of
// scope for #652 and tracked separately.

// mergeSlackGlobals copies operator-tunable Slack globals from src (a
// label-parsed scratch Global) into dst (the live c.Global.SlackConfig).
// Returns true when any field was overwritten so callers can flip their
// `changed` flag. See file header for the precedence policy.
func mergeSlackGlobals(dst, src *middlewares.SlackConfig) bool {
	changed := false
	if dst.SlackWebhook == "" && src.SlackWebhook != "" {
		dst.SlackWebhook = src.SlackWebhook
		changed = true
	}
	// *bool: INI nil ⇒ take label. Copy via a fresh allocation so the live
	// config does not share storage with the scratch (which is GC'd after
	// the reconcile completes).
	if dst.SlackOnlyOnError == nil && src.SlackOnlyOnError != nil {
		v := *src.SlackOnlyOnError
		dst.SlackOnlyOnError = &v
		changed = true
	}
	return changed
}

// mergeMailGlobals copies operator-tunable Mail globals from src into dst.
// Returns true when any field was overwritten. Split into per-section
// helpers (SMTP transport / email envelope / behavior flags) to keep each
// piece testable and below the linter's cyclomatic complexity threshold.
// See file header for the precedence policy.
func mergeMailGlobals(dst, src *middlewares.MailConfig) bool {
	changed := false
	if mergeMailSMTPGlobals(dst, src) {
		changed = true
	}
	if mergeMailEnvelopeGlobals(dst, src) {
		changed = true
	}
	if mergeMailFlagGlobals(dst, src) {
		changed = true
	}
	return changed
}

// mergeMailSMTPGlobals forwards SMTP transport credentials. SMTPTLSSkipVerify
// is the documented plain-bool exception: a label may UPGRADE false→true
// (more permissive transport) but cannot downgrade. Mirrors
// mergeMailDefaults' policy in cli/config.go and is pinned by
// TestMergeMailGlobals_TLSSkipVerify_LabelCannotDisable.
func mergeMailSMTPGlobals(dst, src *middlewares.MailConfig) bool {
	changed := false
	if dst.SMTPHost == "" && src.SMTPHost != "" {
		dst.SMTPHost = src.SMTPHost
		changed = true
	}
	if dst.SMTPPort == 0 && src.SMTPPort != 0 {
		dst.SMTPPort = src.SMTPPort
		changed = true
	}
	if dst.SMTPUser == "" && src.SMTPUser != "" {
		dst.SMTPUser = src.SMTPUser
		changed = true
	}
	if dst.SMTPPassword == "" && src.SMTPPassword != "" {
		dst.SMTPPassword = src.SMTPPassword
		changed = true
	}
	if !dst.SMTPTLSSkipVerify && src.SMTPTLSSkipVerify {
		dst.SMTPTLSSkipVerify = true
		changed = true
	}
	return changed
}

// mergeMailEnvelopeGlobals forwards the email From/To/Subject envelope
// fields. All three use the empty-as-unset sentinel.
func mergeMailEnvelopeGlobals(dst, src *middlewares.MailConfig) bool {
	changed := false
	if dst.EmailTo == "" && src.EmailTo != "" {
		dst.EmailTo = src.EmailTo
		changed = true
	}
	if dst.EmailFrom == "" && src.EmailFrom != "" {
		dst.EmailFrom = src.EmailFrom
		changed = true
	}
	if dst.EmailSubject == "" && src.EmailSubject != "" {
		dst.EmailSubject = src.EmailSubject
		changed = true
	}
	return changed
}

// mergeMailFlagGlobals forwards the *bool MailOnlyOnError. Allocates a
// fresh pointer so the live config does not share storage with the GC'd
// scratch.
func mergeMailFlagGlobals(dst, src *middlewares.MailConfig) bool {
	if dst.MailOnlyOnError == nil && src.MailOnlyOnError != nil {
		v := *src.MailOnlyOnError
		dst.MailOnlyOnError = &v
		return true
	}
	return false
}

// mergeSaveGlobals copies operator-tunable Save globals from src into dst.
// Returns true when any field was overwritten.
func mergeSaveGlobals(dst, src *middlewares.SaveConfig) bool {
	changed := false
	if dst.SaveFolder == "" && src.SaveFolder != "" {
		dst.SaveFolder = src.SaveFolder
		changed = true
	}
	if dst.SaveOnlyOnError == nil && src.SaveOnlyOnError != nil {
		v := *src.SaveOnlyOnError
		dst.SaveOnlyOnError = &v
		changed = true
	}
	if dst.RestoreHistory == nil && src.RestoreHistory != nil {
		v := *src.RestoreHistory
		dst.RestoreHistory = &v
		changed = true
	}
	if dst.RestoreHistoryMaxAge == 0 && src.RestoreHistoryMaxAge != 0 {
		dst.RestoreHistoryMaxAge = src.RestoreHistoryMaxAge
		changed = true
	}
	if dst.SaveMode == "" && src.SaveMode != "" {
		dst.SaveMode = src.SaveMode
		changed = true
	}
	if dst.SaveFolderMode == "" && src.SaveFolderMode != "" {
		dst.SaveFolderMode = src.SaveFolderMode
		changed = true
	}
	return changed
}

// mergeSchedulingGlobals copies the four scheduling-flavored globals
// (log-level, max-runtime, notification-cooldown, enable-strict-validation)
// from src into dst. Returns true when any field was overwritten.
//
// MaxRuntime uses the 24h-as-default sentinel (matches the Config struct's
// `default:"24h"` tag and the documented policy in
// mergeWebhookGlobals.PresetCacheTTL). NotificationCooldown uses the
// 0-as-unset sentinel (matches `default:"0"`). LogLevel uses empty-as-unset.
// EnableStrictValidation is a plain bool — see file header for the
// "label can enable, not disable" rationale.
//
// Config.Global is an anonymous struct, so we pass pointers to *Config and
// reach through .Global. Reviewers please note: the helper does NOT touch
// Slack/Mail/Save sub-structs — those have their own per-subsystem helpers
// above. Splitting along subsystem boundaries keeps each helper testable in
// isolation and matches the mergeWebhookGlobals shape.
func mergeSchedulingGlobals(dst, src *Config) bool {
	changed := false
	if dst.Global.LogLevel == "" && src.Global.LogLevel != "" {
		dst.Global.LogLevel = src.Global.LogLevel
		changed = true
	}
	const defaultMaxRuntime = 24 * time.Hour
	if dst.Global.MaxRuntime == defaultMaxRuntime &&
		src.Global.MaxRuntime != 0 &&
		src.Global.MaxRuntime != defaultMaxRuntime {
		dst.Global.MaxRuntime = src.Global.MaxRuntime
		changed = true
	}
	if dst.Global.NotificationCooldown == 0 && src.Global.NotificationCooldown != 0 {
		dst.Global.NotificationCooldown = src.Global.NotificationCooldown
		changed = true
	}
	// EnableStrictValidation: plain bool, label may UPGRADE only.
	if !dst.Global.EnableStrictValidation && src.Global.EnableStrictValidation {
		dst.Global.EnableStrictValidation = true
		changed = true
	}
	return changed
}

// applyAllowListedGlobals forwards every label-decoded, non-webhook global
// allow-listed in cli/docker-labels.go::globalLabelAllowList from the
// scratch config produced by buildFromDockerContainers into the live
// c.Global. It is the structural sibling of mergeWebhookConfigs's
// mergeWebhookGlobals call — see #652. Returns true when any field was
// overwritten so callers can decide whether to re-prep middlewares /
// re-apply log level / re-init notification dedup.
//
// SAFETY: caller must hold c.mu when invoking this on the runtime-reconcile
// path (dockerContainersUpdate). The boot path (mergeJobsFromDockerContainers)
// runs single-threaded inside InitializeApp and does not need the lock.
func (c *Config) applyAllowListedGlobals(parsed *Config) bool {
	if parsed == nil {
		return false
	}
	changed := false
	if mergeSlackGlobals(&c.Global.SlackConfig, &parsed.Global.SlackConfig) {
		changed = true
	}
	if mergeMailGlobals(&c.Global.MailConfig, &parsed.Global.MailConfig) {
		changed = true
	}
	if mergeSaveGlobals(&c.Global.SaveConfig, &parsed.Global.SaveConfig) {
		changed = true
	}
	if mergeSchedulingGlobals(c, parsed) {
		changed = true
	}
	return changed
}

// refreshRuntimeKnobsAfterGlobalMerge re-applies the two process-wide knobs
// (log level, notification deduplicator) that were already initialized from
// the INI side before applyAllowListedGlobals had a chance to forward
// label-decoded overrides into c.Global. Without this:
//
//   - boot path: a container label setting `ofelia.notification-cooldown=30s`
//     reaches c.Global.NotificationCooldown via applyAllowListedGlobals, but
//     initNotificationDedup() already ran with the old (zero) cooldown so
//     c.notificationDedup stays nil for the entire process — same shape of
//     downstream-consumer-wired-too-early bug as #652 itself.
//
//   - runtime path: a container label change to log-level or
//     notification-cooldown updates c.Global but never refreshes the
//     per-process side effects.
//
// `prevLogLevel` and `prevCooldown` are the snapshot the caller took BEFORE
// applyAllowListedGlobals ran; the helper compares against the current
// c.Global to decide which knob actually changed and only re-runs that one.
// This avoids re-initializing the deduplicator (which allocates a fresh map)
// when only Slack/Mail/Save fields changed.
func (c *Config) refreshRuntimeKnobsAfterGlobalMerge(prevLogLevel string, prevCooldown time.Duration) {
	if c.Global.LogLevel != prevLogLevel && c.Global.LogLevel != "" {
		if err := ApplyLogLevel(c.Global.LogLevel, c.levelVar); err != nil {
			c.logger.Warn("Failed to apply log level from container label (using current)",
				"error", err.Error())
		}
	}
	if c.Global.NotificationCooldown != prevCooldown {
		// initNotificationDedup is a no-op when cooldown <= 0, so calling it
		// when a label DISABLES dedup leaves the existing deduplicator in
		// place rather than tearing it down. That asymmetry matches the
		// existing INI live-reload behavior (cli/config.go::iniConfigUpdate
		// also does not tear down the deduplicator) — disabling dedup
		// requires a daemon restart. Documented intentionally.
		c.initNotificationDedup()
	}
}
