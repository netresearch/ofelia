package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

// =============================================================================
// Execute() mutation tests (lines 47-67)
// =============================================================================

// TestDoctorExecute_ApplyLogLevelError targets CONDITIONALS_NEGATION at line 47
// (err != nil for ApplyLogLevel). Even when log level fails, Execute should
// continue (not abort). The warning should be logged.
func TestDoctorExecute_ApplyLogLevelError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	err := os.WriteFile(configPath, []byte("[global]\n[job-local \"test\"]\nschedule = @daily\ncommand = echo test\n"), 0o644)
	require.NoError(t, err)

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		LogLevel:   "INVALID_LEVEL", // This should cause ApplyLogLevel to fail
		Logger:     logger,
		JSON:       true,
	}

	// Execute should succeed (log level error is non-fatal)
	_ = cmd.Execute(nil)
	assert.True(t, logger.HasWarning("Failed to apply log level"),
		"Expected warning about failed log level")
}

// TestDoctorExecute_AutoDetectionWhenEmpty targets CONDITIONALS_NEGATION at line 52
// (c.ConfigFile == ""). When ConfigFile is empty, auto-detection must be triggered.
func TestDoctorExecute_AutoDetectionWhenEmpty(t *testing.T) {
	origDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	// Create ofelia.ini in the temp dir
	err = os.WriteFile(filepath.Join(tmpDir, "ofelia.ini"),
		[]byte("[global]\n[job-local \"test\"]\nschedule = @daily\ncommand = echo test\n"), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: "", // Empty -> auto-detect
		Logger:     test.NewTestLogger(),
		JSON:       true,
	}

	_ = cmd.Execute(nil)
	assert.True(t, cmd.configAutoDetected, "configAutoDetected must be true when ConfigFile was empty")
	assert.NotEmpty(t, cmd.ConfigFile, "ConfigFile should be populated after auto-detection")
}

// TestDoctorExecute_ExplicitConfigSkipsAutoDetection targets the negation at line 52.
// When ConfigFile is set, auto-detection must NOT be triggered.
func TestDoctorExecute_ExplicitConfigSkipsAutoDetection(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "custom.ini")
	err := os.WriteFile(configPath, []byte("[global]\n[job-local \"test\"]\nschedule = @daily\ncommand = echo test\n"), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
		JSON:       true,
	}

	_ = cmd.Execute(nil)
	assert.False(t, cmd.configAutoDetected, "configAutoDetected must be false when explicit ConfigFile is provided")
}

// TestDoctorExecute_AutoDetectionFoundNotEmpty targets line 55:29
// (found != ""). When findConfigFile returns a path, it should be used.
func TestDoctorExecute_AutoDetectionFoundNotEmpty(t *testing.T) {
	origDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	err = os.WriteFile(filepath.Join(tmpDir, "ofelia.ini"),
		[]byte("[global]\n"), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: "",
		Logger:     test.NewTestLogger(),
		JSON:       true,
	}

	_ = cmd.Execute(nil)
	assert.Equal(t, "./ofelia.ini", cmd.ConfigFile,
		"ConfigFile must be set to the found config path")
}

// TestDoctorExecute_AutoDetectionNotFound_FallsBack targets the else branch at line 55:29.
// When no config file is found, fallback path should be used.
func TestDoctorExecute_AutoDetectionNotFound_FallsBack(t *testing.T) {
	origDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	// No config files created in tmpDir
	cmd := &DoctorCommand{
		ConfigFile: "",
		Logger:     test.NewTestLogger(),
		JSON:       true,
	}

	_ = cmd.Execute(nil)
	assert.Equal(t, "/etc/ofelia/config.ini", cmd.ConfigFile,
		"ConfigFile must fall back to /etc/ofelia/config.ini when no config found")
}

