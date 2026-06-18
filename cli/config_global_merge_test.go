// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

// TestMergeSlackGlobals_LabelFillsEmpty asserts that label-decoded Slack values
// reach c.Global.SlackConfig when the INI side is empty/zero.
//
// Sibling fix to #650 — see #652. The webhook-only fix did not cover Slack
// globals; mergeJobsFromDockerContainers / dockerContainersUpdate built a
// scratch Config, decoded labels into scratch.Global.SlackConfig, then dropped
// the scratch on the floor. This test pins the new mergeSlackGlobals contract.
func TestMergeSlackGlobals_LabelFillsEmpty(t *testing.T) {
	t.Parallel()

	dst := middlewares.SlackConfig{}
	srcOnly := true
	src := middlewares.SlackConfig{
		SlackWebhook:     "https://hooks.slack.example/svc/T/B/X",
		SlackOnlyOnError: &srcOnly,
	}

	changed := mergeSlackGlobals(&dst, &src)

	assert.True(t, changed, "label-only Slack values must report changed=true")
	assert.Equal(t, "https://hooks.slack.example/svc/T/B/X", dst.SlackWebhook,
		"empty INI SlackWebhook must inherit from label (#652)")
	require.NotNil(t, dst.SlackOnlyOnError, "empty INI SlackOnlyOnError must inherit pointer from label")
	assert.True(t, *dst.SlackOnlyOnError, "inherited SlackOnlyOnError must preserve label value")
}

// TestMergeSlackGlobals_INIWins asserts that an operator-set INI Slack value
// is NOT clobbered by a container label. Mirrors the
// mergeWebhookGlobals AllowedHosts="*" precedence policy.
func TestMergeSlackGlobals_INIWins(t *testing.T) {
	t.Parallel()

	iniOnly := false
	dst := middlewares.SlackConfig{
		SlackWebhook:     "https://ini.example/hook",
		SlackOnlyOnError: &iniOnly,
	}
	srcOnly := true
	src := middlewares.SlackConfig{
		SlackWebhook:     "https://label.example/hook",
		SlackOnlyOnError: &srcOnly,
	}

	changed := mergeSlackGlobals(&dst, &src)

	assert.False(t, changed, "label values must not flip changed when INI already set everything")
	assert.Equal(t, "https://ini.example/hook", dst.SlackWebhook,
		"INI-set SlackWebhook must take precedence over label (#652)")
	require.NotNil(t, dst.SlackOnlyOnError)
	assert.False(t, *dst.SlackOnlyOnError, "INI-set SlackOnlyOnError must take precedence over label")
}

// TestMergeMailGlobals_LabelFillsEmpty pins the SMTP fields, recipient fields,
// and the MailOnlyOnError pointer. Plain bool SMTPTLSSkipVerify uses the
// "label can enable, not disable" policy mirrored from mergeMailDefaults.
func TestMergeMailGlobals_LabelFillsEmpty(t *testing.T) {
	t.Parallel()

	dst := middlewares.MailConfig{}
	srcOnly := true
	src := middlewares.MailConfig{
		SMTPHost:          "mail.example.com",
		SMTPPort:          587,
		SMTPUser:          "ofelia",
		SMTPPassword:      "s3cret",
		SMTPTLSSkipVerify: true,
		EmailTo:           "ops@example.com",
		EmailFrom:         "ofelia@example.com",
		EmailSubject:      "[ofelia] {{.Name}}",
		MailOnlyOnError:   &srcOnly,
	}

	changed := mergeMailGlobals(&dst, &src)

	assert.True(t, changed, "label-only Mail values must report changed=true")
	assert.Equal(t, "mail.example.com", dst.SMTPHost, "empty INI SMTPHost must inherit from label (#652)")
	assert.Equal(t, 587, dst.SMTPPort)
	assert.Equal(t, "ofelia", dst.SMTPUser)
	assert.Equal(t, "s3cret", dst.SMTPPassword)
	assert.True(t, dst.SMTPTLSSkipVerify, "label can ENABLE SMTPTLSSkipVerify when INI is false (parity with mergeMailDefaults)")
	assert.Equal(t, "ops@example.com", dst.EmailTo)
	assert.Equal(t, "ofelia@example.com", dst.EmailFrom)
	assert.Equal(t, "[ofelia] {{.Name}}", dst.EmailSubject)
	require.NotNil(t, dst.MailOnlyOnError)
	assert.True(t, *dst.MailOnlyOnError)
}

