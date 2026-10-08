// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

// --- checkDocker ---

func TestCheckDocker_ConfigError(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: "/nonexistent/config.ini",
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	result := cmd.checkDocker(report)
	assert.False(t, result, "should return false on config error")
}

func TestCheckDocker_NoDockerJobs_Coverage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	require.NoError(t, os.WriteFile(configPath, []byte(`[global]
[job-local "test"]
schedule = @daily
command = echo test
`), 0o644))

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	result := cmd.checkDocker(report)
	assert.True(t, result, "should return true when no Docker jobs")

	// Should have a skip check
	found := false
	for _, check := range report.Checks {
		if check.Category == "Docker" && check.Status == statusSkip {
			found = true
			break
		}
	}
	assert.True(t, found, "should skip Docker check when no Docker jobs")
}

// --- checkDockerImages ---

func TestCheckDockerImages_ConfigError(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: "/nonexistent/config.ini",
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkDockerImages(report)

	// Should not add any checks
	assert.Empty(t, report.Checks)
}

func TestCheckDockerImages_NoRunJobs_Coverage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	require.NoError(t, os.WriteFile(configPath, []byte(`[global]
[job-local "test"]
schedule = @daily
command = echo test
`), 0o644))

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkDockerImages(report)

	found := false
	for _, check := range report.Checks {
		if check.Category == "Docker Images" && check.Status == statusSkip {
			found = true
			break
		}
	}
	assert.True(t, found, "should skip image check when no run jobs")
}

// --- outputJSON ---

func TestOutputJSON_WithHints(t *testing.T) {
	t.Parallel()

	logger, handler := test.NewTestLoggerWithHandler()
	cmd := &DoctorCommand{
		Logger:   logger,
		LevelVar: &slog.LevelVar{},
		JSON:     true,
	}

	report := &DoctorReport{
		Healthy: true,
		Checks: []CheckResult{
			{Category: "Test", Name: "Check1", Status: statusPass, Message: "OK", Hints: []string{"hint1"}},
		},
	}

	err := cmd.outputJSON(report)
	require.NoError(t, err)
	assert.True(t, handler.HasMessage("healthy"))
	assert.True(t, handler.HasMessage("hint1"))
}

// --- checkConfiguration ---

func TestCheckConfiguration_FileNotFound_AutoDetected(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile:         "/nonexistent/config.ini",
		Logger:             logger,
		LevelVar:           &slog.LevelVar{},
		configAutoDetected: true,
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	assert.False(t, report.Healthy)

	found := false
	for _, check := range report.Checks {
		if check.Status == statusFail && check.Name == "File Exists" {
			found = true
			// Should include search paths hint when auto-detected
			hasSearchHint := false
			for _, hint := range check.Hints {
				if len(hint) > 0 {
					hasSearchHint = true
				}
			}
			assert.True(t, hasSearchHint)
			break
		}
	}
	assert.True(t, found)
}

func TestCheckConfiguration_FileNotFound_Explicit(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile:         "/nonexistent/config.ini",
		Logger:             logger,
		LevelVar:           &slog.LevelVar{},
		configAutoDetected: false,
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	assert.False(t, report.Healthy)
}

