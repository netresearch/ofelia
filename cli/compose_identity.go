// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrComposeJobIdentityTaken rejects registrations that would independently
// schedule the same Compose job on Ofelia's Docker endpoint.
var ErrComposeJobIdentityTaken = errors.New("duplicate compose job identity")

// composeJobIdentity uses the resolved Compose name, not the registration's
// alias or its source paths. All registrations use the daemon's one Docker
// endpoint, so the endpoint component of ADR-004's identity is implicit.
type composeJobIdentity struct {
	project string
	job     string
}

// rejectDuplicateComposeJobIdentities rejects every project that shares a
// scheduled job identity. A registration is atomic: none of its other jobs
// remain armed. It runs both after loading and after last-valid fallback.
func (c *Config) rejectDuplicateComposeJobIdentities() {
	owners := make(map[composeJobIdentity][]string)
	for _, job := range c.ComposeJobs {
		if job.composeRegistration != "" {
			identity := composeJobIdentity{project: job.Project.ProjectName, job: job.Service}
			owners[identity] = append(owners[identity], job.composeRegistration)
		}
	}
	rejected := make(map[string][]string)
	for identity, registrations := range owners {
		if len(registrations) < 2 {
			continue
		}
		sort.Strings(registrations)
		detail := fmt.Sprintf("project %q, job %q, registrations %s",
			identity.project, identity.job, strings.Join(registrations, ", "))
		for _, name := range registrations {
			rejected[name] = append(rejected[name], detail)
		}
	}
	names := make([]string, 0, len(rejected))
	for name := range rejected {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sort.Strings(rejected[name])
		c.rejectComposeProject(name, fmt.Errorf("%w: %s", ErrComposeJobIdentityTaken, strings.Join(rejected[name], "; ")))
	}
	for name, job := range c.ComposeJobs {
		if _, duplicate := rejected[job.composeRegistration]; duplicate {
			delete(c.ComposeJobs, name)
		}
	}
}
