package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeWithMetadata_BasicDecode(t *testing.T) {
	t.Parallel()

	type Config struct {
		Name  string `mapstructure:"name"`
		Count int    `mapstructure:"count"`
	}

	input := map[string]interface{}{
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

	input := map[string]interface{}{
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

	input := map[string]interface{}{
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

	input := map[string]interface{}{
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

	input := map[string]interface{}{
		"poll-interval": 30,
		"Poll-Interval": 30, // duplicate with different case
		"no-poll":       true,
	}

	keys := collectInputKeys(input)

	assert.True(t, keys["pollinterval"])
	assert.True(t, keys["nopoll"])
}