// TestMergeMailGlobals_INIWins pins the precedence half: explicit INI values
// for every field must survive a label-defined service container.
func TestMergeMailGlobals_INIWins(t *testing.T) {
	t.Parallel()

	iniOnly := false
	dst := middlewares.MailConfig{
		SMTPHost:          "ini.example.com",
		SMTPPort:          25,
		SMTPUser:          "ini-user",
		SMTPPassword:      "ini-pw",
		SMTPTLSSkipVerify: false,
		EmailTo:           "ini-to@example.com",
		EmailFrom:         "ini-from@example.com",
		EmailSubject:      "INI subject",
		MailOnlyOnError:   &iniOnly,
	}
	srcOnly := true
	src := middlewares.MailConfig{
		SMTPHost:          "label.example.com",
		SMTPPort:          2525,
		SMTPUser:          "label-user",
		SMTPPassword:      "label-pw",
		SMTPTLSSkipVerify: true, // label cannot disable; but here INI=false, label=true — see test below
		EmailTo:           "label-to@example.com",
		EmailFrom:         "label-from@example.com",
		EmailSubject:      "label subject",
		MailOnlyOnError:   &srcOnly,
	}

	changed := mergeMailGlobals(&dst, &src)

	// Note: SMTPTLSSkipVerify is the documented exception. Mirroring
	// mergeMailDefaults' policy (cli/config.go), a label may UPGRADE an INI
	// false→true (insecure) but cannot downgrade. This is intentionally
	// asymmetric and is documented inline in mergeMailGlobals.
	assert.True(t, changed, "SMTPTLSSkipVerify upgrade must report changed=true even though every other field is INI-protected")
	assert.Equal(t, "ini.example.com", dst.SMTPHost, "INI SMTPHost wins (#652)")
	assert.Equal(t, 25, dst.SMTPPort)
	assert.Equal(t, "ini-user", dst.SMTPUser)
	assert.Equal(t, "ini-pw", dst.SMTPPassword)
	assert.True(t, dst.SMTPTLSSkipVerify, "label may flip SMTPTLSSkipVerify false→true (parity with mergeMailDefaults)")
	assert.Equal(t, "ini-to@example.com", dst.EmailTo)
	assert.Equal(t, "ini-from@example.com", dst.EmailFrom)
	assert.Equal(t, "INI subject", dst.EmailSubject)
	require.NotNil(t, dst.MailOnlyOnError)
	assert.False(t, *dst.MailOnlyOnError, "INI-set MailOnlyOnError must take precedence over label")
}

// TestMergeMailGlobals_TLSSkipVerify_LabelCannotDisable pins the asymmetry:
// when INI explicitly enabled SMTPTLSSkipVerify (insecure), a label cannot
// downgrade it to false. This matches the documented policy in
// mergeMailDefaults (cli/config.go around mergeMailDefaults). The reverse
// asymmetry — label upgrading INI false→true — is covered in the INIWins test.
func TestMergeMailGlobals_TLSSkipVerify_LabelCannotDisable(t *testing.T) {
	t.Parallel()

	dst := middlewares.MailConfig{SMTPTLSSkipVerify: true}
	src := middlewares.MailConfig{SMTPTLSSkipVerify: false}

	changed := mergeMailGlobals(&dst, &src)

	assert.False(t, changed, "label cannot DISABLE SMTPTLSSkipVerify; nothing to report")
	assert.True(t, dst.SMTPTLSSkipVerify,
		"label must NOT downgrade SMTPTLSSkipVerify from true→false (mirrors mergeMailDefaults policy)")
}

