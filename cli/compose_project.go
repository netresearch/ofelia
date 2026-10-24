// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	ini "gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/composeproject"
	"github.com/netresearch/ofelia/middlewares"
)

// composeProjectSection is the INI section prefix that registers a Compose
// project, e.g. [compose-project "shop"].
const composeProjectSection = "compose-project"

// composeProjectLoadTimeout bounds loading one Compose project: reading,
// merging and interpolating its files.
const composeProjectLoadTimeout = 30 * time.Second

// ErrComposeJobNameTaken is returned when a job loaded from a registered
// Compose project would replace a [job-compose] section of the same name.
var ErrComposeJobNameTaken = errors.New("compose project job name already used by a [job-compose] section")

// ErrComposeProjectRejected reports that at least one Compose project was
// rejected; `ofelia validate` returns it, the daemon logs and continues.
var ErrComposeProjectRejected = errors.New("compose projects rejected")

// composeProjectLoader loads registered Compose projects. A variable so tests
// can substitute a fixture.
var composeProjectLoader composeproject.Loader = composeproject.ComposeGoLoader{}

// composeCLICheck verifies that the docker compose CLI can run Compose jobs.
// A variable so unit tests do not depend on the CLI installed on the host.
var composeCLICheck = core.CheckComposeJobsSupport

// ComposeProjectConfig registers a Compose project from a
// [compose-project "name"] INI section (see ADR-004). It selects the project —
// files, profiles, env files, name and directory — and configures no
// container: the workload stays defined in the Compose files. Every job of the
// project with a schedule becomes an Ofelia job named "<name>.<job>". The
// section is INI-only; container labels cannot register a project.
type ComposeProjectConfig struct {
	// Files are the Compose files, merged in the order given (repeat the key).
	Files []string `mapstructure:"file,"`
	// Profiles are activated while loading and when running a job; jobs behind
	// other profiles are not scheduled.
	Profiles []string `mapstructure:"profile,"`
	// EnvFiles replace the default .env file for interpolation.
	EnvFiles []string `mapstructure:"env-file,"`
	// ProjectName overrides the project name Compose would derive.
	ProjectName string `mapstructure:"project-name"`
	// ProjectDirectory overrides the project's working directory.
	ProjectDirectory string `mapstructure:"project-directory"`
}

// decodeComposeProjectSection decodes one [compose-project "name"] section.
func decodeComposeProjectSection(section *ini.Section, c *Config, unknownJobs *[]jobUnknownKeys) error {
	name := parseJobName(strings.TrimSpace(section.Name()), composeProjectSection)
	p := &ComposeProjectConfig{}
	result, err := decodeWithMetadata(sectionToMap(section), p)
	if err != nil {
		return fmt.Errorf("failed to decode compose project %q configuration: %w", name, err)
	}
	if len(result.UnusedKeys) > 0 && unknownJobs != nil {
		*unknownJobs = append(*unknownJobs, jobUnknownKeys{
			JobType: composeProjectSection, JobName: name, UnknownKeys: result.UnusedKeys,
		})
	}
	c.ComposeProjects[name] = p
	return nil
}

// rejectedRevisionPrefix marks a project that was rejected. Its recorded
// revision is the prefix plus the error, so a later poll reloads only when the
// error changes or the project loads.
const rejectedRevisionPrefix = "rejected: "

// expandComposeProjects loads every registered Compose project and adds one
// ComposeJobConfig per scheduled job to c.ComposeJobs. A project that fails to
// load, or that has a job outside the supported trigger subset, is rejected
// with an error log and schedules no job; the rest of the configuration,
// including other projects, is unaffected — the same way an invalid job is
// rejected by registerJob. A missing or too old Compose CLI rejects every
// project. A scheduled job either runs as specified or not at all.
func (c *Config) expandComposeProjects() {
	names := make([]string, 0, len(c.ComposeProjects))
	for name := range c.ComposeProjects {
		names = append(names, name)
	}
	sort.Strings(names)
	c.composeFiles = nil
	c.composeRevisions = nil
	c.composeRejected = nil
	if len(names) == 0 {
		return
	}
	c.composeRevisions = make(map[string]string, len(names))
	c.composeRejected = make(map[string]bool)
	ctx, cancel := context.WithTimeout(context.Background(), composeProjectLoadTimeout)
	defer cancel()
	if err := composeCLICheck(ctx); err != nil {
		for _, name := range names {
			c.rejectComposeProject(name, err)
		}
		return
	}
	for _, name := range names {
		if err := c.expandComposeProject(name, c.ComposeProjects[name]); err != nil {
			c.rejectComposeProject(name, err)
		}
	}
}

// rejectComposeProject logs why a project schedules no job and records the
// error as its revision.
func (c *Config) rejectComposeProject(name string, err error) {
	c.composeRejected[name] = true
	// A project that loaded and was rejected afterwards keeps its loaded
	// revision, which is what currentComposeRevision compares against.
	if _, loaded := c.composeRevisions[name]; !loaded {
		c.composeRevisions[name] = rejectedRevisionPrefix + err.Error()
	}
	c.logger.Error(fmt.Sprintf("compose project %q rejected, its jobs are not scheduled: %v", name, err))
}

