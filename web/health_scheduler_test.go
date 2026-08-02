// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// The scheduler check answered "healthy" unconditionally, with a note saying a
// real implementation would check the scheduler. So a daemon whose job had a
// mistyped schedule — a job that never fires — still served a green /health,
// which is the probe the integration docs tell operators to point a container
// healthcheck at. These pin that it now reports what it can actually see.

func newSchedulerWithJobs(t *testing.T, jobs ...core.Job) *core.Scheduler {
	t.Helper()
	s := core.NewScheduler(test.NewTestLogger())
	for _, j := range jobs {
		// Rejections are the point of several of these cases, so the error is
		// deliberately not asserted here.
		_ = s.AddJob(j)
	}
	return s
}

func localJob(name, schedule string) core.Job {
	return &core.LocalJob{BareJob: core.BareJob{Name: name, Schedule: schedule, Command: "true"}}
}

// checkSchedulerNow runs the check once and returns what it recorded, without
// waiting for the periodic loop.
func checkSchedulerNow(t *testing.T, s *core.Scheduler) HealthCheck {
	t.Helper()
	hc := NewHealthChecker(nil, s, "test")
	hc.checkScheduler()

	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.checks["scheduler"]
}

func TestCheckScheduler_HealthyWhenEveryJobIsRegistered(t *testing.T) {
	t.Parallel()

	got := checkSchedulerNow(t, newSchedulerWithJobs(t,
		localJob("a", "@every 1h"),
		localJob("b", "@daily"),
	))

	if got.Status != HealthStatusHealthy {
		t.Errorf("status = %q, want healthy (message: %s)", got.Status, got.Message)
	}
}

// TestCheckScheduler_DegradedWhenAJobWasRefused is the case the stub could not
// see: the daemon is running, but a configured job is not.
func TestCheckScheduler_DegradedWhenAJobWasRefused(t *testing.T) {
	t.Parallel()

	got := checkSchedulerNow(t, newSchedulerWithJobs(t,
		localJob("fine", "@every 1h"),
		localJob("broken", "not-a-schedule"),
	))

	if got.Status != HealthStatusDegraded {
		t.Fatalf("status = %q, want degraded", got.Status)
	}
	// The name is what turns the report into something actionable.
	if !strings.Contains(got.Message, "broken") {
		t.Errorf("message does not name the refused job: %q", got.Message)
	}
	if strings.Contains(got.Message, "fine") {
		t.Errorf("message names a job that registered fine: %q", got.Message)
	}
}

// TestCheckScheduler_RecoversWhenTheJobIsFixed covers the runtime half: a
// config reloaded with the schedule corrected has to clear the complaint, or
// the report would stay degraded until someone restarted the daemon.
func TestCheckScheduler_RecoversWhenTheJobIsFixed(t *testing.T) {
	t.Parallel()

	s := newSchedulerWithJobs(t, localJob("job", "not-a-schedule"))
	if got := checkSchedulerNow(t, s); got.Status != HealthStatusDegraded {
		t.Fatalf("status before the fix = %q, want degraded", got.Status)
	}

	if err := s.AddJob(localJob("job", "@every 1h")); err != nil {
		t.Fatalf("re-adding the corrected job: %v", err)
	}

	if got := checkSchedulerNow(t, s); got.Status != HealthStatusHealthy {
		t.Errorf("status after the fix = %q, want healthy (message: %s)", got.Status, got.Message)
	}
}

// TestCheckScheduler_NoSchedulerIsNotHealth pins that an unwired checker says
// so rather than claiming health it has not established — the exact habit that
// made the old stub useless.
func TestCheckScheduler_NoSchedulerIsNotHealth(t *testing.T) {
	t.Parallel()

	if got := checkSchedulerNow(t, nil); got.Status == HealthStatusHealthy {
		t.Errorf("a checker with no scheduler reported healthy: %q", got.Message)
	}
}
