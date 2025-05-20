package core

import (
	"errors"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// TestBuildPullOptionsSingle tests buildPullOptions for a single-segment image name.
func TestBuildPullOptionsSingle(t *testing.T) {
	orig := dockercfg
	defer func() { dockercfg = orig }()
	dockercfg = nil

	image := "alpine"
	opts, auth := buildPullOptions(image)
	if opts.Repository != "alpine" {
		t.Errorf("expected repository 'alpine', got '%s'", opts.Repository)
	}
	if opts.Tag != "latest" {
		t.Errorf("expected tag 'latest', got '%s'", opts.Tag)
	}
	if opts.Registry != "" {
		t.Errorf("expected empty registry, got '%s'", opts.Registry)
	}
	if auth != (docker.AuthConfiguration{}) {
		t.Errorf("expected empty auth, got %+v", auth)
	}
}

// TestBuildPullOptionsThreeParts tests buildPullOptions for a three-part registry/org/name image.
func TestBuildPullOptionsThreeParts(t *testing.T) {
	image := "host:5000/org/repo:mytag"
	opts, _ := buildPullOptions(image)
	if opts.Repository != "host:5000/org/repo" {
		t.Errorf("expected repository 'host:5000/org/repo', got '%s'", opts.Repository)
	}
	if opts.Tag != "mytag" {
		t.Errorf("expected tag 'mytag', got '%s'", opts.Tag)
	}
	if opts.Registry != "host:5000" {
		t.Errorf("expected registry 'host:5000', got '%s'", opts.Registry)
	}
}

// TestParseRegistryVarious tests parseRegistry with different repository formats.
func TestParseRegistryVarious(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"alpine", ""},
		{"org/repo", ""},
		{"domain.com/repo", "domain.com"},
		{"domain.com/org/repo", "domain.com"},
		{"registry.io:5000/repo", "registry.io:5000"},
	}
	for _, tc := range tests {
		got := parseRegistry(tc.repo)
		if got != tc.want {
			t.Errorf("parseRegistry(%q) = %q; want %q", tc.repo, got, tc.want)
		}
	}
}

// TestExecutionLifecycle tests Execution.Start and Stop with no error.
func TestExecutionLifecycle(t *testing.T) {
	e := NewExecution()
	// Ensure initial state
	if e.IsRunning {
		t.Error("expected IsRunning false before start")
	}
	// Start execution
	before := time.Now()
	e.Start()
	if !e.IsRunning {
		t.Error("expected IsRunning true after start")
	}
	// Stop with no error
	e.Stop(nil)
	if e.IsRunning {
		t.Error("expected IsRunning false after stop")
	}
	if e.Failed {
		t.Error("expected Failed false with no error")
	}
	if e.Skipped {
		t.Error("expected Skipped false with no error")
	}
	if e.Error != nil {
		t.Errorf("expected Error nil with no error, got %v", e.Error)
	}
	if e.Duration <= 0 || e.Date.Before(before) {
		t.Errorf("expected positive Duration and Date >= start time, got Duration %v, Date %v", e.Duration, e.Date)
	}
}

// TestExecutionStopError tests Execution.Stop with a regular error.
func TestExecutionStopError(t *testing.T) {
	e := NewExecution()
	e.Start()
	errIn := errors.New("fail")
	e.Stop(errIn)
	if e.IsRunning {
		t.Error("expected IsRunning false after stop")
	}
	if !e.Failed {
		t.Error("expected Failed true after error")
	}
	if e.Skipped {
		t.Error("expected Skipped false after error")
	}
	if e.Error != errIn {
		t.Errorf("expected Error %v, got %v", errIn, e.Error)
	}
}

// TestExecutionStopSkipped tests Execution.Stop with ErrSkippedExecution.
func TestExecutionStopSkipped(t *testing.T) {
	e := NewExecution()
	e.Start()
	e.Stop(ErrSkippedExecution)
	if e.IsRunning {
		t.Error("expected IsRunning false after stop")
	}
	if !e.Skipped {
		t.Error("expected Skipped true after skipped error")
	}
	if e.Failed {
		t.Error("expected Failed false after skipped error")
	}
	if e.Error != nil {
		t.Errorf("expected Error nil after skipped error, got %v", e.Error)
	}
}
