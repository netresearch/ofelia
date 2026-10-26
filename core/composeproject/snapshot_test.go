// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package composeproject

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoad_ExecutionSnapshotResolvesFileInputs covers includes, resolved paths and captured
// env/label/config/secret data, proving later loads cannot mutate an already accepted model.
func TestLoad_ExecutionSnapshotResolvesFileInputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}
	write("inputs.env", "OFELIA_SNAPSHOT_VALUE=accepted-value\n")
	write("labels.env", "snapshot.label=accepted-label\n")
	write("settings.conf", "accepted-config\n")
	write("secret.txt", "accepted-secret\n")
	write("job.yaml", `name: snapshot
configs:
  settings:
    file: ./settings.conf
secrets:
  credential:
    file: ./secret.txt
services:
  dep:
    image: alpine:3.22
    env_file: inputs.env
jobs:
  backup:
    image: alpine:3.22
    depends_on: [dep]
    command: [echo, accepted-command]
    env_file: inputs.env
    label_file: labels.env
    configs: [settings]
    secrets: [credential]
    volumes: [./data:/data]
    triggers:
      schedule:
        - cron: "0 2 * * *"
          missed_fires: skip
`)
	path := write("compose.yaml", "include: [job.yaml]\n")
	project, err := ComposeGoLoader{}.Load(context.Background(), Spec{Name: "snapshot", Files: []string{path}})
	require.NoError(t, err)
	var model struct {
		Jobs     types.Jobs     `json:"jobs"`
		Services types.Services `json:"services"`
	}
	require.NoError(t, json.Unmarshal([]byte(project.Snapshot), &model))
	job := model.Jobs["backup"]
	assert.Empty(t, job.EnvFiles)
	assert.Empty(t, job.LabelFiles)
	assert.Equal(t, "accepted-value", *job.Environment["OFELIA_SNAPSHOT_VALUE"])
	assert.Equal(t, "accepted-label", job.Labels["snapshot.label"])
	assert.Empty(t, model.Services["dep"].EnvFiles)
	assert.Equal(t, "accepted-value", *model.Services["dep"].Environment["OFELIA_SNAPSHOT_VALUE"])
	assert.Equal(t, filepath.Join(dir, "data"), job.Volumes[0].Source, "included relative paths are resolved at load")
	assert.Equal(t, []byte("accepted-config\n"), project.SnapshotFiles["configs/settings"].Content)
	assert.Equal(t, []byte("accepted-secret\n"), project.SnapshotFiles["secrets/credential"].Content)

	before := project.Snapshot
	revision := project.Jobs[0].Revision
	write("settings.conf", "new-config\n")
	write("secret.txt", "new-secret\n")
	write("inputs.env", "OFELIA_SNAPSHOT_VALUE=new-value\n")
	write("labels.env", "snapshot.label=new-label\n")
	changed, err := ComposeGoLoader{}.Load(context.Background(), Spec{Name: "snapshot", Files: []string{path}})
	require.NoError(t, err)
	assert.NotEqual(t, project.Revision, changed.Revision)
	assert.NotEqual(t, revision, changed.Jobs[0].Revision, "the job revision covers captured resource contents")
	assert.Equal(t, before, project.Snapshot, "a later load cannot mutate an accepted model")
	assert.Equal(t, []byte("accepted-secret\n"), project.SnapshotFiles["secrets/credential"].Content)
}

// TestLoad_ExecutionSnapshotCapturesEnvironmentResources verifies environment-backed resources retain native
// upload semantics while original variable names cannot influence later runs.
func TestLoad_ExecutionSnapshotCapturesEnvironmentResources(t *testing.T) {
	t.Setenv("OFELIA_SNAPSHOT_SECRET", "secret-$literal")
	t.Setenv("OFELIA_SNAPSHOT_CONFIG", "config-$literal")
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	model := `name: environment
configs:
  config:
    environment: OFELIA_SNAPSHOT_CONFIG
secrets:
  secret:
    environment: OFELIA_SNAPSHOT_SECRET
jobs:
  backup:
    image: alpine:3.22
    configs: [config]
    secrets: [secret]
    triggers:
      schedule:
        - cron: "0 2 * * *"
          missed_fires: skip
`
	require.NoError(t, os.WriteFile(path, []byte(model), 0o600))
	project, err := ComposeGoLoader{}.Load(context.Background(), Spec{Name: "environment", Files: []string{path}})
	require.NoError(t, err)
	assert.Empty(t, project.SnapshotFiles, "environment-backed inputs keep Compose's native upload semantics")
	assert.Contains(t, project.SnapshotEnvironment, "OFELIA_SNAPSHOT_"+strings.ToUpper(shortRevision([]byte("configs/config"))))
	assert.Equal(t, "config-$literal", project.SnapshotEnvironment["OFELIA_SNAPSHOT_"+strings.ToUpper(shortRevision([]byte("configs/config")))])
	assert.Equal(t, "secret-$literal", project.SnapshotEnvironment["OFELIA_SNAPSHOT_"+strings.ToUpper(shortRevision([]byte("secrets/secret")))])
	assert.NotContains(t, project.Snapshot, "OFELIA_SNAPSHOT_SECRET")
}

