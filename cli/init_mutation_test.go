package cli

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/ini.v1"
)

// TestValidateScheduleMutationCoverage provides comprehensive tests targeting
// mutation testing gaps in validateSchedule function
func TestValidateScheduleMutationCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		schedule  string
		wantError bool
	}{
		// Empty schedule - boundary condition
		{"empty string", "", true},
		{"whitespace only", "   ", true},

		// Special descriptors - exact matches
		{"@yearly exact", "@yearly", false},
		{"@annually exact", "@annually", false},
		{"@monthly exact", "@monthly", false},
		{"@weekly exact", "@weekly", false},
		{"@daily exact", "@daily", false},
		{"@midnight exact", "@midnight", false},
		{"@hourly exact", "@hourly", false},

		// @every variations - boundary conditions
		{"@every with space", "@every 1h", false},
		{"@every minutes", "@every 30m", false},
		{"@every seconds", "@every 45s", false},
		{"@every combined", "@every 1h30m15s", false},
		{"@every no space", "@every1h", true}, // invalid - no space

		// Invalid special descriptors
		{"@invalid descriptor", "@invalid", true},
		{"@yearly with suffix", "@yearly-extra", true},
		{"@daily with space", "@daily ", true},
		{"@ alone", "@", true},
		{"@e partial", "@e", true},
		{"@ever typo", "@ever 1h", true},

		// Valid 5-field cron expressions
		{"5-field all stars", "* * * * *", false},
		{"5-field specific", "0 0 1 1 *", false},
		{"5-field ranges", "0-30 9-17 * * 1-5", false},
		{"5-field steps", "*/5 */2 * * *", false},
		{"5-field lists", "0,15,30,45 * * * *", false},

		// 6-field cron expressions (NOT supported - parser requires exactly 5 fields)
		{"6-field with seconds", "0 * * * * *", true},
		{"6-field all stars", "* * * * * *", true},
		{"6-field specific", "30 0 0 1 1 *", true},

		// Invalid cron - field count
		{"4 fields only", "* * * *", true},
		{"3 fields only", "* * *", true},
		{"7 fields", "* * * * * * *", true},
		{"1 field", "*", true},

		// Invalid cron - bad values (caught by parser)
		{"invalid minute 60", "60 * * * *", true},
		{"invalid hour 25", "* 25 * * *", true},
		{"invalid day 32", "* * 32 * *", true},
		{"invalid month 13", "* * * 13 *", true},
		{"invalid dow 8", "* * * * 8", true},

		// Edge cases with special characters
		{"question mark", "0 0 ? * *", false}, // ? is valid in some parsers
		{"hash for random", "0 0 # * *", true},
		{"invalid chars", "a b c d e", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateSchedule(tt.schedule)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSchedule(%q) error = %v, wantError %v", tt.schedule, err, tt.wantError)
			}
		})
	}
}

