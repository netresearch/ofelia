//go:build e2e && unix
// +build e2e,unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestE2E_LocalJob_RunsOnSchedule verifies the full pipeline for a local
// (non-Docker) job: the binary boots, parses the INI, schedules the job,
// executes it on the configured cadence and — crucially — the job's
// side-effects (file writes, stdout) are visible outside ofelia.
//
// Why a file-based side effect: the local job's stdout is streamed through
// ofelia's logger to the parent stdout. Checking that captured stdout already
// proves the Run hook fired, but checking a file written *by the child shell*
// additionally proves the spawned process actually ran with the right env.
func TestE2E_LocalJob_RunsOnSchedule(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	markerFile := filepath.Join(workDir, "marker.log")

	// Ofelia parses `command =` with shell-like arg splitting (gobs/args),
	// then passes the resulting []string directly to the child. So we keep
	// the inner double-quotes around the `sh -c` script and write a simple
	// unquoted path — double-quoting it a second time would make the shell
	// see literal `"/tmp/.../marker.log"` and hit `end of file unexpected`.
	configBody := fmt.Sprintf(`[global]
  log-level = info

[job-local "e2e-echo"]
  schedule = @every 1s
  command = sh -c "echo OFELIA_E2E_LOCAL_MARKER >> %s"
`, markerFile)

	configPath := writeConfig(t, configBody)
	daemon := startDaemon(t, configPath)
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	// Allow at least 3 scheduling ticks (@every 1s × 3 + slack).
	// slog's TextHandler escapes quotes, so the literal text in stdout is
	// `[Job \"e2e-echo\" (...)]`. Match on the unambiguous job-id prefix
	// instead to side-step handler-specific escaping.
	if err := daemon.waitForLog(`Job \"e2e-echo\"`, 8*time.Second); err != nil {
		t.Fatalf("no execution log seen: %v\nstdout=%s",
			err, daemon.stdout.String())
	}

	// Poll for the marker to show up in the file rather than using a fixed
	// sleep — loaded CI runners can exhibit multi-second fs-sync latency.
	var (
		data        []byte
		readErr     error
		markerLines int
	)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, readErr = os.ReadFile(markerFile)
		if readErr == nil {
			markerLines = strings.Count(string(data), "OFELIA_E2E_LOCAL_MARKER")
			if markerLines > 0 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if readErr != nil {
		t.Fatalf("read marker file %s: %v", markerFile, readErr)
	}
	if markerLines == 0 {
		t.Fatalf("marker not found in file; job likely did not execute.\n"+
			"file contents: %q\nstdout=%s",
			string(data), daemon.stdout.String())
	}
	t.Logf("job ran %d time(s); marker file has %d line(s)",
		daemon.countLogOccurrences(`Job \"e2e-echo\"`), markerLines)
}

// TestE2E_LocalJob_RunOnStartup verifies the `run-on-startup = true` flag:
// the job fires once immediately at boot, not only on its cron schedule. We
// use a far-future cron so the schedule alone would never fire within the
// test window; if the marker appears, only the startup-trigger could have
// caused it.
func TestE2E_LocalJob_RunOnStartup(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	markerFile := filepath.Join(workDir, "startup.log")

	configBody := fmt.Sprintf(`[global]
  log-level = info

[job-local "e2e-startup"]
  schedule = 0 0 1 1 *
  run-on-startup = true
  command = sh -c "echo OFELIA_E2E_STARTUP_MARKER > %s"
`, markerFile)

	configPath := writeConfig(t, configBody)
	daemon := startDaemon(t, configPath)
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	// Wait up to 10s for the one-shot run.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(markerFile); err == nil &&
			strings.Contains(string(data), "OFELIA_E2E_STARTUP_MARKER") {
			return // success
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("run-on-startup did not fire within 10s\nstdout=%s",
		daemon.stdout.String())
}

// TestE2E_LocalJob_SurvivesMultipleExecutions asserts scheduler stability:
// a fast-cadence job must keep firing across many ticks without the
// scheduler hanging, panicking or losing entries. Regression guard for
// scheduling drift / goroutine leaks.
func TestE2E_LocalJob_SurvivesMultipleExecutions(t *testing.T) {
	t.Parallel()

	// Ofelia's scheduler enforces a 1s minimum interval via the cron library
	// (see core/scheduler_mutation_test.go). Sub-second cadences are rejected.
	configBody := `[global]
  log-level = info

[job-local "e2e-fast"]
  schedule = @every 1s
  command = true
`

	configPath := writeConfig(t, configBody)
	daemon := startDaemon(t, configPath)
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	// 6s / 1s ≈ 6 ticks. Require at least 3 to allow for startup latency
	// and scheduler jitter on loaded CI runners.
	time.Sleep(6 * time.Second)

	// Only count *Finished* entries so partial "Started" lines don't inflate.
	runs := daemon.countLogOccurrences(`Finished in`)
	if runs < 3 {
		t.Errorf("expected at least 3 finished runs in 6s, got %d.\nstdout=%s",
			runs, daemon.stdout.String())
	}

	// Sanity-check process is still healthy.
	if daemon.exited() {
		t.Fatalf("daemon exited unexpectedly during fast-cadence run\nstdout=%s\nstderr=%s",
			daemon.stdout.String(), daemon.stderr.String())
	}

	// Clean shutdown confirms the scheduler is not wedged.
	_ = daemon.signal(syscall.SIGTERM)
	if _, exited := daemon.waitExit(10 * time.Second); !exited {
		t.Errorf("daemon did not exit within 10s after SIGTERM following %d runs",
			runs)
	}
}
