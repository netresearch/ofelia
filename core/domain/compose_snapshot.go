// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package domain

// ComposeSnapshotFile preserves the bytes and host-file access of a config
// or secret that Compose bind-mounts into a workload. Parent-directory access
// protects the retained copy; the mounted inode keeps its original identity.
type ComposeSnapshotFile struct {
	Content []byte
	Mode    uint32
	UID     int
	GID     int
}
