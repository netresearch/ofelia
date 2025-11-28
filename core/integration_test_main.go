//go:build integration
// +build integration

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
