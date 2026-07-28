//go:build e2e && unix
// +build e2e,unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_Validate_MalformedINI asserts the `validate` command surfaces a
// useful, user-actionable error when the INI syntax is broken. End-to-end
// coverage matters here because the error is produced by a chain of
// components (flag parser → config loader → ini library → stderr writer)
// that's hard to fake in unit tests.
func TestE2E_Validate_MalformedINI(t *testing.T) {
	t.Parallel()

	configPath := writeConfig(t, "[unterminated-section\n  key = value\n")

	stdout, stderr, err := runCommand(t, "validate", "--config="+configPath)

	// Note on exit code: ofelia.go intentionally calls `return` instead of
	// os.Exit(1) after go-flags reports the error (see ofelia.go ~L132),
	// so the process exits 0. We therefore assert on the human-readable
	// error text — that's what the user actually sees in CI logs.
	_ = err

	combined := stdout + stderr
	for _, needle := range []string{
		"unclosed section",
		"INI syntax",
	} {
		if !strings.Contains(combined, needle) {
			t.Errorf("expected validate output to mention %q, got:\nstdout=%s\nstderr=%s",
				needle, stdout, stderr)
		}
	}
}

// TestE2E_Validate_MissingConfigFile asserts the missing-file code path is
// reported in a way a human operator can act on (it points at the path and
// offers the `ls -l` hint).
func TestE2E_Validate_MissingConfigFile(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "does-not-exist.ini")
	stdout, stderr, _ := runCommand(t, "validate", "--config="+missingPath)

	combined := stdout + stderr
	for _, needle := range []string{
		"no such file or directory",
		missingPath,
	} {
		if !strings.Contains(combined, needle) {
			t.Errorf("expected validate output to mention %q, got:\nstdout=%s\nstderr=%s",
				needle, stdout, stderr)
		}
	}
}

// TestE2E_Validate_AcceptsValidConfig is the happy-path counterpart: a
// well-formed config (both globals + a local job + a docker job) is
// accepted and the structured JSON dump is produced on stdout. Regression
// guard so we notice if a change accidentally rejects valid inputs.
func TestE2E_Validate_AcceptsValidConfig(t *testing.T) {
	t.Parallel()

	configBody := `[global]
  log-level = info

[job-local "hello"]
  schedule = @every 30s
  command = echo hello

[job-run "world"]
  schedule = @every 1m
  image = alpine:3.20
  command = echo world
`

	configPath := writeConfig(t, configBody)
	stdout, stderr, _ := runCommand(t, "validate", "--config="+configPath)

	// JSON dump should mention both jobs we defined.
	for _, needle := range []string{`"hello"`, `"world"`, `"Image": "alpine:3.20"`} {
		if !strings.Contains(stdout, needle) {
			t.Errorf("expected validate stdout to contain %q, got:\nstdout=%s\nstderr=%s",
				needle, stdout, stderr)
		}
	}
}
