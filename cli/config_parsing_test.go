package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// TestBuildFromString_AllJobTypes tests BuildFromString with all job types
func TestBuildFromString_AllJobTypes(t *testing.T) {
	t.Parallel()
	configStr := `
[global]
log-level = debug

[job-exec "exec-test"]
schedule = @every 5s
container = test-container
command = echo exec

[job-run "run-test"]
schedule = @every 10s
image = alpine
command = echo run

[job-local "local-test"]
schedule = @every 15s
command = echo local

[job-service-run "service-test"]
schedule = @every 20s
image = nginx
command = echo service

[job-compose "compose-test"]
schedule = @every 25s
command = up -d
`

	logger := test.NewTestLogger()
	cfg, err := BuildFromString(configStr, logger)
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}

	// Verify all job types were parsed
	if len(cfg.ExecJobs) != 1 {
		t.Errorf("Expected 1 exec job, got %d", len(cfg.ExecJobs))
	}
	if len(cfg.RunJobs) != 1 {
		t.Errorf("Expected 1 run job, got %d", len(cfg.RunJobs))
	}
	if len(cfg.LocalJobs) != 1 {
		t.Errorf("Expected 1 local job, got %d", len(cfg.LocalJobs))
	}
	if len(cfg.ServiceJobs) != 1 {
		t.Errorf("Expected 1 service job, got %d", len(cfg.ServiceJobs))
	}
	if len(cfg.ComposeJobs) != 1 {
		t.Errorf("Expected 1 compose job, got %d", len(cfg.ComposeJobs))
	}

	// Verify global config was parsed
	if cfg.Global.LogLevel != "debug" {
		t.Errorf("Expected log level 'debug', got %q", cfg.Global.LogLevel)
	}
}

// TestBuildFromFile_WithGlobPattern tests BuildFromFile with glob patterns
func TestBuildFromFile_WithGlobPattern(t *testing.T) {
	t.Parallel()
	// Create temporary directory
	dir := t.TempDir()

	// Create multiple config files
	file1Content := `
[job-exec "job1"]
schedule = @every 5s
command = echo job1
`
	file2Content := `
[job-run "job2"]
schedule = @every 10s
image = alpine
command = echo job2
`
	file3Content := `
[job-local "job3"]
schedule = @every 15s
command = echo job3
`

	files := map[string]string{
		"01-exec.ini":  file1Content,
		"02-run.ini":   file2Content,
		"03-local.ini": file3Content,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to write file %s: %v", name, err)
		}
	}

	// Test with glob pattern
	pattern := filepath.Join(dir, "*.ini")
	logger := test.NewTestLogger()
	cfg, err := BuildFromFile(pattern, logger)
	if err != nil {
		t.Fatalf("BuildFromFile failed: %v", err)
	}

	// Verify all jobs were loaded
	if len(cfg.ExecJobs) != 1 {
		t.Errorf("Expected 1 exec job, got %d", len(cfg.ExecJobs))
	}
	if len(cfg.RunJobs) != 1 {
		t.Errorf("Expected 1 run job, got %d", len(cfg.RunJobs))
	}
	if len(cfg.LocalJobs) != 1 {
		t.Errorf("Expected 1 local job, got %d", len(cfg.LocalJobs))
	}

	// Verify config files were tracked
	if len(cfg.configFiles) != 3 {
		t.Errorf("Expected 3 config files tracked, got %d", len(cfg.configFiles))
	}
}

// TestBuildFromFile_InvalidGlobPattern tests error handling for invalid glob patterns
func TestBuildFromFile_InvalidGlobPattern(t *testing.T) {
	t.Parallel()
	// Invalid glob pattern (malformed bracket expression)
	invalidPattern := "/invalid/[z-a]/*.ini"

	logger := test.NewTestLogger()
	_, err := BuildFromFile(invalidPattern, logger)

	if err == nil {
		t.Error("Expected error for invalid glob pattern, got nil")
	}
}