// TestRunJobConfigToINIMutationCoverage tests edge cases for runJobConfig
func TestRunJobConfigToINIMutationCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		job      *runJobConfig
		validate func(*testing.T, *ini.Section)
	}{
		{
			name: "all optional fields empty",
			job: &runJobConfig{
				JobName:  "minimal",
				Schedule: "@hourly",
				Image:    "alpine",
				Command:  "echo",
				Volume:   "",
				Network:  "",
				Delete:   false,
			},
			validate: func(t *testing.T, section *ini.Section) {
				// Volume should not be set when empty
				if section.HasKey("volume") && section.Key("volume").String() != "" {
					t.Error("volume key should not be set or be empty when Volume is empty")
				}
				// Network should not be set when empty
				if section.HasKey("network") && section.Key("network").String() != "" {
					t.Error("network key should not be set or be empty when Network is empty")
				}
				// Delete should not be present when false
				if section.HasKey("delete") {
					t.Error("delete key should not be present when Delete is false")
				}
			},
		},
		{
			name: "volume set but network empty",
			job: &runJobConfig{
				JobName:  "partial",
				Schedule: "@daily",
				Image:    "nginx",
				Command:  "nginx -t",
				Volume:   "/data:/data",
				Network:  "",
				Delete:   true,
			},
			validate: func(t *testing.T, section *ini.Section) {
				if got := section.Key("volume").String(); got != "/data:/data" {
					t.Errorf("volume = %q, want %q", got, "/data:/data")
				}
				// Network should not be set when empty
				if section.HasKey("network") && section.Key("network").String() != "" {
					t.Error("network should be empty")
				}
				if got := section.Key("delete").String(); got != "true" {
					t.Errorf("delete = %q, want %q", got, "true")
				}
			},
		},
		{
			name: "network set but volume empty",
			job: &runJobConfig{
				JobName:  "network-only",
				Schedule: "*/5 * * * *",
				Image:    "redis",
				Command:  "redis-cli ping",
				Volume:   "",
				Network:  "my-network",
				Delete:   false,
			},
			validate: func(t *testing.T, section *ini.Section) {
				// Volume should not be set when empty
				if section.HasKey("volume") && section.Key("volume").String() != "" {
					t.Error("volume should be empty")
				}
				if got := section.Key("network").String(); got != "my-network" {
					t.Errorf("network = %q, want %q", got, "my-network")
				}
			},
		},
		{
			name: "special characters in values",
			job: &runJobConfig{
				JobName:  "special-chars",
				Schedule: "@every 30s",
				Image:    "my-registry.io/my-image:v1.2.3-beta",
				Command:  "echo 'hello world' && ls -la",
				Volume:   "/path with spaces:/container/path",
				Network:  "network_with_underscore",
				Delete:   true,
			},
			validate: func(t *testing.T, section *ini.Section) {
				if got := section.Key("image").String(); got != "my-registry.io/my-image:v1.2.3-beta" {
					t.Errorf("image = %q, want registry path with tag", got)
				}
				if got := section.Key("command").String(); got != "echo 'hello world' && ls -la" {
					t.Errorf("command = %q, want command with special chars", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := ini.Empty()
			section := cfg.Section("test")

			if err := tt.job.ToINI(section); err != nil {
				t.Fatalf("ToINI() error = %v", err)
			}

			tt.validate(t, section)
		})
	}
}

// TestLocalJobConfigToINIMutationCoverage tests edge cases for localJobConfig
func TestLocalJobConfigToINIMutationCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		job      *localJobConfig
		validate func(*testing.T, *ini.Section)
	}{
		{
			name: "empty dir",
			job: &localJobConfig{
				JobName:  "no-dir",
				Schedule: "@hourly",
				Command:  "echo test",
				Dir:      "",
			},
			validate: func(t *testing.T, section *ini.Section) {
				// Dir should not be set when empty
				if section.HasKey("dir") && section.Key("dir").String() != "" {
					t.Error("dir key should not be set or be empty when Dir is empty")
				}
			},
		},
		{
			name: "dir with spaces",
			job: &localJobConfig{
				JobName:  "spaced-dir",
				Schedule: "@daily",
				Command:  "backup.sh",
				Dir:      "/path/with spaces/and more",
			},
			validate: func(t *testing.T, section *ini.Section) {
				if got := section.Key("dir").String(); got != "/path/with spaces/and more" {
					t.Errorf("dir = %q, want path with spaces", got)
				}
			},
		},
		{
			name: "complex command",
			job: &localJobConfig{
				JobName:  "complex",
				Schedule: "0 */2 * * *",
				Command:  "bash -c 'for i in {1..10}; do echo $i; done'",
				Dir:      "/tmp",
			},
			validate: func(t *testing.T, section *ini.Section) {
				expected := "bash -c 'for i in {1..10}; do echo $i; done'"
				if got := section.Key("command").String(); got != expected {
					t.Errorf("command = %q, want %q", got, expected)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := ini.Empty()
			section := cfg.Section("test")

			if err := tt.job.ToINI(section); err != nil {
				t.Fatalf("ToINI() error = %v", err)
			}

			tt.validate(t, section)
		})
	}
}

