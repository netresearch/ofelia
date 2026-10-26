// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package composeproject

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ExecutionSnapshotSkipsInactiveOptionalDependencyInputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	model := `name: optional-inactive
configs:
  optional:
    file: missing-optional.conf
services:
  optional:
    image: alpine:3.22
    profiles: [optional]
    env_file: missing-optional.env
    configs: [optional]
jobs:
  backup:
    image: alpine:3.22
    depends_on:
      optional:
        condition: service_started
        required: false
    triggers:
      schedule:
        - cron: "0 2 * * *"
          missed_fires: skip
`
	require.NoError(t, os.WriteFile(path, []byte(model), 0o600))
	spec := Spec{Name: "optional-inactive", Files: []string{path}}
	options, err := projectOptions(spec)
	require.NoError(t, err)
	projectOptions, err := composecli.NewProjectOptions(spec.Files, options...)
	require.NoError(t, err)
	source, err := projectOptions.LoadProject(context.Background())
	require.NoError(t, err, "the normal Compose loader accepts a missing inactive optional dependency input")
	accepted, err := Extract(spec.Name, source)
	require.NoError(t, err)
	require.Len(t, accepted.Jobs, 1)
	project, err := snapshotProject(spec.Name, source)
	require.NoError(t, err, "snapshotting must retain the normal inactive optional dependency behavior")
	require.Len(t, project.Jobs, 1)
	var snapshot struct {
		Services types.Services `json:"services"`
		Jobs     types.Jobs     `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal([]byte(project.Snapshot), &snapshot))
	assert.NotContains(t, snapshot.Services, "optional")
	assert.NotContains(t, snapshot.Jobs["backup"].DependsOn, "optional")
	assert.Contains(t, source.Jobs["backup"].DependsOn, "optional", "selection cannot mutate the accepted source")

	// Enabling the dependency's profile restores its input requirements and
	// makes its accepted file contents part of the scheduled job's revision.
	spec.Profiles = []string{"optional"}
	_, err = ComposeGoLoader{}.Load(context.Background(), spec)
	require.Error(t, err, "an active dependency still requires its environment file")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "missing-optional.env"), []byte("VALUE=accepted\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "missing-optional.conf"), []byte("accepted-config"), 0o600))
	active, err := ComposeGoLoader{}.Load(context.Background(), spec)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(active.Snapshot), &snapshot))
	assert.Contains(t, snapshot.Services, "optional")
	assert.Contains(t, snapshot.Jobs["backup"].DependsOn, "optional")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "missing-optional.conf"), []byte("changed-config"), 0o600))
	changed, err := ComposeGoLoader{}.Load(context.Background(), spec)
	require.NoError(t, err)
	assert.NotEqual(t, active.Jobs[0].Revision, changed.Jobs[0].Revision)
}
