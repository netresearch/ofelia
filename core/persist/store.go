// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// CurrentVersion is the on-disk schema version this build writes.
// Bump (with a migration path) on any future-incompatible change.
const CurrentVersion = 1

// ErrUnsupportedVersion is returned by Load when the file's Version
// field is newer than CurrentVersion. We intentionally fail closed
// rather than guess at a forward migration the operator hasn't opted
// into — silent data loss is the worse outcome.
var ErrUnsupportedVersion = errors.New("persist: unsupported state-file version")

// JobType enumerates the API-creatable job kinds. Mirrors web.jobRequest.Type
// so the round-trip from API → state-file → load is lossless. Kept as
// a string alias rather than an int enum so the on-disk format reads
// naturally and so new types can be added without renumbering.
type JobType string

const (
	JobTypeRun     JobType = "run"
	JobTypeExec    JobType = "exec"
	JobTypeLocal   JobType = "local"
	JobTypeCompose JobType = "compose"
)

// Job is the per-entry on-disk representation. Carries the union of
// fields the web API accepts (web.jobRequest); fields not relevant to
// a given Type stay zero-value and are omitted by `omitempty`. Adding
// a new field is backward-compatible (older builds drop it on read,
// new builds round-trip it).
type Job struct {
	Type     JobType `json:"type"`
	Schedule string  `json:"schedule"`
	Command  string  `json:"command,omitempty"`
	// Run + Exec
	Container string `json:"container,omitempty"`
	// Run-only
	Image string `json:"image,omitempty"`
	// Compose-only
	File    string `json:"file,omitempty"`
	Service string `json:"service,omitempty"`
	Exec    bool   `json:"exec,omitempty"`
}

// State is the on-disk document.
type State struct {
	Version  int             `json:"version"`
	Jobs     map[string]*Job `json:"jobs,omitempty"`     // keyed by job name
	Disabled []string        `json:"disabled,omitempty"` // job names disabled; sorted on write for determinism
}

// Store wraps an on-disk State with concurrency-safe mutators that
// atomically persist on every change. Construct with NewStore(path);
// a zero path means "disabled" and every mutator becomes a no-op so
// callers don't have to nil-check at every call site.
type Store struct {
	path string

	mu    sync.RWMutex
	state State
}

// NewStore returns a Store backed by the given path. An empty path
// disables persistence — Load returns an empty state, mutators are
// no-ops, Save never touches disk. This lets callers wire a single
// nil-safe Store without branching on configuration.
func NewStore(path string) *Store {
	return &Store{
		path:  path,
		state: State{Version: CurrentVersion},
	}
}

// Enabled reports whether the store has a backing file.
func (s *Store) Enabled() bool {
	return s.path != ""
}

// Path returns the configured file path (empty if disabled).
func (s *Store) Path() string { return s.path }

// Load reads the state file into memory. Missing file is treated as
// "fresh start" (empty state, no error) so first boot with persistence
// enabled doesn't fail before any API call has run. A malformed file
// or a future Version returns an error — boot must fail loudly rather
// than silently drop persisted state.
func (s *Store) Load() error {
	if !s.Enabled() {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("persist: read %s: %w", s.path, err)
	}
	if len(raw) == 0 {
		return nil
	}
	var loaded State
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return fmt.Errorf("persist: decode %s: %w", s.path, err)
	}
	if loaded.Version > CurrentVersion {
		return fmt.Errorf("%w: file has version %d, this build supports up to %d",
			ErrUnsupportedVersion, loaded.Version, CurrentVersion)
	}
	if loaded.Version == 0 {
		loaded.Version = CurrentVersion
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = loaded
	return nil
}

// Snapshot returns a deep-ish copy of the current state. Callers must
// not mutate the returned slices/maps in place if they want isolation
// from subsequent Store changes — typical callers (boot-time loader,
// tests) only read.
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

// PutJob inserts or replaces a job entry and persists. No-op if the
// store is disabled.
func (s *Store) PutJob(name string, job Job) error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Jobs == nil {
		s.state.Jobs = make(map[string]*Job)
	}
	cp := job
	s.state.Jobs[name] = &cp
	return s.saveLocked()
}

// RemoveJob deletes the job entry (if present) and persists. Removing
// the job does NOT clear it from the disabled list — disable status is
// kept independent because operators may pre-disable a job before the
// API recreates it. Returns nil if the name wasn't tracked.
func (s *Store) RemoveJob(name string) error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Jobs == nil {
		return nil
	}
	if _, ok := s.state.Jobs[name]; !ok {
		return nil
	}
	delete(s.state.Jobs, name)
	return s.saveLocked()
}

// SetDisabled marks a job name as disabled (persisted across restart).
// Calling with a name already disabled is a no-op (no save). Idempotent
// to keep API handlers simple.
func (s *Store) SetDisabled(name string) error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.state.Disabled {
		if n == name {
			return nil
		}
	}
	s.state.Disabled = append(s.state.Disabled, name)
	sort.Strings(s.state.Disabled)
	return s.saveLocked()
}

// ClearDisabled removes a job name from the disabled list. No-op if
// not present.
func (s *Store) ClearDisabled(name string) error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.state.Disabled[:0]
	changed := false
	for _, n := range s.state.Disabled {
		if n == name {
			changed = true
			continue
		}
		out = append(out, n)
	}
	s.state.Disabled = out
	if !changed {
		return nil
	}
	return s.saveLocked()
}

// saveLocked serializes the current in-memory state and atomically
// replaces the file via tmp+rename. Caller MUST hold s.mu for write —
// the mutator-and-save cycle must be atomic, otherwise concurrent
// PutJob calls can interleave mutate→save→mutate→save such that the
// later save races to rename and the earlier mutate's data wins
// (caught by TestStore_ConcurrentMutators).
func (s *Store) saveLocked() error {
	snap := s.snapshotLocked()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("persist: encode: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("persist: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("persist: tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("persist: write tempfile: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("persist: sync tempfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("persist: close tempfile: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		cleanup()
		return fmt.Errorf("persist: rename %s → %s: %w", tmpPath, s.path, err)
	}
	return nil
}

// snapshotLocked is the lock-free body of Snapshot; callers must hold
// at least an RLock.
func (s *Store) snapshotLocked() State {
	out := State{Version: s.state.Version}
	if out.Version == 0 {
		out.Version = CurrentVersion
	}
	if len(s.state.Jobs) > 0 {
		out.Jobs = make(map[string]*Job, len(s.state.Jobs))
		for k, v := range s.state.Jobs {
			j := *v
			out.Jobs[k] = &j
		}
	}
	if len(s.state.Disabled) > 0 {
		out.Disabled = append([]string(nil), s.state.Disabled...)
	}
	return out
}
