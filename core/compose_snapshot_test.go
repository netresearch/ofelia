// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
)

func snapshotCommand() *exec.Cmd {
	return &exec.Cmd{Args: []string{"docker", "compose", "-f", "-", "run", "--rm", "backup"}}
}

func snapshotTestFile(content string) domain.ComposeSnapshotFile {
	return domain.ComposeSnapshotFile{Content: []byte(content), Mode: 0o600, UID: os.Geteuid(), GID: os.Getegid()}
}

func TestComposeJobSnapshot_RetainsInputsAndRemovesTemporaryModel(t *testing.T) {
	t.Parallel()
	job := NewComposeJob()
	job.Project.Snapshot = &ComposeExecutionSnapshot{
		Model: `{"name":"snapshot","services":{},"configs":{"settings":{"file":"old.conf"}},` +
			`"secrets":{"token":{"file":"old.secret"}},"jobs":{"backup":{"image":"alpine:3.22","command":["echo","$literal"]}}}`,
		Files: map[string]domain.ComposeSnapshotFile{
			"configs/settings": snapshotTestFile("accepted-config"), "secrets/token": snapshotTestFile("accepted-secret"),
		},
	}
	// prepareSnapshot receives the command after input validation and reserves
	// the first -f for the accepted model, never either original input path.
	cmd := snapshotCommand()
	cleanup, err := job.prepareSnapshot(cmd)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	modelFile, envFile := cmd.Args[5], cmd.Args[3]
	model, err := os.ReadFile(modelFile)
	require.NoError(t, err)
	assert.Contains(t, string(model), "$$literal", "already resolved dollars survive Compose's next parse")
	var config struct {
		Configs map[string]struct{ File string } `json:"configs"`
		Secrets map[string]struct{ File string } `json:"secrets"`
	}
	require.NoError(t, json.Unmarshal(model, &config))
	files := map[string]string{config.Configs["settings"].File: "accepted-config", config.Secrets["token"].File: "accepted-secret"}
	for path, want := range files {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, want, string(data))
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Zero(t, info.Mode().Perm()&0o077)
	}
	data, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Empty(t, data)
	serialized, err := json.Marshal(job)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "accepted-secret")
	assert.NotContains(t, string(serialized), "Snapshot")

	cleanup()
	for path := range files {
		_, err := os.Stat(path)
		require.NoError(t, err, "dependency services may restart after the CLI exits")
	}
	_, err = os.Stat(modelFile)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(envFile)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestComposeJobSnapshot_InvalidModelCleansPrivateDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	job := NewComposeJob()
	job.Project.Snapshot = &ComposeExecutionSnapshot{Model: "invalid-json"}
	_, err := job.prepareSnapshot(snapshotCommand())
	require.Error(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestComposeJobSnapshot_RejectsMissingCapturedResource(t *testing.T) {
	t.Parallel()
	snapshot := &ComposeExecutionSnapshot{
		Model: `{"configs":{}}`, Files: map[string]domain.ComposeSnapshotFile{"configs/missing": snapshotTestFile("secret")},
	}
	_, err := materializeSnapshot(snapshot, t.TempDir())
	require.ErrorIs(t, err, errInvalidComposeSnapshot)
	assert.NotContains(t, err.Error(), "secret", "input bytes must not be included in errors")
}

func TestComposeJobSnapshot_EnvironmentRoundTrip(t *testing.T) {
	t.Parallel()
	want := map[string]string{"OFELIA_SNAPSHOT_A": "value-'$literal\\\nsecond\rline", "OFELIA_SNAPSHOT_B": ""}
	got, err := dotenv.Parse(strings.NewReader(snapshotEnvironment(want)))
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestComposeJobSnapshot_ConcurrentInputsKeepBytesAndAccess(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inputs")
	input := snapshotTestFile("accepted-concurrent-input")
	input.Mode = 0o644
	paths, errors := make(chan string, 8), make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			path, err := retainSnapshotInput(dir, input)
			paths <- path
			errors <- err
		})
	}
	workers.Wait()
	close(paths)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var accepted string
	for path := range paths {
		if accepted == "" {
			accepted = path
		}
		assert.Equal(t, accepted, path)
	}
	info, err := os.Stat(accepted)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "the mounted inode preserves non-root readability")
	}
	input.Mode = 0o400
	restricted, err := retainSnapshotInput(dir, input)
	require.NoError(t, err)
	assert.NotEqual(t, accepted, restricted, "different access semantics cannot share one captured inode")
	require.NoError(t, os.WriteFile(accepted, []byte("tampered-input"), 0o600))
	input.Mode = 0o644
	_, err = retainSnapshotInput(dir, input)
	require.ErrorIs(t, err, errInvalidComposeSnapshot)
}

