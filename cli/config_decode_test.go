// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

func TestDecodeWithMetadata_BasicDecode(t *testing.T) {
	t.Parallel()

	type Config struct {
		Name  string `mapstructure:"name"`
		Count int    `mapstructure:"count"`
	}

	input := map[string]any{
		"name":  "test",
		"count": 42,
	}

	var cfg Config
	result, err := decodeWithMetadata(input, &cfg)

	require.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 42, cfg.Count)
	assert.NotNil(t, result)
	assert.True(t, result.UsedKeys["name"])
	assert.True(t, result.UsedKeys["count"])
	assert.Empty(t, result.UnusedKeys)
}

func TestDecodeWithMetadata_UnusedKeys(t *testing.T) {
	t.Parallel()

	type Config struct {
		Name string `mapstructure:"name"`
	}

	input := map[string]any{
		"name":    "test",
		"unknown": "value",
		"typo":    123,
	}

	var cfg Config
	result, err := decodeWithMetadata(input, &cfg)

	require.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.NotNil(t, result)
	assert.True(t, result.UsedKeys["name"])
	assert.Len(t, result.UnusedKeys, 2)
	assert.Contains(t, result.UnusedKeys, "unknown")
	assert.Contains(t, result.UnusedKeys, "typo")
}

func TestDecodeWithMetadata_CaseInsensitive(t *testing.T) {
	t.Parallel()

	type Config struct {
		PollInterval int `mapstructure:"poll-interval"`
	}

	input := map[string]any{
		"Poll-Interval": 30,
	}

	var cfg Config
	result, err := decodeWithMetadata(input, &cfg)

	require.NoError(t, err)
	assert.Equal(t, 30, cfg.PollInterval)
	assert.NotNil(t, result)
}

func TestDecodeWithMetadata_WeakTyping(t *testing.T) {
	t.Parallel()

	type Config struct {
		Count   int  `mapstructure:"count"`
		Enabled bool `mapstructure:"enabled"`
	}

	input := map[string]any{
		"count":   "42",   // string to int
		"enabled": "true", // string to bool
	}

	var cfg Config
	result, err := decodeWithMetadata(input, &cfg)

	require.NoError(t, err)
	assert.Equal(t, 42, cfg.Count)
	assert.True(t, cfg.Enabled)
	assert.NotNil(t, result)
}

func TestNormalizeKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"poll-interval", "pollinterval"},
		{"Poll-Interval", "pollinterval"},
		{"POLL_INTERVAL", "pollinterval"},
		{"pollInterval", "pollinterval"},
		{"no-poll", "nopoll"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeKey(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMergeUsedKeys(t *testing.T) {
	t.Parallel()

	result1 := &DecodeResult{
		UsedKeys: map[string]bool{"key1": true, "key2": true},
	}
	result2 := &DecodeResult{
		UsedKeys: map[string]bool{"key2": true, "key3": true},
	}
	result3 := (*DecodeResult)(nil) // nil result should be handled

	merged := mergeUsedKeys(result1, result2, result3)

	assert.True(t, merged["key1"])
	assert.True(t, merged["key2"])
	assert.True(t, merged["key3"])
	assert.Len(t, merged, 3)
}

func TestCollectInputKeys(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"poll-interval": 30,
		"Poll-Interval": 30, // duplicate with different case
		"no-poll":       true,
	}

	keys := collectInputKeys(input)

	assert.True(t, keys["pollinterval"])
	assert.True(t, keys["nopoll"])
}

func TestExtractMapstructureKeys_SimpleStruct(t *testing.T) {
	t.Parallel()

	type SimpleConfig struct {
		Name    string `mapstructure:"name"`
		Count   int    `mapstructure:"count"`
		Enabled bool   `mapstructure:"enabled"`
	}

	keys := extractMapstructureKeys(SimpleConfig{})

	assert.Contains(t, keys, "name")
	assert.Contains(t, keys, "count")
	assert.Contains(t, keys, "enabled")
	assert.Len(t, keys, 3)
}

func TestExtractMapstructureKeys_WithSquash(t *testing.T) {
	t.Parallel()

	type Embedded struct {
		Field1 string `mapstructure:"field1"`
		Field2 int    `mapstructure:"field2"`
	}

	type Config struct {
		Embedded `mapstructure:",squash"`
		Own      string `mapstructure:"own"`
	}

	keys := extractMapstructureKeys(Config{})

	assert.Contains(t, keys, "field1")
	assert.Contains(t, keys, "field2")
	assert.Contains(t, keys, "own")
}

func TestExtractMapstructureKeys_IgnoresMinusTag(t *testing.T) {
	t.Parallel()

	type Config struct {
		Name    string `mapstructure:"name"`
		Private string `mapstructure:"-"`
	}

	keys := extractMapstructureKeys(Config{})

	assert.Contains(t, keys, "name")
	assert.NotContains(t, keys, "-")
	assert.NotContains(t, keys, "Private")
	assert.Len(t, keys, 1)
}

func TestExtractMapstructureKeys_ExecJobConfig(t *testing.T) {
	t.Parallel()

	// Test with actual ExecJobConfig to ensure it works with real config structs
	keys := extractMapstructureKeys(ExecJobConfig{})

	// Should contain keys from BareJob (via squash)
	assert.Contains(t, keys, "schedule")
	assert.Contains(t, keys, "command")
	assert.Contains(t, keys, "container")

	// Should contain keys from ExecJob
	assert.Contains(t, keys, "environment")
	assert.Contains(t, keys, "working-dir")

	// Should contain keys from middleware configs (via squash)
	assert.Contains(t, keys, "no-overlap")
	assert.Contains(t, keys, "slack-webhook")
	assert.Contains(t, keys, "smtp-host")
}

func TestExtractMapstructureKeys_RunJobConfig(t *testing.T) {
	t.Parallel()

	keys := extractMapstructureKeys(RunJobConfig{})

	// Should contain keys from BareJob (via squash)
	assert.Contains(t, keys, "schedule")
	assert.Contains(t, keys, "command")

	// Should contain keys from RunJob
	assert.Contains(t, keys, "image")
	assert.Contains(t, keys, "network")
	assert.Contains(t, keys, "volume")

	// Should contain keys from middleware configs (via squash)
	assert.Contains(t, keys, "no-overlap")
}

func TestExtractMapstructureKeys_LocalJobConfig(t *testing.T) {
	t.Parallel()

	keys := extractMapstructureKeys(LocalJobConfig{})

	// Should contain keys from BareJob (via squash)
	assert.Contains(t, keys, "schedule")
	assert.Contains(t, keys, "command")

	// Should contain keys from LocalJob
	assert.Contains(t, keys, "dir")
	assert.Contains(t, keys, "environment")

	// Should contain keys from middleware configs (via squash)
	assert.Contains(t, keys, "no-overlap")
}

func TestGetKnownKeysForJobType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		jobType      string
		expectKeys   []string
		expectNilFor string
	}{
		{
			jobType:    "job-exec",
			expectKeys: []string{"schedule", "command", "container", "environment", "privileged"},
		},
		{
			jobType:    "job-run",
			expectKeys: []string{"schedule", "command", "image", "network", "working-dir"},
		},
		{
			jobType:    "job-local",
			expectKeys: []string{"schedule", "command", "dir", "environment"},
		},
		{
			jobType:    "job-service-run",
			expectKeys: []string{"schedule", "command", "image", "environment", "hostname", "dir", "volume"},
		},
		{
			jobType:    "job-compose",
			expectKeys: []string{"schedule", "file", "service", "exec"},
		},
		{
			jobType:      "unknown-job-type",
			expectNilFor: "unknown job type should return nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.jobType, func(t *testing.T) {
			keys := getKnownKeysForJobType(tt.jobType)
			if tt.expectNilFor != "" {
				assert.Nil(t, keys, tt.expectNilFor)
				return
			}
			for _, expected := range tt.expectKeys {
				assert.Contains(t, keys, expected)
			}
		})
	}
}

// Integration tests for job section unknown key warnings