// TestDoctorExecute_JSONvsHumanOutput targets the JSON conditional at line 61
// (c.JSON) and the negation at line 67 (c.JSON for human output).
func TestDoctorExecute_JSONvsHumanOutput(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	err := os.WriteFile(configPath, []byte("[global]\n[job-local \"test\"]\nschedule = @daily\ncommand = echo test\n"), 0o644)
	require.NoError(t, err)

	t.Run("JSON mode outputs JSON", func(t *testing.T) {
		logger := test.NewTestLogger()
		cmd := &DoctorCommand{
			ConfigFile: configPath,
			Logger:     logger,
			JSON:       true,
		}
		_ = cmd.Execute(nil)
		// JSON output should have been logged
		messages := logger.GetMessages()
		foundJSON := false
		for _, msg := range messages {
			if strings.HasPrefix(strings.TrimSpace(msg.Message), "{") {
				foundJSON = true
				break
			}
		}
		assert.True(t, foundJSON, "JSON mode should output JSON content")
	})

	t.Run("Human mode outputs human-readable text", func(t *testing.T) {
		logger := test.NewTestLogger()
		cmd := &DoctorCommand{
			ConfigFile: configPath,
			Logger:     logger,
			JSON:       false,
		}
		_ = cmd.Execute(nil)
		assert.True(t, logger.HasMessage("Ofelia Health Check"),
			"Human mode should output 'Ofelia Health Check' header")
	})
}

// =============================================================================
// checkConfiguration mutation tests (lines 105-172)
// =============================================================================

// TestCheckConfiguration_FileNotExist targets CONDITIONALS_NEGATION at line 105.
// When the config file does not exist, it must set report.Healthy to false.
func TestCheckConfiguration_FileNotExist(t *testing.T) {
	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: "/nonexistent/path/config.ini",
		Logger:     logger,
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	assert.False(t, report.Healthy, "report must be unhealthy for missing config")
	require.NotEmpty(t, report.Checks)
	assert.Equal(t, statusFail, report.Checks[0].Status)
	assert.Equal(t, "File Exists", report.Checks[0].Name)
}

