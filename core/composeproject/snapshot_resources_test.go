// Copyright (c) 2026 Netresearch DTT GmbH
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

// TestLoad_ServiceResourceFingerprintTracksOnlyItsAcceptedInputs distinguishes config, secret, hook and access
// changes from job, registration, profile and unrelated-resource edits.
func TestLoad_ServiceResourceFingerprintTracksOnlyItsAcceptedInputs(t *testing.T) {
	t.Setenv("OFELIA_RESOURCE_REVIEW_NATIVE", "accepted-native")
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	for _, name := range []string{"settings", "token", "hook", "unrelated"} {
		write(name, "accepted-"+name)
	}
	model := `name: service-resources
configs:
  settings:
    file: settings
  hook:
    file: hook
  unrelated:
    file: unrelated
secrets:
  token:
    file: token
  native:
    environment: OFELIA_RESOURCE_REVIEW_NATIVE
services:
  dependency:
    image: alpine:3.22
    labels: {customer: accepted}
    configs: [settings]
    secrets: [token, native]
    pre_start:
      - command: ["true"]
        configs: [hook]
jobs:
  backup:
    image: alpine:3.22
    command: [echo, accepted-job]
    depends_on: [dependency]
    triggers:
      schedule:
        - cron: "0 2 * * *"
          missed_fires: skip
`
	path := filepath.Join(dir, "compose.yaml")
	load := func(definition, registration string, profiles []string) string {
		t.Helper()
		write("compose.yaml", definition)
		project, err := ComposeGoLoader{}.Load(context.Background(), Spec{Name: registration, Files: []string{path}, Profiles: profiles})
		require.NoError(t, err)
		var snapshot struct{ Services types.Services }
		require.NoError(t, json.Unmarshal([]byte(project.Snapshot), &snapshot))
		assert.Equal(t, "accepted", snapshot.Services["dependency"].Labels["customer"])
		fingerprint := snapshot.Services["dependency"].Labels[snapshotResourceLabel]
		require.NotEmpty(t, fingerprint)
		return fingerprint
	}
	accepted := load(model, "first", nil)
	assert.Equal(t, accepted, load(model, "second", []string{"unrelated-profile"}), "aliases and profile selection are not service inputs")
	changedJob := strings.ReplaceAll(strings.ReplaceAll(model, "accepted-job", "other-job"), "0 2 * * *", "0 3 * * *")
	assert.Equal(t, accepted, load(changedJob, "first", nil), "job commands and schedules cannot recreate a shared service")
	write("unrelated", "updated-unrelated")
	assert.Equal(t, accepted, load(model, "first", nil), "unreferenced resources cannot recreate the service")
	for _, name := range []string{"settings", "token", "hook"} {
		write(name, "updated-"+name)
		assert.NotEqual(t, accepted, load(model, "first", nil), name+" must update the service")
		write(name, "accepted-"+name)
	}
	require.NoError(t, os.Chmod(filepath.Join(dir, "settings"), 0o644))
	assert.NotEqual(t, accepted, load(model, "first", nil), "file access changes are accepted input changes")
	require.NoError(t, os.Chmod(filepath.Join(dir, "settings"), 0o600))
	t.Setenv("OFELIA_RESOURCE_REVIEW_NATIVE", "updated-native")
	assert.NotEqual(t, accepted, load(model, "first", nil), "native environment-backed inputs must update the service")
}

// TestServiceResourceFingerprintPreservesSourceLabelsAndIgnoresItsOwnLabel protects shared source maps and
// prevents the generated fingerprint from feeding back into its next hash.
func TestServiceResourceFingerprintPreservesSourceLabelsAndIgnoresItsOwnLabel(t *testing.T) {
	t.Parallel()
	labels := types.Labels{"customer": "accepted", snapshotResourceLabel: "previous-fingerprint"}
	project := &types.Project{
		Services: types.Services{"dependency": {
			ContainerSpec: types.ContainerSpec{
				Labels:  labels,
				Configs: []types.ServiceConfigObjConfig{{Source: "settings"}},
			},
		}},
		Configs: types.Configs{"settings": {Content: "accepted-settings"}},
	}
	require.NoError(t, labelServiceResources(project, nil, nil))
	fingerprint := project.Services["dependency"].Labels[snapshotResourceLabel]
	assert.NotEqual(t, "previous-fingerprint", fingerprint)
	assert.Equal(t, "previous-fingerprint", labels[snapshotResourceLabel], "injection cannot mutate a shared source label map")
	require.NoError(t, labelServiceResources(project, nil, nil))
	assert.Equal(t, fingerprint, project.Services["dependency"].Labels[snapshotResourceLabel], "the generated label cannot feed its own hash")
}