// TestLoad_ExecutionSnapshotIgnoresUnrelatedInactiveInputs ensures dormant profiles do not require their
// production files merely to schedule an independent active job.
func TestLoad_ExecutionSnapshotIgnoresUnrelatedInactiveInputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	model := `name: inactive
services:
  production:
    image: alpine:3.22
    profiles: [production]
    env_file: missing-production.env
configs:
  unused:
    file: missing-production.conf
jobs:
  unused:
    image: alpine:3.22
    profiles: [production]
    env_file: missing-production-job.env
    triggers:
      schedule: ["0 1 * * *"]
  backup:
    image: alpine:3.22
    triggers:
      schedule:
        - cron: "0 2 * * *"
          missed_fires: skip
`
	require.NoError(t, os.WriteFile(path, []byte(model), 0o600))
	project, err := ComposeGoLoader{}.Load(context.Background(), Spec{Name: "inactive", Files: []string{path}})
	require.NoError(t, err)
	require.Len(t, project.Jobs, 1)
	assert.Equal(t, "backup", project.Jobs[0].Name)
	assert.NotContains(t, project.Snapshot, "missing-production")
}

// TestLoad_ExecutionSnapshotPreservesRawEnvironment checks raw env-file values retain their quotes and dollars
// instead of being normalized a second time.
func TestLoad_ExecutionSnapshotPreservesRawEnvironment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	model := `name: raw-environment
jobs:
  backup:
    image: alpine:3.22
    env_file:
      - path: job.env
        format: raw
    triggers:
      schedule:
        - cron: "0 2 * * *"
          missed_fires: skip
`
	require.NoError(t, os.WriteFile(path, []byte(model), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "job.env"), []byte("VALUE=\"literal-$value\"\n"), 0o600))
	project, err := ComposeGoLoader{}.Load(context.Background(), Spec{Name: "raw", Files: []string{path}})
	require.NoError(t, err)
	var snapshot struct{ Jobs types.Jobs }
	require.NoError(t, json.Unmarshal([]byte(project.Snapshot), &snapshot))
	assert.Equal(t, `"literal-$value"`, *snapshot.Jobs["backup"].Environment["VALUE"])
}

// TestLoad_ExecutionSnapshotIncludesDependencyHookResources ensures a config mounted only by a pre-start hook
// still changes the owning job's accepted revision.
func TestLoad_ExecutionSnapshotIncludesDependencyHookResources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	config := filepath.Join(dir, "hook.conf")
	model := `name: hook-snapshot
configs:
  hook:
    file: hook.conf
services:
  dependency:
    image: alpine:3.22
    pre_start:
      - command: [cat, /hook]
        configs: [hook]
jobs:
  backup:
    image: alpine:3.22
    depends_on: [dependency]
    triggers:
      schedule:
        - cron: "0 2 * * *"
          missed_fires: skip
`
	require.NoError(t, os.WriteFile(path, []byte(model), 0o600))
	require.NoError(t, os.WriteFile(config, []byte("accepted-hook"), 0o600))
	spec := Spec{Name: "hook", Files: []string{path}}
	before, err := ComposeGoLoader{}.Load(context.Background(), spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(config, []byte("updated-hook"), 0o600))
	after, err := ComposeGoLoader{}.Load(context.Background(), spec)
	require.NoError(t, err)
	assert.NotEqual(t, before.Jobs[0].Revision, after.Jobs[0].Revision, "hook resource changes replace the accepted job")
}