// TestCheckConfiguration_AutoDetectedShowsSearched targets the configAutoDetected
// conditional at line 116 (increment/decrement mutant at line 146:32 maps to
// the hints append). When auto-detected, hints must include "Searched:".
func TestCheckConfiguration_AutoDetectedShowsSearched(t *testing.T) {
	cmd := &DoctorCommand{
		ConfigFile:         "/nonexistent/config.ini",
		Logger:             test.NewTestLogger(),
		configAutoDetected: true,
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	assert.False(t, report.Healthy)
	require.NotEmpty(t, report.Checks)
	hintsStr := strings.Join(report.Checks[0].Hints, " ")
	assert.Contains(t, hintsStr, "Searched:", "auto-detected config should show searched paths")
}

// TestCheckConfiguration_ExplicitConfigHidesSearched targets the negation of
// configAutoDetected at line 116. When explicit, hints must NOT include "Searched:".
func TestCheckConfiguration_ExplicitConfigHidesSearched(t *testing.T) {
	cmd := &DoctorCommand{
		ConfigFile:         "/nonexistent/config.ini",
		Logger:             test.NewTestLogger(),
		configAutoDetected: false,
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	assert.False(t, report.Healthy)
	require.NotEmpty(t, report.Checks)
	hintsStr := strings.Join(report.Checks[0].Hints, " ")
	assert.NotContains(t, hintsStr, "Searched:", "explicit config should NOT show searched paths")
}

// TestCheckConfiguration_ParseError targets CONDITIONALS_NEGATION at line 127.
// When the config file has invalid syntax, must report parse error.
func TestCheckConfiguration_ParseError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bad.ini")
	err := os.WriteFile(configPath, []byte("[invalid section"), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	assert.False(t, report.Healthy)
	// Find the "Valid Syntax" check
	foundSyntaxFail := false
	for _, check := range report.Checks {
		if check.Name == "Valid Syntax" {
			assert.Equal(t, statusFail, check.Status)
			assert.Contains(t, check.Message, "Parse error")
			foundSyntaxFail = true
		}
	}
	assert.True(t, foundSyntaxFail, "must have a 'Valid Syntax' fail check")
}

// TestCheckConfiguration_ValidConfig targets the positive path at line 144.
// Verifies the pass check result for valid configuration.
func TestCheckConfiguration_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "good.ini")
	err := os.WriteFile(configPath, []byte("[global]\n[job-local \"test\"]\nschedule = @daily\ncommand = echo test\n"), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	assert.True(t, report.Healthy)
	// Find "File Exists" and "Valid Syntax" checks
	var fileCheck, syntaxCheck *CheckResult
	for i := range report.Checks {
		switch report.Checks[i].Name {
		case "File Exists":
			fileCheck = &report.Checks[i]
		case "Valid Syntax":
			syntaxCheck = &report.Checks[i]
		}
	}
	require.NotNil(t, fileCheck, "must have 'File Exists' check")
	assert.Equal(t, statusPass, fileCheck.Status)
	require.NotNil(t, syntaxCheck, "must have 'Valid Syntax' check")
	assert.Equal(t, statusPass, syntaxCheck.Status)
}

// TestCheckConfiguration_DeprecatedOptions targets line 155 (len(deprecations) > 0)
// and line 165 (the else branch for no deprecations).
func TestCheckConfiguration_NoDeprecations(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	err := os.WriteFile(configPath, []byte("[global]\n[job-local \"test\"]\nschedule = @daily\ncommand = echo test\n"), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	// Must have the "Deprecated Options" pass check
	foundDepCheck := false
	for _, check := range report.Checks {
		if check.Name == "Deprecated Options" {
			assert.Equal(t, statusPass, check.Status)
			assert.Contains(t, check.Message, "No deprecated options")
			foundDepCheck = true
		}
	}
	assert.True(t, foundDepCheck, "must have 'Deprecated Options' pass check")
}

// =============================================================================
// checkSchedules & job count arithmetic mutation tests (lines 200, 488-489)
// =============================================================================

// TestCheckConfiguration_JobCount targets ARITHMETIC_BASE mutants at lines 200:32,
// 200:54, 201:22. These would change + to - or other arithmetic in job count.
func TestCheckConfiguration_JobCount(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-local "local1"]
schedule = @daily
command = echo local1

[job-local "local2"]
schedule = @hourly
command = echo local2

[job-exec "exec1"]
schedule = @every 5s
command = echo exec
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	// Find the "Jobs Defined" check and verify count is correct
	for _, check := range report.Checks {
		if check.Name == "Jobs Defined" {
			// 2 local + 1 exec + 0 run + 0 service = 3
			assert.Contains(t, check.Message, "3 job(s) configured",
				"Job count must be 3 (2 local + 1 exec)")
		}
	}
}

// TestCheckSchedules_TotalJobCount targets ARITHMETIC_BASE and CONDITIONALS_BOUNDARY
// mutants at lines 488-489 in checkSchedules. These would alter the total job count
// arithmetic (len(RunJobs) + len(LocalJobs) + len(ExecJobs) + len(ServiceJobs) + len(ComposeJobs)).
func TestCheckSchedules_TotalJobCount(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-local "local1"]
schedule = @daily
command = echo local1

[job-exec "exec1"]
schedule = @every 5s
command = echo exec
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
		JSON:       true,
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkSchedules(report)

	// Find "All Schedules Valid" check and verify count
	for _, check := range report.Checks {
		if check.Name == "All Schedules Valid" {
			// 1 local + 1 exec + 0 run + 0 service + 0 compose = 2
			assert.Contains(t, check.Message, "2 schedule(s) validated",
				"Total schedule count must be 2 (1 local + 1 exec)")
		}
	}
}

// TestCheckSchedules_AllFiveJobTypes verifies all 5 job type counts contribute correctly.
// This targets each ARITHMETIC_BASE mutant that would change a + to - in the sum.
func TestCheckSchedules_AllFiveJobTypes(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-local "l1"]
schedule = @daily
command = echo l1

[job-exec "e1"]
schedule = @every 5s
command = echo e1

[job-run "r1"]
schedule = @hourly
image = alpine
command = echo r1

[job-service-run "s1"]
schedule = @weekly
image = nginx
command = echo s1

[job-compose "c1"]
schedule = @monthly
command = echo c1
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
		JSON:       true,
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkSchedules(report)

	// Find "All Schedules Valid" check
	for _, check := range report.Checks {
		if check.Name == "All Schedules Valid" {
			// 1 local + 1 exec + 1 run + 1 service + 1 compose = 5
			assert.Contains(t, check.Message, "5 schedule(s) validated",
				"Total schedule count must be 5 (one of each job type)")
		}
	}
}

