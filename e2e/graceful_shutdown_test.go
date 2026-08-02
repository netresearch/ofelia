//go:build e2e && unix
// +build e2e,unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package e2e

import (
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestE2E_GracefulShutdown_SIGTERM verifies the daemon exits cleanly when
// SIGTERM arrives mid-schedule. This exercises ShutdownManager, the
// GracefulScheduler wrapper and the main-loop `<-c.done` rendezvous —
// all real code paths you cannot hit via unit tests that stub out `os.Signal`.
func TestE2E_GracefulShutdown_SIGTERM(t *testing.T) {
	t.Parallel()

	configBody := `[global]
  log-level = info

[job-local "e2e-shutdown"]
  schedule = @every 1s
  command = sh -c "sleep 0.1"
`

	configPath := writeConfig(t, configBody)
	// The web server is enabled so the shutdown exercises more than one hook
	// priority group, which is what the completion assertion below is about.
	//
	// It is worth knowing what this test can and cannot catch: exiting without
	// waiting for the hooks is a race, and a release build loses it every time
	// (measured 0/5 completions), but this harness builds with -race, whose
	// instrumentation reliably flips the outcome (5/5). So the assertion below
	// would not have failed on the broken code here. The deterministic guard
	// for that is TestShutdownDoneClosesOnlyAfterEveryHookRan in core.
	daemon := startDaemon(t, configPath, "--enable-web", "--web-address="+reserveLoopbackAddr(t))
	defer daemon.shutdown(t, 10*time.Second) // safety net in case signal is lost

	// Let the scheduler tick at least once so there is actual work to
	// drain during shutdown (regression guard for `gracefulStop` hanging
	// on active jobs).
	if err := daemon.waitForLog(`Job \"e2e-shutdown\"`, 5*time.Second); err != nil {
		t.Fatalf("job never ran before shutdown: %v\nstdout=%s",
			err, daemon.stdout.String())
	}

	start := time.Now()
	if err := daemon.signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal daemon: %v", err)
	}

	exitErr, exited := daemon.waitExit(15 * time.Second)
	if !exited {
		t.Fatalf("daemon did not exit within 15s of SIGTERM\nstdout=%s\nstderr=%s",
			daemon.stdout.String(), daemon.stderr.String())
	}
	if exitErr != nil {
		t.Errorf("daemon exited with error after SIGTERM: %v\nstderr=%s",
			exitErr, daemon.stderr.String())
	}

	elapsed := time.Since(start)
	if elapsed > 10*time.Second {
		t.Errorf("shutdown took %s (>10s); graceful shutdown should be fast when no long-running jobs are pending",
			elapsed)
	}
	t.Logf("daemon shut down cleanly in %s", elapsed)

	// Verify the shutdown banner is emitted — proves the signal reached
	// ShutdownManager rather than the process being killed by a harness.
	//
	// The second needle used to be "graceful shutdown", which also matches
	// "Starting graceful shutdown" — logged *before* the first hook runs. So
	// this passed while the daemon exited mid-shutdown and every hook group
	// after the first was killed in flight. Only the completion line requires
	// that all of them actually finished.
	out := daemon.stdout.String()
	for _, needle := range []string{
		"Received shutdown signal",
		"Graceful shutdown completed successfully",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("expected shutdown log to mention %q, got:\nstdout=%s",
				needle, out)
		}
	}
}

// TestE2E_GracefulShutdown_SIGINT covers the same path under SIGINT (Ctrl+C)
// since the shutdown manager registers both signals independently.
func TestE2E_GracefulShutdown_SIGINT(t *testing.T) {
	t.Parallel()

	configBody := `[global]
  log-level = info

[job-local "e2e-interrupt"]
  schedule = @every 5s
  command = true
`

	configPath := writeConfig(t, configBody)
	daemon := startDaemon(t, configPath)
	defer daemon.shutdown(t, 10*time.Second)

	// Small grace period so the scheduler has registered its timers.
	time.Sleep(500 * time.Millisecond)

	if err := daemon.signal(syscall.SIGINT); err != nil {
		t.Fatalf("signal daemon: %v", err)
	}
	if _, exited := daemon.waitExit(10 * time.Second); !exited {
		t.Fatalf("daemon did not exit within 10s of SIGINT\nstdout=%s\nstderr=%s",
			daemon.stdout.String(), daemon.stderr.String())
	}

	if !strings.Contains(daemon.stdout.String(), "Received shutdown signal") {
		t.Errorf("expected shutdown banner; got:\nstdout=%s",
			daemon.stdout.String())
	}
}
