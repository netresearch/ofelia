// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStore_Disabled_AllMutatorsAreNoOps pins the "empty path means
// disabled" contract — every mutator must succeed without touching
// the filesystem so call sites can stay nil-check-free even when the
// operator hasn't opted into persistence (the default).
func TestStore_Disabled_AllMutatorsAreNoOps(t *testing.T) {
	t.Parallel()
	s := NewStore("")
	require.False(t, s.Enabled())
	require.NoError(t, s.Load())
	require.NoError(t, s.PutJob("foo", Job{Type: JobTypeLocal, Schedule: "@daily"}))
	require.NoError(t, s.RemoveJob("foo"))
	require.NoError(t, s.SetDisabled("bar"))
	require.NoError(t, s.ClearDisabled("bar"))

	snap := s.Snapshot()
	assert.Empty(t, snap.Jobs)
	assert.Empty(t, snap.Disabled)
}

// TestStore_PutJob_PersistsAcrossInstances is the end-to-end contract:
// after PutJob, a freshly-constructed Store at the same path must Load
// and see the same job. Round-trip via real file I/O (no mocks) so the
// JSON encoding and atomic rename are exercised together.
func TestStore_PutJob_PersistsAcrossInstances(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	writer := NewStore(path)
	require.True(t, writer.Enabled())

	job := Job{
		Type:      JobTypeRun,
		Schedule:  "@daily",
		Command:   "pg_dump mydb",
		Image:     "postgres:15",
		Container: "",
	}
	require.NoError(t, writer.PutJob("backup-db", job))

	reader := NewStore(path)
	require.NoError(t, reader.Load())
	snap := reader.Snapshot()
	require.Contains(t, snap.Jobs, "backup-db")
	got := snap.Jobs["backup-db"]
	assert.Equal(t, JobTypeRun, got.Type)
	assert.Equal(t, "@daily", got.Schedule)
	assert.Equal(t, "pg_dump mydb", got.Command)
	assert.Equal(t, "postgres:15", got.Image)
}

// TestStore_RemoveJob_LeavesDisabledIntact pins the design decision
// that the "disabled" list is independent of the "jobs" list. Removing
// a job from the API must NOT silently re-enable it on next API
// recreation — operators sometimes pre-disable a job slot, recreate
// it, and want the disable to stick.
func TestStore_RemoveJob_LeavesDisabledIntact(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.PutJob("foo", Job{Type: JobTypeLocal, Schedule: "@daily", Command: "true"}))
	require.NoError(t, s.SetDisabled("foo"))
	require.NoError(t, s.RemoveJob("foo"))

	reader := NewStore(path)
	require.NoError(t, reader.Load())
	snap := reader.Snapshot()
	assert.NotContains(t, snap.Jobs, "foo")
	assert.Contains(t, snap.Disabled, "foo",
		"disabled list must survive RemoveJob — operators pre-disable slots that the API later refills")
}

// TestStore_SetDisabled_Idempotent pins that calling SetDisabled twice
// with the same name doesn't duplicate or churn the file. Important
// because the web disable handler doesn't know whether the job is
// already disabled and shouldn't have to.
func TestStore_SetDisabled_Idempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.SetDisabled("foo"))
	require.NoError(t, s.SetDisabled("foo"))
	require.NoError(t, s.SetDisabled("foo"))

	snap := s.Snapshot()
	assert.Equal(t, []string{"foo"}, snap.Disabled,
		"duplicate SetDisabled calls must not duplicate entries")
}

// TestStore_ClearDisabled_RemovesOnly pins that ClearDisabled removes
// the requested name and only that name. Sorted-order invariant
// after removal is verified to keep the file deterministic for ops
// diffing with git.
func TestStore_ClearDisabled_RemovesOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.SetDisabled("c"))
	require.NoError(t, s.SetDisabled("a"))
	require.NoError(t, s.SetDisabled("b"))
	require.NoError(t, s.ClearDisabled("b"))

	snap := s.Snapshot()
	assert.Equal(t, []string{"a", "c"}, snap.Disabled,
		"disabled list must stay sorted after a removal so the file diff is deterministic")
}

// TestStore_Load_MissingFile_TreatsAsEmpty pins the first-boot UX —
// a fresh state-file path that doesn't exist yet must not fail Load.
// Failing here would block daemon boot the first time persistence is
// turned on.
func TestStore_Load_MissingFile_TreatsAsEmpty(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	s := NewStore(path)
	require.NoError(t, s.Load())
	snap := s.Snapshot()
	assert.Empty(t, snap.Jobs)
	assert.Empty(t, snap.Disabled)
	assert.Equal(t, CurrentVersion, snap.Version)
}