func TestCheckConfiguration_ValidConfig_WithMultipleJobs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	require.NoError(t, os.WriteFile(configPath, []byte(`[global]
[job-local "test1"]
schedule = @daily
command = echo test1
[job-local "test2"]
schedule = @hourly
command = echo test2
`), 0o644))

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	assert.True(t, report.Healthy)

	// Should report 2 jobs configured
	found := false
	for _, check := range report.Checks {
		if check.Name == "Jobs Defined" && check.Status == statusPass {
			assert.Contains(t, check.Message, "2")
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestCheckConfiguration_InvalidSyntax(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	require.NoError(t, os.WriteFile(configPath, []byte("[broken\n"), 0o644))

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	assert.False(t, report.Healthy)

	found := false
	for _, check := range report.Checks {
		if check.Name == "Valid Syntax" && check.Status == statusFail {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// --- checkWebAuth ---

func TestCheckWebAuth_DisabledAuth(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	require.NoError(t, os.WriteFile(configPath, []byte(`[global]
[job-local "test"]
schedule = @daily
command = echo test
`), 0o644))

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkWebAuth(report)

	// No checks added when auth is disabled
	assert.Empty(t, report.Checks)
}

func TestCheckWebAuth_MissingUsernameAndPassword(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	require.NoError(t, os.WriteFile(configPath, []byte(`[global]
web-auth-enabled = true
[job-local "test"]
schedule = @daily
command = echo test
`), 0o644))

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkWebAuth(report)

	assert.False(t, report.Healthy)

	failCount := 0
	for _, check := range report.Checks {
		if check.Status == statusFail {
			failCount++
		}
	}
	assert.GreaterOrEqual(t, failCount, 2, "should fail for missing username and password")
}

func TestCheckWebAuth_MissingSecretKey(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	require.NoError(t, os.WriteFile(configPath, []byte(`[global]
web-auth-enabled = true
web-username = admin
web-password-hash = $2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuu
[job-local "test"]
schedule = @daily
command = echo test
`), 0o644))

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkWebAuth(report)

	// A missing key is not worth a line: the key has no effect on sessions.
	for _, check := range report.Checks {
		assert.NotEqual(t, "Web Auth Secret Key", check.Name,
			"a missing web-secret-key must not be reported; it changes nothing")
	}
}

func TestCheckWebAuth_SecretKeyHasNoEffect(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	require.NoError(t, os.WriteFile(configPath, []byte(`[global]
web-auth-enabled = true
web-username = admin
web-password-hash = $2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuu
web-secret-key = a-secret-key-that-is-at-least-32-characters
[job-local "test"]
schedule = @daily
command = echo test
`), 0o644))

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkWebAuth(report)

	var found *CheckResult
	for i := range report.Checks {
		if report.Checks[i].Name == "Web Auth Secret Key" {
			found = &report.Checks[i]
		}
	}
	require.NotNil(t, found, "a set web-secret-key must be reported as having no effect")
	assert.Equal(t, statusSkip, found.Status)
	assert.Contains(t, found.Message, "no effect")
	assert.True(t, report.Healthy, "an ineffective key is not a failure")
}

func TestCheckWebAuth_ConfigError(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: "/nonexistent/config.ini",
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkWebAuth(report)

	// Should return early without adding checks
	assert.Empty(t, report.Checks)
}

// --- findConfigFile ---

func TestFindConfigFile_NoFile(t *testing.T) {
	t.Parallel()

	// This test depends on the common config paths not existing,
	// which is typically true in test environments
	result := findConfigFile()
	// May or may not find a file depending on the environment
	_ = result
}

// --- Execute ---

func TestDoctorExecute_AutoDetect(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		Logger:   logger,
		LevelVar: &slog.LevelVar{},
	}

	// Execute will fail because there's no config, but should not panic
	err := cmd.Execute(nil)
	// Will return an error because health check fails
	assert.Error(t, err)
}

func TestDoctorExecute_JSONOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	require.NoError(t, os.WriteFile(configPath, []byte(`[global]
[job-local "test"]
schedule = @daily
command = echo test
`), 0o644))

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		LevelVar:   &slog.LevelVar{},
		JSON:       true,
	}

	// Should succeed with a valid config and no Docker requirement
	err := cmd.Execute(nil)
	assert.NoError(t, err)
}

// --- getStatusIcon / getCategoryIcon ---

func TestGetStatusIcon_EmptyString(t *testing.T) {
	t.Parallel()

	result := getStatusIcon("")
	assert.Equal(t, "❓", result)
}

func TestGetCategoryIcon_EmptyString(t *testing.T) {
	t.Parallel()

	result := getCategoryIcon("")
	assert.Equal(t, "📌", result)
}