// TestCheckSchedules_InvalidScheduleSetsUnhealthy targets the allValid flag behavior.
func TestCheckSchedules_InvalidScheduleSetsUnhealthy(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-local "bad"]
schedule = invalid
command = echo test
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}

	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkSchedules(report)

	assert.False(t, report.Healthy, "report must be unhealthy when schedule is invalid")
	foundFail := false
	for _, check := range report.Checks {
		if check.Status == statusFail && check.Category == "Job Schedules" {
			foundFail = true
			assert.Contains(t, check.Message, "Invalid schedule")
		}
	}
	assert.True(t, foundFail, "must have a failing schedule check")
}

// =============================================================================
// outputJSON mutation tests (line 580)
// =============================================================================

// TestOutputJSON_HealthyReport targets the !report.Healthy check at line 580.
// Healthy report should return nil error.
func TestOutputJSON_HealthyReport(t *testing.T) {
	logger := test.NewTestLogger()
	cmd := &DoctorCommand{Logger: logger, JSON: true}

	report := &DoctorReport{
		Healthy: true,
		Checks:  []CheckResult{{Category: "Test", Name: "Check", Status: statusPass}},
	}

	err := cmd.outputJSON(report)
	require.NoError(t, err, "outputJSON must return nil for healthy report")

	// Verify JSON was output
	messages := logger.GetMessages()
	foundJSON := false
	for _, msg := range messages {
		var parsed DoctorReport
		if json.Unmarshal([]byte(msg.Message), &parsed) == nil {
			foundJSON = true
			assert.True(t, parsed.Healthy)
		}
	}
	assert.True(t, foundJSON, "must output valid JSON")
}

// TestOutputJSON_UnhealthyReport targets the !report.Healthy branch returning error.
func TestOutputJSON_UnhealthyReport(t *testing.T) {
	logger := test.NewTestLogger()
	cmd := &DoctorCommand{Logger: logger, JSON: true}

	report := &DoctorReport{
		Healthy: false,
		Checks:  []CheckResult{{Category: "Test", Name: "Check", Status: statusFail, Message: "broken"}},
	}

	err := cmd.outputJSON(report)
	require.Error(t, err, "outputJSON must return error for unhealthy report")
	assert.Contains(t, err.Error(), "health check failed")
}

// =============================================================================
// outputHuman mutation tests (lines 625-647)
// =============================================================================

// TestOutputHuman_CountsFailAndSkip targets the fail/skip counting loop
// and conditionals at lines 628, 630, 635, 637, 644.
func TestOutputHuman_CountsFailAndSkip(t *testing.T) {
	logger := test.NewTestLogger()
	cmd := &DoctorCommand{Logger: logger}

	t.Run("healthy with skips shows skip count", func(t *testing.T) {
		lgr := test.NewTestLogger()
		cmd := &DoctorCommand{Logger: lgr}
		report := &DoctorReport{
			Healthy: true,
			Checks: []CheckResult{
				{Category: "Configuration", Name: "Check1", Status: statusPass},
				{Category: "Docker", Name: "Check2", Status: statusSkip, Message: "skipped"},
			},
		}
		err := cmd.outputHuman(report)
		require.NoError(t, err)
		assert.True(t, lgr.HasMessage("All checks passed"),
			"healthy report must show 'All checks passed'")
		assert.True(t, lgr.HasMessage("1 check(s) skipped as not applicable"),
			"must show skip count for healthy report")
	})

	t.Run("unhealthy with fails and skips", func(t *testing.T) {
		lgr := test.NewTestLogger()
		cmd := &DoctorCommand{Logger: lgr}
		report := &DoctorReport{
			Healthy: false,
			Checks: []CheckResult{
				{Category: "Configuration", Name: "Check1", Status: statusFail, Message: "broken"},
				{Category: "Configuration", Name: "Check2", Status: statusFail, Message: "also broken"},
				{Category: "Docker", Name: "Check3", Status: statusSkip, Message: "skipped"},
			},
		}
		err := cmd.outputHuman(report)
		require.Error(t, err)
		assert.True(t, lgr.HasMessage("2 issue(s) found"),
			"must show correct fail count")
		assert.True(t, lgr.HasMessage("1 check(s) skipped due to blockers"),
			"must show skip count for unhealthy report")
	})

	t.Run("unhealthy without skips", func(t *testing.T) {
		lgr := test.NewTestLogger()
		cmd = &DoctorCommand{Logger: lgr}
		report := &DoctorReport{
			Healthy: false,
			Checks: []CheckResult{
				{Category: "Configuration", Name: "Check1", Status: statusFail, Message: "broken"},
			},
		}
		err := cmd.outputHuman(report)
		require.Error(t, err)
		assert.True(t, lgr.HasMessage("1 issue(s) found"))
		assert.False(t, lgr.HasMessage("skipped due to blockers"),
			"must NOT show skip count when no skips")
	})

	t.Run("healthy without skips", func(t *testing.T) {
		lgr := test.NewTestLogger()
		cmd = &DoctorCommand{Logger: lgr}
		report := &DoctorReport{
			Healthy: true,
			Checks: []CheckResult{
				{Category: "Configuration", Name: "Check1", Status: statusPass},
			},
		}
		err := cmd.outputHuman(report)
		require.NoError(t, err)
		assert.True(t, lgr.HasMessage("All checks passed"))
		assert.False(t, lgr.HasMessage("skipped"),
			"must NOT show skip count when no skips")
	})

	_ = cmd // silence unused
}