// TestStore_Load_MalformedJSON_Fails pins fail-closed semantics. A
// corrupted file must NOT be silently rewritten with an empty state —
// that would erase whatever state the operator had backed up and
// they'd have no chance to recover.
func TestStore_Load_MalformedJSON_Fails(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid"), 0o600))
	s := NewStore(path)
	err := s.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode",
		"malformed JSON must surface a decode error so operators see the cause")
}

// TestStore_Load_FutureVersion_Fails pins fail-closed forward
// compatibility. A file written by a newer build must NOT be silently
// re-saved at the current version — that would strip whatever fields
// the newer build added.
func TestStore_Load_FutureVersion_Fails(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	raw, err := json.Marshal(map[string]any{"version": CurrentVersion + 1})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o600))

	s := NewStore(path)
	err = s.Load()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedVersion,
		"future version must surface ErrUnsupportedVersion so callers can react explicitly")
}

// TestStore_Save_AtomicReplacement pins the tmp+rename invariant by
// observing that no `.state-*.json.tmp` siblings linger after a write.
// Pre-fix, a crash mid-write would leave the operator with a half-
// written state-file; atomic rename means readers either see the old
// file or the new one, never a torn write.
func TestStore_Save_AtomicReplacement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewStore(path)
	for range 5 {
		require.NoError(t, s.PutJob("foo", Job{Type: JobTypeLocal, Schedule: "@daily", Command: "true"}))
	}

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp",
			"no .tmp siblings must linger after successful save — rename is the atomic commit")
	}
	assert.True(t, fileExists(path), "main state file must exist after save")
}

// TestStore_Save_DeterministicBytes pins that re-saving identical
// state produces byte-identical files. Determinism matters because
// operators may version-control or rsync the state file and noisy
// diffs from non-deterministic map iteration would burn them.
func TestStore_Save_DeterministicBytes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.PutJob("b", Job{Type: JobTypeLocal, Schedule: "@daily"}))
	require.NoError(t, s.PutJob("a", Job{Type: JobTypeLocal, Schedule: "@hourly"}))
	require.NoError(t, s.SetDisabled("c"))
	first, err := os.ReadFile(path)
	require.NoError(t, err)

	// Re-issue identical mutations; bytes must match.
	require.NoError(t, s.PutJob("b", Job{Type: JobTypeLocal, Schedule: "@daily"}))
	require.NoError(t, s.PutJob("a", Job{Type: JobTypeLocal, Schedule: "@hourly"}))
	require.NoError(t, s.SetDisabled("c"))
	second, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second),
		"re-saving identical state must produce byte-identical output (no map-order drift)")
}

// TestStore_ConcurrentMutators pins thread-safety. Two goroutines
// hammering the same Store with distinct jobs must both round-trip
// without losing entries — the internal mutex must serialize saves.
func TestStore_ConcurrentMutators(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)

	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = s.PutJob(jobName("a", i), Job{Type: JobTypeLocal, Schedule: "@daily"})
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = s.PutJob(jobName("b", i), Job{Type: JobTypeLocal, Schedule: "@hourly"})
		}(i)
	}
	wg.Wait()

	reader := NewStore(path)
	require.NoError(t, reader.Load())
	snap := reader.Snapshot()
	assert.Len(t, snap.Jobs, 2*n,
		"concurrent PutJob calls must all persist — no save-overwrite race")
}

// TestStore_Snapshot_IsolatedFromInternalState pins the deep-copy
// claim in Snapshot's docstring. Mutating the returned slices/maps
// must NOT bleed into the Store's internal state — otherwise a
// caller iterating a snapshot while another goroutine mutates the
// Store would race on the same backing array.
func TestStore_Snapshot_IsolatedFromInternalState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.PutJob("x", Job{Type: JobTypeLocal, Schedule: "@daily"}))
	require.NoError(t, s.SetDisabled("y"))

	snap := s.Snapshot()
	snap.Jobs["x"].Schedule = "MUTATED" // mutate the returned struct
	snap.Jobs["new"] = &Job{Type: JobTypeLocal}
	snap.Disabled = append(snap.Disabled, "z")

	second := s.Snapshot()
	assert.Equal(t, "@daily", second.Jobs["x"].Schedule,
		"mutating snapshot Job pointer must not bleed into Store state")
	assert.NotContains(t, second.Jobs, "new",
		"adding entries to snapshot map must not bleed into Store state")
	assert.Equal(t, []string{"y"}, second.Disabled,
		"appending to snapshot Disabled slice must not bleed into Store state")
}