func TestJobSectionUnknownKeyWarning_ExecJob(t *testing.T) {
	t.Parallel()

	// Config with unknown key "schdule" (typo for "schedule")
	configStr := `
[job-exec "test-job"]
schdule = @every 5s
container = my-container
command = echo hello
unknown-key = some-value
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	// Should have 2 warnings: one for "schdule" (with suggestion) and one for "unknown-key"
	assert.Equal(t, 2, handler.WarningCount(), "Expected 2 warnings for unknown keys")
	assert.True(t, handler.HasWarning("Unknown configuration key 'schdule'"),
		"Should warn about 'schdule'")
	assert.True(t, handler.HasWarning("did you mean 'schedule'"),
		"Should suggest 'schedule' for 'schdule'")
	assert.True(t, handler.HasWarning("Unknown configuration key 'unknown-key'"),
		"Should warn about 'unknown-key'")
	assert.True(t, handler.HasWarning("job-exec \"test-job\""),
		"Warning should include the job section name")
}

func TestJobSectionUnknownKeyWarning_RunJob(t *testing.T) {
	t.Parallel()

	// Config with unknown key "nettwork" (typo for "network")
	// Note: image is required for job-run, so we include it with a typo for another field
	configStr := `
[job-run "test-run"]
schedule = @every 10s
image = busybox
command = echo test
nettwork = my-network
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, 1, handler.WarningCount(), "Expected 1 warning for unknown key")
	assert.True(t, handler.HasWarning("Unknown configuration key 'nettwork'"),
		"Should warn about 'nettwork'")
	assert.True(t, handler.HasWarning("did you mean 'network'"),
		"Should suggest 'network' for 'nettwork'")
	assert.True(t, handler.HasWarning("job-run \"test-run\""),
		"Warning should include the job section name")
}

func TestJobSectionUnknownKeyWarning_LocalJob(t *testing.T) {
	t.Parallel()

	// Config with unknown key "comnand" (typo for "command")
	configStr := `
[job-local "local-test"]
schedule = @daily
comnand = /usr/local/bin/backup.sh
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, 1, handler.WarningCount(), "Expected 1 warning for unknown key")
	assert.True(t, handler.HasWarning("Unknown configuration key 'comnand'"),
		"Should warn about 'comnand'")
	assert.True(t, handler.HasWarning("did you mean 'command'"),
		"Should suggest 'command' for 'comnand'")
	assert.True(t, handler.HasWarning("job-local \"local-test\""),
		"Warning should include the job section name")
}

func TestJobSectionUnknownKeyWarning_NoSuggestion(t *testing.T) {
	t.Parallel()

	// Config with unknown key that has no close match
	configStr := `
[job-exec "test-job"]
schedule = @every 5s
container = my-container
command = echo hello
zzz-random-key = some-value
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, 1, handler.WarningCount(), "Expected 1 warning for unknown key")
	assert.True(t, handler.HasWarning("Unknown configuration key 'zzz-random-key'"),
		"Should warn about 'zzz-random-key'")
	assert.True(t, handler.HasWarning("typo?"),
		"Should show 'typo?' when no close match found")
	assert.False(t, handler.HasWarning("did you mean"),
		"Should not suggest when no close match")
}

func TestJobSectionUnknownKeyWarning_ValidConfig(t *testing.T) {
	t.Parallel()

	// Config with all valid keys - should produce no warnings
	configStr := `
[job-exec "valid-job"]
schedule = @every 5s
container = my-container
command = echo hello
environment = FOO=bar
no-overlap = true
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, 0, handler.WarningCount(), "Expected no warnings for valid config")
}

func TestJobSectionUnknownKeyWarning_MultipleJobs(t *testing.T) {
	t.Parallel()

	// Config with unknown keys in multiple job sections
	configStr := `
[job-exec "job1"]
schedule = @every 5s
container = container1
command = echo 1
typo1 = value1

[job-run "job2"]
schedule = @every 10s
image = busybox
command = echo 2
typo2 = value2
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, 2, handler.WarningCount(), "Expected 2 warnings for unknown keys in different jobs")
	assert.True(t, handler.HasWarning("job-exec \"job1\""),
		"Should have warning for job1")
	assert.True(t, handler.HasWarning("job-run \"job2\""),
		"Should have warning for job2")
}

