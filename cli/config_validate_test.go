package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidateCron(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		isValid bool
	}{
		// Valid standard cron
		{"standard 5 fields", "0 * * * *", true},
		{"standard 6 fields", "0 0 * * * *", true},
		{"every minute", "* * * * *", true},
		{"complex", "0,30 8-17 * * 1-5", true},

		// Valid special expressions
		{"@hourly", "@hourly", true},
		{"@daily", "@daily", true},
		{"@weekly", "@weekly", true},
		{"@monthly", "@monthly", true},
		{"@yearly", "@yearly", true},
		{"@annually", "@annually", true},
		{"@midnight", "@midnight", true},
		{"@triggered", "@triggered", true},
		{"@manual", "@manual", true},
		{"@none", "@none", true},
		{"@every 5m", "@every 5m", true},
		{"@every 1h30m", "@every 1h30m", true},

		// Invalid
		{"too few fields", "* * *", false},
		{"too many fields", "* * * * * * *", false},
		{"invalid special", "@invalid", false},
		{"empty", "", true}, // Empty is valid (required check handles this)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type Config struct {
				Schedule string `validate:"cron"`
			}

			cfg := Config{Schedule: tt.value}
			err := ValidateConfig(&cfg)

			if tt.isValid {
				assert.NoError(t, err, "expected valid for %q", tt.value)
			} else {
				assert.Error(t, err, "expected invalid for %q", tt.value)
			}
		})
	}
}

func TestValidateDockerImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		isValid bool
	}{
		// Valid images
		{"simple", "nginx", true},
		{"with tag", "nginx:latest", true},
		{"with registry", "docker.io/nginx", true},
		{"with registry and tag", "docker.io/nginx:1.19", true},
		{"private registry", "myregistry.com/myimage", true},
		{"private registry with port", "localhost:5000/myimage", true},
		{"with digest", "nginx@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", true},
		{"complex path", "gcr.io/my-project/my-image:v1.0.0", true},

		// Empty is valid (required check handles this)
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type Config struct {
				Image string `validate:"dockerimage"`
			}

			cfg := Config{Image: tt.value}
			err := ValidateConfig(&cfg)

			if tt.isValid {
				assert.NoError(t, err, "expected valid for %q", tt.value)
			} else {
				assert.Error(t, err, "expected invalid for %q", tt.value)
			}
		})
	}
}

func TestValidateRange(t *testing.T) {
	t.Parallel()

	type Config struct {
		Port    int           `validate:"gte=1,lte=65535"`
		Timeout time.Duration `validate:"gte=0"`
	}

	tests := []struct {
		name    string
		cfg     Config
		isValid bool
	}{
		{"valid port", Config{Port: 8080, Timeout: time.Second}, true},
		{"port too low", Config{Port: 0, Timeout: time.Second}, false},
		{"port too high", Config{Port: 70000, Timeout: time.Second}, false},
		{"negative timeout", Config{Port: 8080, Timeout: -time.Second}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(&tt.cfg)

			if tt.isValid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestValidateEnum(t *testing.T) {
	t.Parallel()

	type Config struct {
		LogLevel string `validate:"omitempty,oneof=debug info warning error"`
	}

	tests := []struct {
		name    string
		value   string
		isValid bool
	}{
		{"valid debug", "debug", true},
		{"valid info", "info", true},
		{"valid warning", "warning", true},
		{"valid error", "error", true},
		{"empty (omitempty)", "", true},
		{"invalid", "trace", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{LogLevel: tt.value}
			err := ValidateConfig(&cfg)

			if tt.isValid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestLevenshteinDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a        string
		b        string
		expected int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "adc", 1},
		{"abc", "dbc", 1},
		{"abc", "def", 3},
		{"schedule", "scheduel", 2}, // common typo
		{"command", "comand", 1},    // common typo
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			result := levenshteinDistance(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindClosestMatch(t *testing.T) {
	t.Parallel()

	candidates := []string{"schedule", "command", "container", "image", "user"}

	tests := []struct {
		input    string
		expected string
	}{
		{"scheduel", "schedule"},  // typo
		{"comand", "command"},     // typo
		{"contaner", "container"}, // typo
		{"xyz", ""},               // no close match
		{"totally-different", ""}, // no close match
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := findClosestMatch(tt.input, candidates)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateUnknownKeyWarnings(t *testing.T) {
	t.Parallel()

	knownKeys := []string{"schedule", "command", "container"}
	unusedKeys := []string{"scheduel", "xyz"}

	warnings := GenerateUnknownKeyWarnings("[job-exec \"test\"]", unusedKeys, knownKeys)

	assert.Len(t, warnings, 2)

	// First warning should have a suggestion
	assert.Equal(t, "[job-exec \"test\"]", warnings[0].Section)
	assert.Equal(t, "scheduel", warnings[0].Key)
	assert.Equal(t, "schedule", warnings[0].Suggestion)

	// Second warning should have no suggestion
	assert.Equal(t, "xyz", warnings[1].Key)
	assert.Empty(t, warnings[1].Suggestion)
}
