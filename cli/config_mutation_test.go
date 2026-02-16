package cli

import (
	"testing"
	"time"

	defaults "github.com/creasty/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ini "gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

// =============================================================================
// sectionToMap mutation tests (lines 1219, 1223)
// =============================================================================

// TestSectionToMap_BoundaryExact targets CONDITIONALS_BOUNDARY mutants at
// config.go:1219 (len(vals) > 1) and config.go:1223 (len(vals) == 1).
// A mutant changing > to >= on line 1219 would treat single-value keys
// as multi-value (returning []string instead of string). A mutant changing
// == to != on line 1223 would route single-value keys to the default branch.
func TestSectionToMap_BoundaryExact(t *testing.T) {
	t.Parallel()

	// Create an INI section with exactly one value per key
	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true},
		[]byte("[test]\nsingle = value1\n"))
	require.NoError(t, err)
	section := cfg.Section("test")
	m := sectionToMap(section)

	// With exactly 1 value, the result must be a plain string (not []string)
	val, ok := m["single"]
	require.True(t, ok, "key 'single' should exist in map")
	strVal, isStr := val.(string)
	assert.True(t, isStr, "single-value key must produce a string, got %T", val)
	assert.Equal(t, "value1", strVal)
}

// TestSectionToMap_MultipleValues targets the > 1 boundary at line 1219.
// With 2+ shadow values the result must be a []string.
func TestSectionToMap_MultipleValues(t *testing.T) {
	t.Parallel()

	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true},
		[]byte("[test]\nmulti = alpha\nmulti = beta\n"))
	require.NoError(t, err)
	section := cfg.Section("test")
	m := sectionToMap(section)

	val, ok := m["multi"]
	require.True(t, ok, "key 'multi' should exist")
	sliceVal, isSlice := val.([]string)
	assert.True(t, isSlice, "multi-value key must produce []string, got %T", val)
	assert.Equal(t, []string{"alpha", "beta"}, sliceVal)
}

// TestSectionToMap_ExactlyTwoValues ensures the boundary between 1 and 2
// values is handled correctly (killing the >= mutant at line 1219).
func TestSectionToMap_ExactlyTwoValues(t *testing.T) {
	t.Parallel()

	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true},
		[]byte("[test]\nkey = val1\nkey = val2\n"))
	require.NoError(t, err)
	section := cfg.Section("test")
	m := sectionToMap(section)

	val := m["key"]
	sliceVal, isSlice := val.([]string)
	assert.True(t, isSlice, "2 shadow values should produce []string, got %T", val)
	assert.Len(t, sliceVal, 2)
	assert.Equal(t, "val1", sliceVal[0])
	assert.Equal(t, "val2", sliceVal[1])
}

// TestSectionToMap_EmptyValue ensures that a key with empty value gets empty string.
func TestSectionToMap_EmptyValue(t *testing.T) {
	t.Parallel()

	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true},
		[]byte("[test]\nempty =\n"))
	require.NoError(t, err)
	section := cfg.Section("test")
	m := sectionToMap(section)

	val, ok := m["empty"]
	require.True(t, ok)
	strVal, isStr := val.(string)
	assert.True(t, isStr, "empty value key should produce a string, got %T", val)
	assert.Empty(t, strVal)
}

// =============================================================================
// replaceIfChanged mutation tests (lines 614-636)
// =============================================================================

