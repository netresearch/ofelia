// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build unix

package composeproject

import (
	"os"
	"syscall"
)

// snapshotFileIdentity captures host UID/GID from the opened file metadata; unavailable identity is represented
// explicitly rather than guessed.
func snapshotFileIdentity(info os.FileInfo) (int, int) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, -1
	}
	return int(stat.Uid), int(stat.Gid)
}
