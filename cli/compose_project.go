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

// composeProjectLoader loads registered Compose projects. A variable so tests
// can substitute a fixture.
var composeProjectLoader composeproject.Loader = composeproject.ComposeGoLoader{}

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

// expandComposeProjects loads every registered Compose project and adds one
// ComposeJobConfig per scheduled job to c.ComposeJobs. A project that fails to
// load, or that has a job outside the supported trigger subset, fails the whole
// configuration: it either runs as specified or not at all.
func (c *Config) expandComposeProjects() error {
	names := make([]string, 0, len(c.ComposeProjects))
	for name := range c.ComposeProjects {
		names = append(names, name)
	}
	sort.Strings(names)
	c.composeFiles = nil
	for _, name := range names {
		if err := c.expandComposeProject(name, c.ComposeProjects[name]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) expandComposeProject(name string, p *ComposeProjectConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), composeProjectLoadTimeout)
	defer cancel()
	spec := composeproject.Spec{
		Name: name, Files: p.Files, Profiles: p.Profiles, EnvFiles: p.EnvFiles,
		ProjectName: p.ProjectName, ProjectDirectory: p.ProjectDirectory,
	}
	project, err := composeProjectLoader.Load(ctx, spec)
	if err != nil {
		return fmt.Errorf("load compose project %q: %w", name, err)
	}
	c.composeFiles = append(c.composeFiles, p.Files...)
	c.composeFiles = append(c.composeFiles, p.EnvFiles...)
	for _, job := range project.Jobs {
		jobName := name + "." + job.Name
		if _, taken := c.ComposeJobs[jobName]; taken {
			return fmt.Errorf("%w: %q", ErrComposeJobNameTaken, jobName)
		}
		c.ComposeJobs[jobName] = &ComposeJobConfig{
			ComposeJob: core.ComposeJob{
				BareJob: core.BareJob{Name: jobName, Schedule: job.Schedule},
				Service: job.Name,
				Project: core.ComposeProjectContext{
					Files:            p.Files,
					Profiles:         p.Profiles,
					EnvFiles:         p.EnvFiles,
					ProjectName:      project.Name,
					ProjectDirectory: p.ProjectDirectory,
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
