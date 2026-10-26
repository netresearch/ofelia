// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build !unix

package core

import (
	"fmt"
	"os"

	"github.com/netresearch/ofelia/core/domain"
)

// Non-Unix hosts enforce private directory access through their native ACLs.
// Lstat and regular-file checks at the caller still reject symlinks.
func snapshotPathPrivate(_ os.FileInfo) bool { return true }

func setSnapshotInputIdentity(file *os.File, input domain.ComposeSnapshotFile) error {
	if err := file.Chmod(os.FileMode(input.Mode)); err != nil {
		return fmt.Errorf("set captured input mode: %w", err)
	}
	return nil
}

func snapshotInputIdentityMatches(_ os.FileInfo, _ domain.ComposeSnapshotFile) bool { return true }