// TestMergeSaveGlobals_LabelFillsEmpty covers SaveFolder + the three pointer/
// duration fields gated by the SaveConfig allow-list entries.
func TestMergeSaveGlobals_LabelFillsEmpty(t *testing.T) {
	t.Parallel()

	dst := middlewares.SaveConfig{}
	srcOnly := true
	srcRestore := false
	src := middlewares.SaveConfig{
		SaveFolder:           "/var/log/ofelia",
		SaveOnlyOnError:      &srcOnly,
		RestoreHistory:       &srcRestore,
		RestoreHistoryMaxAge: 12 * time.Hour,
		SaveMode:             "0644",
		SaveFolderMode:       "0755",
	}

	changed := mergeSaveGlobals(&dst, &src)

	assert.True(t, changed, "label-only Save values must report changed=true")
	assert.Equal(t, "/var/log/ofelia", dst.SaveFolder, "empty INI SaveFolder must inherit from label (#652)")
	require.NotNil(t, dst.SaveOnlyOnError)
	assert.True(t, *dst.SaveOnlyOnError)
	require.NotNil(t, dst.RestoreHistory)
	assert.False(t, *dst.RestoreHistory, "label-set RestoreHistory pointer must be inherited verbatim, including false")
	assert.Equal(t, 12*time.Hour, dst.RestoreHistoryMaxAge)
	assert.Equal(t, "0644", dst.SaveMode, "empty INI save-mode must inherit from label")
	assert.Equal(t, "0755", dst.SaveFolderMode, "empty INI save-folder-mode must inherit from label")
}

// TestMergeSaveGlobals_INIWins pins the precedence half for Save fields.
func TestMergeSaveGlobals_INIWins(t *testing.T) {
	t.Parallel()

	iniOnly := false
	iniRestore := true
	dst := middlewares.SaveConfig{
		SaveFolder:           "/ini/save",
		SaveOnlyOnError:      &iniOnly,
		RestoreHistory:       &iniRestore,
		RestoreHistoryMaxAge: 6 * time.Hour,
		SaveMode:             "0600",
		SaveFolderMode:       "0750",
	}
	srcOnly := true
	srcRestore := false
	src := middlewares.SaveConfig{
		SaveFolder:           "/label/save",
		SaveOnlyOnError:      &srcOnly,
		RestoreHistory:       &srcRestore,
		RestoreHistoryMaxAge: 48 * time.Hour,
		SaveMode:             "0644",
		SaveFolderMode:       "0755",
	}

	changed := mergeSaveGlobals(&dst, &src)

	assert.False(t, changed, "all fields INI-set; merge must be a no-op")
	assert.Equal(t, "/ini/save", dst.SaveFolder, "INI SaveFolder wins (#652)")
	require.NotNil(t, dst.SaveOnlyOnError)
	assert.False(t, *dst.SaveOnlyOnError)
	require.NotNil(t, dst.RestoreHistory)
	assert.True(t, *dst.RestoreHistory)
	assert.Equal(t, 6*time.Hour, dst.RestoreHistoryMaxAge)
	assert.Equal(t, "0600", dst.SaveMode, "INI save-mode wins over label")
	assert.Equal(t, "0750", dst.SaveFolderMode, "INI save-folder-mode wins over label")
}

