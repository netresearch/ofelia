// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
)

// The validator walks structs and used to skip maps, and every job lives in
// one — so no job was ever reachable by it. A job with an unparsable schedule
// passed validation, the daemon logged a warning and started, and the job
// never fired. These pin that jobs are inspected, and that what is demanded of
// them is what the runtime actually needs rather than whatever happens to lack
// a default tag.

// jobsConfig mirrors the real shape closely enough to exercise the traversal:
// job sections are maps of name to a struct that embeds its runtime job, which
// in turn embeds the fields every job shares.
type BareJobFields struct {
	Schedule string
	Command  string
}

type ExecJobFields struct {
	BareJobFields `mapstructure:",squash"`
	Container     string
}

type RunJobFields struct {
	BareJobFields `mapstructure:",squash"`
	Image         string
	Container     string
}

type jobsConfig struct {
	ExecJobs  map[string]*ExecJobFields `gcfg:"job-exec"`
	RunJobs   map[string]*RunJobFields  `gcfg:"job-run"`
	LocalJobs map[string]*BareJobFields `gcfg:"job-local"`
}

func validateJobs(t *testing.T, cfg *jobsConfig) error {
	t.Helper()
	return NewConfigValidator(cfg).Validate()
}

// TestJobValidation_CatchesUnparsableSchedule is the case from the report: the
// schedule is present, so no required-field check fires, but it cannot be
// parsed and the job would never run.
func TestJobValidation_CatchesUnparsableSchedule(t *testing.T) {
	t.Parallel()

	err := validateJobs(t, &jobsConfig{
		LocalJobs: map[string]*BareJobFields{
			"broken": {Schedule: "not-a-schedule", Command: "echo hi"},
		},
	})
	if err == nil {
		t.Fatal("an unparsable schedule was accepted")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("the error does not name the offending job: %v", err)
	}
	if !strings.Contains(err.Error(), "cron") {
		t.Errorf("the error does not say what is wrong with it: %v", err)
	}
}

func TestJobValidation_AcceptsValidSchedules(t *testing.T) {
	t.Parallel()

	for _, schedule := range []string{"@every 30s", "@daily", "0 */5 * * * *", "0 0 * * *"} {
		t.Run(schedule, func(t *testing.T) {
			t.Parallel()
			err := validateJobs(t, &jobsConfig{
				LocalJobs: map[string]*BareJobFields{"ok": {Schedule: schedule, Command: "echo hi"}},
			})
			if err != nil {
				t.Errorf("schedule %q was rejected: %v", schedule, err)
			}
		})
	}
}

// TestJobValidation_RequiresWhatTheRuntimeNeeds covers the per-kind
// requirements. Each case omits exactly one thing the job cannot run without.
func TestJobValidation_RequiresWhatTheRuntimeNeeds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     *jobsConfig
		missing string
	}{
		{
			name:    "job-local without a command",
			cfg:     &jobsConfig{LocalJobs: map[string]*BareJobFields{"j": {Schedule: "@daily"}}},
			missing: "command",
		},
		{
			name:    "job without a schedule",
			cfg:     &jobsConfig{LocalJobs: map[string]*BareJobFields{"j": {Command: "echo hi"}}},
			missing: "schedule",
		},
		{
			name: "job-exec without a container",
			cfg: &jobsConfig{ExecJobs: map[string]*ExecJobFields{
				"j": {BareJobFields: BareJobFields{Schedule: "@daily", Command: "echo hi"}},
			}},
			missing: "container",
		},
		{
			name: "job-run without an image or a container",
			cfg: &jobsConfig{RunJobs: map[string]*RunJobFields{
				"j": {BareJobFields: BareJobFields{Schedule: "@daily"}},
			}},
			missing: "image or container",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateJobs(t, tc.cfg)
			if err == nil {
				t.Fatalf("a job missing %s was accepted", tc.missing)
			}
			if !strings.Contains(err.Error(), tc.missing) {
				t.Errorf("the error does not name %q: %v", tc.missing, err)
			}
			if !strings.Contains(err.Error(), `"j"`) {
				t.Errorf("the error does not name the job: %v", err)
			}
		})
	}
}

// TestJobValidation_EitherOrIsSatisfiedByOne pins that job-run accepts an
// image or an existing container, matching core.RunJob.Validate. Demanding
// both would reject configurations that run today.
func TestJobValidation_EitherOrIsSatisfiedByOne(t *testing.T) {
	t.Parallel()

	cases := map[string]*RunJobFields{
		"image only":     {BareJobFields: BareJobFields{Schedule: "@daily"}, Image: "alpine:3.20"},
		"container only": {BareJobFields: BareJobFields{Schedule: "@daily"}, Container: "existing"},
		"both":           {BareJobFields: BareJobFields{Schedule: "@daily"}, Image: "alpine:3.20", Container: "existing"},
	}

	for name, job := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validateJobs(t, &jobsConfig{RunJobs: map[string]*RunJobFields{"j": job}}); err != nil {
				t.Errorf("a job-run with %s was rejected: %v", name, err)
			}
		})
	}
}

// TestJobValidation_DoesNotDemandOptionalFields is the guard against the
// obvious way to get this wrong. The rule that a field without a default tag
// is required is tuned to the global section; letting it loose on jobs would
// demand nearly every key a job can carry and reject configurations that work.
func TestJobValidation_DoesNotDemandOptionalFields(t *testing.T) {
	t.Parallel()

	type WideJob struct {
		BareJobFields `mapstructure:",squash"`
		Container     string
		User          string
		Network       string
		Workdir       string
		Entrypoint    string
	}
	type wideConfig struct {
		ExecJobs map[string]*WideJob `gcfg:"job-exec"`
	}

	// Everything the runtime needs is present; the rest is left empty on
	// purpose, as most real configs do.
	cfg := &wideConfig{ExecJobs: map[string]*WideJob{
		"j": {BareJobFields: BareJobFields{Schedule: "@daily", Command: "echo hi"}, Container: "c"},
	}}

	if err := NewConfigValidator(cfg).Validate(); err != nil {
		t.Errorf("optional job fields were demanded: %v", err)
	}
}
