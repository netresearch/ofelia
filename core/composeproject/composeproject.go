// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

// Package composeproject loads a Compose project and turns the scheduled
// entries of its top-level `jobs:` into job definitions Ofelia can schedule,
// following ADR-004 (docs/adr/ADR-004-compose-jobs-as-job-source.md).
//
// Loading uses compose-go, the reference implementation of the Compose
// specification. Execution is not part of this package: the jobs it returns
// are run through the `docker compose` CLI by core.ComposeJob.
//
// Only a documented subset of the trigger model is accepted. Everything else
// is rejected at load time with an error that names the job and the setting,
// because mapping a setting silently to a different behavior would be a
// compatibility bug.
package composeproject

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/netresearch/go-cron"
)

// Errors returned for job definitions outside the supported subset. Each
// returned error wraps one of them and names the project, job and setting.
var (
	ErrUnsupportedTrigger = errors.New("unsupported compose job trigger")
	ErrInvalidSpec        = errors.New("invalid compose project")
)

// revisionLength is the number of hex characters kept from the SHA-256 of the
// loaded model; enough to tell revisions apart in logs.
const revisionLength = 12

// Spec is what an operator registers: the files, profiles and environment
// that select one Compose project. It never configures containers; the
// workload stays defined in the Compose files.
type Spec struct {
	// Name is the registration name, the prefix of every job name.
	Name string
	// Files are the Compose files, merged in this order.
	Files []string
	// Profiles are activated while loading; jobs behind other profiles stay
	// unarmed, as the specification defines.
	Profiles []string
	// EnvFiles replace the default .env file for interpolation.
	EnvFiles []string
	// ProjectName overrides the project name compose-go would derive.
	ProjectName string
	// ProjectDirectory overrides the working directory of the project.
	ProjectDirectory string
}

// Project is a loaded Compose project with the jobs Ofelia schedules.
type Project struct {
	// Name is the resolved Compose project name.
	Name string
	// WorkingDir is the resolved, absolute project directory.
	WorkingDir string
	// Profiles are the active profiles the project was loaded with, from the
	// registration or, when it names none, from COMPOSE_PROFILES. A run passes
	// them explicitly, so it uses exactly the loaded selection.
	Profiles []string
	// Revision identifies the loaded model; it changes when the merged,
	// interpolated configuration changes.
	Revision string
	// Jobs are the scheduled jobs, sorted by name.
	Jobs []Job
	// Unscheduled names the active jobs without a schedule; they stay
	// manual-only and are not scheduled by Ofelia.
	Unscheduled []string
}

// Job is one scheduled Compose job.
type Job struct {
	// Name is the job name in the Compose file.
	Name string
	// Schedule is the cron expression for Ofelia's scheduler, prefixed with
	// CRON_TZ=<zone> when the job names a time zone.
	Schedule string
	// TimeZone is the zone the schedule is evaluated in: the job's timezone,
	// or a description of the scheduler's local zone when the job has none.
	TimeZone string
	// Revision identifies the job's own definition in the loaded model. It
	// changes only when this job changes, so a reload replaces only the jobs
	// whose definition changed.
	Revision string
	// MissedFires is the schedule's missed_fires policy, MissedFiresOne or
	// MissedFiresSkip; unset in the file means one.
	MissedFires string
	// Concurrency is the schedule's concurrency policy, ConcurrencyForbid or
	// ConcurrencyQueue; unset in the file means forbid.
	Concurrency string
}

// The schedule policies of the Compose specification.
const (
	MissedFiresOne    = "one"
	MissedFiresSkip   = "skip"
	ConcurrencyForbid = "forbid"
	ConcurrencyQueue  = "queue"
)

// NeedsScheduleState reports whether the job's schedule needs Ofelia's
// durable schedule state (ADR-005): a catch-up for a missed fire, or a queue
// slot.
func (j Job) NeedsScheduleState() bool {
	return j.MissedFires == MissedFiresOne || j.Concurrency == ConcurrencyQueue
}

// Loader loads a Compose project. It exists so that the cli package does not
// depend on compose-go directly and tests can substitute a fixture.
type Loader interface {
	Load(ctx context.Context, spec Spec) (*Project, error)
}

// ComposeGoLoader loads projects with compose-go, using Ofelia's own process
// environment for interpolation, as the docker CLI does.
type ComposeGoLoader struct{}