// TestBuildFromFile_NonExistentFile tests handling of non-existent files
func TestBuildFromFile_NonExistentFile(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	_, err := BuildFromFile("/nonexistent/ofelia.ini", logger)

	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

// TestIniConfigUpdate_WithChangedFiles tests iniConfigUpdate detects file changes
func TestIniConfigUpdate_WithChangedFiles(t *testing.T) {
	t.Parallel()
	// This test verifies the file change detection logic
	dir := t.TempDir()

	configFile := filepath.Join(dir, "config.ini")
	initialContent := `
[job-run "job1"]
schedule = @every 10s
image = alpine
command = echo initial
`

	// Write initial config
	if err := os.WriteFile(configFile, []byte(initialContent), 0o644); err != nil {
		t.Fatalf("Failed to write initial config: %v", err)
	}

	logger := test.NewTestLogger()
	cfg, err := BuildFromFile(configFile, logger)
	if err != nil {
		t.Fatalf("BuildFromFile failed: %v", err)
	}

	// Initialize scheduler and handler
	cfg.sh = core.NewScheduler(logger)
	cfg.dockerHandler = &DockerHandler{logger: logger}

	// Call iniConfigUpdate - should detect no change
	err = cfg.iniConfigUpdate()
	if err != nil {
		t.Errorf("iniConfigUpdate failed: %v", err)
	}

	// Now modify the file
	updatedContent := `
[job-run "job1"]
schedule = @every 20s
image = alpine
command = echo updated
`
	// Wait a bit to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(configFile, []byte(updatedContent), 0o644); err != nil {
		t.Fatalf("Failed to write updated config: %v", err)
	}

	// Update the config's modtime to the past so change will be detected
	cfg.configModTime = cfg.configModTime.Add(-1 * time.Minute)

	// Call iniConfigUpdate again - should detect change
	err = cfg.iniConfigUpdate()
	if err != nil {
		t.Errorf("iniConfigUpdate after change failed: %v", err)
	}
}

// TestBuildFromString_JobValidation tests job configuration validation
func TestBuildFromString_JobValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		config    string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "job-run_valid_with_image",
			config: `
[job-run "valid-run"]
schedule = @every 10s
image = alpine
command = echo hello
`,
			wantErr: false,
		},
		{
			name: "job-run_valid_with_container",
			config: `
[job-run "valid-run"]
schedule = @every 10s
container = existing-container
`,
			wantErr: false,
		},
		{
			name: "job-run_invalid_missing_image_and_container",
			config: `
[job-run "invalid-run"]
schedule = @every 10s
command = echo hello
`,
			wantErr:   true,
			errSubstr: "job-run requires either 'image'",
		},
		{
			name: "job-service-run_valid_with_image",
			config: `
[job-service-run "valid-service"]
schedule = @every 10s
image = nginx
`,
			wantErr: false,
		},
		{
			name: "job-service-run_invalid_missing_image",
			config: `
[job-service-run "invalid-service"]
schedule = @every 10s
command = echo hello
`,
			wantErr:   true,
			errSubstr: "job-service-run requires 'image'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger := test.NewTestLogger()
			_, err := BuildFromString(tt.config, logger)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
					return
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("Expected error to contain %q, got %q", tt.errSubstr, err.Error())
				}
			} else if err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

