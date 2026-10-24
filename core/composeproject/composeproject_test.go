// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package composeproject

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/go-cron"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func load(t *testing.T, spec Spec) (*Project, error) {
	t.Helper()
	for i, f := range spec.Files {
		spec.Files[i] = filepath.Join("testdata", f)
	}
	for i, f := range spec.EnvFiles {
		spec.EnvFiles[i] = filepath.Join("testdata", f)
	}
	return ComposeGoLoader{}.Load(context.Background(), spec)
}

func jobByName(t *testing.T, p *Project, name string) Job {
	t.Helper()
	for _, j := range p.Jobs {
		if j.Name == name {
			return j
		}
	}
	t.Fatalf("job %q not scheduled; got %+v", name, p.Jobs)
	return Job{}
}

func TestLoad_AcceptedJobs(t *testing.T) {
	p, err := load(t, Spec{Name: "shop", Files: []string{"accepted.yaml"}})
	require.NoError(t, err)

	assert.Equal(t, "shop", p.Name)
	assert.Len(t, p.Revision, revisionLength)
	require.Len(t, p.Jobs, 2, "backup and cleanup are scheduled; gated sits behind an inactive profile")

	backup := jobByName(t, p, "backup")
	assert.Equal(t, "CRON_TZ=Europe/Berlin 0 2 * * *", backup.Schedule)
	assert.Equal(t, "Europe/Berlin", backup.TimeZone)

	cleanup := jobByName(t, p, "cleanup")
	assert.Equal(t, "30 3 * * 1", cleanup.Schedule, "no timezone: the scheduler's local zone applies")
	assert.NotEmpty(t, cleanup.TimeZone, "the local zone is resolved and recorded")

	assert.Equal(t, []string{"report"}, p.Unscheduled, "a job without a schedule stays manual-only")
}

func TestLoad_SchedulesParseWithOfeliasCronParser(t *testing.T) {
	p, err := load(t, Spec{Name: "shop", Files: []string{"accepted.yaml"}})
	require.NoError(t, err)
	for _, j := range p.Jobs {
		_, err := cron.FullParser().Parse(j.Schedule)
		require.NoError(t, err, "schedule %q of job %q", j.Schedule, j.Name)
	}
}

func TestLoad_ProfileArmsGatedJob(t *testing.T) {
	p, err := load(t, Spec{Name: "shop", Files: []string{"accepted.yaml"}, Profiles: []string{"extra"}})
	require.NoError(t, err)
	assert.Equal(t, "0 4 * * *", jobByName(t, p, "gated").Schedule)
}

func TestLoad_MergesFilesInOrder(t *testing.T) {
	p, err := load(t, Spec{Name: "m", Files: []string{"base.yaml", "override.yaml"}})
	require.NoError(t, err)
	assert.Equal(t, "merged-override", p.Name, "the later file wins for a scalar")
	assert.Equal(t, "0 1 * * *", jobByName(t, p, "sync").Schedule, "the base file's schedule is kept")

	base, err := load(t, Spec{Name: "m", Files: []string{"base.yaml"}})
	require.NoError(t, err)
	assert.NotEqual(t, base.Revision, p.Revision, "a different merged model has a different revision")
}

func TestLoad_InterpolatesFromEnvFile(t *testing.T) {
	p, err := load(t, Spec{Name: "i", Files: []string{"interpolated.yaml"}, EnvFiles: []string{"interp.env"}})
	require.NoError(t, err)
	assert.Equal(t, "45 5 * * *", jobByName(t, p, "sync").Schedule)
}

func TestLoad_ProjectNameOverride(t *testing.T) {
	p, err := load(t, Spec{Name: "shop", Files: []string{"accepted.yaml"}, ProjectName: "shop-prod"})
	require.NoError(t, err)
	assert.Equal(t, "shop-prod", p.Name)
}

func TestLoad_NoFiles(t *testing.T) {
	_, err := ComposeGoLoader{}.Load(context.Background(), Spec{Name: "empty"})
	require.ErrorIs(t, err, ErrInvalidSpec)
}