// TestGlobalSectionUnknownKeyWarning_DidYouMean pins the parity fix from
// issue #678: a typo on any [global] mapstructure-tagged key should produce
// a "did you mean?" line citing the nearest match within Levenshtein
// threshold. Pre-fix, only job-section warnings carried the suggestion;
// [global] just emitted "(typo?)" and left the operator to guess.
func TestGlobalSectionUnknownKeyWarning_DidYouMean(t *testing.T) {
	t.Parallel()

	// "webhook-defauls-preset" swaps t/s on "webhook-default-preset" — the
	// exact mistype called out in the issue body.
	//nolint:misspell // intentional typo for did-you-mean assertion
	configStr := `
[global]
webhook-defauls-preset = json-post
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, 1, handler.WarningCount(), "Expected 1 warning for unknown key")
	//nolint:misspell // intentional typo for did-you-mean assertion
	assert.True(t, handler.HasWarning("Unknown configuration key 'webhook-defauls-preset'"),
		"Should warn about 'webhook-defauls-preset'")
	assert.True(t, handler.HasWarning("[global] section"),
		"Should name the [global] section")
	//nolint:misspell // intentional typo for did-you-mean assertion
	assert.True(t, handler.HasWarning("did you mean 'webhook-default-preset'"),
		"Should suggest 'webhook-default-preset' for 'webhook-defauls-preset'")
}

// TestGlobalSectionUnknownKeyWarning_NoSuggestion pins that the [global]
// path still falls back to "(typo?)" — same shape as the existing job-section
// no-suggestion test — when no close match exists. Asymmetry against the
// suggestion case is what makes the parity fix meaningful.
func TestGlobalSectionUnknownKeyWarning_NoSuggestion(t *testing.T) {
	t.Parallel()

	configStr := `
[global]
zzz-totally-unrelated-key = value
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, 1, handler.WarningCount(), "Expected 1 warning for unknown key")
	assert.True(t, handler.HasWarning("Unknown configuration key 'zzz-totally-unrelated-key'"),
		"Should warn about 'zzz-totally-unrelated-key'")
	assert.True(t, handler.HasWarning("[global] section"),
		"Should name the [global] section")
	assert.True(t, handler.HasWarning("typo?"),
		"Should fall back to '(typo?)' when no close match found")
	assert.False(t, handler.HasWarning("did you mean"),
		"Should not suggest when no close match")
}

// TestDockerSectionUnknownKeyWarning_DidYouMean confirms the same parity fix
// also applies to the [docker] section path (#678). The two sections share
// the same code path, but explicitly asserting both prevents a future split
// from silently dropping suggestions on one half. Also pins cross-section
// isolation: a [docker] typo must NOT be misattributed to [global].
func TestDockerSectionUnknownKeyWarning_DidYouMean(t *testing.T) {
	t.Parallel()

	// "inculde-stopped" swaps u/n on "include-stopped".
	configStr := `
[docker]
inculde-stopped = true
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, 1, handler.WarningCount(), "Expected 1 warning for unknown key")
	assert.True(t, handler.HasWarning("Unknown configuration key 'inculde-stopped'"),
		"Should warn about 'inculde-stopped'")
	assert.True(t, handler.HasWarning("[docker] section"),
		"Should name the [docker] section")
	assert.False(t, handler.HasWarning("[global] section"),
		"Should NOT misattribute a [docker] typo to the [global] section")
	assert.True(t, handler.HasWarning("did you mean 'include-stopped'"),
		"Should suggest 'include-stopped' for 'inculde-stopped'")
}

// TestGlobalSectionUnknownKeyWarning_NonSquashedKey complements the
// _DidYouMean test (which targets `webhook-default-preset` inside the
// squashed WebhookGlobalConfig) by exercising a key declared DIRECTLY on
// the anonymous Global struct. Together they lock both halves of
// extractMapstructureKeysFromType: squash recursion AND direct-field
// enumeration. Without this, a refactor that breaks one path but not the
// other could pass the existing tests.
func TestGlobalSectionUnknownKeyWarning_NonSquashedKey(t *testing.T) {
	t.Parallel()

	// "enabel-pprof" swaps the el on "enable-pprof", a key declared
	// directly on Config{}.Global (not via squash).
	configStr := `
[global]
enabel-pprof = true
`

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromString(configStr, logger)
	require.NoError(t, err)

	assert.Equal(t, 1, handler.WarningCount(), "Expected 1 warning for unknown key")
	assert.True(t, handler.HasWarning("Unknown configuration key 'enabel-pprof'"),
		"Should warn about 'enabel-pprof'")
	assert.True(t, handler.HasWarning("did you mean 'enable-pprof'"),
		"Should suggest 'enable-pprof' for 'enabel-pprof'")
}

// TestGlobalSectionUnknownKeyWarning_FileBased pins the suggestion
// behavior on the BuildFromFile path (logUnknownKeyWarnings), the
// production code path the issue reporter actually hits. The other
// _DidYouMean tests cover BuildFromString (filename == ""); this one
// covers the four-arm switch's filename != "" branch and asserts the
// "of <filename>" substring lands in the message.
func TestGlobalSectionUnknownKeyWarning_FileBased(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	//nolint:misspell // intentional typo for did-you-mean assertion
	content := `[global]
webhook-defauls-preset = json-post
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	logger, handler := test.NewTestLoggerWithHandler()
	_, err := BuildFromFile(configPath, logger)
	require.NoError(t, err)

	assert.Equal(t, 1, handler.WarningCount(), "Expected 1 warning for unknown key")
	//nolint:misspell // intentional typo for did-you-mean assertion
	assert.True(t, handler.HasWarning("Unknown configuration key 'webhook-defauls-preset'"),
		"Should warn about the typo")
	assert.True(t, handler.HasWarning("[global] section"),
		"Should name the [global] section")
	assert.True(t, handler.HasWarning("of "+configPath),
		"Should include the filename (file-based path's message variant)")
	assert.True(t, handler.HasWarning("did you mean 'webhook-default-preset'"),
		"Should suggest 'webhook-default-preset' even on the file-based path")
}

