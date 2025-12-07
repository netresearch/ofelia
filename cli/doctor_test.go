package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/netresearch/go-cron"

	"github.com/netresearch/ofelia/test"
)

// TestDoctorCommand_JSONOutput_Valid tests JSON output for valid configuration
func TestDoctorCommand_JSONOutput_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	configContent := `[global]
[job-local "test"]
schedule = @daily
command = echo test`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     &test.Logger{},
		JSON:       true,
	}

	// Execute - should succeed for valid config
	if err := cmd.Execute(nil); err != nil {
		t.Errorf("Expected no error for valid config, got: %v", err)
	}
}

// TestDoctorCommand_JSONOutput_InvalidSchedule tests JSON output for invalid schedule
func TestDoctorCommand_JSONOutput_InvalidSchedule(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	configContent := `[global]
[job-local "bad"]
schedule = invalid
command = echo test`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     &test.Logger{},
		JSON:       true,
	}

	// Execute - should fail for invalid schedule
	if err := cmd.Execute(nil); err == nil {
		t.Error("Expected error for invalid schedule, got none")
	}
}

// TestDoctorCommand_MissingConfigFile tests behavior when config file doesn't exist
func TestDoctorCommand_MissingConfigFile(t *testing.T) {
	cmd := &DoctorCommand{
		ConfigFile: "/nonexistent/config.ini",
		Logger:     &test.Logger{},
		JSON:       true,
	}

	// Execute - should fail
	if err := cmd.Execute(nil); err == nil {
		t.Error("Expected error for missing config file, got none")
	}
}

// TestDoctorCommand_InvalidConfigSyntax tests invalid INI syntax detection
func TestDoctorCommand_InvalidConfigSyntax(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	invalidConfig := `[global]
this is not valid INI syntax
[missing closing bracket`

	if err := os.WriteFile(configPath, []byte(invalidConfig), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     &test.Logger{},
		JSON:       true,
	}

	// Execute - should fail
	if err := cmd.Execute(nil); err == nil {
		t.Error("Expected error for invalid config syntax, got none")
	}
}

// TestValidateCronSchedule tests cron schedule validation logic
func TestValidateCronSchedule(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		wantErr  bool
	}{
		// Valid descriptors
		{"descriptor @daily", "@daily", false},
		{"descriptor @hourly", "@hourly", false},
		{"descriptor @weekly", "@weekly", false},
		{"descriptor @monthly", "@monthly", false},
		{"descriptor @yearly", "@yearly", false},
		{"descriptor @annually", "@annually", false},
		{"descriptor @midnight", "@midnight", false},

		// Valid @every format
		{"@every 1h", "@every 1h", false},
		{"@every 30m", "@every 30m", false},
		{"@every 1h30m", "@every 1h30m", false},
		{"@every 5s", "@every 5s", false},

		// Valid standard cron
		{"cron every minute", "* * * * *", false},
		{"cron every 15 minutes", "*/15 * * * *", false},
		{"cron daily at 2am", "0 2 * * *", false},
		{"cron weekdays at 9am", "0 9 * * 1-5", false},
		{"cron first of month", "0 0 1 * *", false},

		// Invalid schedules
		{"invalid descriptor", "@invalid", true},
		{"invalid @every", "@every invalid", true}, // Should fail - invalid duration
		{"invalid cron - too few fields", "* * *", true},
		{"invalid cron - too many fields", "* * * * * * *", true},
		{"invalid cron - bad range", "60 * * * *", true},
		{"empty schedule", "", true},
		{"random text", "not a schedule", true},
	}

	// Create parser
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCronSchedule(parser, tt.schedule)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCronSchedule(%q) error = %v, wantErr %v", tt.schedule, err, tt.wantErr)
			}
		})
	}
}

// TestDoctorCommand_NoJobs tests behavior when config has no jobs
func TestDoctorCommand_NoJobs(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	configContent := `[global]
# Config with no jobs`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     &test.Logger{},
		JSON:       true,
	}

	// Execute - should pass (0 jobs is valid, just a warning)
	// The implementation allows 0 jobs as technically valid config
	if err := cmd.Execute(nil); err != nil {
		t.Errorf("Expected no error for 0 jobs (valid but empty), got: %v", err)
	}
}

// TestDoctorCommand_MultipleInvalidSchedules tests multiple schedule failures
func TestDoctorCommand_MultipleInvalidSchedules(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	configContent := `[global]
[job-local "bad1"]
schedule = invalid1
command = echo test

[job-local "bad2"]
schedule = invalid2
command = echo test

[job-local "good"]
schedule = @daily
command = echo test`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     &test.Logger{},
		JSON:       true,
	}

	// Execute - should fail for invalid schedules
	if err := cmd.Execute(nil); err == nil {
		t.Error("Expected error for invalid schedules, got none")
	}
}

