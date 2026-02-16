package cli

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/ini.v1"
)

// discardLogger returns a silent logger for tests that don't need log inspection.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// TestValidateSchedule tests the schedule validation function
func TestValidateSchedule(t *testing.T) {
	tests := []struct {
		name      string
		schedule  string
		wantError bool
	}{
		// Valid descriptors
		{"valid @yearly", "@yearly", false},
		{"valid @annually", "@annually", false},
		{"valid @monthly", "@monthly", false},
		{"valid @weekly", "@weekly", false},
		{"valid @daily", "@daily", false},
		{"valid @midnight", "@midnight", false},
		{"valid @hourly", "@hourly", false},

		// Valid @every formats
		{"valid @every 1h", "@every 1h", false},
		{"valid @every 30m", "@every 30m", false},
		{"valid @every 1h30m", "@every 1h30m", false},

		// Valid cron expressions
		{"valid cron daily", "0 2 * * *", false},
		{"valid cron every 15 min", "*/15 * * * *", false},
		{"valid cron specific time", "30 14 * * 1-5", false},

		// Invalid cases
		{"empty schedule", "", true},
		{"invalid descriptor", "@invalid", true},
		{"invalid cron", "* * * *", true},            // missing field
		{"invalid cron format", "60 25 * * *", true}, // invalid values
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSchedule(tt.schedule)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSchedule(%q) error = %v, wantError %v", tt.schedule, err, tt.wantError)
			}
		})
	}
}

