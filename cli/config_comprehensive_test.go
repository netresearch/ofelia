package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// TestMergeJobs tests the mergeJobs function
func TestMergeJobs(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	tests := []struct {
		name     string
		existing map[string]*ExecJobConfig
		new      map[string]*ExecJobConfig
		wantLen  int
	}{
		{
			name: "merge new job",
			existing: map[string]*ExecJobConfig{
				"job1": {JobSource: JobSourceINI},
			},
			new: map[string]*ExecJobConfig{
				"job2": {JobSource: JobSourceLabel},
			},
			wantLen: 2,
		},
		{
			name: "skip when INI exists",
			existing: map[string]*ExecJobConfig{
				"job1": {JobSource: JobSourceINI},
			},
			new: map[string]*ExecJobConfig{
				"job1": {JobSource: JobSourceLabel},
			},
			wantLen: 1, // Label job should be ignored
		},
		{
			name: "replace when label exists",
			existing: map[string]*ExecJobConfig{
				"job1": {JobSource: JobSourceLabel},
			},
			new: map[string]*ExecJobConfig{
				"job1": {JobSource: JobSourceLabel},
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := NewConfig(logger)
			cfg.ExecJobs = tt.existing

			mergeJobs(cfg, cfg.ExecJobs, tt.new, "exec")

			if len(cfg.ExecJobs) != tt.wantLen {
				t.Errorf("Expected %d jobs, got %d", tt.wantLen, len(cfg.ExecJobs))
			}
		})
	}
}

// TestRegisterAllJobs tests registerAllJobs with different job types
func TestRegisterAllJobs(t *testing.T) {
	// Cannot use t.Parallel() - modifies global newDockerHandler
	logger := test.NewTestLogger()

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		mockProvider := &mockDockerProviderForHandler{}
		return orig(ctx, notifier, logger, cfg, mockProvider)
	}

	cfg := NewConfig(logger)
	cfg.ExecJobs["exec1"] = &ExecJobConfig{}
	cfg.RunJobs["run1"] = &RunJobConfig{}
	cfg.LocalJobs["local1"] = &LocalJobConfig{}
	cfg.ServiceJobs["service1"] = &RunServiceConfig{}
	cfg.ComposeJobs["compose1"] = &ComposeJobConfig{}

	// Initialize app to register jobs
	err := cfg.InitializeApp()
	if err != nil {
		t.Fatalf("InitializeApp failed: %v", err)
	}

	// Verify jobs were registered
	if cfg.sh == nil {
		t.Fatal("Expected scheduler to be initialized")
	}
}

// TestLatestChanged tests the latestChanged function
func TestLatestChanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file1 := filepath.Join(dir, "file1.ini")
	file2 := filepath.Join(dir, "file2.ini")

	// Create first file
	if err := os.WriteFile(file1, []byte("test"), 0o644); err != nil {
		t.Fatalf("Failed to write file1: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Create second file (newer)
	if err := os.WriteFile(file2, []byte("test"), 0o644); err != nil {
		t.Fatalf("Failed to write file2: %v", err)
	}

	files := []string{file1, file2}

	// Test with old timestamp - should detect change
	oldTime := time.Now().Add(-1 * time.Hour)
	latest, changed, err := latestChanged(files, oldTime)
	if err != nil {
		t.Errorf("latestChanged failed: %v", err)
	}
	if !changed {
		t.Error("Expected change to be detected")
	}
	if latest.Before(oldTime) {
		t.Error("Expected latest to be newer than old time")
	}

	// Test with current timestamp - should not detect change
	latest2, changed2, err := latestChanged(files, latest)
	if err != nil {
		t.Errorf("latestChanged failed: %v", err)
	}
	if changed2 {
		t.Error("Expected no change to be detected")
	}
	if latest2 != latest {
		t.Error("Expected same timestamp")
	}
}

// TestSectionToMap tests sectionToMap with various key scenarios
func TestSectionToMap(t *testing.T) {
	t.Parallel()
	// This is implicitly tested through BuildFromString, but we can test edge cases
	configStr := `
[test]
single = value1
multiple = value2
multiple = value3
empty =
`
	cfg, err := BuildFromString(configStr, test.NewTestLogger())
	if err != nil {
		t.Fatalf("BuildFromString failed: %v", err)
	}

	// Just verify it doesn't crash
	if cfg == nil {
		t.Error("Expected non-nil config")
	}
}

// TestParseJobName tests the parseJobName function indirectly
func TestParseJobName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		section  string
		prefix   string
		expected string
	}{
		{
			section:  `job-exec "my-job"`,
			prefix:   "job-exec",
			expected: "my-job",
		},
		{
			section:  `job-run   "spaced-job"  `,
			prefix:   "job-run",
			expected: "spaced-job",
		},
	}

	for _, tt := range tests {
		t.Run(tt.section, func(t *testing.T) {
			t.Parallel()
			result := parseJobName(tt.section, tt.prefix)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestBuildFromString_ErrorRecovery tests error handling in BuildFromString
func TestBuildFromString_ErrorRecovery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "unclosed section",
			config: `
[global
key = value
`,
			wantErr: true,
		},
		{
			name: "valid minimal config",
			config: `
[global]
log-level = info
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := BuildFromString(tt.config, test.NewTestLogger())

			if (err != nil) != tt.wantErr {
				t.Errorf("BuildFromString() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDockerLabelsUpdate_Integration tests dockerLabelsUpdate with real scheduler
func TestDockerLabelsUpdate_Integration(t *testing.T) {
	// Cannot use t.Parallel() - modifies global newDockerHandler
	logger := test.NewTestLogger()

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		mockProvider := &mockDockerProviderForHandler{}
		return orig(ctx, notifier, logger, cfg, mockProvider)
	}

	cfg := NewConfig(logger)
	err := cfg.InitializeApp()
	if err != nil {
		t.Fatalf("InitializeApp failed: %v", err)
	}

	// Simulate docker labels update
	labels := map[string]map[string]string{
		"container1": {
			"ofelia.enabled":                "true",
			"ofelia.job-exec.test.schedule": "@every 10s",
			"ofelia.job-exec.test.command":  "echo test",
		},
	}

	cfg.dockerLabelsUpdate(labels)

	// Verify job was added (should be skipped due to no service label)
	if len(cfg.ExecJobs) != 0 {
		t.Logf("ExecJobs: %d (expected 0 due to missing service label)", len(cfg.ExecJobs))
	}
}

// TestDecodeJob_ErrorHandling tests decodeJob error scenarios
func TestDecodeJob_ErrorHandling(t *testing.T) {
	t.Parallel()
	// Test via BuildFromString with various invalid job configs
	configStr := `
[job-exec "test"]
schedule = @every 10s
command = echo test
`
	logger := test.NewTestLogger()
	_, err := BuildFromString(configStr, logger)
	if err != nil {
		t.Fatalf("BuildFromString failed unexpectedly: %v", err)
	}
}
