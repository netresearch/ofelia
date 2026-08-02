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

// Everything a user reaches for before ofelia ever schedules anything — the
// version banner, the help listing, `validate` in a deploy gate — is judged by
// its exit status long before anyone reads its output. These tests run the
// real binary and assert that status, because the process boundary is where
// the contract lives: a unit test can call a function and inspect its error,
// but only the binary can be wrong about what it hands back to the shell.
//
// This surface previously exited 0 for every failure, which made
// `ofelia validate --config=… || exit 1` a no-op in any pipeline that used it.

// TestE2E_ExitCode_UnknownCommand pins that a typo is a failure. A shell
// wrapper that dispatches to ofelia has nothing else to go on.
func TestE2E_ExitCode_UnknownCommand(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runCommand(t, "definitely-not-a-command")
	assertExitCode(t, err, 1, stdout, stderr)

	// The listing is what tells the user what they should have typed.
	if !strings.Contains(stdout+stderr, "daemon") {
		t.Errorf("an unknown command should print the available commands, got:\nstdout=%s\nstderr=%s",
			stdout, stderr)
	}
}

// TestE2E_ExitCode_SuccessPaths covers the other direction: asking for
// information succeeded, so these must not report failure. Getting this
// backwards would break every CI step that runs `ofelia version` as a probe.
func TestE2E_ExitCode_SuccessPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
	}{
		{name: "version subcommand", args: []string{"version"}},
		{name: "version flag", args: []string{"--version"}},
		{name: "help flag", args: []string{"--help"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, err := runCommand(t, tc.args...)
			assertExitCode(t, err, 0, stdout, stderr)

			if strings.TrimSpace(stdout+stderr) == "" {
				t.Errorf("%v printed nothing", tc.args)
			}
		})
	}
}

// TestE2E_ExitCode_SemanticFailureExits1 complements the INI-syntax case in
// config_validation_test.go with a file that parses but is semantically wrong.
// Both have to fail, and for a deploy gate the distinction does not matter —
// what matters is that neither is silently accepted.
//
// The first version of this test used an unparsable schedule and only passed
// because validation demanded web-UI credentials from a config that had no web
// UI. That false positive is gone, and jobs are not reachable by the validator
// at all, so the config here fails on a global field that genuinely is checked.
// The schedule gap is tracked separately in #773.
func TestE2E_ExitCode_SemanticFailureExits1(t *testing.T) {
	t.Parallel()

	configPath := writeConfig(t, `[global]
  web-address = definitely-not-an-address

[job-local "hello"]
  schedule = @every 30s
  command = echo hi
`)

	stdout, stderr, err := runCommand(t, "validate", "--config="+configPath)
	assertExitCode(t, err, 1, stdout, stderr)

	if !strings.Contains(stdout+stderr, "validation failed") {
		t.Errorf("expected a validation failure message, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestE2E_ExitCode_ValidateIsUsableAsAGate is the test that would have caught
// the original defect on its own: it uses validate exactly as a deployment
// pipeline does — run it, branch on the status — over a good and a bad config,
// and requires the two to be distinguishable.
func TestE2E_ExitCode_ValidateIsUsableAsAGate(t *testing.T) {
	t.Parallel()

	good := writeConfig(t, `[global]
  log-level = info

[job-local "hello"]
  schedule = @every 30s
  command = echo hello
`)
	bad := filepath.Join(t.TempDir(), "missing.ini")

	_, _, goodErr := runCommand(t, "validate", "--config="+good)
	_, _, badErr := runCommand(t, "validate", "--config="+bad)

	if goodErr != nil {
		t.Errorf("a valid config was rejected: %v", goodErr)
	}
	if badErr == nil {
		t.Error("a missing config was accepted; validate cannot be used as a gate")
	}
}