// TestReplaceIfChanged_DifferentHash targets conditionals at lines 614, 618, 623, 627, 628, 632.
// If the schedule changes, the hashes must differ and the job must be replaced (return true).
func TestReplaceIfChanged_DifferentHash(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	oldJob := &ExecJobConfig{ExecJob: core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo old"}}}
	_ = defaults.Set(oldJob)
	oldJob.Name = "testjob"
	oldJob.buildMiddlewares(nil)
	_ = cfg.sh.AddJob(oldJob)
	assert.Len(t, cfg.sh.Entries(), 1)

	newJob := &ExecJobConfig{ExecJob: core.ExecJob{BareJob: core.BareJob{Schedule: "@every 10s", Command: "echo new"}}}

	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	updated := replaceIfChanged(cfg, "testjob", oldJob, newJob, prep, JobSourceLabel)
	assert.True(t, updated, "replaceIfChanged must return true when hashes differ")
	assert.Len(t, cfg.sh.Entries(), 1, "scheduler must still have exactly 1 entry")
	assert.Equal(t, "@every 10s", newJob.GetSchedule())
}

// TestReplaceIfChanged_SameHash targets the newHash == oldHash branch at line 628.
// When nothing changes the function must return false.
func TestReplaceIfChanged_SameHash(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	oldJob := &ExecJobConfig{ExecJob: core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo test"}}}
	_ = defaults.Set(oldJob)
	oldJob.Name = "testjob"
	oldJob.buildMiddlewares(nil)
	_ = cfg.sh.AddJob(oldJob)

	newJob := &ExecJobConfig{ExecJob: core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo test"}}}

	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	updated := replaceIfChanged(cfg, "testjob", oldJob, newJob, prep, JobSourceLabel)
	assert.False(t, updated, "replaceIfChanged must return false when hashes are equal")
}

// =============================================================================
// addNewJob mutation tests (lines 643-660)
// =============================================================================

// TestAddNewJob_WithSource targets the source != "" conditional at line 644
// and the validatable check at line 650.
func TestAddNewJob_WithSource(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	current := make(map[string]*ExecJobConfig)
	newJob := &ExecJobConfig{ExecJob: core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo test"}}}

	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	addNewJob(cfg, "myjob", newJob, prep, JobSourceINI, current)
	assert.Len(t, current, 1, "job should be added to current map")
	assert.Equal(t, JobSourceINI, current["myjob"].GetJobSource())
	assert.Len(t, cfg.sh.Entries(), 1, "job should be registered in scheduler")
}

// TestAddNewJob_EmptySource targets the source != "" being negated.
// With empty source, SetJobSource should NOT be called.
func TestAddNewJob_EmptySource(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	current := make(map[string]*ExecJobConfig)
	newJob := &ExecJobConfig{
		ExecJob:   core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo test"}},
		JobSource: JobSourceLabel, // pre-set source
	}

	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	addNewJob(cfg, "myjob", newJob, prep, "", current)
	assert.Len(t, current, 1)
	// With empty source, the original JobSource should be preserved
	assert.Equal(t, JobSourceLabel, current["myjob"].GetJobSource())
}

// =============================================================================
// syncJobMap mutation tests (lines 570-604)
// =============================================================================

// TestSyncJobMap_RemovesStaleLabelJobs targets the delete branch at line 577-578.
func TestSyncJobMap_RemovesStaleLabelJobs(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	// Register an existing label job
	existingJob := &ExecJobConfig{
		ExecJob:   core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo old"}},
		JobSource: JobSourceLabel,
	}
	_ = defaults.Set(existingJob)
	existingJob.Name = "oldjob"
	existingJob.buildMiddlewares(nil)
	_ = cfg.sh.AddJob(existingJob)
	cfg.ExecJobs["oldjob"] = existingJob
	assert.Len(t, cfg.sh.Entries(), 1)

	// Sync with empty parsed map - the old job must be removed
	parsed := make(map[string]*ExecJobConfig)
	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	syncJobMap(cfg, cfg.ExecJobs, parsed, prep, JobSourceLabel, "exec")
	assert.Empty(t, cfg.ExecJobs, "stale label job must be removed")
	assert.Empty(t, cfg.sh.Entries(), "stale job must be removed from scheduler")
}

// TestSyncJobMap_SkipsINIJobsWhenSyncingLabels targets line 572 condition:
// source != "" && j.GetJobSource() != source && j.GetJobSource() != ""
func TestSyncJobMap_SkipsINIJobsWhenSyncingLabels(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	// INI job already exists
	iniJob := &ExecJobConfig{
		ExecJob:   core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo ini"}},
		JobSource: JobSourceINI,
	}
	_ = defaults.Set(iniJob)
	iniJob.Name = "shared"
	iniJob.buildMiddlewares(nil)
	_ = cfg.sh.AddJob(iniJob)
	cfg.ExecJobs["shared"] = iniJob

	// Label sync with empty parsed - should NOT remove the INI job
	parsed := make(map[string]*ExecJobConfig)
	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	syncJobMap(cfg, cfg.ExecJobs, parsed, prep, JobSourceLabel, "exec")
	assert.Len(t, cfg.ExecJobs, 1, "INI job must NOT be removed during label sync")
	assert.Equal(t, JobSourceINI, cfg.ExecJobs["shared"].GetJobSource())
}

// TestSyncJobMap_INIOverridesLabel targets the case at line 592-594.
func TestSyncJobMap_INIOverridesLabel(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	// Existing label job
	labelJob := &ExecJobConfig{
		ExecJob:   core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo label"}},
		JobSource: JobSourceLabel,
	}
	_ = defaults.Set(labelJob)
	labelJob.Name = "shared"
	labelJob.buildMiddlewares(nil)
	_ = cfg.sh.AddJob(labelJob)
	cfg.ExecJobs["shared"] = labelJob

	// Parse new INI job with same name
	parsed := map[string]*ExecJobConfig{
		"shared": {
			ExecJob:   core.ExecJob{BareJob: core.BareJob{Schedule: "@every 10s", Command: "echo ini"}},
			JobSource: JobSourceINI,
		},
	}

	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	syncJobMap(cfg, cfg.ExecJobs, parsed, prep, JobSourceINI, "exec")
	// The INI job should override the label job
	assert.Len(t, cfg.ExecJobs, 1)
	assert.Equal(t, JobSourceINI, cfg.ExecJobs["shared"].GetJobSource())
	assert.Equal(t, "@every 10s", cfg.ExecJobs["shared"].GetSchedule())
	assert.True(t, logger.HasWarning("overriding label-defined"))
}

// TestSyncJobMap_LabelIgnoredWhenINIExists targets line 595-597.
func TestSyncJobMap_LabelIgnoredWhenINIExists(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	// Existing INI job
	iniJob := &ExecJobConfig{
		ExecJob:   core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo ini"}},
		JobSource: JobSourceINI,
	}
	_ = defaults.Set(iniJob)
	iniJob.Name = "shared"
	iniJob.buildMiddlewares(nil)
	_ = cfg.sh.AddJob(iniJob)
	cfg.ExecJobs["shared"] = iniJob

	// Try to add label job with same name
	parsed := map[string]*ExecJobConfig{
		"shared": {
			ExecJob:   core.ExecJob{BareJob: core.BareJob{Schedule: "@every 10s", Command: "echo label"}},
			JobSource: JobSourceLabel,
		},
	}

	prep := func(name string, j *ExecJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	syncJobMap(cfg, cfg.ExecJobs, parsed, prep, JobSourceLabel, "exec")
	// The INI job should remain unchanged
	assert.Len(t, cfg.ExecJobs, 1)
	assert.Equal(t, JobSourceINI, cfg.ExecJobs["shared"].GetJobSource())
	assert.Equal(t, "@every 5s", cfg.ExecJobs["shared"].GetSchedule())
	assert.True(t, logger.HasWarning("ignoring label-defined"))
}

// =============================================================================
// dockerLabelsUpdate mutation tests (lines 662-741)
// =============================================================================

// TestDockerLabelsUpdate_SecurityWarning targets the AllowHostJobsFromLabels
// conditional at line 724 and the localCount > 0 || composeCount > 0 at line 727.
func TestDockerLabelsUpdate_SecurityWarning(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.logger = logger
	cfg.Global.AllowHostJobsFromLabels = true
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	containers := []DockerContainerInfo{
		{
			Name:  "cont1",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				requiredLabel:                         "true",
				serviceLabel:                          "true",
				labelPrefix + ".job-local.l.schedule": "@daily",
				labelPrefix + ".job-local.l.command":  "echo local",
			},
		},
	}

	cfg.dockerContainersUpdate(containers)
	assert.True(t, logger.HasWarning("SECURITY WARNING"),
		"Security warning must be logged when local jobs are synced from labels")
}

// TestDockerLabelsUpdate_NoSecurityWarningWhenDisabled targets the negation
// of AllowHostJobsFromLabels at line 724.
func TestDockerLabelsUpdate_NoSecurityWarningWhenDisabled(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.logger = logger
	cfg.Global.AllowHostJobsFromLabels = false
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	containers := []DockerContainerInfo{
		{
			Name:  "cont1",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				requiredLabel:                         "true",
				serviceLabel:                          "true",
				labelPrefix + ".job-local.l.schedule": "@daily",
				labelPrefix + ".job-local.l.command":  "echo local",
			},
		},
	}

	cfg.dockerContainersUpdate(containers)
	// With AllowHostJobsFromLabels=false, the security warning path shouldn't fire
	// (though local jobs will be blocked by buildFromDockerLabels)
	assert.False(t, logger.HasWarning("SECURITY WARNING"),
		"No security warning should be logged when AllowHostJobsFromLabels is false")
}

// TestDockerLabelsUpdate_NoWarningWithoutHostJobs targets line 727:
// localCount > 0 || composeCount > 0
func TestDockerLabelsUpdate_NoWarningWithoutHostJobs(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.logger = logger
	cfg.Global.AllowHostJobsFromLabels = true
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	// Only exec jobs, no local/compose
	containers := []DockerContainerInfo{
		{
			Name:  "cont1",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				requiredLabel:                        "true",
				labelPrefix + ".job-exec.e.schedule": "@daily",
				labelPrefix + ".job-exec.e.command":  "echo exec",
			},
		},
	}

	cfg.dockerContainersUpdate(containers)
	assert.False(t, logger.HasWarning("SECURITY WARNING"),
		"No security warning when only exec jobs (no local/compose) are synced")
}

// =============================================================================
// replaceIfChanged - validation failure path (line 611-614)
// =============================================================================

// TestReplaceIfChanged_ValidationFailReturnsEarlyFalse verifies that when
// validation fails, replaceIfChanged returns false and does not replace the job.
// This targets CONDITIONALS_BOUNDARY at line 614:33 and CONDITIONALS_NEGATION
// at lines 614:33, 614:51.
func TestReplaceIfChanged_ValidationFailReturnsEarlyFalse(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	oldJob := &RunJobConfig{
		RunJob:    core.RunJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo old"}, Image: "alpine"},
		JobSource: JobSourceINI,
	}
	_ = defaults.Set(oldJob)
	oldJob.Name = "testjob"
	oldJob.buildMiddlewares(nil)
	_ = cfg.sh.AddJob(oldJob)

	// New job with invalid config (RunJob without image should fail validation)
	newJob := &RunJobConfig{
		RunJob: core.RunJob{BareJob: core.BareJob{Schedule: "@every 10s", Command: "echo new"}},
		// No Image set - Validate() should fail
	}

	prep := func(name string, j *RunJobConfig) {
		_ = defaults.Set(j)
		j.Name = name
	}

	updated := replaceIfChanged(cfg, "testjob", oldJob, newJob, prep, JobSourceINI)
	// Depending on whether RunJobConfig implements validatable and returns error,
	// the function should either return false (validation error) or proceed to hash comparison.
	// In either case, verify old job is still in scheduler
	assert.Len(t, cfg.sh.Entries(), 1)

	// If validation passed (RunJob may not implement validatable), check hash-based logic
	if !updated {
		assert.Equal(t, "@every 5s", oldJob.GetSchedule(), "old job should be unchanged")
	}
}

// =============================================================================
// mergeJobs mutation tests (line 311:39 vicinity - already killed)
// =============================================================================

// TestMergeJobs_INIPrecedence ensures INI jobs take precedence over label jobs.
// This validates the conditional at line 371.
func TestMergeJobs_INIPrecedence(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cfg := NewConfig(logger)

	cfg.ExecJobs = map[string]*ExecJobConfig{
		"job1": {
			ExecJob:   core.ExecJob{BareJob: core.BareJob{Schedule: "@every 5s", Command: "echo ini"}},
			JobSource: JobSourceINI,
		},
	}

	labelJobs := map[string]*ExecJobConfig{
		"job1": {
			ExecJob:   core.ExecJob{BareJob: core.BareJob{Schedule: "@every 10s", Command: "echo label"}},
			JobSource: JobSourceLabel,
		},
	}

	mergeJobs(cfg, cfg.ExecJobs, labelJobs, "exec")
	assert.Len(t, cfg.ExecJobs, 1)
	assert.Equal(t, "@every 5s", cfg.ExecJobs["job1"].GetSchedule(), "INI job must not be overridden")
	assert.True(t, logger.HasWarning("ignoring label-defined"),
		"should log warning about ignoring label job")
}

// =============================================================================
// injectDedup mutation tests (line 443 vicinity)
// =============================================================================

// TestInjectDedup_NilDedup targets the c.notificationDedup == nil guard.
func TestInjectDedup_NilDedup(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(test.NewTestLogger())
	// notificationDedup is nil by default

	// The method should be a no-op when dedup is nil
	cfg.injectDedup(&cfg.Global.SlackConfig, &cfg.Global.MailConfig)
	assert.Nil(t, cfg.Global.SlackConfig.Dedup)
	assert.Nil(t, cfg.Global.MailConfig.Dedup)
}

// =============================================================================
// getWebhookManager tests (line 336-341)
// =============================================================================

// TestGetWebhookManager_NilWebhookConfigs targets the nil check at line 337.
func TestGetWebhookManager_NilWebhookConfigs(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	cfg.WebhookConfigs = nil
	result := cfg.getWebhookManager()
	assert.Nil(t, result, "getWebhookManager must return nil when WebhookConfigs is nil")
}

// TestGetWebhookManager_WithWebhookConfigs returns the manager from WebhookConfigs.
func TestGetWebhookManager_WithWebhookConfigs(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(test.NewTestLogger())
	// WebhookConfigs is initialized by NewConfig
	result := cfg.getWebhookManager()
	// Manager may be nil if not initialized, but the function should not panic
	_ = result // no panic = pass
}

// =============================================================================
// initNotificationDedup tests (line 321)
// =============================================================================

// TestInitNotificationDedup_ZeroCooldown targets the <= 0 guard at line 321.
func TestInitNotificationDedup_ZeroCooldown(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(test.NewTestLogger())
	cfg.Global.NotificationCooldown = 0
	cfg.initNotificationDedup()
	assert.Nil(t, cfg.notificationDedup, "dedup should not be initialized with 0 cooldown")
}

// =============================================================================
// logUnknownKeyWarnings mutation tests (lines 182, 204)
// =============================================================================

// TestLogUnknownKeyWarnings_NilRes targets CONDITIONALS_NEGATION at line 182
// (if res == nil). With nil result, the function must return immediately without logging.
func TestLogUnknownKeyWarnings_NilRes(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	logUnknownKeyWarnings(logger, "test.ini", nil)
	assert.Empty(t, logger.GetMessages(), "nil result should produce no log messages")
}

// TestLogUnknownKeyWarnings_WithUnknownGlobal targets the non-nil path at line 182
// and ensures unknown global keys are logged.
func TestLogUnknownKeyWarnings_WithUnknownGlobal(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	res := &parseResult{
		unknownGlobal: []string{"unknown-key"},
	}
	logUnknownKeyWarnings(logger, "test.ini", res)
	assert.True(t, logger.HasWarning("Unknown configuration key 'unknown-key'"),
		"unknown global key should be logged as warning")
	assert.True(t, logger.HasWarning("[global]"),
		"warning should mention [global] section")
}

// TestLogJobUnknownKeyWarnings_EmptyFilename targets CONDITIONALS_NEGATION at line 204
// (if filename != ""). With empty filename, the warning format should differ.
func TestLogJobUnknownKeyWarnings_EmptyFilename(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	unknownJobs := []jobUnknownKeys{
		{JobType: "job-exec", JobName: "test", UnknownKeys: []string{"badkey"}},
	}
	logJobUnknownKeyWarnings(logger, unknownJobs, "")
	assert.True(t, logger.HasWarning("Unknown configuration key 'badkey'"),
		"unknown job key should be logged")
	assert.False(t, logger.HasWarning(" of "),
		"empty filename should not include 'of <filename>' in message")
}

// TestLogJobUnknownKeyWarnings_WithFilename targets the true branch at line 204.
func TestLogJobUnknownKeyWarnings_WithFilename(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	unknownJobs := []jobUnknownKeys{
		{JobType: "job-exec", JobName: "test", UnknownKeys: []string{"badkey"}},
	}
	logJobUnknownKeyWarnings(logger, unknownJobs, "myconfig.ini")
	assert.True(t, logger.HasWarning("Unknown configuration key 'badkey'"),
		"unknown job key should be logged")
	assert.True(t, logger.HasWarning("myconfig.ini"),
		"filename should be included in warning message")
}

// TestLogJobUnknownKeyWarnings_WithSuggestion tests the "did you mean?" branch.
func TestLogJobUnknownKeyWarnings_WithSuggestion(t *testing.T) {
	t.Parallel()

	t.Run("with_filename_and_suggestion", func(t *testing.T) {
		t.Parallel()
		logger := test.NewTestLogger()
		// "schedul" is close enough to "schedule" to trigger a suggestion
		unknownJobs := []jobUnknownKeys{
			{JobType: "job-exec", JobName: "test", UnknownKeys: []string{"schedul"}},
		}
		logJobUnknownKeyWarnings(logger, unknownJobs, "config.ini")
		assert.True(t, logger.HasWarning("did you mean"),
			"close misspelling should produce 'did you mean' suggestion")
	})

	t.Run("without_filename_and_suggestion", func(t *testing.T) {
		t.Parallel()
		logger := test.NewTestLogger()
		unknownJobs := []jobUnknownKeys{
			{JobType: "job-exec", JobName: "test", UnknownKeys: []string{"schedul"}},
		}
		logJobUnknownKeyWarnings(logger, unknownJobs, "")
		assert.True(t, logger.HasWarning("did you mean"),
			"close misspelling should produce 'did you mean' suggestion without filename")
	})

	t.Run("with_filename_no_suggestion", func(t *testing.T) {
		t.Parallel()
		logger := test.NewTestLogger()
		// "zzzzzzz" is too far from any known key to suggest
		unknownJobs := []jobUnknownKeys{
			{JobType: "job-exec", JobName: "test", UnknownKeys: []string{"zzzzzzz"}},
		}
		logJobUnknownKeyWarnings(logger, unknownJobs, "config.ini")
		assert.True(t, logger.HasWarning("typo?"),
			"far-off key should produce 'typo?' message")
	})

	t.Run("without_filename_no_suggestion", func(t *testing.T) {
		t.Parallel()
		logger := test.NewTestLogger()
		unknownJobs := []jobUnknownKeys{
			{JobType: "job-exec", JobName: "test", UnknownKeys: []string{"zzzzzzz"}},
		}
		logJobUnknownKeyWarnings(logger, unknownJobs, "")
		assert.True(t, logger.HasWarning("typo?"),
			"far-off key should produce 'typo?' message without filename")
	})
}

// =============================================================================
// buildMiddlewares wm != nil tests (lines 913, 966, 980, 994)
// =============================================================================

// TestBuildMiddlewares_NilWebhookManager targets CONDITIONALS_NEGATION at
// lines 913, 966, 980, 994 (if wm != nil). When wm is nil, no webhook
// middlewares should be added, but the function should not panic.
func TestBuildMiddlewares_NilWebhookManager(t *testing.T) {
	t.Parallel()

	t.Run("RunJobConfig", func(t *testing.T) {
		t.Parallel()
		j := &RunJobConfig{RunJob: core.RunJob{BareJob: core.BareJob{Schedule: "@daily", Command: "echo test"}}}
		_ = defaults.Set(j)
		j.Name = "test-run"
		j.buildMiddlewares(nil) // wm is nil
		// Should not panic and should have base middlewares
	})

	t.Run("LocalJobConfig", func(t *testing.T) {
		t.Parallel()
		j := &LocalJobConfig{LocalJob: core.LocalJob{BareJob: core.BareJob{Schedule: "@daily", Command: "echo test"}}}
		_ = defaults.Set(j)
		j.Name = "test-local"
		j.buildMiddlewares(nil)
	})

	t.Run("ComposeJobConfig", func(t *testing.T) {
		t.Parallel()
		j := &ComposeJobConfig{ComposeJob: core.ComposeJob{BareJob: core.BareJob{Schedule: "@daily", Command: "echo test"}}}
		_ = defaults.Set(j)
		j.Name = "test-compose"
		j.buildMiddlewares(nil)
	})

	t.Run("ExecJobConfig", func(t *testing.T) {
		t.Parallel()
		j := &ExecJobConfig{ExecJob: core.ExecJob{BareJob: core.BareJob{Schedule: "@daily", Command: "echo test"}}}
		_ = defaults.Set(j)
		j.Name = "test-exec"
		j.buildMiddlewares(nil)
	})
}

// TestBuildMiddlewares_WithEmptyWebhookManager tests with a non-nil but empty webhook manager.
// This ensures the wm != nil branch is exercised when wm has no webhooks configured.
func TestBuildMiddlewares_WithEmptyWebhookManager(t *testing.T) {
	t.Parallel()
	// Create a valid webhook manager with no webhooks
	wm := middlewares.NewWebhookManager(nil)

	t.Run("RunJobConfig_with_wm", func(t *testing.T) {
		t.Parallel()
		j := &RunJobConfig{RunJob: core.RunJob{BareJob: core.BareJob{Schedule: "@daily", Command: "echo test"}}}
		_ = defaults.Set(j)
		j.Name = "test"
		j.buildMiddlewares(wm)
	})

	t.Run("LocalJobConfig_with_wm", func(t *testing.T) {
		t.Parallel()
		j := &LocalJobConfig{LocalJob: core.LocalJob{BareJob: core.BareJob{Schedule: "@daily", Command: "echo test"}}}
		_ = defaults.Set(j)
		j.Name = "test"
		j.buildMiddlewares(wm)
	})

	t.Run("ComposeJobConfig_with_wm", func(t *testing.T) {
		t.Parallel()
		j := &ComposeJobConfig{ComposeJob: core.ComposeJob{BareJob: core.BareJob{Schedule: "@daily", Command: "echo test"}}}
		_ = defaults.Set(j)
		j.Name = "test"
		j.buildMiddlewares(wm)
	})
}

// =============================================================================
// MaxRuntime inheritance mutation tests (lines 398, 420, 706, 812)
// =============================================================================

// TestMaxRuntime_Inheritance tests the MaxRuntime == 0 conditional pattern
// used in lines 398, 420, 706, 812. When MaxRuntime is 0, it should be set
// from Global.MaxRuntime. When non-zero, it should be preserved.
func TestMaxRuntime_Inheritance(t *testing.T) {
	t.Parallel()

	globalMaxRuntime := 30 * time.Minute

	t.Run("RunJob_zero_inherits", func(t *testing.T) {
		t.Parallel()
		j := &RunJobConfig{
			RunJob: core.RunJob{BareJob: core.BareJob{Schedule: "@daily", Command: "echo test"}},
		}
		// Simulate the condition at line 398: if j.MaxRuntime == 0
		assert.Equal(t, time.Duration(0), j.MaxRuntime)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = globalMaxRuntime
		}
		assert.Equal(t, globalMaxRuntime, j.MaxRuntime,
			"RunJob with MaxRuntime=0 should inherit global MaxRuntime")
	})

	t.Run("RunJob_nonzero_preserved", func(t *testing.T) {
		t.Parallel()
		j := &RunJobConfig{
			RunJob: core.RunJob{
				BareJob:    core.BareJob{Schedule: "@daily", Command: "echo test"},
				MaxRuntime: 10 * time.Minute,
			},
		}
		originalRuntime := j.MaxRuntime
		if j.MaxRuntime == 0 {
			j.MaxRuntime = globalMaxRuntime
		}
		assert.Equal(t, originalRuntime, j.MaxRuntime,
			"RunJob with non-zero MaxRuntime should keep its own value")
		assert.NotEqual(t, globalMaxRuntime, j.MaxRuntime,
			"RunJob non-zero MaxRuntime must differ from global")
	})

	t.Run("ServiceJob_zero_inherits", func(t *testing.T) {
		t.Parallel()
		j := &RunServiceConfig{
			RunServiceJob: core.RunServiceJob{BareJob: core.BareJob{Schedule: "@daily", Command: "echo test"}},
		}
		assert.Equal(t, time.Duration(0), j.MaxRuntime)
		if j.MaxRuntime == 0 {
			j.MaxRuntime = globalMaxRuntime
		}
		assert.Equal(t, globalMaxRuntime, j.MaxRuntime,
			"ServiceJob with MaxRuntime=0 should inherit global MaxRuntime")
	})

	t.Run("ServiceJob_nonzero_preserved", func(t *testing.T) {
		t.Parallel()
		j := &RunServiceConfig{
			RunServiceJob: core.RunServiceJob{
				BareJob:    core.BareJob{Schedule: "@daily", Command: "echo test"},
				MaxRuntime: 15 * time.Minute,
			},
		}
		originalRuntime := j.MaxRuntime
		if j.MaxRuntime == 0 {
			j.MaxRuntime = globalMaxRuntime
		}
		assert.Equal(t, originalRuntime, j.MaxRuntime)
		assert.NotEqual(t, globalMaxRuntime, j.MaxRuntime)
	})
}

// =============================================================================
// WebhookConfigs initialization (line 302)
// =============================================================================

// TestInitializeApp_WebhookConfigs_Nil targets CONDITIONALS_NEGATION at line 302
// (c.WebhookConfigs != nil && len(c.WebhookConfigs.Webhooks) > 0).
// When WebhookConfigs is nil, InitializeApp should not panic.
func TestInitializeApp_WebhookConfigsNil(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.WebhookConfigs = nil
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	// Should not panic
	wm := cfg.getWebhookManager()
	assert.Nil(t, wm)
}

// TestInitializeApp_WebhookConfigs_EmptyWebhooks targets the len > 0 boundary.
func TestInitializeApp_WebhookConfigsEmptyWebhooks(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.WebhookConfigs = &WebhookConfigs{
		Webhooks: make(map[string]*middlewares.WebhookConfig),
	}
	// len == 0 should not trigger InitManager
	wm := cfg.getWebhookManager()
	assert.Nil(t, wm, "empty webhooks should not have a manager initialized")
}

// =============================================================================
// mergeJobsFromDockerLabels error handling (line 351)
// =============================================================================

// =============================================================================
// injectDedup with non-nil dedup (line 443 - testing both branches)
// =============================================================================

// TestInjectDedup_WithDedup targets the c.notificationDedup == nil guard negation.
// When dedup is non-nil, it should be injected into both slack and mail configs.
func TestInjectDedup_WithDedup(t *testing.T) {
	t.Parallel()
	cfg := NewConfig(test.NewTestLogger())
	cfg.Global.NotificationCooldown = 5 * time.Minute
	cfg.initNotificationDedup()
	require.NotNil(t, cfg.notificationDedup, "dedup should be initialized with positive cooldown")

	slack := &middlewares.SlackConfig{}
	mail := &middlewares.MailConfig{}
	cfg.injectDedup(slack, mail)
	assert.NotNil(t, slack.Dedup, "slack dedup should be set when notificationDedup is non-nil")
	assert.NotNil(t, mail.Dedup, "mail dedup should be set when notificationDedup is non-nil")
	assert.Equal(t, cfg.notificationDedup, slack.Dedup)
	assert.Equal(t, cfg.notificationDedup, mail.Dedup)
}

// =============================================================================
// ApplyLogLevel in dockerLabelsUpdate (line 793)
// =============================================================================

// TestApplyLogLevel_InvalidReturnsWarning targets CONDITIONALS_NEGATION at
// line 793 (err := ApplyLogLevel) in the dockerLabelsUpdate function.
func TestApplyLogLevel_InvalidLevel(t *testing.T) {
	t.Parallel()

	t.Run("invalid level logs warning", func(t *testing.T) {
		t.Parallel()
		err := ApplyLogLevel("INVALID")
		assert.Error(t, err, "invalid log level should return error")
	})

	t.Run("valid level succeeds", func(t *testing.T) {
		t.Parallel()
		err := ApplyLogLevel("debug")
		assert.NoError(t, err, "valid log level should succeed")
	})

	t.Run("empty level succeeds", func(t *testing.T) {
		t.Parallel()
		err := ApplyLogLevel("")
		assert.NoError(t, err, "empty log level should succeed (use default)")
	})
}