// TestBuildFromString_RunOnStartup tests parsing of run-on-startup option
func TestBuildFromString_RunOnStartup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		config         string
		wantRunOnStart bool
		wantSchedule   string
	}{
		{
			name: "run-on-startup_true",
			config: `
[job-exec "startup-job"]
schedule = @every 1h
container = my-container
command = echo hello
run-on-startup = true
`,
			wantRunOnStart: true,
			wantSchedule:   "@every 1h",
		},
		{
			name: "run-on-startup_false",
			config: `
[job-exec "no-startup-job"]
schedule = @every 1h
container = my-container
command = echo hello
run-on-startup = false
`,
			wantRunOnStart: false,
			wantSchedule:   "@every 1h",
		},
		{
			name: "run-on-startup_default",
			config: `
[job-exec "default-job"]
schedule = @every 1h
container = my-container
command = echo hello
`,
			wantRunOnStart: false, // Default is false
			wantSchedule:   "@every 1h",
		},
		{
			name: "run-on-startup_with_triggered_schedule",
			config: `
[job-exec "triggered-job"]
schedule = @triggered
container = my-container
command = echo hello
run-on-startup = true
`,
			wantRunOnStart: true,
			wantSchedule:   "@triggered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger := test.NewTestLogger()
			cfg, err := BuildFromString(tt.config, logger)
			if err != nil {
				t.Fatalf("BuildFromString failed: %v", err)
			}

			if len(cfg.ExecJobs) != 1 {
				t.Fatalf("Expected 1 exec job, got %d", len(cfg.ExecJobs))
			}

			for _, job := range cfg.ExecJobs {
				if job.RunOnStartup != tt.wantRunOnStart {
					t.Errorf("RunOnStartup = %v, want %v", job.RunOnStartup, tt.wantRunOnStart)
				}
				if job.Schedule != tt.wantSchedule {
					t.Errorf("Schedule = %v, want %v", job.Schedule, tt.wantSchedule)
				}
			}
		})
	}
}

// TestBuildFromString_RunOnStartupAllJobTypes tests run-on-startup works for all job types
func TestBuildFromString_RunOnStartupAllJobTypes(t *testing.T) {
	t.Parallel()
	configStr := `
[job-exec "exec-startup"]
schedule = @every 1h
container = test-container
command = echo exec
run-on-startup = true

[job-run "run-startup"]
schedule = @every 1h
image = alpine
command = echo run
run-on-startup = true

[job-local "local-startup"]
schedule = @every 1h
command = echo local
run-on-startup = true

[job-service-run "service-startup"]
schedule = @every 1h
image = nginx
command = echo service
run-on-startup = true

[job-compose "compose-startup"]
schedule = @every 1h
command = up -d
run-on-startup = true
`

	logger := test.NewTestLogger()
	cfg, err := BuildFromString(configStr, logger)
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}

	// Verify all job types have run-on-startup enabled
	for _, job := range cfg.ExecJobs {
		if !job.RunOnStartup {
			t.Errorf("ExecJob %q: RunOnStartup = false, want true", job.Name)
		}
	}
	for _, job := range cfg.RunJobs {
		if !job.RunOnStartup {
			t.Errorf("RunJob %q: RunOnStartup = false, want true", job.Name)
		}
	}
	for _, job := range cfg.LocalJobs {
		if !job.RunOnStartup {
			t.Errorf("LocalJob %q: RunOnStartup = false, want true", job.Name)
		}
	}
	for _, job := range cfg.ServiceJobs {
		if !job.RunOnStartup {
			t.Errorf("ServiceJob %q: RunOnStartup = false, want true", job.Name)
		}
	}
	for _, job := range cfg.ComposeJobs {
		if !job.RunOnStartup {
			t.Errorf("ComposeJob %q: RunOnStartup = false, want true", job.Name)
		}
	}
}

// TestResolveConfigFiles tests the resolveConfigFiles function
func TestResolveConfigFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		wantErr bool
		wantMin int // minimum number of files expected
	}{
		{
			name:    "single file",
			wantErr: false,
			wantMin: 1,
		},
		{
			name:    "glob pattern with multiple files",
			wantErr: false,
			wantMin: 2,
		},
		{
			name:    "non-existent file returns pattern as literal",
			wantErr: false,
			wantMin: 1, // returns the pattern itself
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var pattern string
			switch tt.name {
			case "single file":
				path := filepath.Join(t.TempDir(), "ofelia.ini")
				require.NoError(t, os.WriteFile(path, []byte(""), 0o644))
				pattern = path
			case "glob pattern with multiple files":
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "a.ini"), []byte(""), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "b.ini"), []byte(""), 0o644))
				pattern = filepath.Join(dir, "*.ini")
			default:
				pattern = "/nonexistent/file.ini"
			}

			files, err := resolveConfigFiles(pattern)

			if (err != nil) != tt.wantErr {
				t.Errorf("resolveConfigFiles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(files) < tt.wantMin {
				t.Errorf("Expected at least %d files, got %d", tt.wantMin, len(files))
			}
		})
	}
}