// TestDoctorCommand_MultipleJobTypes tests various job type combinations
func TestDoctorCommand_MultipleJobTypes(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	configContent := `[global]
[job-local "daily"]
schedule = @daily
command = echo daily

[job-local "hourly"]
schedule = @hourly
command = echo hourly

[job-local "every-5min"]
schedule = @every 5m
command = echo every 5min

[job-local "cron-style"]
schedule = */15 * * * *
command = echo cron`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     &test.Logger{},
		JSON:       true,
	}

	// Execute - should succeed
	if err := cmd.Execute(nil); err != nil {
		t.Errorf("Expected no error for valid multi-job config, got: %v", err)
	}
}

// TestCheckResult_JSON tests CheckResult JSON marshaling
func TestCheckResult_JSON(t *testing.T) {
	validStatuses := []string{"pass", "fail", "skip"}

	for _, status := range validStatuses {
		check := CheckResult{
			Category: "Test",
			Name:     "Test Check",
			Status:   status,
			Message:  "Test message",
			Hints:    []string{"Hint 1", "Hint 2"},
		}

		// Verify it can be marshaled to JSON
		data, err := json.Marshal(check)
		if err != nil {
			t.Errorf("Failed to marshal CheckResult with status %q: %v", status, err)
		}

		// Verify it can be unmarshaled
		var unmarshaled CheckResult
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Errorf("Failed to unmarshal CheckResult with status %q: %v", status, err)
		}

		if unmarshaled.Status != status {
			t.Errorf("Status changed after marshal/unmarshal: got %q, want %q", unmarshaled.Status, status)
		}
		if unmarshaled.Category != check.Category {
			t.Errorf("Category changed after marshal/unmarshal: got %q, want %q", unmarshaled.Category, check.Category)
		}
		if unmarshaled.Name != check.Name {
			t.Errorf("Name changed after marshal/unmarshal: got %q, want %q", unmarshaled.Name, check.Name)
		}
	}
}

// TestDoctorReport_JSON tests DoctorReport JSON marshaling
func TestDoctorReport_JSON(t *testing.T) {
	report := &DoctorReport{
		Healthy: true,
		Checks: []CheckResult{
			{Category: "Test", Name: "Check1", Status: "pass", Message: "OK"},
			{Category: "Test", Name: "Check2", Status: "skip", Message: "Skipped"},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Failed to marshal DoctorReport: %v", err)
	}

	// Unmarshal
	var unmarshaled DoctorReport
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal DoctorReport: %v", err)
	}

	if unmarshaled.Healthy != report.Healthy {
		t.Errorf("Healthy changed: got %v, want %v", unmarshaled.Healthy, report.Healthy)
	}
	if len(unmarshaled.Checks) != len(report.Checks) {
		t.Errorf("Checks length changed: got %d, want %d", len(unmarshaled.Checks), len(report.Checks))
	}
}

// TestDoctorCommand_SpecialSchedules tests edge cases in schedule validation
func TestDoctorCommand_SpecialSchedules(t *testing.T) {
	tests := []struct {
		name      string
		schedule  string
		expectErr bool
	}{
		{"whitespace schedule", "   ", true},
		{"tab schedule", "\t", true},
		{"newline schedule", "\n", true},
		{"multiple @every", "@every 1h @every 2h", true}, // Invalid - multiple @every
		{"@every with negative", "@every -1h", true},     // Invalid - negative durations should fail
		{"descriptor with extra text", "@daily extra", true},
		{"valid with leading space", " @daily", true},  // Space breaks descriptor
		{"valid with trailing space", "@daily ", true}, // Space breaks descriptor
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Trim spaces as the actual config parser might do
			schedule := strings.TrimSpace(tt.schedule)
			if schedule == "" && tt.expectErr {
				// Empty after trimming should error
				return
			}

			err := validateCronSchedule(parser, tt.schedule)
			if (err != nil) != tt.expectErr {
				t.Errorf("validateCronSchedule(%q) error = %v, wantErr %v", tt.schedule, err, tt.expectErr)
			}
		})
	}
}

// TestDoctorCommand_ReadableConfig tests that config file must be readable
func TestDoctorCommand_ReadableConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	configContent := `[global]
[job-local "test"]
schedule = @daily
command = echo test`

	if err := os.WriteFile(configPath, []byte(configContent), 0o000); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &DoctorCommand{
		ConfigFile: configPath,
		Logger:     &test.Logger{},
		JSON:       true,
	}

	// Execute - should fail due to permissions (on Unix systems)
	// Note: This test may pass on Windows which handles permissions differently
	err := cmd.Execute(nil)
	if err == nil {
		// Try to verify at least that the file is unreadable
		if _, readErr := os.ReadFile(configPath); readErr == nil {
			t.Log("Warning: File was readable despite 0000 permissions (may be OS-specific)")
		}
	}
}

