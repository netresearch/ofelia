// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

// Package cli wires the daemon together: it parses flags and the INI
// configuration, discovers jobs from Docker container labels, reconciles the
// scheduler when either source changes, and owns the process lifecycle.
package cli
