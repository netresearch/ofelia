// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build windows

package cli

// globMeta holds the characters that make a config path a glob for
// filepath.Glob; on Windows a backslash separates path elements.
const globMeta = "*?["