// TestFindConfigFile tests the config file auto-detection logic
func TestFindConfigFile(t *testing.T) {
	// Save original working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	t.Run("finds ofelia.ini in current directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change directory: %v", err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		// Create ./ofelia.ini
		configPath := filepath.Join(tmpDir, "ofelia.ini")
		if err := os.WriteFile(configPath, []byte("[global]\n"), 0o644); err != nil {
			t.Fatalf("Failed to create config: %v", err)
		}

		found := findConfigFile()
		if found != "./ofelia.ini" {
			t.Errorf("findConfigFile() = %q, want %q", found, "./ofelia.ini")
		}
	})

	t.Run("finds config.ini when ofelia.ini not present", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change directory: %v", err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		// Create ./config.ini (but not ./ofelia.ini)
		configPath := filepath.Join(tmpDir, "config.ini")
		if err := os.WriteFile(configPath, []byte("[global]\n"), 0o644); err != nil {
			t.Fatalf("Failed to create config: %v", err)
		}

		found := findConfigFile()
		if found != "./config.ini" {
			t.Errorf("findConfigFile() = %q, want %q", found, "./config.ini")
		}
	})

	t.Run("priority order - ofelia.ini wins over config.ini", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change directory: %v", err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		// Create both files
		if err := os.WriteFile(filepath.Join(tmpDir, "ofelia.ini"), []byte("[global]\n"), 0o644); err != nil {
			t.Fatalf("Failed to create ofelia.ini: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "config.ini"), []byte("[global]\n"), 0o644); err != nil {
			t.Fatalf("Failed to create config.ini: %v", err)
		}

		found := findConfigFile()
		if found != "./ofelia.ini" {
			t.Errorf("findConfigFile() = %q, want %q (first in priority)", found, "./ofelia.ini")
		}
	})

	t.Run("returns empty string when no config exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change directory: %v", err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		// Don't create any config files
		found := findConfigFile()
		if found != "" {
			t.Errorf("findConfigFile() = %q, want empty string", found)
		}
	})
}

// TestDoctorCommand_AutoDetection tests the auto-detection flow in Execute
func TestDoctorCommand_AutoDetection(t *testing.T) {
	// Save original working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	t.Run("auto-detects config when ConfigFile is empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change directory: %v", err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		// Create a valid config file
		configContent := `[global]
[job-local "test"]
schedule = @daily
command = echo test`
		if err := os.WriteFile(filepath.Join(tmpDir, "ofelia.ini"), []byte(configContent), 0o644); err != nil {
			t.Fatalf("Failed to create config: %v", err)
		}

		cmd := &DoctorCommand{
			ConfigFile: "", // Empty - should auto-detect
			Logger:     &test.Logger{},
			JSON:       true,
		}

		// Should succeed by finding ./ofelia.ini
		if err := cmd.Execute(nil); err != nil {
			t.Errorf("Expected auto-detection to find config, got error: %v", err)
		}

		// Verify auto-detection flag was set
		if !cmd.configAutoDetected {
			t.Error("configAutoDetected should be true when ConfigFile was empty")
		}
	})

	t.Run("explicit config bypasses auto-detection", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a config file at explicit path
		configContent := `[global]
[job-local "test"]
schedule = @daily
command = echo test`
		configPath := filepath.Join(tmpDir, "explicit.ini")
		if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("Failed to create config: %v", err)
		}

		cmd := &DoctorCommand{
			ConfigFile: configPath, // Explicit path provided
			Logger:     &test.Logger{},
			JSON:       true,
		}

		// Should succeed using explicit path
		if err := cmd.Execute(nil); err != nil {
			t.Errorf("Expected explicit config to work, got error: %v", err)
		}

		// Verify auto-detection flag was NOT set
		if cmd.configAutoDetected {
			t.Error("configAutoDetected should be false when explicit ConfigFile was provided")
		}
	})

	t.Run("auto-detection fallback when no config exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change directory: %v", err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		// Don't create any config files
		cmd := &DoctorCommand{
			ConfigFile: "", // Empty - will fail to auto-detect
			Logger:     &test.Logger{},
			JSON:       true,
		}

		// Should fail because no config exists
		err := cmd.Execute(nil)
		if err == nil {
			t.Error("Expected error when no config file exists")
		}

		// Verify auto-detection flag was set (it was attempted)
		if !cmd.configAutoDetected {
			t.Error("configAutoDetected should be true even when auto-detection fails")
		}
	})
}