// Phase 8: Additional coverage tests for config_decode.go

func TestWeakDecodeConsistent_NilInput(t *testing.T) {
	t.Parallel()

	type Config struct {
		Name string `mapstructure:"name"`
	}

	var cfg Config
	err := weakDecodeConsistent(nil, &cfg)
	require.NoError(t, err)
	assert.Empty(t, cfg.Name)
}

func TestWeakDecodeConsistent_NestedMapInput(t *testing.T) {
	t.Parallel()

	type Inner struct {
		Value string `mapstructure:"value"`
	}
	type Config struct {
		Inner Inner `mapstructure:"inner"`
	}

	input := map[string]any{
		"inner": map[string]any{
			"value": "nested-value",
		},
	}

	var cfg Config
	err := weakDecodeConsistent(input, &cfg)
	require.NoError(t, err)
	assert.Equal(t, "nested-value", cfg.Inner.Value)
}

func TestWeakDecodeConsistent_CaseInsensitive(t *testing.T) {
	t.Parallel()

	type Config struct {
		PollInterval int `mapstructure:"poll-interval"`
	}

	input := map[string]any{
		"Poll-Interval": 42,
	}

	var cfg Config
	err := weakDecodeConsistent(input, &cfg)
	require.NoError(t, err)
	assert.Equal(t, 42, cfg.PollInterval)
}

func TestWeakDecodeConsistent_InvalidOutput(t *testing.T) {
	t.Parallel()

	var result string
	err := weakDecodeConsistent(map[string]any{"key": "val"}, result)
	assert.Error(t, err)
}

func TestDecodeWithMetadata_EmptyInput(t *testing.T) {
	t.Parallel()

	type Config struct {
		Name string `mapstructure:"name"`
	}

	var cfg Config
	result, err := decodeWithMetadata(map[string]any{}, &cfg)
	require.NoError(t, err)
	assert.Empty(t, cfg.Name)
	assert.Empty(t, result.UsedKeys)
	assert.Empty(t, result.UnusedKeys)
}

func TestExtractMapstructureKeys_NoTagFallsBackToFieldName(t *testing.T) {
	t.Parallel()

	type Config struct {
		NoTag   string
		WithTag string `mapstructure:"with-tag"`
	}

	keys := extractMapstructureKeys(Config{})
	assert.Contains(t, keys, "notag")
	assert.Contains(t, keys, "with-tag")
}