// TestRunJobConfigToINI tests the runJobConfig ToINI method
func TestRunJobConfigToINI(t *testing.T) {
	tests := []struct {
		name     string
		job      *runJobConfig
		validate func(*testing.T, *ini.Section)
	}{
		{
			name: "complete run job",
			job: &runJobConfig{
				JobName:  "test-job",
				Schedule: "@daily",
				Image:    "alpine:latest",
				Command:  "echo hello",
				Volume:   "/host:/container",
				Network:  "bridge",
				Delete:   true,
			},
			validate: func(t *testing.T, section *ini.Section) {
				if got := section.Key("schedule").String(); got != "@daily" {
					t.Errorf("schedule = %q, want %q", got, "@daily")
				}
				if got := section.Key("image").String(); got != "alpine:latest" {
					t.Errorf("image = %q, want %q", got, "alpine:latest")
				}
				if got := section.Key("command").String(); got != "echo hello" {
					t.Errorf("command = %q, want %q", got, "echo hello")
				}
				if got := section.Key("volume").String(); got != "/host:/container" {
					t.Errorf("volume = %q, want %q", got, "/host:/container")
				}
				if got := section.Key("network").String(); got != "bridge" {
					t.Errorf("network = %q, want %q", got, "bridge")
				}
				if got := section.Key("delete").String(); got != "true" {
					t.Errorf("delete = %q, want %q", got, "true")
				}
			},
		},
		{
			name: "minimal run job without optional fields",
			job: &runJobConfig{
				JobName:  "minimal-job",
				Schedule: "@hourly",
				Image:    "postgres:16",
				Command:  "pg_dump",
				Delete:   false,
			},
			validate: func(t *testing.T, section *ini.Section) {
				if got := section.Key("schedule").String(); got != "@hourly" {
					t.Errorf("schedule = %q, want %q", got, "@hourly")
				}
				if got := section.Key("volume").String(); got != "" {
					t.Errorf("volume should be empty, got %q", got)
				}
				if got := section.Key("network").String(); got != "" {
					t.Errorf("network should be empty, got %q", got)
				}
				if section.HasKey("delete") {
					t.Error("delete key should not be present when false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ini.Empty()
			section := cfg.Section("test")

			if err := tt.job.ToINI(section); err != nil {
				t.Fatalf("ToINI() error = %v", err)
			}

			tt.validate(t, section)
		})
	}
}

// TestLocalJobConfigToINI tests the localJobConfig ToINI method
func TestLocalJobConfigToINI(t *testing.T) {
	tests := []struct {
		name     string
		job      *localJobConfig
		validate func(*testing.T, *ini.Section)
	}{
		{
			name: "complete local job",
			job: &localJobConfig{
				JobName:  "backup",
				Schedule: "0 2 * * *",
				Command:  "/usr/local/bin/backup.sh",
				Dir:      "/var/backups",
			},
			validate: func(t *testing.T, section *ini.Section) {
				if got := section.Key("schedule").String(); got != "0 2 * * *" {
					t.Errorf("schedule = %q, want %q", got, "0 2 * * *")
				}
				if got := section.Key("command").String(); got != "/usr/local/bin/backup.sh" {
					t.Errorf("command = %q, want %q", got, "/usr/local/bin/backup.sh")
				}
				if got := section.Key("dir").String(); got != "/var/backups" {
					t.Errorf("dir = %q, want %q", got, "/var/backups")
				}
			},
		},
		{
			name: "minimal local job without dir",
			job: &localJobConfig{
				JobName:  "simple",
				Schedule: "@every 5m",
				Command:  "echo test",
			},
			validate: func(t *testing.T, section *ini.Section) {
				if got := section.Key("schedule").String(); got != "@every 5m" {
					t.Errorf("schedule = %q, want %q", got, "@every 5m")
				}
				if got := section.Key("dir").String(); got != "" {
					t.Errorf("dir should be empty, got %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ini.Empty()
			section := cfg.Section("test")

			if err := tt.job.ToINI(section); err != nil {
				t.Fatalf("ToINI() error = %v", err)
			}

			tt.validate(t, section)
		})
	}
}

// TestJobConfigInterface tests that types implement initJobConfig interface
func TestJobConfigInterface(t *testing.T) {
	var _ initJobConfig = (*runJobConfig)(nil)
	var _ initJobConfig = (*localJobConfig)(nil)
}

// TestJobConfigTypesAndNames tests Type() and Name() methods
func TestJobConfigTypesAndNames(t *testing.T) {
	tests := []struct {
		name     string
		job      initJobConfig
		wantType string
		wantName string
	}{
		{
			name:     "run job type and name",
			job:      &runJobConfig{JobName: "test-run"},
			wantType: "job-run",
			wantName: "test-run",
		},
		{
			name:     "local job type and name",
			job:      &localJobConfig{JobName: "test-local"},
			wantType: "job-local",
			wantName: "test-local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.job.Type(); got != tt.wantType {
				t.Errorf("Type() = %q, want %q", got, tt.wantType)
			}
			if got := tt.job.Name(); got != tt.wantName {
				t.Errorf("Name() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

// TestSaveConfig tests the config file generation
func TestSaveConfig(t *testing.T) {
	// Create a temporary directory for test outputs
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		config   *initConfig
		output   string
		validate func(*testing.T, string)
	}{
		{
			name: "complete config with web UI and jobs",
			config: &initConfig{
				Global: &globalConfig{
					EnableWeb: true,
					WebAddr:   "127.0.0.1:8081",
					LogLevel:  "info",
				},
				Jobs: []initJobConfig{
					&runJobConfig{
						JobName:  "backup",
						Schedule: "@daily",
						Image:    "postgres:16",
						Command:  "pg_dump",
						Volume:   "/backups:/backup",
						Delete:   true,
					},
					&localJobConfig{
						JobName:  "cleanup",
						Schedule: "@weekly",
						Command:  "find /tmp -type f -mtime +7 -delete",
						Dir:      "/tmp",
					},
				},
			},
			output: filepath.Join(tmpDir, "complete.ini"),
			validate: func(t *testing.T, path string) {
				cfg, err := ini.Load(path)
				if err != nil {
					t.Fatalf("Failed to load generated config: %v", err)
				}

				// Check global section
				global := cfg.Section("global")
				if got := global.Key("enable-web").String(); got != "true" {
					t.Errorf("enable-web = %q, want %q", got, "true")
				}
				if got := global.Key("web-address").String(); got != "127.0.0.1:8081" {
					t.Errorf("web-address = %q, want %q", got, "127.0.0.1:8081")
				}
				if got := global.Key("log-level").String(); got != "info" {
					t.Errorf("log-level = %q, want %q", got, "info")
				}

				// Check job sections
				runSection := cfg.Section(`job-run "backup"`)
				if got := runSection.Key("schedule").String(); got != "@daily" {
					t.Errorf("backup schedule = %q, want %q", got, "@daily")
				}
				if got := runSection.Key("image").String(); got != "postgres:16" {
					t.Errorf("backup image = %q, want %q", got, "postgres:16")
				}

				localSection := cfg.Section(`job-local "cleanup"`)
				if got := localSection.Key("schedule").String(); got != "@weekly" {
					t.Errorf("cleanup schedule = %q, want %q", got, "@weekly")
				}
			},
		},
		{
			name: "minimal config without web UI",
			config: &initConfig{
				Global: &globalConfig{
					EnableWeb: false,
					LogLevel:  "warning",
				},
				Jobs: []initJobConfig{},
			},
			output: filepath.Join(tmpDir, "minimal.ini"),
			validate: func(t *testing.T, path string) {
				cfg, err := ini.Load(path)
				if err != nil {
					t.Fatalf("Failed to load generated config: %v", err)
				}

				global := cfg.Section("global")
				if global.HasKey("enable-web") {
					t.Error("enable-web should not be present when false")
				}
				if got := global.Key("log-level").String(); got != "warning" {
					t.Errorf("log-level = %q, want %q", got, "warning")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &InitCommand{
				Output: tt.output,
				Logger: discardLogger(),
			}

			if err := cmd.saveConfig(tt.config); err != nil {
				t.Fatalf("saveConfig() error = %v", err)
			}

			// Verify file exists
			if _, err := os.Stat(tt.output); os.IsNotExist(err) {
				t.Fatalf("Config file was not created at %s", tt.output)
			}

			// Verify directory was created (permissions may vary by OS)
			dir := filepath.Dir(tt.output)
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				t.Errorf("Directory was not created at %s", dir)
			}

			// Run custom validation
			tt.validate(t, tt.output)
		})
	}
}

// TestSaveConfigCreatesDirectory tests that saveConfig creates missing directories
func TestSaveConfigCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "deep", "config.ini")

	cmd := &InitCommand{
		Output: nestedPath,
		Logger: discardLogger(),
	}

	config := &initConfig{
		Global: &globalConfig{LogLevel: "info"},
		Jobs:   []initJobConfig{},
	}

	if err := cmd.saveConfig(config); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	// Verify nested directory was created
	if _, err := os.Stat(filepath.Join(tmpDir, "nested", "deep")); os.IsNotExist(err) {
		t.Error("Nested directory was not created")
	}

	// Verify file exists
	if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
		t.Error("Config file was not created in nested directory")
	}
}
