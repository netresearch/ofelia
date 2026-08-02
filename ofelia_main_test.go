// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"strings"
	"testing"
)

// main() is reachable from a test because every one of its exits is a plain
// return: the flag-error path was deliberately changed to return instead of
// calling os.Exit(1) (see the comment at its final return). Exercising it here
// covers the command wiring — a command dropped from the parser, or a
// constructor that starts panicking, fails these tests instead of only showing
// up when a user runs the binary.
//
// These tests never pass a real command such as `daemon`, which would start a
// scheduler. Only paths that parse and return are used.

// runMain invokes main() with the given argv while redirecting stdout, so the
// parser's help output does not drown the test log. It restores both os.Args
// and os.Stdout before returning and reports what main() printed.
func runMain(t *testing.T, argv ...string) string {
	t.Helper()

	origArgs, origStdout := os.Args, os.Stdout
	t.Cleanup(func() {
		os.Args = origArgs
		os.Stdout = origStdout
	})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}

	// A pipe's buffer is finite and the help text is long, so drain it
	// concurrently; writing more than the buffer holds would otherwise block
	// main() forever.
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

	os.Args = append([]string{"ofelia"}, argv...)
	os.Stdout = w

	main()

	_ = w.Close()
	out := <-captured
	_ = r.Close()
	return out
}

// TestMain_VersionFlag covers the short-circuit before the parser is built:
// --version must print and return without constructing any command.
//
//nolint:paralleltest // mutates os.Args and os.Stdout, which are process-global
func TestMain_VersionFlag(t *testing.T) {
	// No t.Parallel(): os.Args and os.Stdout are process-global.
	out := runMain(t, "--version")
	if strings.TrimSpace(out) == "" {
		t.Error("--version printed nothing")
	}
}

// TestMain_Help covers the full command-registration path and the
// flags.WroteHelp branch: every AddCommand call runs before the parser reports
// that it wrote help.
//
//nolint:paralleltest // mutates os.Args and os.Stdout, which are process-global
func TestMain_Help(t *testing.T) {
	out := runMain(t, "--help")

	// Each registered command should appear in the help output. This is what
	// turns the test from "main did not panic" into a check that the command
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
//nolint:paralleltest // mutates os.Args and os.Stdout, which are process-global
func TestMain_UnknownCommand(t *testing.T) {
	out := runMain(t, "definitely-not-a-command")
	if !strings.Contains(out, "daemon") {
		t.Errorf("an unknown command should print the help listing; got %q", out)
	}
}

// TestMain_NoArguments covers the same error branch reached with no command at
// all, which is what a bare `ofelia` invocation does.
//
//nolint:paralleltest // mutates os.Args and os.Stdout, which are process-global
func TestMain_NoArguments(t *testing.T) {
	out := runMain(t)
	if strings.TrimSpace(out) == "" {
		t.Error("a bare invocation printed nothing; expected the help listing")
	}
}

// TestMain_LogLevelFlagIsPreParsed pins that --log-level is consumed by the
// pre-parser before the real parser runs, so an early log level applies to
// everything the commands log. It reaches the same help path, but with the
// pre-parse branch populated.
//
//nolint:paralleltest // mutates os.Args and os.Stdout, which are process-global
func TestMain_LogLevelFlagIsPreParsed(t *testing.T) {
	out := runMain(t, "--log-level", "debug", "--help")
	if !strings.Contains(out, "daemon") {
		t.Errorf("expected help output with a log level set; got %q", out)
	}
}

// TestMain_ConfigFlagMissingFileIsTolerated pins that a --config path which
// does not exist does not stop startup: the INI load is best-effort and only
// supplies a log level.
//
//nolint:paralleltest // mutates os.Args and os.Stdout, which are process-global
func TestMain_ConfigFlagMissingFileIsTolerated(t *testing.T) {
	missing := t.TempDir() + "/no-such-config.ini"
	out := runMain(t, "--config", missing, "--help")
	if !strings.Contains(out, "daemon") {
		t.Errorf("a missing --config file should be tolerated; got %q", out)
	}
}
