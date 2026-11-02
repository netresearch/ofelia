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
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

const composeMaxRuntimeINI = `[global]
max-runtime = 30m

[job-compose "inherits"]
schedule = @daily
service = task

[job-compose "own"]
schedule = @daily
service = task
max-runtime = 5m

[job-compose "exec"]
schedule = @daily
service = task
exec = true
`

// registerCompose builds iniContent and registers its jobs the way the daemon
// does at start.
func registerCompose(t *testing.T, iniContent string) *Config {
	t.Helper()
	logger := test.NewTestLogger()
	c, err := BuildFromFile(writeFile(t, t.TempDir(), "ofelia.ini", iniContent), logger)
	require.NoError(t, err)
	c.sh = core.NewScheduler(logger)
	c.buildSchedulerMiddlewares(c.sh)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	c.levelVar = &slog.LevelVar{}
	c.registerAllJobs()
	return c
}

func TestComposeJobs_InheritGlobalMaxRuntimeAtStart(t *testing.T) {
	t.Parallel()
	c := registerCompose(t, composeMaxRuntimeINI)

	assert.Equal(t, 30*time.Minute, c.sh.MaxRuntimeFor(c.ComposeJobs["inherits"]), "a run-mode job inherits [global] max-runtime")
	assert.Equal(t, 5*time.Minute, c.sh.MaxRuntimeFor(c.ComposeJobs["own"]), "a job's own max-runtime wins")
	assert.Zero(t, c.ComposeJobs["exec"].MaxRuntime, "exec mode cannot be stopped, so it inherits nothing")
	assert.Equal(t, 24*time.Hour, c.sh.MaxRuntimeFor(c.ComposeJobs["exec"]), "exec mode stays on the scheduler default")
}

func TestComposeProjectJobs_InheritGlobalMaxRuntime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := writeFile(t, dir, "compose.yaml", shopCompose)
	c := registerCompose(t, "[global]\nmax-runtime = 30m\n\n[compose-project \"shop\"]\nfile = "+composePath+"\n")

	require.Contains(t, c.ComposeJobs, "shop.backup")
	assert.Equal(t, 30*time.Minute, c.sh.MaxRuntimeFor(c.ComposeJobs["shop.backup"]))
}

func TestComposeJobs_InheritGlobalMaxRuntimeOnReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := "[global]\nmax-runtime = 30m\n\n[job-local \"x\"]\nschedule = @daily\ncommand = true\n"
	iniPath := writeFile(t, dir, "ofelia.ini", base)
	c := prepareReload(t, iniPath)

	touchFile(t, iniPath, base+"\n[job-compose \"added\"]\nschedule = @daily\nservice = task\n", time.Minute)
	require.NoError(t, c.iniConfigUpdate())

	require.Contains(t, c.ComposeJobs, "added")
	assert.Equal(t, 30*time.Minute, c.sh.MaxRuntimeFor(c.ComposeJobs["added"]))
}

func TestComposeJobs_InheritGlobalMaxRuntimeFromLabels(t *testing.T) {
	t.Parallel()
	cfg := NewConfig(test.NewTestLogger())
	cfg.Global.AllowHostJobsFromLabels = true
	cfg.Global.MaxRuntime = 30 * time.Minute
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(test.NewTestLogger())
	cfg.buildSchedulerMiddlewares(cfg.sh)

	cfg.dockerContainersUpdate([]DockerContainerInfo{{
		Name:  "cont1",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			requiredLabel: "true",
			serviceLabel:  "true",
			labelPrefix + ".job-compose.run.schedule":        "@hourly",
			labelPrefix + ".job-compose.run.service":         "task",
			labelPrefix + ".job-compose.viaexec.schedule":    "@hourly",
			labelPrefix + ".job-compose.viaexec.service":     "task",
			labelPrefix + ".job-compose.viaexec.exec":        "true",
			labelPrefix + ".job-compose.viaexec.max-runtime": "5m",
		},
	}})

	run := findComposeJob(t, cfg, "run")
	assert.Equal(t, 30*time.Minute, cfg.sh.MaxRuntimeFor(run))
	viaExec := findComposeJob(t, cfg, "viaexec")
	assert.Zero(t, viaExec.MaxRuntime, "exec mode inherits nothing and drops its own value")
	assert.Equal(t, 24*time.Hour, cfg.sh.MaxRuntimeFor(viaExec))
}

// findComposeJob returns the label-defined compose job whose name ends in
// suffix; label jobs are named after their container.
func findComposeJob(t *testing.T, cfg *Config, suffix string) *ComposeJobConfig {
	t.Helper()
	for name, j := range cfg.ComposeJobs {
		if strings.HasSuffix(name, suffix) {
			return j
		}
	}
	t.Fatalf("no compose job ending in %q among %d jobs", suffix, len(cfg.ComposeJobs))
	return nil
}

// A configuration file with max-runtime on an exec-mode compose job loaded
// before compose jobs honored the key, so it keeps loading: the value is
// dropped with a warning instead of failing the whole configuration.
func TestComposeJob_ExecModeIgnoresOwnMaxRuntime(t *testing.T) {
	t.Parallel()
	logger, handler := test.NewTestLoggerWithHandler()
	c, err := BuildFromFile(writeFile(t, t.TempDir(), "ofelia.ini",
		"[job-compose \"x\"]\nschedule = @daily\nservice = task\nexec = true\nmax-runtime = 5m\n"), logger)
	require.NoError(t, err, "the configuration must keep loading")
	c.sh = core.NewScheduler(logger)
	c.buildSchedulerMiddlewares(c.sh)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	c.levelVar = &slog.LevelVar{}
	c.registerAllJobs()

	require.Contains(t, c.ComposeJobs, "x")
	assert.Zero(t, c.ComposeJobs["x"].MaxRuntime, "exec mode cannot be bounded")
	assert.True(t, handler.HasWarning("max-runtime is ignored"), "the dropped value must be reported")
}
