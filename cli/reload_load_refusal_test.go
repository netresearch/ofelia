// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

const okLocalJob = "[job-local \"ok\"]\nschedule = @daily\ncommand = true\n"

// A job a reload adds that cannot run is reported until a later reload
// removes it.
func TestReload_RefusedNewJobIsReportedUntilRemoved(t *testing.T) {
	t.Parallel()
	cfg, reload := iniReloadConfig(t, okLocalJob)
	reload(okLocalJob + "[job-local \"broken\"]\nschedule = @daily\n")
	require.Contains(t, cfg.sh.GetUnschedulableJobs(), "broken")

	reload(okLocalJob)
	assert.NotContains(t, cfg.sh.GetUnschedulableJobs(), "broken", "the report outlives the section")
}

// Fixing the job registers it and clears the report.
func TestReload_RefusedNewJobFixedClearsTheReport(t *testing.T) {
	t.Parallel()
	cfg, reload := iniReloadConfig(t, okLocalJob)
	reload(okLocalJob + "[job-local \"broken\"]\nschedule = @daily\n")
	reload(okLocalJob + "[job-local \"broken\"]\nschedule = @daily\ncommand = true\n")

	assert.NotNil(t, cfg.sh.GetAnyJob("broken"))
	assert.NotContains(t, cfg.sh.GetUnschedulableJobs(), "broken")
}

// A name that a job from the state file or the web API runs under is not
// reported as not scheduled because its INI definition is invalid.
func TestReload_RefusedJobHeldByAPIJobIsNotReported(t *testing.T) {
	t.Parallel()
	cfg, reload := iniReloadConfig(t, okLocalJob)
	held := &core.LocalJob{}
	held.Name, held.Schedule, held.Command = "broken", "@daily", "true"
	require.NoError(t, cfg.sh.AddJob(held))

	reload(okLocalJob + "[job-local \"broken\"]\nschedule = @daily\n")
	assert.NotContains(t, cfg.sh.GetUnschedulableJobs(), "broken")
	assert.Same(t, held, cfg.sh.GetAnyJob("broken"))
}

// A reload that makes a running job invalid keeps the job running and
// reports the refused definition; reverting the definition clears the report.
func TestReload_InvalidChangeKeepsTheRunningJobAndReportsIt(t *testing.T) {
	t.Parallel()
	valid := okLocalJob + "[job-local \"report\"]\nschedule = @daily\ncommand = echo ok\n"
	cfg, reload := iniReloadConfig(t, valid)

	reload(okLocalJob + "[job-local \"report\"]\nschedule = @daily\n")
	require.NotNil(t, cfg.sh.GetAnyJob("report"))
	assert.Equal(t, "echo ok", cfg.sh.GetAnyJob("report").GetCommand())
	assert.Contains(t, cfg.sh.GetUnschedulableJobs(), "report", "the refused definition is not reported")

	reload(valid)
	assert.NotContains(t, cfg.sh.GetUnschedulableJobs(), "report")
	assert.Equal(t, "echo ok", cfg.sh.GetAnyJob("report").GetCommand())
}
