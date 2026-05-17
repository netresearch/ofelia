// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

// Package persist stores the subset of scheduler state that originates
// from the web UI / REST API and that operators reasonably expect to
// survive a daemon restart (issue #593).
//
// Scope (intentional):
//   - Jobs created or updated via POST /api/jobs (origin="api").
//   - The set of *disabled* job names — disable applies regardless of
//     origin, so an operator can pause an INI-defined job from the UI
//     and have that pause survive restart.
//
// Out of scope:
//   - Jobs defined in INI or via Docker labels: those sources are
//     authoritative and reloaded fresh every boot.
//   - Runtime stats (history, last-run, errors): rebuilt by core.
//   - Webhook configuration, global settings, anything from [global].
//
// Format: stdlib-encoding/json, written atomically (tmp + rename) on
// every mutation. The on-disk schema carries a Version field so
// future-incompatible changes can be migrated explicitly rather than
// silently drift. The file is human-readable so operators can inspect
// or back it up with rsync; hand-editing is supported but discouraged
// in favor of API calls (a malformed file fails the daemon boot).
//
// Concurrency: a single *Store is safe for concurrent calls from the
// web handlers; an internal RWMutex guards both the in-memory snapshot
// and the file write. Saves are serialized; readers don't block on a
// save in progress.
package persist