// TestLoad_RejectsUnsupportedTriggers checks each rejected shape separately:
// the error names the project, the job and the setting.
func TestLoad_RejectsUnsupportedTriggers(t *testing.T) {
	p, err := load(t, Spec{Name: "bad", Files: []string{"rejected.yaml"}})
	require.Error(t, err)
	assert.Nil(t, p, "a project with a rejected job is not loaded at all")
	require.ErrorIs(t, err, ErrUnsupportedTrigger)

	cases := map[string]string{
		"plain":         "missed_fires is unset and defaults to one",
		"missed_one":    "missed_fires: one is not supported",
		"queued":        "concurrency: queue is not supported",
		"twice":         "2 schedule entries",
		"no_manual":     "triggers.manual: false",
		"dep_no_manual": `depends on job "locked", which sets triggers.manual: false`,
		"six_fields":    "has 6 fields; a five-field expression is required",
		"bad_zone":      `timezone "Mars/Olympus"`,
		"bad_value":     `cron "61 2 * * *"`,
	}
	msgs := joinedMessages(err)
	for job, want := range cases {
		prefix := `compose project "bad", job "` + job + `": `
		assert.True(t, containsLine(msgs, prefix, want), "job %s: no error %q in:\n%s", job, want, strings.Join(msgs, "\n"))
	}
	for _, m := range msgs {
		assert.False(t, strings.HasPrefix(m, `compose project "bad", job "locked": `),
			"locked has no schedule and is only reported through its dependent: %s", m)
	}
}

func joinedMessages(err error) []string {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		errs := joined.Unwrap()
		out := make([]string, 0, len(errs))
		for _, e := range errs {
			out = append(out, e.Error())
		}
		return out
	}
	return []string{err.Error()}
}

func containsLine(msgs []string, prefix, want string) bool {
	for _, m := range msgs {
		if strings.HasPrefix(m, prefix) && strings.Contains(m, want) {
			return true
		}
	}
	return false
}

func TestLoad_TimeZoneIsEvaluated(t *testing.T) {
	p, err := load(t, Spec{Name: "shop", Files: []string{"accepted.yaml"}})
	require.NoError(t, err)
	sched, err := cron.FullParser().Parse(jobByName(t, p, "backup").Schedule)
	require.NoError(t, err)

	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	// A date without a DST change: 02:00 occurs once.
	next := sched.Next(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	assert.Equal(t, time.Date(2026, 11, 11, 2, 0, 0, 0, berlin).UTC(), next.UTC(),
		"0 2 * * * in Europe/Berlin fires at 02:00 Berlin time")
}

// TestLoad_RevisionCoversDependencyClosure changes a service the job depends
// on, then one it does not: `docker compose run` starts the closure, so only
// the first may change the job's revision.
func TestLoad_RevisionCoversDependencyClosure(t *testing.T) {
	const tmpl = `name: dep
services:
  db:
    image: %s
  web:
    image: %s
jobs:
  prep:
    image: alpine:3.22
    depends_on: [db]
    triggers:
      manual: true
  backup:
    image: alpine:3.22
    depends_on: [prep]
    triggers:
      schedule:
        - cron: "0 2 * * *"
          missed_fires: skip
`
	revision := func(dbImage, webImage string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "compose.yaml")
		require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(tmpl, dbImage, webImage)), 0o600))
		p, err := ComposeGoLoader{}.Load(context.Background(), Spec{Name: "dep", Files: []string{path}})
		require.NoError(t, err)
		return jobByName(t, p, "backup").Revision
	}
	base := revision("postgres:16", "nginx:1")
	assert.NotEqual(t, base, revision("postgres:17", "nginx:1"),
		"a change to a service in the depends_on closure (through a job) changes the revision")
	assert.Equal(t, base, revision("postgres:16", "nginx:2"),
		"a change to a service outside the closure leaves the revision unchanged")
}

func TestLoad_RevisionIsPerJob(t *testing.T) {
	base, err := load(t, Spec{Name: "m", Files: []string{"base.yaml"}})
	require.NoError(t, err)
	merged, err := load(t, Spec{Name: "m", Files: []string{"base.yaml", "override.yaml"}})
	require.NoError(t, err)
	assert.NotEqual(t, jobByName(t, base, "sync").Revision, jobByName(t, merged, "sync").Revision,
		"a changed job definition changes the job revision")

	a, err := load(t, Spec{Name: "shop", Files: []string{"accepted.yaml"}})
	require.NoError(t, err)
	b, err := load(t, Spec{Name: "shop", Files: []string{"accepted.yaml"}, Profiles: []string{"extra"}})
	require.NoError(t, err)
	assert.Equal(t, jobByName(t, a, "backup").Revision, jobByName(t, b, "backup").Revision,
		"arming another job leaves this job's revision unchanged")
	assert.NotEqual(t, a.Revision, b.Revision, "the project revision covers the whole model")
}