// TestOutputHuman_CheckWithAndWithoutMessage targets line 610
// (check.Message != "") and its negation.
func TestOutputHuman_CheckWithAndWithoutMessage(t *testing.T) {
	t.Run("check with message", func(t *testing.T) {
		lgr := test.NewTestLogger()
		cmd := &DoctorCommand{Logger: lgr}
		report := &DoctorReport{
			Healthy: true,
			Checks: []CheckResult{
				{Category: "Configuration", Name: "MyCheck", Status: statusPass, Message: "detailed info"},
			},
		}
		_ = cmd.outputHuman(report)
		assert.True(t, lgr.HasMessage("detailed info"))
	})

	t.Run("check without message", func(t *testing.T) {
		lgr := test.NewTestLogger()
		cmd := &DoctorCommand{Logger: lgr}
		report := &DoctorReport{
			Healthy: true,
			Checks: []CheckResult{
				{Category: "Configuration", Name: "NoMsg", Status: statusPass, Message: ""},
			},
		}
		_ = cmd.outputHuman(report)
		assert.True(t, lgr.HasMessage("NoMsg"))
	})
}

// TestOutputHuman_CategoryNotInOrder targets the !exists check at line 601.
// Unknown categories should be silently skipped in output.
func TestOutputHuman_CategoryNotInOrder(t *testing.T) {
	lgr := test.NewTestLogger()
	cmd := &DoctorCommand{Logger: lgr}
	report := &DoctorReport{
		Healthy: true,
		Checks: []CheckResult{
			{Category: "UnknownCategory", Name: "Check1", Status: statusPass},
			{Category: "Configuration", Name: "Check2", Status: statusPass},
		},
	}
	err := cmd.outputHuman(report)
	require.NoError(t, err)
	// "UnknownCategory" is not in categoryOrder, so it should not appear
	assert.False(t, lgr.HasMessage("UnknownCategory"),
		"unknown categories should not appear in output")
	assert.True(t, lgr.HasMessage("Configuration"),
		"known categories should appear")
}

// =============================================================================
// getCategoryIcon mutation tests (line 652, 658, 663)
// =============================================================================

// TestGetCategoryIcon_AllBranches exhaustively tests all branches to kill
// CONDITIONALS_NEGATION at line 652 (icon, ok := ...) and line 663 (default).
func TestGetCategoryIcon_AllBranches(t *testing.T) {
	t.Parallel()

	// Known categories must return their specific icon
	assert.Equal(t, "📋", getCategoryIcon("Configuration"))
	assert.Equal(t, "🐳", getCategoryIcon("Docker"))
	assert.Equal(t, "📅", getCategoryIcon("Job Schedules"))
	assert.Equal(t, "🖼️", getCategoryIcon("Docker Images"))

	// Unknown category must return default icon, NOT any of the specific ones
	result := getCategoryIcon("UnknownCategory")
	assert.Equal(t, "📌", result)
	assert.NotEqual(t, "📋", result)
	assert.NotEqual(t, "🐳", result)
	assert.NotEqual(t, "📅", result)
	assert.NotEqual(t, "🖼️", result)
}