func TestComposeJobSnapshot_RejectsRetainedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require Windows privileges")
	}
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inputs")
	input := snapshotTestFile("accepted-symlink-input")
	path, err := retainSnapshotInput(dir, input)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	target := filepath.Join(t.TempDir(), "other-input")
	require.NoError(t, os.WriteFile(target, input.Content, 0o600))
	require.NoError(t, os.Symlink(target, path))
	_, err = retainSnapshotInput(dir, input)
	require.ErrorIs(t, err, errInvalidComposeSnapshot)
}

func TestComposeJobSnapshot_DollarKeysKeepTheirLiteralNames(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker compose CLI is unavailable")
	}
	if _, err := ComposeCLIVersion(t.Context()); err != nil {
		t.Skipf("docker compose CLI is unavailable: %v", err)
	}
	job := NewComposeJob()
	job.Project.Snapshot = &ComposeExecutionSnapshot{
		Model: `{"name":"dollar-keys","services":{"task":{"image":"alpine:3.22",` +
			`"labels":{"com.example.$literal":"accepted-$value"}}}}`,
	}
	cmd := snapshotCommand()
	cleanup, err := job.prepareSnapshot(cmd)
	require.NoError(t, err)
	defer cleanup()
	config := exec.Command("docker", "compose", "--env-file", cmd.Args[3], "-f", cmd.Args[5], "config", "--format", "json") //nolint:gosec // test-owned snapshot paths
	output, err := config.Output()
	require.NoError(t, err)
	var model struct {
		Services map[string]struct{ Labels map[string]string } `json:"services"`
	}
	require.NoError(t, json.Unmarshal(output, &model))
	// Compose's config exporter re-escapes dollar signs, including label
	// keys. Compare its result with a correctly escaped original control.
	control := filepath.Join(t.TempDir(), "control.json")
	data := `{"name":"dollar-keys","services":{"task":{"image":"alpine:3.22",` +
		`"labels":{"com.example.$literal":"accepted-$$value"}}}}`
	require.NoError(t, os.WriteFile(control, []byte(data), 0o600))
	controlCmd := exec.Command("docker", "compose", "--env-file", cmd.Args[3], "-f", control, "config", "--format", "json") //nolint:gosec // test-owned file
	controlOutput, err := controlCmd.Output()
	require.NoError(t, err)
	var expected struct {
		Services map[string]struct{ Labels map[string]string } `json:"services"`
	}
	require.NoError(t, json.Unmarshal(controlOutput, &expected))
	assert.Equal(t, expected.Services["task"].Labels, model.Services["task"].Labels)
}

func TestComposeJobSnapshot_PreservesLargeIntegerValues(t *testing.T) {
	t.Parallel()
	data, err := escapeSnapshotInterpolation(`{"services":{"task":{"shm_size":9007199254740993}}}`)
	require.NoError(t, err)
	assert.Contains(t, string(data), "9007199254740993")
}

func TestComposeJobSnapshot_RejectsUnsafeInputDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions and symlinks are required")
	}
	t.Parallel()
	for _, symlink := range []bool{false, true} {
		name := "shared writable directory"
		if symlink {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "inputs")
			target := dir
			if symlink {
				target = t.TempDir()
				require.NoError(t, os.Symlink(target, dir))
			} else {
				require.NoError(t, os.Mkdir(dir, 0o777))
				require.NoError(t, os.Chmod(dir, 0o777))
			}
			_, err := retainSnapshotInput(dir, snapshotTestFile("accepted-private-input"))
			require.ErrorIs(t, err, errInvalidComposeSnapshot)
			entries, err := os.ReadDir(target)
			require.NoError(t, err)
			assert.Empty(t, entries, "no input may be written through an unsafe pre-created cache path")
		})
	}
}
