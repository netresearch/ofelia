package cli

import (
	"testing"

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
			expectKeys: []string{"schedule", "command", "container", "environment"},
		},
		{
			jobType:    "job-run",
			expectKeys: []string{"schedule", "command", "image", "network"},
		},
		{
			jobType:    "job-local",
			expectKeys: []string{"schedule", "command", "dir", "environment"},
		},
		{
			jobType:    "job-service-run",
			expectKeys: []string{"schedule", "command", "image"},
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
