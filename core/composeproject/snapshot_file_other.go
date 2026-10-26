// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build !unix

package composeproject

import "os"

func snapshotFileIdentity(_ os.FileInfo) (int, int) { return -1, -1 }
