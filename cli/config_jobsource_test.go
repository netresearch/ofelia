package cli

import (
	"testing"

	"github.com/netresearch/ofelia/core"
)

// TestComposeJobConfig_GetSetJobSource tests GetJobSource and SetJobSource for ComposeJobConfig
func TestComposeJobConfig_GetSetJobSource(t *testing.T) {
	t.Parallel()
	job := &ComposeJobConfig{}

	// Test initial value
	source := job.GetJobSource()
	if source != "" {
		t.Errorf("Expected empty JobSource, got %q", source)
	}

	// Test SetJobSource
	job.SetJobSource(JobSourceINI)
	source = job.GetJobSource()
	if source != JobSourceINI {
		t.Errorf("Expected JobSourceINI, got %q", source)
	}

	// Test changing source
	job.SetJobSource(JobSourceLabel)
	source = job.GetJobSource()
	if source != JobSourceLabel {
		t.Errorf("Expected JobSourceLabel, got %q", source)
	}
}

// TestExecJobConfig_GetSetJobSource tests GetJobSource and SetJobSource for ExecJobConfig
func TestExecJobConfig_GetSetJobSource(t *testing.T) {
	t.Parallel()
	job := &ExecJobConfig{}

	job.SetJobSource(JobSourceINI)
	if job.GetJobSource() != JobSourceINI {
		t.Error("ExecJobConfig GetJobSource/SetJobSource failed")
	}
}

// TestRunJobConfig_GetSetJobSource tests GetJobSource and SetJobSource for RunJobConfig
func TestRunJobConfig_GetSetJobSource(t *testing.T) {
	t.Parallel()
	job := &RunJobConfig{}

	job.SetJobSource(JobSourceLabel)
	if job.GetJobSource() != JobSourceLabel {
		t.Error("RunJobConfig GetJobSource/SetJobSource failed")
	}
}

// TestLocalJobConfig_GetSetJobSource tests GetJobSource and SetJobSource for LocalJobConfig
func TestLocalJobConfig_GetSetJobSource(t *testing.T) {
	t.Parallel()
	job := &LocalJobConfig{}

	job.SetJobSource(JobSourceINI)
	if job.GetJobSource() != JobSourceINI {
		t.Error("LocalJobConfig GetJobSource/SetJobSource failed")
	}
}

// TestRunServiceConfig_GetSetJobSource tests GetJobSource and SetJobSource for RunServiceConfig
func TestRunServiceConfig_GetSetJobSource(t *testing.T) {
	t.Parallel()
	job := &RunServiceConfig{}

	job.SetJobSource(JobSourceLabel)
	if job.GetJobSource() != JobSourceLabel {
		t.Error("RunServiceConfig GetJobSource/SetJobSource failed")
	}
}

// TestJobSourceString tests JobSource as string type
func TestJobSourceString(t *testing.T) {
	t.Parallel()
	var src JobSource = "test-source"
	if string(src) != "test-source" {
		t.Error("JobSource string conversion failed")
	}

	// Test constants
	if string(JobSourceINI) != "ini" {
		t.Errorf("JobSourceINI constant incorrect: %q", JobSourceINI)
	}
	if string(JobSourceLabel) != "label" {
		t.Errorf("JobSourceLabel constant incorrect: %q", JobSourceLabel)
	}
}

// TestRunJobConfig_Hash tests the Hash method for RunJobConfig
func TestRunJobConfig_Hash(t *testing.T) {
	t.Parallel()
	job1 := &RunJobConfig{
		RunJob: core.RunJob{
			BareJob: core.BareJob{
				Schedule: "@every 10s",
				Command:  "echo test",
			},
		},
	}

	job2 := &RunJobConfig{
		RunJob: core.RunJob{
			BareJob: core.BareJob{
				Schedule: "@every 10s",
				Command:  "echo test",
			},
		},
	}

	job3 := &RunJobConfig{
		RunJob: core.RunJob{
			BareJob: core.BareJob{
				Schedule: "@every 20s",
				Command:  "echo test",
			},
		},
	}

	hash1, err1 := job1.Hash()
	if err1 != nil {
		t.Errorf("Hash() error = %v", err1)
	}

	hash2, err2 := job2.Hash()
	if err2 != nil {
		t.Errorf("Hash() error = %v", err2)
	}

	hash3, err3 := job3.Hash()
	if err3 != nil {
		t.Errorf("Hash() error = %v", err3)
	}

	// Same config should produce same hash
	if hash1 != hash2 {
		t.Error("Expected identical configs to have same hash")
	}

	// Different config should produce different hash
	if hash1 == hash3 {
		t.Error("Expected different configs to have different hash")
	}
}
