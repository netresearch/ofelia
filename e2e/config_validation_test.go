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

// TestE2E_Validate_ChecksWithoutStrictFlag pins that running validate is
// itself the request to have the config checked. The checks used to sit behind
// `enable-strict-validation`, so the one command whose purpose is validation
// answered "looks fine" for a config it had not inspected whenever that
// runtime toggle was off — which is the default.
//
// The toggle still governs whether the daemon refuses to start; it no longer
// governs whether the checker checks.
func TestE2E_Validate_ChecksWithoutStrictFlag(t *testing.T) {
	t.Parallel()

	// A malformed listen address, with no enable-strict-validation in sight.
	configPath := writeConfig(t, `[global]
  web-address = definitely-not-an-address

[job-local "hello"]
  schedule = @every 30s
  command = echo hello
`)

	stdout, stderr, err := runCommand(t, "validate", "--config="+configPath)
	if err == nil {
		t.Errorf("an invalid web-address was accepted without the strict flag:\nstdout=%s\nstderr=%s",
			stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "web-address") {
		t.Errorf("expected the offending field to be named, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestE2E_Validate_WebCredentialsOnlyWhenAuthEnabled pins the conditional
// requirement from the other side: a config with no web UI must not be asked
// for web-UI credentials, or enabling validation at all becomes impractical.
func TestE2E_Validate_WebCredentialsOnlyWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	configPath := writeConfig(t, `[global]
  enable-strict-validation = true

[job-local "hello"]
  schedule = @every 30s
  command = echo hello
`)

	stdout, stderr, err := runCommand(t, "validate", "--config="+configPath)
	if err != nil {
		t.Errorf("a config without a web UI was rejected: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	for _, unwanted := range []string{"web-password-hash", "web-secret-key"} {
		if strings.Contains(stdout+stderr, unwanted+"': is required") {
			t.Errorf("%s was demanded although web auth is off", unwanted)
		}
	}
}

// TestE2E_Validate_RejectsUnparsableSchedule is the case #773 was opened for.
//
// The config parses as INI and is only wrong semantically: the schedule cannot
// be parsed, so the scheduler refuses the job and it never runs. validate used
// to accept it, because the validator walked structs and skipped maps — and
// every job lives in one, so no job was reachable by it at all.
func TestE2E_Validate_RejectsUnparsableSchedule(t *testing.T) {
	t.Parallel()

	configPath := writeConfig(t, `[job-local "broken"]
  schedule = not-a-schedule
  command = echo hi
`)

	stdout, stderr, err := runCommand(t, "validate", "--config="+configPath)
	assertExitCode(t, err, 1, stdout, stderr)

	combined := stdout + stderr
	for _, needle := range []string{"broken", "cron"} {
		if !strings.Contains(combined, needle) {
			t.Errorf("expected the error to mention %q, got:\nstdout=%s\nstderr=%s", needle, stdout, stderr)
		}
	}
}

// TestE2E_Validate_RejectsJobMissingWhatItNeeds covers the requirement side:
// a job-exec with no container fails on every single run with
// `run_exec container "": invalid container name or ID`. Catching it here
// turns a job that fails forever into a config error caught before deployment.
func TestE2E_Validate_RejectsJobMissingWhatItNeeds(t *testing.T) {
	t.Parallel()

	configPath := writeConfig(t, `[job-exec "no-container"]
  schedule = @every 30s
  command = echo hi
`)

	stdout, stderr, err := runCommand(t, "validate", "--config="+configPath)
	assertExitCode(t, err, 1, stdout, stderr)

	if !strings.Contains(stdout+stderr, "container") {
		t.Errorf("expected the missing container to be named, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestE2E_Validate_AcceptsTheShippedExample guards the other direction with
// the configuration the project hands to new users: adding job checks must not
// start rejecting it.
func TestE2E_Validate_AcceptsTheShippedExample(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runCommand(t, "validate", "--config=../example/ofelia.ini")
	assertExitCode(t, err, 0, stdout, stderr)
}
