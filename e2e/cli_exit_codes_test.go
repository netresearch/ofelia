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

// TestE2E_ExitCode_StrictValidationFails complements the INI-syntax case in
// config_validation_test.go with a file that parses but does not satisfy
// strict validation. Both have to fail, and for a deploy gate the distinction
// does not matter — what matters is that neither is silently accepted.
//
// Strict validation is opt-in (`enable-strict-validation`, default false), so
// the config turns it on. Without it ofelia accepts semantically broken jobs
// here — including an unparsable schedule, which the daemon then logs as a
// warning while starting anyway, leaving a job that never fires. That is a
// separate question from exit codes and is not pinned here.
func TestE2E_ExitCode_StrictValidationFails(t *testing.T) {
	t.Parallel()

	configPath := writeConfig(t, `[global]
  enable-strict-validation = true

[job-local "broken"]
  schedule = not-a-schedule
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

// TestE2E_GlobalFlagsBeforeSubcommand pins that the flags ofelia pre-parses
// out of argv are accepted in the position a user would naturally write them.
//
// `--log-level` and `--config` are read before the logger exists, so they look
// global — but the top-level parser did not declare them, and putting them
// ahead of the subcommand was rejected with `unknown flag`. Both positions now
// have to behave the same, since nothing about the flag suggests otherwise.
func TestE2E_GlobalFlagsBeforeSubcommand(t *testing.T) {
	t.Parallel()

	configPath := writeConfig(t, `[global]
  log-level = info

[job-local "hello"]
  schedule = @every 30s
  command = echo hi
`)

	cases := []struct {
		name string
		args []string
	}{
		{name: "config before", args: []string{"--config=" + configPath, "validate"}},
		{name: "config after", args: []string{"validate", "--config=" + configPath}},
		{name: "log-level before", args: []string{"--log-level=debug", "--config=" + configPath, "validate"}},
		{name: "log-level after", args: []string{"validate", "--log-level=debug", "--config=" + configPath}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, err := runCommand(t, tc.args...)
			assertExitCode(t, err, 0, stdout, stderr)

			if strings.Contains(stdout+stderr, "unknown flag") {
				t.Errorf("%v was rejected as an unknown flag:\nstdout=%s\nstderr=%s", tc.args, stdout, stderr)
			}
		})
	}
}

// TestE2E_GlobalConfigFlagIsHonoured guards the half that a "does it parse"
// test would miss: the path has to actually reach the command. Declaring the
// flag on the parser is not enough on its own — the subcommands carried their
// own `--config` default, which overwrote the pre-parsed value and sent
// validate to /etc/ofelia/config.ini regardless of what was asked for.
func TestE2E_GlobalConfigFlagIsHonoured(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "not-here.ini")

	stdout, stderr, err := runCommand(t, "--config="+missing, "validate")
	if err == nil {
		t.Fatalf("validating a missing config succeeded:\nstdout=%s\nstderr=%s", stdout, stderr)
	}

	// The named path proves the flag was used rather than silently replaced
	// by the compiled-in default.
	if !strings.Contains(stdout+stderr, missing) {
		t.Errorf("the error names a different config than the one requested (%s):\nstdout=%s\nstderr=%s",
			missing, stdout, stderr)
	}
}
