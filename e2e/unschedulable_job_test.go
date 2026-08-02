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

// TestE2E_UnschedulableJob_IsReported pins that the rejection is named at
// error level and that the reported count reflects what is actually scheduled.
func TestE2E_UnschedulableJob_IsReported(t *testing.T) {
	t.Parallel()

	configPath := writeConfig(t, `[global]
  log-level = info

[job-local "broken"]
  schedule = not-a-schedule
  command = echo hi
`)

	daemon := startDaemon(t, configPath)
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	if err := daemon.waitForLog("Scheduler started", 15*time.Second); err != nil {
		t.Fatalf("daemon never reported a started scheduler: %v\nstdout=%s", err, daemon.stdout.String())
	}

	out := daemon.stdout.String() + daemon.stderr.String()

	// The count has to describe reality. Reporting the configured total here
	// is what made a job that never runs look like a job that does.
	if !strings.Contains(out, "jobCount=0") {
		t.Errorf("expected jobCount=0 for a config whose only job was rejected, got:\n%s", out)
	}

	// And the job has to be named, at a level that is not filtered out of
	// production logging.
	if !strings.Contains(out, "job will not run") {
		t.Errorf("the rejected job was not reported as unrunnable:\n%s", out)
	}
	if !strings.Contains(out, "broken") {
		t.Errorf("the report does not name the offending job:\n%s", out)
	}
}

// TestE2E_SchedulableJob_ReportsHonestCount is the counterpart: a config whose
// jobs are all accepted must not gain a discrepancy warning, or the signal
// added above would be noise everyone learns to ignore.
func TestE2E_SchedulableJob_ReportsHonestCount(t *testing.T) {
	t.Parallel()

	configPath := writeConfig(t, `[global]
  log-level = info

[job-local "fine"]
  schedule = @every 1h
  command = echo hi
`)

	daemon := startDaemon(t, configPath)
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	if err := daemon.waitForLog("Scheduler started", 15*time.Second); err != nil {
		t.Fatalf("daemon never reported a started scheduler: %v\nstdout=%s", err, daemon.stdout.String())
	}

	out := daemon.stdout.String() + daemon.stderr.String()

	if !strings.Contains(out, "jobCount=1") {
		t.Errorf("expected jobCount=1 for one valid job, got:\n%s", out)
	}
	for _, unwanted := range []string{"job will not run", "not every configured job was scheduled"} {
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

	configPath := writeConfig(t, `[global]
  log-level = info

[job-local "good"]
  schedule = @every 1h
  command = echo fine

[job-local "bad"]
  schedule = every-other-tuesday
  command = echo nope
`)

	daemon := startDaemon(t, configPath)
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	if err := daemon.waitForLog("Scheduler started", 15*time.Second); err != nil {
		t.Fatalf("daemon never reported a started scheduler: %v\nstdout=%s", err, daemon.stdout.String())
	}

	out := daemon.stdout.String() + daemon.stderr.String()

	if !strings.Contains(out, "jobCount=1") {
		t.Errorf("expected jobCount=1 (one of two jobs scheduled), got:\n%s", out)
	}
	if !strings.Contains(out, "bad") {
		t.Errorf("the rejected job was not named:\n%s", out)
	}
	if !strings.Contains(out, "not every configured job was scheduled") {
		t.Errorf("the discrepancy between configured and scheduled was not reported:\n%s", out)
	}
}