// keepLastLoadedComposeJobs carries the jobs of a project that loaded before
// and is rejected by this reload into parsed, unchanged, so a broken edit to a
// Compose file does not unschedule jobs that ran correctly. The rejection is
// logged when the project is expanded; the jobs are replaced once the project
// loads again. At startup there is nothing to keep.
func (c *Config) keepLastLoadedComposeJobs(parsed *Config) {
	// Decided by the jobs that exist, not by an earlier rejection: a second
	// reload while the project is still rejected must keep them too.
	for name := range parsed.composeRejected {
		kept := 0
		for jobName, j := range c.ComposeJobs {
			if j.composeRegistration == name && parsed.ComposeJobs[jobName] == nil {
				parsed.ComposeJobs[jobName] = j
				kept++
			}
		}
		if kept > 0 {
			c.logger.Warn(fmt.Sprintf("compose project %q: keeping its %d last loaded job(s) until it loads again", name, kept))
		}
	}
}

// rejectedComposeProjects returns the names of the projects the last load
// rejected, sorted.
func (c *Config) rejectedComposeProjects() []string {
	names := make([]string, 0, len(c.composeRejected))
	for name := range c.composeRejected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// composeSpec turns a registration into the loader's input.
func composeSpec(name string, p *ComposeProjectConfig) composeproject.Spec {
	return composeproject.Spec{
		Name: name, Files: p.Files, Profiles: p.Profiles, EnvFiles: p.EnvFiles,
		ProjectName: p.ProjectName, ProjectDirectory: p.ProjectDirectory,
	}
}

// composeProjectsChanged reloads every registered project and reports whether
// a resolved model differs from the one the configuration was built from. It
// catches what watching file times cannot: a change in a default .env, in a
// file pulled in through include: or extends at any depth, or in an
// interpolated value. A project that starts or stops loading, or whose load
// error changes, also counts as changed. A project rejected for the Compose CLI
// counts as changed once the CLI check passes.
func (c *Config) composeProjectsChanged() bool {
	if len(c.composeRevisions) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), composeProjectLoadTimeout)
	defer cancel()
	cliErr := composeCLICheck(ctx)
	for name, p := range c.ComposeProjects {
		if c.currentComposeRevision(ctx, name, p, cliErr) != c.composeRevisions[name] {
			return true
		}
	}
	return false
}

// currentComposeRevision returns the revision expandComposeProject would
// record for the project now: the loaded revision, or the rejection marker.
// It covers load errors only; a job rejected later (a name collision) keeps
// the loaded revision, because a reload would reject it the same way.
func (c *Config) currentComposeRevision(ctx context.Context, name string, p *ComposeProjectConfig, cliErr error) string {
	if cliErr != nil {
		return rejectedRevisionPrefix + cliErr.Error()
	}
	project, err := composeProjectLoader.Load(ctx, composeSpec(name, p))
	if err != nil {
		return rejectedRevisionPrefix + fmt.Errorf("load compose project %q: %w", name, err).Error()
	}
	return project.Revision
}

func (c *Config) expandComposeProject(name string, p *ComposeProjectConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), composeProjectLoadTimeout)
	defer cancel()
	project, err := composeProjectLoader.Load(ctx, composeSpec(name, p))
	if err != nil {
		return fmt.Errorf("load compose project %q: %w", name, err)
	}
	c.composeFiles = append(c.composeFiles, p.Files...)
	c.composeFiles = append(c.composeFiles, p.EnvFiles...)
	c.composeRevisions[name] = project.Revision
	// Check every name first, so a rejected project schedules none of its jobs.
	for _, job := range project.Jobs {
		if jobName := name + "." + job.Name; c.ComposeJobs[jobName] != nil {
			return fmt.Errorf("%w: %q", ErrComposeJobNameTaken, jobName)
		}
	}
	for _, job := range project.Jobs {
		jobName := name + "." + job.Name
		c.ComposeJobs[jobName] = &ComposeJobConfig{
			composeRegistration: name,
			ComposeJob: core.ComposeJob{
				BareJob: core.BareJob{Name: jobName, Schedule: job.Schedule},
				Service: job.Name,
				// The resolved name, profiles and directory, so a run uses the
				// context the project was loaded with, including values that
				// came from COMPOSE_PROJECT_NAME or COMPOSE_PROFILES.
				Project: core.ComposeProjectContext{
					Files:            p.Files,
					Profiles:         project.Profiles,
					EnvFiles:         p.EnvFiles,
					ProjectName:      project.Name,
					ProjectDirectory: project.WorkingDir,
					Revision:         job.Revision,
					TimeZone:         job.TimeZone,
				},
			},
			// concurrency: forbid, the only accepted policy, is the existing
			// per-job overlap prevention.
			OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
			JobSource:     JobSourceINI,
		}
		c.logger.Info(fmt.Sprintf("compose project %q (revision %s): scheduling job %q as %q (%s, time zone %s, job revision %s)",
			name, project.Revision, job.Name, jobName, job.Schedule, job.TimeZone, job.Revision))
	}
	if len(project.Unscheduled) > 0 {
		c.logger.Info(fmt.Sprintf("compose project %q: jobs without a schedule stay manual-only: %s",
			name, strings.Join(project.Unscheduled, ", ")))
	}
	return nil
}