// TestMergeSchedulingGlobals_LabelFillsEmpty pins log-level / max-runtime /
// notification-cooldown / enable-strict-validation. MaxRuntime uses a
// 24h-as-default sentinel mirroring mergeWebhookGlobals' PresetCacheTTL policy.
// EnableStrictValidation uses the same "label can enable, not disable" pattern
// as SMTPTLSSkipVerify because it is a plain bool.
func TestMergeSchedulingGlobals_LabelFillsEmpty(t *testing.T) {
	t.Parallel()

	// dst built by NewConfig defaults: MaxRuntime=24h, NotificationCooldown=0,
	// EnableStrictValidation=false, LogLevel="".
	c := NewConfig(test.NewTestLogger())
	require.Equal(t, 24*time.Hour, c.Global.MaxRuntime,
		"baseline: NewConfig must seed MaxRuntime to documented default")
	require.Equal(t, time.Duration(0), c.Global.NotificationCooldown)
	require.False(t, c.Global.EnableStrictValidation)
	require.Empty(t, c.Global.LogLevel)

	src := newScratchConfig(c) // value-copy mirrors production scratch
	src.Global.LogLevel = "debug"
	src.Global.MaxRuntime = 2 * time.Hour
	src.Global.NotificationCooldown = 30 * time.Second
	src.Global.EnableStrictValidation = true

	changed := mergeSchedulingGlobals(c, src)

	assert.True(t, changed, "label-only scheduling values must report changed=true")
	assert.Equal(t, "debug", c.Global.LogLevel, "empty INI LogLevel must inherit from label (#652)")
	assert.Equal(t, 2*time.Hour, c.Global.MaxRuntime,
		"INI MaxRuntime at default 24h sentinel must accept label override (#652)")
	assert.Equal(t, 30*time.Second, c.Global.NotificationCooldown)
	assert.True(t, c.Global.EnableStrictValidation,
		"label can ENABLE EnableStrictValidation (parity with mergeMailDefaults SMTPTLSSkipVerify policy)")
}

// TestMergeSchedulingGlobals_INIWins pins precedence: an operator who set any
// of these fields in the [global] INI section must NOT see them clobbered by a
// container label.
func TestMergeSchedulingGlobals_INIWins(t *testing.T) {
	t.Parallel()

	c := NewConfig(test.NewTestLogger())
	c.Global.LogLevel = "warn"
	c.Global.MaxRuntime = 4 * time.Hour // != 24h default → operator-set sentinel
	c.Global.NotificationCooldown = 10 * time.Second
	c.Global.EnableStrictValidation = true

	src := newScratchConfig(c)
	src.Global.LogLevel = "debug"
	src.Global.MaxRuntime = 2 * time.Hour
	src.Global.NotificationCooldown = 60 * time.Second
	src.Global.EnableStrictValidation = false // label cannot disable

	changed := mergeSchedulingGlobals(c, src)

	assert.False(t, changed, "all fields INI-set; merge must be a no-op")
	assert.Equal(t, "warn", c.Global.LogLevel, "INI LogLevel wins (#652)")
	assert.Equal(t, 4*time.Hour, c.Global.MaxRuntime)
	assert.Equal(t, 10*time.Second, c.Global.NotificationCooldown)
	assert.True(t, c.Global.EnableStrictValidation,
		"label cannot DISABLE EnableStrictValidation once INI enabled it")
}

// TestSyncOnLabelMutation_Slack covers the runtime label-reconcile path for
// Slack globals. Mirrors TestSyncWebhookConfigs_PresetCacheTTLOnLabelMutation.
// Asserts that mutating an ofelia.slack-webhook label on a service container
// propagates into c.Global.SlackConfig at next dockerContainersUpdate.
func TestSyncOnLabelMutation_Slack(t *testing.T) {
	t.Parallel()

	c := NewConfig(test.NewTestLogger())
	require.Empty(t, c.Global.SlackWebhook, "baseline: empty INI Slack")

	parsed := newScratchConfig(c)
	parsed.Global.SlackWebhook = "https://hooks.slack.example/svc/T/B/X"

	changed := c.applyAllowListedGlobals(parsed)

	assert.True(t, changed, "applyAllowListedGlobals must report changed=true on Slack mutation")
	assert.Equal(t, "https://hooks.slack.example/svc/T/B/X", c.Global.SlackWebhook,
		"runtime label reconcile must forward Slack globals (#652)")
}

// TestSyncOnLabelMutation_Mail covers the runtime label-reconcile path for
// Mail globals — the headline regression from #652. Setting
// ofelia.smtp-host=mail.example.com on a service container must update
// c.Global.MailConfig.SMTPHost so downstream mergeNotificationDefaults sees it.
func TestSyncOnLabelMutation_Mail(t *testing.T) {
	t.Parallel()

	c := NewConfig(test.NewTestLogger())
	require.Empty(t, c.Global.SMTPHost, "baseline: empty INI Mail")

	parsed := newScratchConfig(c)
	parsed.Global.SMTPHost = "mail.example.com"
	parsed.Global.SMTPPort = 587

	changed := c.applyAllowListedGlobals(parsed)

	assert.True(t, changed, "applyAllowListedGlobals must report changed=true on Mail mutation")
	assert.Equal(t, "mail.example.com", c.Global.SMTPHost,
		"runtime label reconcile must forward Mail globals (#652) — headline regression")
	assert.Equal(t, 587, c.Global.SMTPPort)
}

