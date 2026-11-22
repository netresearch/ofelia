// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// A job refused at boot is reported until a reload removes its section.
func TestReload_BootRefusalEndsWithTheSection(t *testing.T) {
	t.Parallel()
	cfg, reload := iniReloadConfig(t, okLocalJob+"[job-local \"broken\"]\nschedule = @daily\n")
	require.Contains(t, cfg.sh.GetUnschedulableJobs(), "broken")

	reload(okLocalJob)
	assert.NotContains(t, cfg.sh.GetUnschedulableJobs(), "broken", "the boot report outlives the section")
}

// A definition of a later job kind that cannot run is not reported when a
// job of an earlier kind already runs under the name.
func TestRegisterAllJobs_RefusalDoesNotHideARunningJob(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	cfg, err := BuildFromString("[job-exec \"x\"]\nschedule = @daily\ncontainer = app\ncommand = id\n"+
		"[job-local \"x\"]\nschedule = @daily\n", logger)
	require.NoError(t, err)
	cfg.sh = core.NewScheduler(logger)
	cfg.registerAllJobs()

	require.NotNil(t, cfg.sh.GetAnyJob("x"))
	assert.NotContains(t, cfg.sh.GetUnschedulableJobs(), "x", "a running job is listed as not scheduled")
}

// Removing one refused definition keeps the report of another refused
// definition of the same name.
func TestReload_ReportStaysWhileAnotherDefinitionOfTheNameIsRefused(t *testing.T) {
	t.Parallel()
	cfg, reload := iniReloadConfig(t, okLocalJob)
	reload(okLocalJob + "[job-exec \"x\"]\nschedule = @daily\ncommand = id\n[job-local \"x\"]\nschedule = @daily\n")
	require.Contains(t, cfg.sh.GetUnschedulableJobs(), "x")

	reload(okLocalJob + "[job-exec \"x\"]\nschedule = @daily\ncommand = id\n")
	assert.Contains(t, cfg.sh.GetUnschedulableJobs(), "x", "the exec definition is still refused")
}

// A definition that assembles again but that the scheduler never accepted
// stays reported.
func TestReload_RestoredDefinitionTheSchedulerRefusedStaysReported(t *testing.T) {
	t.Parallel()
	bad := okLocalJob + "[job-local \"sched\"]\nschedule = not a schedule\ncommand = true\n"
	cfg, reload := iniReloadConfig(t, bad)
	require.Contains(t, cfg.sh.GetUnschedulableJobs(), "sched")

	reload(okLocalJob + "[job-local \"sched\"]\nschedule = not a schedule\n")
	reload(bad)
	assert.Nil(t, cfg.sh.GetAnyJob("sched"))
	assert.Contains(t, cfg.sh.GetUnschedulableJobs(), "sched", "an unscheduled job lost its report")
}
