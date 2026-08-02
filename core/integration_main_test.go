//go:build integration

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"os"
	"testing"
)

// TestMain provides test suite-level setup for integration tests.
// The SDK-based Docker provider is used for all Docker operations.
func TestMain(m *testing.M) {
	// Run all tests
	exitCode := m.Run()

	os.Exit(exitCode)
}
