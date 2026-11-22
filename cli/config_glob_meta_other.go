// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build !windows

package cli

// globMeta holds the characters that make a config path a glob for
// filepath.Glob; on these systems a backslash escapes the next character.
const globMeta = `*?[\`
