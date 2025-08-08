package core

import (
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

// TestNewExecutionInitial tests the initial state of a new Execution.
func TestNewExecutionInitial(t *testing.T) {
    e, err := NewExecution()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if e == nil {
        t.Fatal("expected NewExecution to return non-nil")
    }
	if e.ID == "" {
		t.Error("expected non-empty ID")
	}
	if len(e.ID) != 12 {
		t.Errorf("expected ID length 12, got %d", len(e.ID))
	}
	if e.OutputStream == nil || e.ErrorStream == nil {
		t.Error("expected non-nil output and error streams")
	}
	if e.IsRunning {
		t.Error("expected IsRunning to be false initially")
	}
	if e.Failed {
		t.Error("expected Failed to be false initially")
	}
	if e.Skipped {
		t.Error("expected Skipped to be false initially")
	}
	if e.Error != nil {
		t.Errorf("expected Error to be nil initially, got %v", e.Error)
	}
}

// TestBuildFindLocalImageOptions verifies that buildFindLocalImageOptions sets the correct filter.
func TestBuildFindLocalImageOptions(t *testing.T) {
	image := "myimage"
	opts := buildFindLocalImageOptions(image)
	refs, ok := opts.Filters["reference"]
	if !ok {
		t.Fatal("Filters missing 'reference'")
	}
	if len(refs) != 1 || refs[0] != image {
		t.Errorf("expected refs [\"%s\"], got %v", image, refs)
	}
}

// TestBuildPullOptionsTagSpecified tests buildPullOptions with an explicit tag.
func TestBuildPullOptionsTagSpecified(t *testing.T) {
	orig := dockercfg
	defer func() { dockercfg = orig }()
	dockercfg = nil

	image := "repo/name:tag"
	opts, auth := buildPullOptions(image)
	if opts.Repository != "repo/name" {
		t.Errorf("expected repository 'repo/name', got '%s'", opts.Repository)
	}
	if opts.Tag != "tag" {
		t.Errorf("expected tag 'tag', got '%s'", opts.Tag)
	}
	if opts.Registry != "repo" {
		t.Errorf("expected registry 'repo', got '%s'", opts.Registry)
	}
	if auth != (docker.AuthConfiguration{}) {
		t.Errorf("expected empty auth, got %+v", auth)
	}
}

// TestBuildPullOptionsDefaultTag tests buildPullOptions without specifying a tag.
func TestBuildPullOptionsDefaultTag(t *testing.T) {
	orig := dockercfg
	defer func() { dockercfg = orig }()
	dockercfg = nil

	image := "repo/name"
	opts, auth := buildPullOptions(image)
	if opts.Repository != "repo/name" {
		t.Errorf("expected repository 'repo/name', got '%s'", opts.Repository)
	}
	if opts.Tag != "latest" {
		t.Errorf("expected tag 'latest', got '%s'", opts.Tag)
	}
	if opts.Registry != "repo" {
		t.Errorf("expected registry 'repo', got '%s'", opts.Registry)
	}
	if auth != (docker.AuthConfiguration{}) {
		t.Errorf("expected empty auth, got %+v", auth)
	}
}

// TestBuildAuthConfigurationRegistry tests buildAuthConfiguration for a specific registry entry.
func TestBuildAuthConfigurationRegistry(t *testing.T) {
	orig := dockercfg
	defer func() { dockercfg = orig }()
	dockercfg = &docker.AuthConfigurations{Configs: map[string]docker.AuthConfiguration{
		"reg": {Username: "user", Password: "pass"},
	}}
	auth := buildAuthConfiguration("reg")
	if auth.Username != "user" || auth.Password != "pass" {
		t.Errorf("expected auth for registry 'reg', got %+v", auth)
	}
}

// TestBuildAuthConfigurationDefaultRegistry tests buildAuthConfiguration for default Docker Hub registry.
func TestBuildAuthConfigurationDefaultRegistry(t *testing.T) {
	orig := dockercfg
	defer func() { dockercfg = orig }()
	dockercfg = &docker.AuthConfigurations{Configs: map[string]docker.AuthConfiguration{
		"https://index.docker.io/v2/": {Username: "hub2"},
		"https://index.docker.io/v1/": {Username: "hub1"},
	}}
	auth := buildAuthConfiguration("")
	if auth.Username != "hub2" {
		t.Errorf("expected auth for default registry 'hub2', got %+v", auth)
	}
}

// TestBuildAuthConfigurationNone tests buildAuthConfiguration when dockercfg is nil.
func TestBuildAuthConfigurationNone(t *testing.T) {
	orig := dockercfg
	defer func() { dockercfg = orig }()
	dockercfg = nil
	auth := buildAuthConfiguration("whatever")
	if auth != (docker.AuthConfiguration{}) {
		t.Errorf("expected empty auth, got %+v", auth)
	}
}