// =============================================================================
// getStatusIcon mutation tests (lines 665-676)
// =============================================================================

// TestGetStatusIcon_AllBranches exhaustively tests all branches to kill
// CONDITIONALS_NEGATION at lines 667, 672.
func TestGetStatusIcon_AllBranches(t *testing.T) {
	t.Parallel()

	// Each status must return its specific icon and NOT any other
	passIcon := getStatusIcon(statusPass)
	assert.Equal(t, "✅", passIcon)

	failIcon := getStatusIcon(statusFail)
	assert.Equal(t, "❌", failIcon)
	assert.NotEqual(t, passIcon, failIcon)

	skipIcon := getStatusIcon(statusSkip)
	assert.Equal(t, "⚠️", skipIcon)
	assert.NotEqual(t, passIcon, skipIcon)
	assert.NotEqual(t, failIcon, skipIcon)

	defaultIcon := getStatusIcon("other")
	assert.Equal(t, "❓", defaultIcon)
	assert.NotEqual(t, passIcon, defaultIcon)
	assert.NotEqual(t, failIcon, defaultIcon)
	assert.NotEqual(t, skipIcon, defaultIcon)
}

// =============================================================================
// checkWebAuth mutation tests (lines 105-172 overlap with configuration checks)
// =============================================================================

// TestCheckWebAuth_Enabled_MissingCredentials targets conditionals at lines
// that check WebAuthEnabled, WebUsername=="", WebPasswordHash=="", WebSecretKey=="".
func TestCheckWebAuth_Enabled_MissingCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
web-auth-enabled = true
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkWebAuth(report)

	assert.False(t, report.Healthy)
	// Should have failures for missing username and password hash
	usernameFail := false
	passwordFail := false
	secretSkip := false
	for _, check := range report.Checks {
		if strings.Contains(check.Name, "Username") && check.Status == statusFail {
			usernameFail = true
		}
		if strings.Contains(check.Name, "Password") && check.Status == statusFail {
			passwordFail = true
		}
		if strings.Contains(check.Name, "Secret Key") && check.Status == statusSkip {
			secretSkip = true
		}
	}
	assert.True(t, usernameFail, "missing username should fail")
	assert.True(t, passwordFail, "missing password hash should fail")
	assert.True(t, secretSkip, "missing secret key should be skip/warning")
}

// TestCheckWebAuth_Disabled targets the !conf.Global.WebAuthEnabled check.
// When web auth is disabled, no web auth checks should appear.
func TestCheckWebAuth_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
web-auth-enabled = false
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkWebAuth(report)

	assert.True(t, report.Healthy)
	assert.Empty(t, report.Checks, "no web auth checks when disabled")
}

// TestCheckWebAuth_AllCredentialsSet targets the positive pass path.
func TestCheckWebAuth_AllCredentialsSet(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
web-auth-enabled = true
web-username = admin
web-password-hash = $2a$10$something
web-secret-key = mysecret
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkWebAuth(report)

	assert.True(t, report.Healthy)
	// Should have pass check for web auth
	foundPass := false
	for _, check := range report.Checks {
		if check.Name == "Web Auth" && check.Status == statusPass {
			foundPass = true
			assert.Contains(t, check.Message, "admin")
		}
	}
	assert.True(t, foundPass, "must have 'Web Auth' pass check")
}

// =============================================================================
// Progress reporter conditionals in Execute (lines 89, 95, 100, 106, 121)
// =============================================================================

// TestDoctorExecute_ProgressInHumanMode tests that progress steps are called
// in human mode (non-JSON). This targets the progress != nil checks.
func TestDoctorExecute_ProgressInHumanMode(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	err := os.WriteFile(configPath, []byte("[global]\n[job-local \"test\"]\nschedule = @daily\ncommand = echo test\n"), 0o644)
	require.NoError(t, err)

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		JSON:       false,
	}
	_ = cmd.Execute(nil)

	// In human mode, progress messages should be logged
	assert.True(t, logger.HasMessage("Checking configuration"),
		"human mode should show progress messages")
}