// Load reads, merges and interpolates the project, then extracts its jobs.
func (ComposeGoLoader) Load(ctx context.Context, spec Spec) (*Project, error) {
	if len(spec.Files) == 0 {
		return nil, fmt.Errorf("%w %q: no compose file configured", ErrInvalidSpec, spec.Name)
	}
	opts, err := projectOptions(spec)
	if err != nil {
		return nil, fmt.Errorf("compose project %q: %w", spec.Name, err)
	}
	options, err := cli.NewProjectOptions(spec.Files, opts...)
	if err != nil {
		return nil, fmt.Errorf("compose project %q: %w", spec.Name, err)
	}
	project, err := options.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("compose project %q: %w", spec.Name, err)
	}
	return Extract(spec.Name, project)
}

// projectOptions returns the compose-go options in the order the docker CLI
// applies them (docker/compose v5.6.0, cmd/compose/compose.go
// toProjectOptions), so the project loads as `docker compose` loads it: the
// working directory first, because the default .env is looked up in it; the
// OS environment, which wins over env files; the env files, applied twice as
// the CLI does; then the profiles, which fall back to COMPOSE_PROFILES from the
// environment or the env files; and the name, which falls back to
// COMPOSE_PROJECT_NAME. Discovery of compose files (COMPOSE_FILE, the default
// compose.yaml) is left out: a registration always names its files.
func projectOptions(spec Spec) ([]cli.ProjectOptionsFn, error) {
	opts := []cli.ProjectOptionsFn{
		cli.WithWorkingDirectory(spec.ProjectDirectory),
		cli.WithOsEnv,
	}
	if _, present := os.LookupEnv("PWD"); !present {
		pwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("working directory: %w", err)
		}
		opts = append(opts, cli.WithEnv([]string{"PWD=" + pwd}))
	}
	return append(opts,
		cli.WithEnvFiles(spec.EnvFiles...),
		cli.WithDotEnv,
		// applied twice, as the docker CLI does
		cli.WithEnvFiles(spec.EnvFiles...),
		cli.WithDotEnv,
		cli.WithDefaultProfiles(spec.Profiles...),
		cli.WithName(spec.ProjectName),
	), nil
}