func TestExtractMapstructureKeys_PointerType(t *testing.T) {
	t.Parallel()

	type Config struct {
		Name string `mapstructure:"name"`
	}

	keys := extractMapstructureKeys(&Config{})
	assert.Contains(t, keys, "name")
}

func TestExtractMapstructureKeys_NonStruct(t *testing.T) {
	t.Parallel()

	keys := extractMapstructureKeys("not-a-struct")
	assert.Nil(t, keys)
}

func TestMergeUsedKeys_AllNil(t *testing.T) {
	t.Parallel()

	merged := mergeUsedKeys(nil, nil, nil)
	assert.Empty(t, merged)
}

func TestDecodeWithMetadata_TimeDuration(t *testing.T) {
	t.Parallel()

	type Config struct {
		Timeout time.Duration `mapstructure:"timeout"`
	}

	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{"hours", "1h", time.Hour},
		{"minutes", "30m", 30 * time.Minute},
		{"seconds", "55s", 55 * time.Second},
		{"combined", "1h30m", 90 * time.Minute},
		{"day-equivalent", "24h", 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cfg Config
			_, err := decodeWithMetadata(map[string]any{"timeout": tt.input}, &cfg)
			require.NoError(t, err, "should parse duration string %q", tt.input)
			assert.Equal(t, tt.expected, cfg.Timeout)
		})
	}
}

func TestWeakDecodeConsistent_TimeDuration(t *testing.T) {
	t.Parallel()

	type Config struct {
		Timeout time.Duration `mapstructure:"timeout"`
	}

	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{"hours", "1h", time.Hour},
		{"minutes", "30m", 30 * time.Minute},
		{"seconds", "55s", 55 * time.Second},
		{"combined", "1h30m", 90 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cfg Config
			err := weakDecodeConsistent(map[string]any{"timeout": tt.input}, &cfg)
			require.NoError(t, err, "should parse duration string %q", tt.input)
			assert.Equal(t, tt.expected, cfg.Timeout)
		})
	}
}

func TestBuildFromString_MaxRuntimeDuration(t *testing.T) {
	t.Parallel()

	configStr := `
[job-run "test-job"]
schedule = @every 5s
image = busybox
command = echo hello
max-runtime = 1h
`

	logger, _ := test.NewTestLoggerWithHandler()
	cfg, err := BuildFromString(configStr, logger)
	require.NoError(t, err, "max-runtime = 1h should parse without error")
	require.Contains(t, cfg.RunJobs, "test-job")
	assert.Equal(t, time.Hour, cfg.RunJobs["test-job"].MaxRuntime)
}

func TestBuildFromString_AllDurationFields(t *testing.T) {
	t.Parallel()

	configStr := `
[global]
max-runtime = 2h
notification-cooldown = 5m

[job-exec "test-exec"]
schedule = @every 5s
container = my-container
command = echo hello
`

	logger, _ := test.NewTestLoggerWithHandler()
	cfg, err := BuildFromString(configStr, logger)
	require.NoError(t, err, "global duration fields should parse without error")
	assert.Equal(t, 2*time.Hour, cfg.Global.MaxRuntime)
	assert.Equal(t, 5*time.Minute, cfg.Global.NotificationCooldown)
}

func TestBuildFromString_ServiceJobMaxRuntime(t *testing.T) {
	t.Parallel()

	configStr := `
[job-service-run "test-svc"]
schedule = @every 5s
image = busybox
command = echo hello
max-runtime = 45m
`

	logger, _ := test.NewTestLoggerWithHandler()
	cfg, err := BuildFromString(configStr, logger)
	require.NoError(t, err, "service job max-runtime = 45m should parse without error")
	require.Contains(t, cfg.ServiceJobs, "test-svc")
	assert.Equal(t, 45*time.Minute, cfg.ServiceJobs["test-svc"].MaxRuntime)
}

func TestMergeUsedKeys_FalseValues(t *testing.T) {
	t.Parallel()

	result := &DecodeResult{
		UsedKeys: map[string]bool{"key1": true, "key2": false},
	}

	merged := mergeUsedKeys(result)
	assert.True(t, merged["key1"])
	assert.False(t, merged["key2"], "false values should not be merged")
	assert.Len(t, merged, 1)
}