// TestStore_Load_VersionZero_NormalizesToCurrent pins forward-compat
// for files written by builds before the Version field existed (or by
// hand-edited files where the field was dropped). Version 0 must
// load as CurrentVersion so the next save writes the explicit field.
func TestStore_Load_VersionZero_NormalizesToCurrent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"version":0,"jobs":{"x":{"type":"local","schedule":"@daily"}}}`),
		0o600))
	s := NewStore(path)
	require.NoError(t, s.Load())
	snap := s.Snapshot()
	assert.Equal(t, CurrentVersion, snap.Version,
		"version 0 must normalize to CurrentVersion so subsequent saves carry the explicit field")
	assert.Contains(t, snap.Jobs, "x")
}

// TestStore_Load_ZeroByteFile_TreatsAsEmpty pins the empty-file path
// in Load: a 0-byte state file (e.g. one that was created but never
// successfully written, or truncated mid-save by some external tool)
// must NOT fail boot — it's treated as "fresh start" so the daemon
// can recover automatically on the next mutation.
func TestStore_Load_ZeroByteFile_TreatsAsEmpty(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	s := NewStore(path)
	require.NoError(t, s.Load())
	snap := s.Snapshot()
	assert.Empty(t, snap.Jobs)
	assert.Empty(t, snap.Disabled)
}

// TestStore_Load_RejectsUnknownFields pins DisallowUnknownFields —
// a typo'd field in a hand-edited file (e.g. "scheduel" instead of
// "schedule") must surface as a decode error rather than silently
// load with the field dropped, which would look like the edit did
// nothing.
func TestStore_Load_RejectsUnknownFields(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"version":1,"jobs":{"x":{"type":"local","schedule":"@daily","typo_field":"oops"}}}`),
		0o600))
	s := NewStore(path)
	err := s.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "typo_field",
		"unknown fields must surface in the error so the operator sees what to fix")
}

// TestStore_Load_RejectsOversizeFile pins the 16 MiB ceiling — a
// pathological file (corrupt growth, attacker-controlled) must not
// OOM boot. Fake the size cap by tweaking the constant via a build
// flag would be cleaner, but writing a real ~17 MiB file is portable
// and exercises the same code path.
func TestStore_Load_RejectsOversizeFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	huge := make([]byte, maxStateFileBytes+1)
	for i := range huge {
		huge[i] = ' '
	}
	require.NoError(t, os.WriteFile(path, huge, 0o600))
	s := NewStore(path)
	err := s.Load()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStateFileTooLarge,
		"oversize file must surface ErrStateFileTooLarge so callers can distinguish bootstrap-abort from generic I/O errors")
}

// TestStore_Save_FilePermissions pins the explicit 0o600 mode set in
// saveLocked. State files contain operator-defined commands and
// container names — must not be world-readable even on shared
// volumes with permissive umasks.
func TestStore_Save_FilePermissions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.PutJob("x", Job{Type: JobTypeLocal, Schedule: "@daily"}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"state file must be 0600 — contains operator-supplied command strings")
}

// TestStore_RemoveThenReadd_PersistsNewState pins the rename-then-add
// round-trip: removing a job and adding it back with a different
// schedule must not leave the old schedule resident anywhere — a
// fresh reader sees only the new state.
func TestStore_RemoveThenReadd_PersistsNewState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.PutJob("x", Job{Type: JobTypeLocal, Schedule: "@daily"}))
	require.NoError(t, s.RemoveJob("x"))
	require.NoError(t, s.PutJob("x", Job{Type: JobTypeLocal, Schedule: "@hourly"}))

	reader := NewStore(path)
	require.NoError(t, reader.Load())
	snap := reader.Snapshot()
	require.Contains(t, snap.Jobs, "x")
	assert.Equal(t, "@hourly", snap.Jobs["x"].Schedule,
		"re-added job must reflect the latest schedule, not a stale value from the previous incarnation")
}

// helpers ---------------------------------------------------------------

func jobName(prefix string, i int) string {
	const digits = "0123456789"
	switch {
	case i < 10:
		return prefix + string(digits[i])
	default:
		return prefix + string(digits[i/10]) + string(digits[i%10])
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
