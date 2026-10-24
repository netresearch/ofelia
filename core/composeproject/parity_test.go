// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package composeproject

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

// parityCompose has one job whose schedule is interpolated and one behind a
// profile, so the comparison sees env files, profiles and the project name.
const parityCompose = `jobs:
  a:
    image: alpine:3.22
    triggers:
      schedule:
        - cron: "${MIN:-9} 3 * * *"
          missed_fires: skip
  prof:
    image: alpine:3.22
    profiles: ["sched"]
    triggers:
      schedule:
        - cron: "1 1 * * *"
          missed_fires: skip
`

// loaded is what the comparison looks at: the project name and the cron
// expression of every active job.
type loaded struct {
	Name string
	Jobs map[string]string
}

// requireComposeJobsCLI skips the test unless a docker compose CLI that
// understands top-level jobs is installed.
func requireComposeJobsCLI(t *testing.T) {
	t.Helper()
	version, err := core.ComposeCLIVersion(context.Background())
	if err != nil {
		t.Skipf("docker compose CLI not available: %v", err)
	}
	if !core.ComposeVersionAtLeast(version, core.MinComposeJobsVersion) {
		t.Skipf("docker compose %s does not support jobs; %s or later is required", version, core.MinComposeJobsVersion)
	}
}

// viaDockerCompose loads the project with `docker compose config`.
func viaDockerCompose(t *testing.T, spec Spec) loaded {
	t.Helper()
	args := []string{"compose"}
	for _, f := range spec.Files {
		args = append(args, "-f", f)
	}
	for _, f := range spec.EnvFiles {
		args = append(args, "--env-file", f)
	}
	for _, p := range spec.Profiles {
		args = append(args, "--profile", p)
	}
	if spec.ProjectDirectory != "" {
		args = append(args, "--project-directory", spec.ProjectDirectory)
	}
	if spec.ProjectName != "" {
		args = append(args, "-p", spec.ProjectName)
	}
	args = append(args, "config", "--format", "json")
	cmd := exec.Command("docker", args...) //nolint:gosec // test-owned arguments
	cmd.Dir = t.TempDir()                  // a working directory without a .env
	out, err := cmd.Output()
	require.NoError(t, err, "docker %v", args)
	var model struct {
		Name string `json:"name"`
		Jobs map[string]struct {
			Triggers struct {
				Schedule []struct {
					Cron string `json:"cron"`
				} `json:"schedule"`
			} `json:"triggers"`
		} `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal(out, &model))
	got := loaded{Name: model.Name, Jobs: map[string]string{}}
	for name, j := range model.Jobs {
		if len(j.Triggers.Schedule) > 0 {
			got.Jobs[name] = j.Triggers.Schedule[0].Cron
		}
	}
	return got
}

// viaLoader loads the project with ComposeGoLoader from the same working
// directory docker compose runs in.
func viaLoader(t *testing.T, spec Spec) loaded {
	t.Helper()
	t.Chdir(t.TempDir())
	spec.Name = "parity"
	p, err := ComposeGoLoader{}.Load(context.Background(), spec)
	require.NoError(t, err)
	got := loaded{Name: p.Name, Jobs: map[string]string{}}
	for _, j := range p.Jobs {
		got.Jobs[j.Name] = j.Schedule
	}
	return got
}

// unsetEnv removes key for the test and restores it afterwards. An empty
// value would not do: a variable present in the environment wins over .env.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

func jobNames(l loaded) []string {
	names := make([]string, 0, len(l.Jobs))
	for n := range l.Jobs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// parityFixture writes the compose file into dir and a .env into dir and
// into dir/p with the given contents, and returns the compose file path.
func parityFixture(t *testing.T, dir, rootEnv, projectEnv string) string {
	t.Helper()
	compose := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(compose, []byte(parityCompose), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "p"), 0o700))
	if rootEnv != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(rootEnv), 0o600))
	}
	if projectEnv != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "p", ".env"), []byte(projectEnv), 0o600))
	}
	return compose
}

func assertParity(t *testing.T, spec Spec) loaded {
	t.Helper()
	want := viaDockerCompose(t, spec)
	got := viaLoader(t, spec)
	assert.Equal(t, want.Name, got.Name, "project name")
	assert.Equal(t, jobNames(want), jobNames(got), "active scheduled jobs")
	for name, cron := range want.Jobs {
		assert.Equal(t, cron, got.Jobs[name], "schedule of job %q", name)
	}
	return got
}

func TestParity_DotEnvFromProjectDirectory(t *testing.T) {
	requireComposeJobsCLI(t)
	unsetEnv(t, "COMPOSE_PROFILES")
	dir := t.TempDir()
	compose := parityFixture(t, dir, "MIN=11\n", "MIN=22\n")

	got := assertParity(t, Spec{Files: []string{compose}, ProjectDirectory: filepath.Join(dir, "p")})

	assert.Equal(t, "22 3 * * *", got.Jobs["a"], "the .env of the project directory applies")
}

func TestParity_ComposeProfilesFromDotEnv(t *testing.T) {
	requireComposeJobsCLI(t)
	unsetEnv(t, "COMPOSE_PROFILES")
	dir := t.TempDir()
	compose := parityFixture(t, dir, "", "COMPOSE_PROFILES=sched\n")

	got := assertParity(t, Spec{Files: []string{compose}, ProjectDirectory: filepath.Join(dir, "p")})

	assert.Contains(t, got.Jobs, "prof", "COMPOSE_PROFILES in .env activates the profile")
}

func TestParity_ComposeProfilesFromEnvironment(t *testing.T) {
	requireComposeJobsCLI(t)
	t.Setenv("COMPOSE_PROFILES", "sched")
	dir := t.TempDir()
	compose := parityFixture(t, dir, "", "")

	got := assertParity(t, Spec{Files: []string{compose}})

	assert.Contains(t, got.Jobs, "prof", "COMPOSE_PROFILES in the environment activates the profile")
}

func TestParity_ProjectNameFromDotEnv(t *testing.T) {
	requireComposeJobsCLI(t)
	unsetEnv(t, "COMPOSE_PROFILES")
	dir := t.TempDir()
	compose := parityFixture(t, dir, "", "COMPOSE_PROJECT_NAME=fromenv\n")

	got := assertParity(t, Spec{Files: []string{compose}, ProjectDirectory: filepath.Join(dir, "p")})

	assert.Equal(t, "fromenv", got.Name)
}

func TestParity_ExplicitProfilesAndEnvFile(t *testing.T) {
	requireComposeJobsCLI(t)
	unsetEnv(t, "COMPOSE_PROFILES")
	dir := t.TempDir()
	compose := parityFixture(t, dir, "MIN=11\n", "")
	envFile := filepath.Join(dir, "custom.env")
	require.NoError(t, os.WriteFile(envFile, []byte("MIN=33\n"), 0o600))

	got := assertParity(t, Spec{Files: []string{compose}, EnvFiles: []string{envFile}, Profiles: []string{"sched"}})

	assert.Equal(t, "33 3 * * *", got.Jobs["a"])
	assert.Contains(t, got.Jobs, "prof")
}

// TestLoad_ResolvedProfilesAndWorkingDir pins what a run passes on: the
// profiles from COMPOSE_PROFILES and the absolute project directory.
func TestLoad_ResolvedProfilesAndWorkingDir(t *testing.T) {
	unsetEnv(t, "COMPOSE_PROFILES")
	dir := t.TempDir()
	compose := parityFixture(t, dir, "", "COMPOSE_PROFILES=sched\n")

	p, err := ComposeGoLoader{}.Load(context.Background(),
		Spec{Name: "r", Files: []string{compose}, ProjectDirectory: filepath.Join(dir, "p")})
	require.NoError(t, err)

	assert.Equal(t, []string{"sched"}, p.Profiles)
	assert.Equal(t, filepath.Join(dir, "p"), p.WorkingDir)
}

// TestLoad_NoProfilesYieldsNone keeps an unset COMPOSE_PROFILES from becoming
// an empty --profile argument.
func TestLoad_NoProfilesYieldsNone(t *testing.T) {
	unsetEnv(t, "COMPOSE_PROFILES")
	dir := t.TempDir()
	compose := parityFixture(t, dir, "", "")

	p, err := ComposeGoLoader{}.Load(context.Background(), Spec{Name: "r", Files: []string{compose}})
	require.NoError(t, err)

	assert.Empty(t, p.Profiles)
}