// Extract applies the supported trigger subset to a loaded project. Jobs
// behind an inactive profile are ignored, active jobs without a schedule are
// listed as unscheduled, and every unsupported setting is reported. When any
// job is rejected, Extract returns all rejections joined and no project.
func Extract(registration string, project *types.Project) (*Project, error) {
	revision, err := Revision(project)
	if err != nil {
		return nil, fmt.Errorf("compose project %q: %w", registration, err)
	}
	all := project.AllJobs()
	out := &Project{Name: project.Name, WorkingDir: project.WorkingDir, Revision: revision}
	for _, p := range project.Profiles {
		// COMPOSE_PROFILES unset yields one empty entry; it selects nothing.
		if p != "" {
			out.Profiles = append(out.Profiles, p)
		}
	}
	var errs []error
	names := make([]string, 0, len(project.Jobs))
	for name := range project.Jobs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		job := project.Jobs[name]
		if job.Triggers == nil || len(job.Triggers.Schedule) == 0 {
			out.Unscheduled = append(out.Unscheduled, name)
			continue
		}
		j, jobErrs := extractJob(name, job, all, project.Services)
		for _, e := range jobErrs {
			errs = append(errs, fmt.Errorf("compose project %q, job %q: %w", registration, name, e))
		}
		if len(jobErrs) == 0 {
			out.Jobs = append(out.Jobs, j)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// extractJob checks one scheduled job against the supported subset.
func extractJob(name string, job types.JobConfig, all types.Jobs, services types.Services) (Job, []error) {
	var errs []error
	if len(job.Triggers.Schedule) > 1 {
		errs = append(errs, fmt.Errorf("%w: %d schedule entries; only one per job is supported", ErrUnsupportedTrigger,
			len(job.Triggers.Schedule)))
	}
	if blocked := manualForbidden(name, all, map[string]bool{}); blocked != "" {
		if blocked == name {
			errs = append(errs, fmt.Errorf("%w: triggers.manual: false; Ofelia runs the job through "+
				"`docker compose run`, which refuses it", ErrUnsupportedTrigger))
		} else {
			errs = append(errs, fmt.Errorf("%w: depends on job %q, which sets triggers.manual: false; "+
				"`docker compose run` refuses it", ErrUnsupportedTrigger, blocked))
		}
	}
	sched := job.Triggers.Schedule[0]
	schedule, zone, schedErrs := checkSchedule(sched)
	errs = append(errs, schedErrs...)
	revision, err := jobRevision(name, all, services)
	if err != nil {
		errs = append(errs, err)
	}
	return Job{
		Name: name, Schedule: schedule, TimeZone: zone, Revision: revision,
		MissedFires: orDefault(sched.MissedFires, MissedFiresOne),
		Concurrency: orDefault(sched.Concurrency, ConcurrencyForbid),
	}, errs
}

// orDefault returns v, or def when v is empty.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// jobRevision returns a short hash of a job's definition and of every job and
// service in its depends_on closure, since `docker compose run` starts that
// closure with it: a change to a dependency changes the job's revision.
func jobRevision(name string, all types.Jobs, services types.Services) (string, error) {
	closure := map[string]any{}
	collectClosure(name, all, services, closure)
	data, err := json.Marshal(closure) // map keys are marshaled in sorted order
	if err != nil {
		return "", fmt.Errorf("marshal job: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:revisionLength], nil
}

// collectClosure adds name and, recursively, every job and service it depends
// on to closure, keyed by kind and name.
func collectClosure(name string, all types.Jobs, services types.Services, closure map[string]any) {
	if job, ok := all[name]; ok {
		if _, seen := closure["job:"+name]; seen {
			return
		}
		closure["job:"+name] = job
		for dep := range job.DependsOn {
			collectClosure(dep, all, services, closure)
		}
		return
	}
	if svc, ok := services[name]; ok {
		if _, seen := closure["service:"+name]; seen {
			return
		}
		closure["service:"+name] = svc
		for dep := range svc.DependsOn {
			collectClosure(dep, all, services, closure)
		}
	}
}

// cronParser is the parser Ofelia's scheduler uses (see core.NewScheduler).
var cronParser = cron.FullParser()

// checkSchedule validates one schedule entry and returns the expression for
// Ofelia's scheduler and the zone it is evaluated in.
func checkSchedule(s types.ScheduleConfig) (string, string, []error) {
	var errs []error
	if n := len(strings.Fields(s.Cron)); n != 5 {
		errs = append(errs, fmt.Errorf("%w: cron %q has %d fields; a five-field expression is required",
			ErrUnsupportedTrigger, s.Cron, n))
	}
	switch s.Concurrency {
	case "", ConcurrencyForbid, ConcurrencyQueue:
	default:
		errs = append(errs, fmt.Errorf("%w: concurrency: %q", ErrUnsupportedTrigger, s.Concurrency))
	}
	switch s.MissedFires {
	case "", MissedFiresOne, MissedFiresSkip:
	default:
		errs = append(errs, fmt.Errorf("%w: missed_fires: %q", ErrUnsupportedTrigger, s.MissedFires))
	}
	expr := strings.Join(strings.Fields(s.Cron), " ")
	schedule, zone := expr, localZone()
	if s.Timezone != "" {
		schedule, zone = "CRON_TZ="+s.Timezone+" "+expr, s.Timezone
		if _, err := time.LoadLocation(s.Timezone); err != nil {
			errs = append(errs, fmt.Errorf("%w: timezone %q: %w", ErrUnsupportedTrigger, s.Timezone, err))
			return schedule, zone, errs
		}
	}
	// Parse with the scheduler's own parser, so a job that loads also
	// registers; the field count is reported above instead.
	if len(strings.Fields(s.Cron)) == 5 {
		if _, err := cronParser.Parse(schedule); err != nil {
			errs = append(errs, fmt.Errorf("%w: cron %q: %w", ErrUnsupportedTrigger, s.Cron, err))
		}
	}
	return schedule, zone, errs
}

// localZone describes the scheduler's local time zone, which the
// specification applies when a schedule names none.
func localZone() string {
	name, offset := time.Now().Zone()
	loc := time.Local.String()
	if loc == "Local" {
		return fmt.Sprintf("local (%s, UTC%+03d:%02d)", name, offset/3600, abs(offset%3600)/60)
	}
	return loc
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// manualForbidden returns the name of the first job in name's depends_on
// closure, name included, that sets triggers.manual: false, or "".
func manualForbidden(name string, all types.Jobs, seen map[string]bool) string {
	if seen[name] {
		return ""
	}
	seen[name] = true
	job, ok := all[name]
	if !ok {
		return "" // a service, not a job
	}
	if job.Triggers != nil && job.Triggers.Manual != nil && !*job.Triggers.Manual {
		return name
	}
	deps := make([]string, 0, len(job.DependsOn))
	for dep := range job.DependsOn {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	for _, dep := range deps {
		if blocked := manualForbidden(dep, all, seen); blocked != "" {
			return blocked
		}
	}
	return ""
}

// Revision returns a short hash of the loaded, normalized model.
func Revision(project *types.Project) (string, error) {
	data, err := project.MarshalYAML()
	if err != nil {
		return "", fmt.Errorf("marshal project: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:revisionLength], nil
}
