package core

import (
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

func TestBuildAuthConfigurationFallbacks(t *testing.T) {
	orig := dockercfg
	defer func() { dockercfg = orig }()
	dockercfg = &docker.AuthConfigurations{Configs: map[string]docker.AuthConfiguration{
		"https://index.docker.io/v2/": {Username: "hub2"},
		"https://index.docker.io/v1/": {Username: "hub1"},
	}}
	if got := buildAuthConfiguration(""); got.Username != "hub2" {
		t.Fatalf("expected hub2, got %+v", got)
	}
}