// TestSaveConfigMutationCoverage tests saveConfig edge cases
func TestSaveConfigMutationCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *initConfig
		validate func(*testing.T, string)
	}{
		{
			name: "empty global settings",
			config: &initConfig{
				Global: &globalConfig{
					EnableWeb: false,
					WebAddr:   "",
					LogLevel:  "",
				},
				Jobs: []initJobConfig{},
			},
			validate: func(t *testing.T, path string) {
				cfg, err := ini.Load(path)
				if err != nil {
					t.Fatalf("Failed to load config: %v", err)
				}
				global := cfg.Section("global")
				// When EnableWeb is false, enable-web should not be present
				if global.HasKey("enable-web") {
					t.Error("enable-web should not be present when false")
				}
				// When WebAddr is empty, web-address should not be present
				if global.HasKey("web-address") && global.Key("web-address").String() != "" {
					t.Error("web-address should not be present when empty")
				}
			},
		},
		{
			name: "web enabled but empty address",
			config: &initConfig{
				Global: &globalConfig{
					EnableWeb: true,
					WebAddr:   "",
					LogLevel:  "debug",
				},
				Jobs: []initJobConfig{},
			},
			validate: func(t *testing.T, path string) {
				cfg, err := ini.Load(path)
				if err != nil {
					t.Fatalf("Failed to load config: %v", err)
				}
				global := cfg.Section("global")
				if got := global.Key("enable-web").String(); got != "true" {
					t.Errorf("enable-web = %q, want %q", got, "true")
				}
				// web-address should not be set if empty
				if global.HasKey("web-address") && global.Key("web-address").String() != "" {
					t.Error("web-address should not be set when WebAddr is empty")
				}
			},
		},
		{
			name: "multiple jobs of same type",
			config: &initConfig{
				Global: &globalConfig{LogLevel: "info"},
				Jobs: []initJobConfig{
					&runJobConfig{JobName: "job1", Schedule: "@hourly", Image: "alpine", Command: "echo 1"},
					&runJobConfig{JobName: "job2", Schedule: "@daily", Image: "alpine", Command: "echo 2"},
					&localJobConfig{JobName: "local1", Schedule: "@weekly", Command: "ls"},
					&localJobConfig{JobName: "local2", Schedule: "@monthly", Command: "pwd"},
				},
			},
			validate: func(t *testing.T, path string) {
				cfg, err := ini.Load(path)
				if err != nil {
					t.Fatalf("Failed to load config: %v", err)
				}
				// Check all sections exist
				sections := []string{`job-run "job1"`, `job-run "job2"`, `job-local "local1"`, `job-local "local2"`}
				for _, s := range sections {
					if cfg.Section(s) == nil {
						t.Errorf("Section %q not found", s)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			outputPath := filepath.Join(tmpDir, "test.ini")

			cmd := &InitCommand{
				Output: outputPath,
				Logger: discardLogger(),
			}

			if err := cmd.saveConfig(tt.config); err != nil {
				t.Fatalf("saveConfig() error = %v", err)
			}

			tt.validate(t, outputPath)
		})
	}
}

// TestSaveConfigDirectoryCreation tests directory creation edge cases
func TestSaveConfigDirectoryCreation(t *testing.T) {
	t.Parallel()

	t.Run("deeply nested directory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		deepPath := filepath.Join(tmpDir, "a", "b", "c", "d", "e", "config.ini")

		cmd := &InitCommand{
			Output: deepPath,
			Logger: discardLogger(),
		}

		config := &initConfig{
			Global: &globalConfig{LogLevel: "info"},
			Jobs:   []initJobConfig{},
		}

		if err := cmd.saveConfig(config); err != nil {
			t.Fatalf("saveConfig() error = %v", err)
		}

		if _, err := os.Stat(deepPath); os.IsNotExist(err) {
			t.Error("Config file was not created in deeply nested directory")
		}
	})

	t.Run("current directory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		outputPath := filepath.Join(tmpDir, "config.ini")

		cmd := &InitCommand{
			Output: outputPath,
			Logger: discardLogger(),
		}

		config := &initConfig{
			Global: &globalConfig{LogLevel: "warning"},
			Jobs:   []initJobConfig{},
		}

		if err := cmd.saveConfig(config); err != nil {
			t.Fatalf("saveConfig() error = %v", err)
		}

		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			t.Error("Config file was not created")
		}
	})
}

// TestJobConfigTypeAndNameMethods tests Type() and Name() methods
func TestJobConfigTypeAndNameMethods(t *testing.T) {
	t.Parallel()

	t.Run("runJobConfig methods", func(t *testing.T) {
		job := &runJobConfig{JobName: "test-run-job"}
		if got := job.Type(); got != "job-run" {
			t.Errorf("Type() = %q, want %q", got, "job-run")
		}
		if got := job.Name(); got != "test-run-job" {
			t.Errorf("Name() = %q, want %q", got, "test-run-job")
		}
	})

	t.Run("localJobConfig methods", func(t *testing.T) {
		job := &localJobConfig{JobName: "test-local-job"}
		if got := job.Type(); got != "job-local" {
			t.Errorf("Type() = %q, want %q", got, "job-local")
		}
		if got := job.Name(); got != "test-local-job" {
			t.Errorf("Name() = %q, want %q", got, "test-local-job")
		}
	})

	t.Run("empty job names", func(t *testing.T) {
		runJob := &runJobConfig{JobName: ""}
		localJob := &localJobConfig{JobName: ""}

		if got := runJob.Name(); got != "" {
			t.Errorf("runJobConfig.Name() = %q, want empty", got)
		}
		if got := localJob.Name(); got != "" {
			t.Errorf("localJobConfig.Name() = %q, want empty", got)
		}
	})

	t.Run("special characters in job name", func(t *testing.T) {
		job := &runJobConfig{JobName: "job-with_special.chars"}
		if got := job.Name(); got != "job-with_special.chars" {
			t.Errorf("Name() = %q, want %q", got, "job-with_special.chars")
		}
	})
}

// TestInitConfigStructure tests the initConfig structure
func TestInitConfigStructure(t *testing.T) {
	t.Parallel()

	t.Run("nil global", func(t *testing.T) {
		config := &initConfig{
			Global: nil,
			Jobs:   []initJobConfig{},
		}
		// Just ensure we can create config with nil global
		if config.Global != nil {
			t.Error("Global should be nil")
		}
	})

	t.Run("nil jobs slice", func(t *testing.T) {
		config := &initConfig{
			Global: &globalConfig{},
			Jobs:   nil,
		}
		if config.Jobs != nil {
			t.Error("Jobs should be nil")
		}
	})

	t.Run("empty jobs slice", func(t *testing.T) {
		config := &initConfig{
			Global: &globalConfig{},
			Jobs:   []initJobConfig{},
		}
		if len(config.Jobs) != 0 {
			t.Error("Jobs should be empty")
		}
	})
}
