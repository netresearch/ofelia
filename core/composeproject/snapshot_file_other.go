// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build !unix

package composeproject

import "os"

// snapshotFileIdentity records unavailable Unix ownership explicitly so non-Unix captures rely on the
// platform's supported access semantics.
func snapshotFileIdentity(_ os.FileInfo) (int, int) { return -1, -1 }
