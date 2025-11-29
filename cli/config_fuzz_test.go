package cli

import (
	"testing"

	"github.com/netresearch/ofelia/core"
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
		logger := &nullLogger{}
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
		logger := &nullLogger{}
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

// nullLogger implements core.Logger for testing
type nullLogger struct{}

func (n *nullLogger) Debug(args ...interface{})                                   {}
func (n *nullLogger) Debugf(format string, args ...interface{})                   {}
func (n *nullLogger) Info(args ...interface{})                                    {}
func (n *nullLogger) Infof(format string, args ...interface{})                    {}
func (n *nullLogger) Warning(args ...interface{})                                 {}
func (n *nullLogger) Warningf(format string, args ...interface{})                 {}
func (n *nullLogger) Error(args ...interface{})                                   {}
func (n *nullLogger) Errorf(format string, args ...interface{})                   {}
func (n *nullLogger) Criticalf(format string, args ...interface{})                {}
func (n *nullLogger) Critical(args ...interface{})                                {}
func (n *nullLogger) Notice(args ...interface{})                                  {}
func (n *nullLogger) Noticef(format string, args ...interface{})                  {}
func (n *nullLogger) WithJob(job core.Job) core.Logger                            { return n }
func (n *nullLogger) WithJobName(name, jobType, schedule string) core.Logger      { return n }
func (n *nullLogger) WithContainer(containerID, containerName string) core.Logger { return n }
func (n *nullLogger) WithScheduler(schedulerName string) core.Logger              { return n }
func (n *nullLogger) WithJobExecution(name, executionID string) core.Logger       { return n }
func (n *nullLogger) GetExecutionID() string                                      { return "" }
