// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build unix

package core

import (
	"fmt"
	"os"
	"syscall"

	"github.com/netresearch/ofelia/core/domain"
)

// snapshotPathPrivate requires current ownership and no group/other access before accepted data can be written
// beneath a stable Unix cache directory.
func snapshotPathPrivate(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	// #nosec G115 -- Conversion preserves the Unix uid_t identifier returned by the OS.
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0o077 == 0
}

// setSnapshotInputIdentity preserves the mounted inode's source owner and mode, returning an error when the
// runtime cannot reproduce those access semantics.
func setSnapshotInputIdentity(file *os.File, input domain.ComposeSnapshotFile) error {
	if err := file.Chown(input.UID, input.GID); err != nil {
		return fmt.Errorf("set captured input owner: %w", err)
	}
	if err := file.Chmod(os.FileMode(input.Mode)); err != nil {
		return fmt.Errorf("set captured input mode: %w", err)
	}
	return nil
}

// snapshotInputIdentityMatches checks the retained inode's complete Unix access identity before its bytes can
// be reused for another execution.
func snapshotInputIdentityMatches(info os.FileInfo, input domain.ComposeSnapshotFile) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == input.UID && int(stat.Gid) == input.GID && uint32(info.Mode().Perm()) == input.Mode
}