// TestDoctorExecute_NoProgressInJSONMode tests that progress is NOT shown in JSON mode.
func TestDoctorExecute_NoProgressInJSONMode(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	err := os.WriteFile(configPath, []byte("[global]\n[job-local \"test\"]\nschedule = @daily\ncommand = echo test\n"), 0o644)
	require.NoError(t, err)

	logger := test.NewTestLogger()
	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     logger,
		JSON:       true,
	}
	_ = cmd.Execute(nil)

	// In JSON mode, NO progress messages should appear
	assert.False(t, logger.HasMessage("Checking configuration"),
		"JSON mode should NOT show progress messages")
}

// =============================================================================
// checkSchedules per-job-type invalid schedule tests (lines 399, 435, 453, 471)
// =============================================================================

// TestCheckSchedules_InvalidRunJobSchedule targets CONDITIONALS_NEGATION at line 399
// (if err := validateCronSchedule(job.Schedule); err != nil) for run jobs specifically.
func TestCheckSchedules_InvalidRunJobSchedule(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-run "bad-run"]
schedule = not-a-schedule
image = alpine
command = echo test
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkSchedules(report)

	assert.False(t, report.Healthy, "report must be unhealthy for invalid run job schedule")
	foundRunFail := false
	for _, check := range report.Checks {
		if check.Status == statusFail && strings.Contains(check.Name, "job-run") {
			foundRunFail = true
			assert.Contains(t, check.Message, "Invalid schedule")
		}
	}
	assert.True(t, foundRunFail, "must have a failing run job schedule check")
}

// TestCheckSchedules_InvalidExecJobSchedule targets CONDITIONALS_NEGATION at line 435.
func TestCheckSchedules_InvalidExecJobSchedule(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-exec "bad-exec"]
schedule = invalid-cron
command = echo test
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkSchedules(report)

	assert.False(t, report.Healthy, "report must be unhealthy for invalid exec job schedule")
	foundExecFail := false
	for _, check := range report.Checks {
		if check.Status == statusFail && strings.Contains(check.Name, "job-exec") {
			foundExecFail = true
			assert.Contains(t, check.Message, "Invalid schedule")
		}
	}
	assert.True(t, foundExecFail, "must have a failing exec job schedule check")
}

// TestCheckSchedules_InvalidServiceJobSchedule targets CONDITIONALS_NEGATION at line 453.
func TestCheckSchedules_InvalidServiceJobSchedule(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-service-run "bad-service"]
schedule = bad
image = nginx
command = echo test
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkSchedules(report)

	assert.False(t, report.Healthy, "report must be unhealthy for invalid service job schedule")
	foundServiceFail := false
	for _, check := range report.Checks {
		if check.Status == statusFail && strings.Contains(check.Name, "job-service-run") {
			foundServiceFail = true
			assert.Contains(t, check.Message, "Invalid schedule")
		}
	}
	assert.True(t, foundServiceFail, "must have a failing service job schedule check")
}

// TestCheckSchedules_InvalidComposeJobSchedule targets CONDITIONALS_NEGATION at line 471.
func TestCheckSchedules_InvalidComposeJobSchedule(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-compose "bad-compose"]
schedule = xyz
command = echo test
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkSchedules(report)

	assert.False(t, report.Healthy, "report must be unhealthy for invalid compose job schedule")
	foundComposeFail := false
	for _, check := range report.Checks {
		if check.Status == statusFail && strings.Contains(check.Name, "job-compose") {
			foundComposeFail = true
			assert.Contains(t, check.Message, "Invalid schedule")
		}
	}
	assert.True(t, foundComposeFail, "must have a failing compose job schedule check")
}

// =============================================================================
// hasDockerJobs boundary tests (lines 307-309)
// =============================================================================