// TestSyncOnLabelMutation_Save covers the runtime label-reconcile path for
// Save globals.
func TestSyncOnLabelMutation_Save(t *testing.T) {
	t.Parallel()

	c := NewConfig(test.NewTestLogger())
	require.Empty(t, c.Global.SaveFolder, "baseline: empty INI Save")

	parsed := newScratchConfig(c)
	parsed.Global.SaveFolder = "/var/log/ofelia"
	srcOnly := true
	parsed.Global.SaveOnlyOnError = &srcOnly

	changed := c.applyAllowListedGlobals(parsed)

	assert.True(t, changed, "applyAllowListedGlobals must report changed=true on Save mutation")
	assert.Equal(t, "/var/log/ofelia", c.Global.SaveFolder,
		"runtime label reconcile must forward Save globals (#652)")
	require.NotNil(t, c.Global.SaveOnlyOnError)
	assert.True(t, *c.Global.SaveOnlyOnError)
}

// TestSyncOnLabelMutation_Scheduling covers the runtime label-reconcile path
// for scheduling globals.
func TestSyncOnLabelMutation_Scheduling(t *testing.T) {
	t.Parallel()

	c := NewConfig(test.NewTestLogger())
	require.Empty(t, c.Global.LogLevel, "baseline: empty INI LogLevel")
	require.Equal(t, 24*time.Hour, c.Global.MaxRuntime, "baseline: default MaxRuntime")

	parsed := newScratchConfig(c)
	parsed.Global.LogLevel = "debug"
	parsed.Global.MaxRuntime = 90 * time.Minute
	parsed.Global.NotificationCooldown = 15 * time.Second
	parsed.Global.EnableStrictValidation = true

	changed := c.applyAllowListedGlobals(parsed)

	assert.True(t, changed, "applyAllowListedGlobals must report changed=true on scheduling mutation")
	assert.Equal(t, "debug", c.Global.LogLevel,
		"runtime label reconcile must forward LogLevel (#652)")
	assert.Equal(t, 90*time.Minute, c.Global.MaxRuntime)
	assert.Equal(t, 15*time.Second, c.Global.NotificationCooldown)
	assert.True(t, c.Global.EnableStrictValidation)
}

// TestRefreshRuntimeKnobs_BootCooldown covers the boot-path gap that the
// initial fix surfaced (advisor catch). InitializeApp calls
// initNotificationDedup() with the INI-only value BEFORE
// mergeJobsFromDockerContainers runs applyAllowListedGlobals. Without the
// post-merge refresh, a label-supplied notification-cooldown reaches
// c.Global but c.notificationDedup stays nil for the entire process — same
// shape as the headline #652 regression but for a process-wide knob rather
// than a per-job middleware field.
func TestRefreshRuntimeKnobs_BootCooldown(t *testing.T) {
	// NOT parallel: middlewares.InitNotificationDedup mutates a package-global
	// state — running concurrently with TestRefreshRuntimeKnobs_NoOp_WhenUnchanged
	// (and any other test that calls initNotificationDedup) trips the race
	// detector. The two tests touch a small surface; serial execution costs ~0.
	c := NewConfig(test.NewTestLogger())
	require.Equal(t, time.Duration(0), c.Global.NotificationCooldown,
		"baseline: NewConfig has cooldown=0 → dedup disabled")
	require.Nil(t, c.notificationDedup,
		"baseline: c.notificationDedup must be nil before init")

	// Simulate the boot ordering: initNotificationDedup runs FIRST with the
	// INI-only value (cooldown=0, no-op), then a label sets cooldown=30s.
	c.initNotificationDedup()
	require.Nil(t, c.notificationDedup,
		"initNotificationDedup with cooldown=0 must remain a no-op")

	prevCooldown := c.Global.NotificationCooldown
	c.Global.NotificationCooldown = 30 * time.Second
	c.refreshRuntimeKnobsAfterGlobalMerge("", prevCooldown)

	require.NotNil(t, c.notificationDedup,
		"refreshRuntimeKnobsAfterGlobalMerge must (re-)init the deduplicator after a cooldown change (#652)")
}

