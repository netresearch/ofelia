// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run() is what main() does, minus the call to os.Exit, so a test can assert
// on the exit code the process would have produced. Exercising it here covers
// the command wiring — a command dropped from the parser, or a constructor
// that starts panicking, fails these tests instead of only showing up when a
// user runs the binary — and, since the exit code is the only thing a shell
// reads, that each outcome maps to the right status.
//
// These tests never pass a real command such as `daemon`, which would start a
// scheduler. Only paths that parse and return are used.

// runMain invokes run() with the given argv while redirecting stdout, so the
// parser's help output does not drown the test log. It restores os.Stdout
// before returning and reports both what was printed and the exit code.
func runMain(t *testing.T, argv ...string) (string, int) {
	t.Helper()

	origStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = origStdout })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}

	// A pipe's buffer is finite and the help text is long, so drain it
	// concurrently; writing more than the buffer holds would otherwise block
	// run() forever.
	captured := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
		captured <- sb.String()
	}()

	os.Stdout = w

	code := run(argv)

	_ = w.Close()
	out := <-captured
	_ = r.Close()
	return out, code
}

// TestMain_VersionFlag covers the short-circuit before the parser is built:
// --version must print and return without constructing any command.
//
//nolint:paralleltest // replaces os.Stdout, which is process-global
func TestMain_VersionFlag(t *testing.T) {
	// No t.Parallel(): os.Stdout is process-global.
	out, code := runMain(t, "--version")
	if strings.TrimSpace(out) == "" {
		t.Error("--version printed nothing")
	}
	if code != exitOK {
		t.Errorf("--version exit code = %d, want %d", code, exitOK)
	}
}

// TestMain_Help covers the full command-registration path and the
// flags.WroteHelp branch: every AddCommand call runs before the parser reports
// that it wrote help.
//
//nolint:paralleltest // replaces os.Stdout, which is process-global
func TestMain_Help(t *testing.T) {
	out, code := runMain(t, "--help")

	// Asking for help and getting it is the command succeeding, not failing.
	if code != exitOK {
		t.Errorf("--help exit code = %d, want %d", code, exitOK)
	}

	// Each registered command should appear in the help output. This is what
	// turns the test from "run did not panic" into a check that the command
	// set is intact.
	for _, cmd := range []string{"daemon", "validate", "config", "init", "doctor", "hash-password", "version"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("help output does not mention the %q command", cmd)
		}
	}
}

// TestMain_UnknownCommand covers the flags.Error branch, which prints help plus
// the version string and returns rather than exiting the process.
//
//nolint:paralleltest // replaces os.Stdout, which is process-global
func TestMain_UnknownCommand(t *testing.T) {
	out, code := runMain(t, "definitely-not-a-command")
	if !strings.Contains(out, "daemon") {
		t.Errorf("an unknown command should print the help listing; got %q", out)
	}
	// The status is the only part a shell can act on.
	if code != exitFailure {
		t.Errorf("unknown command exit code = %d, want %d", code, exitFailure)
	}
}

// TestMain_NoArguments covers the same error branch reached with no command at
// all, which is what a bare `ofelia` invocation does.
//
//nolint:paralleltest // replaces os.Stdout, which is process-global
func TestMain_NoArguments(t *testing.T) {
	out, code := runMain(t)
	if strings.TrimSpace(out) == "" {
		t.Error("a bare invocation printed nothing; expected the help listing")
	}
	if code != exitFailure {
		t.Errorf("bare invocation exit code = %d, want %d (no command was given)", code, exitFailure)
	}
}

// TestMain_LogLevelFromConfigFile covers the pre-parse branch that reads the
// log level out of the INI when no --log-level flag was given, which is how a
// containerised deployment configures it.
//
// The flag is passed after the subcommand on purpose: --config and --log-level
// are pre-parsed from anywhere in argv, but the top-level parser does not
// declare them, so `ofelia --config=x validate` is rejected as an unknown flag
// while `ofelia validate --config=x` works. That placement asymmetry is a
// separate defect, not something this test should bake in.
//
//nolint:paralleltest // replaces os.Stdout, which is process-global
func TestMain_LogLevelFromConfigFile(t *testing.T) {
	iniPath := filepath.Join(t.TempDir(), "ofelia.ini")
	body := "[global]\n  log-level = debug\n\n[job-local \"noop\"]\n  schedule = @every 1h\n  command = true\n"
	if err := os.WriteFile(iniPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	out, code := runMain(t, "validate", "--config="+iniPath)
	if code != exitOK {
		t.Errorf("validating a good config exited %d, want %d; output:\n%s", code, exitOK, out)
	}
}

// TestMain_ValidateMissingConfigFails pins the status a scripted caller reads:
// a config file that is not there is a failure, and `ofelia validate … || …`
// has to be able to see it.
//
//nolint:paralleltest // replaces os.Stdout, which is process-global
func TestMain_ValidateMissingConfigFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-config.ini")

	_, code := runMain(t, "validate", "--config="+missing)
	if code != exitFailure {
		t.Errorf("validating a missing config exited %d, want %d", code, exitFailure)
	}
}