// TestCheckDocker_HasDockerJobs_OnlyRunJobs targets CONDITIONALS_BOUNDARY at
// lines 307-309 (len(conf.RunJobs) > 0 || len(conf.ExecJobs) > 0 || len(conf.ServiceJobs) > 0).
// Each component must independently trigger the Docker check.
func TestCheckDocker_HasDockerJobs_OnlyRunJobs(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-run "r1"]
schedule = @daily
image = alpine
command = echo test
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkDocker(report)

	// Should attempt Docker check (and fail since no Docker), NOT skip
	foundDockerCheck := false
	for _, check := range report.Checks {
		if check.Category == "Docker" {
			foundDockerCheck = true
			// Should NOT be statusSkip - it should attempt the check
			assert.NotEqual(t, statusSkip, check.Status,
				"run jobs should trigger Docker check, not skip")
		}
	}
	assert.True(t, foundDockerCheck, "must have Docker category check for run jobs")
}

// TestCheckDocker_NoDockerJobs targets the !hasDockerJobs path at line 311.
func TestCheckDocker_NoDockerJobs(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-local "l1"]
schedule = @daily
command = echo test
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkDocker(report)

	// Should skip Docker check
	foundSkip := false
	for _, check := range report.Checks {
		if check.Category == "Docker" && check.Status == statusSkip {
			foundSkip = true
			assert.Contains(t, check.Message, "No Docker-based jobs")
		}
	}
	assert.True(t, foundSkip, "only local jobs should skip Docker check")
}

// =============================================================================
// outputHuman failCount++ increment test (line 629)
// =============================================================================

// TestOutputHuman_FailCountExact targets INCREMENT_DECREMENT at line 629
// (failCount++). If mutated to failCount--, the count would be wrong.
func TestOutputHuman_FailCountExact(t *testing.T) {
	lgr := test.NewTestLogger()
	cmd := &DoctorCommand{Logger: lgr}

	report := &DoctorReport{
		Healthy: false,
		Checks: []CheckResult{
			{Category: "Configuration", Name: "C1", Status: statusFail, Message: "err1"},
			{Category: "Configuration", Name: "C2", Status: statusFail, Message: "err2"},
			{Category: "Configuration", Name: "C3", Status: statusFail, Message: "err3"},
			{Category: "Configuration", Name: "C4", Status: statusPass},
		},
	}

	err := cmd.outputHuman(report)
	require.Error(t, err)
	// Exactly 3 failures
	assert.True(t, lgr.HasMessage("3 issue(s) found"),
		"must show exactly 3 failures, not any other count")
	assert.False(t, lgr.HasMessage("4 issue(s) found"),
		"must not count pass as failure")
	assert.False(t, lgr.HasMessage("2 issue(s) found"),
		"must not undercount failures")
}

// =============================================================================
// jobCount arithmetic test (line 201)
// =============================================================================

// TestCheckConfiguration_JobCount_ZeroJobs targets ARITHMETIC_BASE at line 201.
// With no jobs, the count should be exactly 0.
func TestCheckConfiguration_JobCount_ZeroJobs(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	err := os.WriteFile(configPath, []byte("[global]\n"), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkConfiguration(report)

	for _, check := range report.Checks {
		if check.Name == "Jobs Defined" {
			assert.Contains(t, check.Message, "0 job(s) configured",
				"Job count with no jobs must be exactly 0")
		}
	}
}

// =============================================================================
// findConfigFile mutation tests
// =============================================================================

// TestFindConfigFile_ReturnsEmptyForNoConfig ensures empty string return
// when no config files exist at any common path.
func TestFindConfigFile_ReturnsEmptyForNoConfig(t *testing.T) {
	origDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	result := findConfigFile()
	assert.Empty(t, result, "must return empty when no config files exist")
}

// =============================================================================
// checkDockerImages - imageMap / len(imageMap) boundary (line 512)
// =============================================================================

// TestCheckDockerImages_NoRunJobs targets the len(imageMap) == 0 check.
func TestCheckDockerImages_NoRunJobs(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	config := `[global]
[job-local "test"]
schedule = @daily
command = echo test
`
	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     test.NewTestLogger(),
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}
	cmd.checkDockerImages(report)

	// With no run jobs, should get a statusSkip result
	foundSkip := false
	for _, check := range report.Checks {
		if check.Category == "Docker Images" && check.Status == statusSkip {
			foundSkip = true
			assert.Contains(t, check.Message, "No job-run jobs")
		}
	}
	assert.True(t, foundSkip, "no run jobs should produce a skip check for Docker Images")
}