// TestRefreshRuntimeKnobs_NoOp_WhenUnchanged pins that the helper does NOT
// allocate a fresh deduplicator on every reconcile when only Slack/Mail/Save
// fields changed. A spurious re-init would lose in-flight dedup state.
func TestRefreshRuntimeKnobs_NoOp_WhenUnchanged(t *testing.T) {
	// NOT parallel — see TestRefreshRuntimeKnobs_BootCooldown.
	c := NewConfig(test.NewTestLogger())
	c.Global.NotificationCooldown = 10 * time.Second
	c.initNotificationDedup()
	require.NotNil(t, c.notificationDedup, "baseline: dedup initialized")
	originalDedup := c.notificationDedup

	// Cooldown unchanged from prev snapshot — refresh must NOT re-init.
	c.refreshRuntimeKnobsAfterGlobalMerge("", 10*time.Second)

	assert.Same(t, originalDedup, c.notificationDedup,
		"refreshRuntimeKnobsAfterGlobalMerge must NOT re-init dedup when cooldown didn't change")
}

// TestApplyAllowListedGlobals_NoOp_WhenAllINISet mirrors the precedence
// helper tests but at the integration level: when every allow-listed global
// is INI-set, a label reconcile must be a no-op (no changed flag, no
// over-writes). Defends against a future refactor accidentally inverting
// the "INI wins" rule.
func TestApplyAllowListedGlobals_NoOp_WhenAllINISet(t *testing.T) {
	t.Parallel()

	c := NewConfig(test.NewTestLogger())
	c.Global.SlackWebhook = "https://ini.slack.example/hook"
	c.Global.SMTPHost = "ini.smtp.example.com"
	c.Global.SaveFolder = "/ini/save"
	c.Global.LogLevel = "warn"
	c.Global.MaxRuntime = 12 * time.Hour
	c.Global.NotificationCooldown = 5 * time.Second
	c.Global.EnableStrictValidation = true

	parsed := newScratchConfig(c)
	// Override every label to a different value — INI must win. Note:
	// newScratchConfig value-copies c.Global, so EnableStrictValidation
	// arrives at parsed.Global as `true` already. We explicitly set it back
	// to `false` so this test ALSO exercises the "label cannot DISABLE"
	// asymmetry on the integration path (not just the unit-level test in
	// TestMergeSchedulingGlobals_INIWins).
	parsed.Global.SlackWebhook = "https://label.slack.example/hook"
	parsed.Global.SMTPHost = "label.smtp.example.com"
	parsed.Global.SaveFolder = "/label/save"
	parsed.Global.LogLevel = "debug"
	parsed.Global.MaxRuntime = 6 * time.Hour
	parsed.Global.NotificationCooldown = 60 * time.Second
	parsed.Global.EnableStrictValidation = false // exercise "label cannot disable"

	changed := c.applyAllowListedGlobals(parsed)

	assert.False(t, changed, "all allow-listed fields INI-set; reconcile must be a no-op")
	assert.Equal(t, "https://ini.slack.example/hook", c.Global.SlackWebhook)
	assert.Equal(t, "ini.smtp.example.com", c.Global.SMTPHost)
	assert.Equal(t, "/ini/save", c.Global.SaveFolder)
	assert.Equal(t, "warn", c.Global.LogLevel)
	assert.Equal(t, 12*time.Hour, c.Global.MaxRuntime)
	assert.Equal(t, 5*time.Second, c.Global.NotificationCooldown)
	assert.True(t, c.Global.EnableStrictValidation)
}
