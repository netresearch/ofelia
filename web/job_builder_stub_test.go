// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"errors"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// stubJobBuilder builds the bare core job a definition names, for tests of
// the handlers in this package, which cannot import cli. How a real job is
// built (defaults, default user, validation) is tested in cli against
// Config.BuildAPIJob.
func stubJobBuilder(provider core.DockerProvider) JobBuilder {
	return func(name string, def persist.Job) (core.Job, error) {
		var job core.Job
		var bare *core.BareJob
		switch def.Type {
		case persist.JobTypeRun:
			if provider == nil {
				return nil, errors.New("docker provider unavailable for run job")
			}
			j := &core.RunJob{Provider: provider, Image: def.Image, Container: def.Container}
			job, bare = j, &j.BareJob
		case persist.JobTypeExec:
			if provider == nil {
				return nil, errors.New("docker provider unavailable for exec job")
			}
			j := &core.ExecJob{Provider: provider, Container: def.Container}
			job, bare = j, &j.BareJob
		case persist.JobTypeCompose:
			j := &core.ComposeJob{Provider: provider, File: def.File, Service: def.Service, Exec: def.Exec}
			job, bare = j, &j.BareJob
		case persist.JobTypeLocal:
			j := &core.LocalJob{}
			job, bare = j, &j.BareJob
		default:
			j := &core.LocalJob{}
			job, bare = j, &j.BareJob
		}
		bare.Name, bare.Schedule, bare.Command = name, def.Schedule, def.Command
		return job, nil
	}
}
