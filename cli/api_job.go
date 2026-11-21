// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"errors"
	"fmt"

	"github.com/gobs/args"

	cfgvalidator "github.com/netresearch/ofelia/config"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

var (
	errRunJobNeedsDocker  = errors.New("docker provider unavailable for run job")
	errExecJobNeedsDocker = errors.New("docker provider unavailable for exec job")
	errNoConfigForRestore = errors.New("no configuration to assemble the job against")
	errUnknownJobType     = errors.New("unknown job type")
)

// BuildAPIJob turns a job definition from the web API or the state file
// into a job, through the same job assembly as a job from the INI file or
// container labels. The web server calls it for create and update, the
// daemon for every job it restores from the state file.
//
// The definition is checked strictly: a field the job cannot honor is an
// error, where a config file only gets a warning. The checks are the
// command-injection checks of config.CommandValidator for compose and
// local jobs, a parsable maxRuntime, and no maxRuntime for a compose job
// in exec mode. Run and exec jobs need a Docker provider. Job assembly then
// runs the job's own Validate, as for every other job source.
//
// It returns the core job, so the scheduler, GET /api/jobs and the state
// file see the same job type as before. The job carries no middlewares of
// its own; the scheduler's global ones apply when it is added.
func (c *Config) BuildAPIJob(name string, def persist.Job) (core.Job, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	provider := c.dockerProvider()
	switch def.Type {
	case persist.JobTypeRun:
		if provider == nil {
			return nil, errRunJobNeedsDocker
		}
		maxRuntime, err := core.ParseMaxRuntime(def.MaxRuntime)
		if err != nil {
			return nil, fmt.Errorf("invalid maxRuntime: %w", err)
		}
		j := &RunJobConfig{}
		j.Schedule, j.Command = def.Schedule, def.Command
		j.Image, j.Container = def.Image, def.Container
		j.User = def.User
		j.MaxRuntime = maxRuntime
		if err := c.assembleJob(name, j); err != nil {
			return nil, err
		}
		return &j.RunJob, nil
	case persist.JobTypeExec:
		if provider == nil {
			return nil, errExecJobNeedsDocker
		}
		j := &ExecJobConfig{}
		j.Schedule, j.Command = def.Schedule, def.Command
		j.Container, j.User = def.Container, def.User
		if err := c.assembleJob(name, j); err != nil {
			return nil, err
		}
		return &j.ExecJob, nil
	case persist.JobTypeCompose:
		j, err := composeJobFromDefinition(def)
		if err != nil {
			return nil, err
		}
		if err := c.assembleJob(name, j); err != nil {
			return nil, err
		}
		return &j.ComposeJob, nil
	case persist.JobTypeLocal, "":
		if err := checkCommandArgs(def.Command); err != nil {
			return nil, err
		}
		j := &LocalJobConfig{}
		j.Schedule, j.Command = def.Schedule, def.Command
		if err := c.assembleJob(name, j); err != nil {
			return nil, err
		}
		return &j.LocalJob, nil
	default:
		return nil, fmt.Errorf("%w %q", errUnknownJobType, def.Type)
	}
}

// composeJobFromDefinition checks a compose job definition and returns it
// unassembled. The max-runtime check runs here, before assembly, because
// assembly drops an exec-mode max-runtime with a warning instead.
func composeJobFromDefinition(def persist.Job) (*ComposeJobConfig, error) {
	validator := cfgvalidator.NewCommandValidator()
	if def.File != "" {
		if err := validator.ValidateFilePath(def.File); err != nil {
			return nil, fmt.Errorf("invalid compose file path: %w", err)
		}
	}
	if err := validator.ValidateServiceName(def.Service); err != nil {
		return nil, fmt.Errorf("invalid service name: %w", err)
	}
	if err := checkCommandArgs(def.Command); err != nil {
		return nil, err
	}
	maxRuntime, err := core.ParseMaxRuntime(def.MaxRuntime)
	if err != nil {
		return nil, fmt.Errorf("invalid maxRuntime: %w", err)
	}
	j := &ComposeJobConfig{}
	j.Schedule, j.Command = def.Schedule, def.Command
	j.File, j.Service, j.Exec = def.File, def.Service, def.Exec
	j.MaxRuntime = maxRuntime
	if err := j.CheckMaxRuntime(); err != nil {
		return nil, fmt.Errorf("invalid maxRuntime: %w", err)
	}
	return j, nil
}

// checkCommandArgs rejects a command whose arguments fail the injection
// checks. An empty command passes; the job reports it when it runs.
func checkCommandArgs(command string) error {
	if command == "" {
		return nil
	}
	if err := cfgvalidator.NewCommandValidator().ValidateCommandArgs(args.GetArgs(command)); err != nil {
		return fmt.Errorf("invalid command arguments: %w", err)
	}
	return nil
}
