package cli

import (
	"testing"

	"github.com/netresearch/ofelia/test"
)

// FuzzBuildFromString tests INI config parsing with arbitrary input.
// This helps find parsing edge cases, panics, and potential security issues.
func FuzzBuildFromString(f *testing.F) {
	// Seed corpus with valid and edge-case inputs
	seeds := []string{
		// Valid minimal config
		`[job-exec "test"]
schedule = @hourly
container = test-container
command = echo hello`,

		// Valid config with global settings
		`[global]
log-level = debug

[job-local "backup"]
schedule = 0 0 * * *
command = /bin/backup.sh`,

		// Multiple job types
		`[job-run "runner"]
schedule = @daily
image = alpine
command = ls

[job-exec "executor"]
schedule = @weekly
container = myapp
command = cleanup`,

		// Edge cases
		"",           // Empty
		"[",          // Incomplete section
		"[section",   // Missing bracket
		"key=value",  // No section
		"[job-exec]", // Missing job name
		`[job-exec "test"]
schedule = not-a-cron`, // Invalid cron
		`[job-exec "test"]
schedule = @hourly
unknown-key = value`, // Unknown key
		"[global]\n\x00\x01\x02", // Binary data
		"[job-exec \"test\"]\nschedule = @hourly\ncommand = $(echo pwned)", // Command injection attempt
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data string) {
		logger := test.NewTestLogger()
		// We don't care about errors - we're looking for panics and crashes
		_, _ = BuildFromString(data, logger)
	})
}

// FuzzDockerLabels tests Docker label parsing with arbitrary input.
func FuzzDockerLabels(f *testing.F) {
	// Seed with various label patterns
	seeds := []string{
		// Valid label key patterns
		"ofelia.job-exec.test.schedule",
		"ofelia.job-exec.test.command",
		"ofelia.job-run.myrunner.image",
		"ofelia.job-local.backup.command",
		"ofelia.global.log-level",

		// Edge cases
		"",
		"ofelia",
		"ofelia.",
		"ofelia.job-exec",
		"ofelia.job-exec.",
		"ofelia.job-exec.name",
		"...",
		"ofelia....",
		"ofelia.unknown-type.name.key",
		"not-ofelia.job-exec.test.schedule",
	}

	for _, seed := range seeds {
		f.Add(seed, "@hourly")
	}

	f.Fuzz(func(t *testing.T, labelKey, labelValue string) {
		logger := test.NewTestLogger()
		c := NewConfig(logger)

		// Create a mock label set as if from a container
		labels := map[string]map[string]string{
			"test-container": {
				labelKey:         labelValue,
				"ofelia.enabled": "true",
			},
		}

		// We don't care about errors - we're looking for panics
		_ = c.buildFromDockerLabels(labels)
	})
}
