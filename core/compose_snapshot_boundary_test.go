// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// TestComposeJobSnapshot_ChildEnvironmentUsesAcceptedInputs checks the actual child-process boundary so ambient
// synthetic variables and profiles cannot override the frozen execution inputs.
func TestComposeJobSnapshot_ChildEnvironmentUsesAcceptedInputs(t *testing.T) {
	t.Setenv("OFELIA_SNAPSHOT_ACCEPTED", "inherited-rejected-value")
	t.Setenv("COMPOSE_PROFILES", "unaccepted-profile")
	installFakeDocker(t, `
if [ "${OFELIA_SNAPSHOT_ACCEPTED+x}" = x ]; then
  echo "inherited snapshot value escaped the filter" >&2
  exit 1
fi
printf 'profiles=%s\n' "$COMPOSE_PROFILES"
cat "$3"
`)
	job := newComposeCancelJob(NewSDKDockerProviderFromClient(mock.NewDockerClient(), nil, nil))
	job.Project.Profiles = []string{"accepted-profile"}
	job.Project.Snapshot = &ComposeExecutionSnapshot{
		Model:       `{"name":"accepted","jobs":{"task":{"image":"alpine:3.22"}}}`,
		Environment: map[string]string{"OFELIA_SNAPSHOT_ACCEPTED": "accepted-value"},
	}
	execution, err := NewExecution()
	require.NoError(t, err)
	defer execution.Cleanup()
	ctx := NewContextWithContext(t.Context(), NewScheduler(test.NewTestLogger()), job, execution)
	require.NoError(t, job.Run(ctx), "child stderr: %s", execution.GetStderr())
	assert.Contains(t, execution.GetStdout(), "profiles=accepted-profile\n")
	assert.Contains(t, execution.GetStdout(), `OFELIA_SNAPSHOT_ACCEPTED="accepted-value"`)
	assert.NotContains(t, execution.GetStdout(), "rejected")
}

// TestComposeJobSnapshot_CacheFailureRemovesPrivateExecutionFiles verifies a blocked cache cannot be
// overwritten or leak input bytes and that failed preparation removes its per-run directory.
func TestComposeJobSnapshot_CacheFailureRemovesPrivateExecutionFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	blocked := filepath.Join(dir, "ofelia-compose-inputs")
	require.NoError(t, os.WriteFile(blocked, []byte("existing-data"), 0o600))
	job := NewComposeJob()
	job.Project.Snapshot = &ComposeExecutionSnapshot{
		Model: `{"configs":{"settings":{"file":"original.conf"}}}`,
		Files: map[string]domain.ComposeSnapshotFile{"configs/settings": snapshotTestFile("private-input")},
	}
	_, err := job.prepareSnapshot(snapshotCommand())
	require.ErrorIs(t, err, errInvalidComposeSnapshot)
	assert.NotContains(t, err.Error(), "private-input")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the failed execution must remove its private model/env directory")
	data, err := os.ReadFile(blocked)
	require.NoError(t, err)
	assert.Equal(t, "existing-data", string(data), "a blocked cache path cannot be overwritten")
}

// TestComposeJobSnapshot_InvalidTempDirectoryDoesNotChangeCommand ensures allocation failure leaves the command
// unprepared, preventing any child from receiving a partial snapshot.
func TestComposeJobSnapshot_InvalidTempDirectoryDoesNotChangeCommand(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	job := NewComposeJob()
	job.Project.Snapshot = &ComposeExecutionSnapshot{Model: `{"name":"accepted"}`}
	command := snapshotCommand()
	want := append([]string(nil), command.Args...)
	_, err := job.prepareSnapshot(command)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Equal(t, want, command.Args, "a failed snapshot must never reach the child process")
}
