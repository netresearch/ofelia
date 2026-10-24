// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

const maxRuntimeReloadJobs = `
[job-run "run-own"]
schedule = @daily
image = alpine
max-runtime = %s

[job-run "run-inherits"]
schedule = @daily
image = alpine

[job-service-run "service-own"]
schedule = @daily
image = alpine
max-runtime = %s

[job-service-run "service-inherits"]
schedule = @daily
image = alpine

[job-compose "compose-own"]
schedule = @daily
service = task
max-runtime = %s

[job-compose "compose-inherits"]
schedule = @daily
service = task
`

func maxRuntimeReloadINI(global, own string) string {
	return "[global]\nmax-runtime = " + global + "\n" +
		strings.ReplaceAll(maxRuntimeReloadJobs, "%s", own)
}

// scheduledMaxRuntime reads the bound from the job the scheduler holds, not
// from the config map, so a reload that left the old job registered shows.
func scheduledMaxRuntime(t *testing.T, c *Config, name string) time.Duration {
	t.Helper()
	j := c.sh.GetJob(name)
	require.NotNil(t, j, "job %q is not scheduled", name)
	mp, ok := j.(core.MaxRuntimeProvider)
	require.True(t, ok, "job %q (%T) has no max-runtime", name, j)
	return mp.GetMaxRuntime()
}

// A reload applies a changed max-runtime: a job's own value, and the [global]
// one for the jobs that inherit it. The scheduler bounds each run from the
// registered job, so a job kept on the old value runs to the old bound.
func TestReload_AppliesChangedMaxRuntime(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	iniPath := writeFile(t, t.TempDir(), "ofelia.ini", maxRuntimeReloadINI("30m", "5m"))
	c, err := BuildFromFile(iniPath, logger)
	require.NoError(t, err)
	c.sh = core.NewScheduler(logger)
	c.buildSchedulerMiddlewares(c.sh)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	c.levelVar = &slog.LevelVar{}
	c.registerAllJobs()

	for _, kind := range []string{"run", "service", "compose"} {
		require.Equal(t, 5*time.Minute, scheduledMaxRuntime(t, c, kind+"-own"))
		require.Equal(t, 30*time.Minute, scheduledMaxRuntime(t, c, kind+"-inherits"))
	}

	// Re-registering cancels a run in progress, so a reload that changes
	// nothing must keep every job, the inheriting ones included.
	kept := map[string]core.Job{}
	for _, kind := range []string{"run", "service", "compose"} {
		for _, name := range []string{kind + "-own", kind + "-inherits"} {
			kept[name] = c.sh.GetJob(name)
		}
	}
	touchFile(t, iniPath, maxRuntimeReloadINI("30m", "5m"), time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	for name, j := range kept {
		require.Same(t, j, c.sh.GetJob(name), "unchanged reload re-registered %s", name)
	}

	touchFile(t, iniPath, maxRuntimeReloadINI("2h", "10m"), 2*time.Minute)
	require.NoError(t, c.iniConfigUpdate())

	for _, kind := range []string{"run", "service", "compose"} {
		assert.Equal(t, 10*time.Minute, scheduledMaxRuntime(t, c, kind+"-own"), "%s job's own value", kind)
		assert.Equal(t, 2*time.Hour, scheduledMaxRuntime(t, c, kind+"-inherits"), "%s job's inherited value", kind)
	}
}
