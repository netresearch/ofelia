//go:build e2e && unix
// +build e2e,unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package e2e

import (
	"strings"
	"testing"
	"time"
)

// A job the scheduler rejects — an unparsable schedule, a duplicate name —
// never runs. The daemon used to carry on with a single warning while
// reporting the job count from the *config*, so an operator saw a healthy
// daemon and a job that silently did nothing. Ofelia exists to run things on
// time; a job that never fires and says nothing is the failure it should be
// loudest about.
//
// These drive the real binary because the value that misled people was a log
// line the daemon prints at startup.

// bootAndCapture starts the daemon on the given config, waits until it reports
// a started scheduler, and returns everything it logged.
//
// The tests below differ only in the config they feed and the lines they
// expect, so the boot sequence lives here rather than once per test.
func bootAndCapture(t *testing.T, configBody string) string {
	t.Helper()

	daemon := startDaemon(t, writeConfig(t, configBody))
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	if err := daemon.waitForLog("Scheduler started", 15*time.Second); err != nil {
		t.Fatalf("daemon never reported a started scheduler: %v\nstdout=%s", err, daemon.stdout.String())
	}
	return daemon.stdout.String() + daemon.stderr.String()
}

// requireLogged fails with the captured output when an expected line is absent.
func requireLogged(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("expected the daemon to log %q, got:\n%s", want, out)
		}
	}
}

// TestE2E_UnschedulableJob_IsReported pins that the rejection is named at
// error level and that the reported count reflects what is actually scheduled.
func TestE2E_UnschedulableJob_IsReported(t *testing.T) {
	t.Parallel()

	out := bootAndCapture(t, `[global]
  log-level = info

[job-local "broken"]
  schedule = not-a-schedule
  command = echo hi
`)

	// The count has to describe reality — reporting the configured total is
	// what made a job that never runs look like a job that does — and the job
	// has to be named, at a level that is not filtered out in production.
	requireLogged(t, out, "jobCount=0", "job will not run", "broken")
}

// TestE2E_SchedulableJob_ReportsHonestCount is the counterpart: a config whose
// jobs are all accepted must not gain a discrepancy warning, or the signal
// added above would be noise everyone learns to ignore.
func TestE2E_SchedulableJob_ReportsHonestCount(t *testing.T) {
	t.Parallel()

	out := bootAndCapture(t, `[global]
  log-level = info

[job-local "fine"]
  schedule = @every 1h
  command = echo hi
`)

	requireLogged(t, out, "jobCount=1")
	for _, unwanted := range []string{"job will not run", "some jobs were not scheduled"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a healthy config produced %q:\n%s", unwanted, out)
		}
	}
}

// TestE2E_PartiallyUnschedulable_KeepsTheGoodJobs pins the deliberate choice
// not to refuse startup: one bad job should not stop the others from running.
// The rejection is reported, the rest are scheduled, and the count says how
// many that is.
func TestE2E_PartiallyUnschedulable_KeepsTheGoodJobs(t *testing.T) {
	t.Parallel()

	out := bootAndCapture(t, `[global]
  log-level = info

[job-local "good"]
  schedule = @every 1h
  command = echo fine

[job-local "bad"]
  schedule = every-other-tuesday
  command = echo nope
`)

	requireLogged(t, out, "jobCount=1", "bad", "some jobs were not scheduled")
}
