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

	// The exit status is what a pipeline reads. This used to be discarded here
	// with a note calling the exit-0 behavior intentional, which meant the
	// test documented the defect instead of catching it: `ofelia validate …
	// || exit 1` could never fire on a broken config.
	assertExitCode(t, err, 1, stdout, stderr)

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
	stdout, stderr, err := runCommand(t, "validate", "--config="+missingPath)

	assertExitCode(t, err, 1, stdout, stderr)

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
	stdout, stderr, err := runCommand(t, "validate", "--config="+configPath)

	// The counterpart to the failure cases: a good config must exit 0, or a
	// pipeline that gates on validate would reject every deployment.
	assertExitCode(t, err, 0, stdout, stderr)

	// JSON dump should mention both jobs we defined.
	for _, needle := range []string{`"hello"`, `"world"`, `"Image": "alpine:3.20"`} {
		if !strings.Contains(stdout, needle) {
			t.Errorf("expected validate stdout to contain %q, got:\nstdout=%s\nstderr=%s",
				needle, stdout, stderr)
		}
	}
}
