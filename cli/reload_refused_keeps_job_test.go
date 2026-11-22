// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// iniReloadConfig returns a config read from an INI file with body, with a
// scheduler and the job registration of boot, and a function that rewrites
// the file and reloads it.
func iniReloadConfig(t *testing.T, body string) (*Config, func(string)) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.ini")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	logger := test.NewTestLogger()
	cfg, err := BuildFromFile(path, logger)
	require.NoError(t, err)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.configPath = path
	cfg.levelVar = &slog.LevelVar{}
	cfg.registerAllJobs()
	return cfg, func(next string) {
		t.Helper()
		require.NoError(t, os.WriteFile(path, []byte(next), 0o644))
		cfg.configModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		require.NoError(t, cfg.iniConfigUpdate())
	}
}

// A reload that changes a job so that the scheduler refuses it keeps the
// previous definition running and reports the refusal; a reload that fixes
// the job registers it and clears the report.
func TestReload_RefusedChangeKeepsTheRunningJob(t *testing.T) {
	t.Parallel()
	cfg, reload := iniReloadConfig(t, "[job-local \"shop.report\"]\nschedule = @daily\ncommand = echo old\n")
	old := cfg.sh.GetAnyJob("shop.report")
	require.NotNil(t, old)

	reload("[job-local \"shop.report\"]\nschedule = not-a-schedule\ncommand = echo new\n")
	assert.Same(t, old, cfg.sh.GetAnyJob("shop.report"), "the running job was replaced or removed")
	assert.True(t, cfg.sh.EntryByName("shop.report").Valid())
	assert.Contains(t, cfg.sh.GetUnschedulableJobs(), "shop.report", "the refusal is not reported")
	assert.Empty(t, cfg.sh.GetRemovedJobs())

	reload("[job-local \"shop.report\"]\nschedule = @hourly\ncommand = echo fixed\n")
	live := cfg.sh.GetAnyJob("shop.report")
	require.NotNil(t, live)
	assert.Equal(t, "echo fixed", live.GetCommand())
	assert.Empty(t, cfg.sh.GetUnschedulableJobs())
}

// The same holds for a job from labels.
func TestReload_RefusedLabelChangeKeepsTheRunningJob(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.WebhookConfigs = NewWebhookConfigs()
	containers := func(schedule string) []DockerContainerInfo {
		return []DockerContainerInfo{{
			Name:  "shop",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                  "true",
				"ofelia.job-exec.report.schedule": schedule,
				"ofelia.job-exec.report.command":  "id",
			},
		}}
	}
	cfg.dockerContainersUpdate(containers("@daily"))
	require.Len(t, cfg.ExecJobs, 1)
	var name string
	for k := range cfg.ExecJobs {
		name = k
	}
	old := cfg.sh.GetAnyJob(name)
	require.NotNil(t, old)

	cfg.dockerContainersUpdate(containers("not-a-schedule"))
	assert.Same(t, old, cfg.sh.GetAnyJob(name), "the running job was replaced or removed")
	assert.Contains(t, cfg.sh.GetUnschedulableJobs(), name)
}

// Taking a refused change back clears the report: the running job is the
// configured one again.
func TestReload_RevertedRefusedChangeClearsTheReport(t *testing.T) {
	t.Parallel()
	original := "[job-local \"shop.report\"]\nschedule = @daily\ncommand = echo old\n"
	cfg, reload := iniReloadConfig(t, original)
	old := cfg.sh.GetAnyJob("shop.report")

	reload("[job-local \"shop.report\"]\nschedule = not-a-schedule\ncommand = echo old\n")
	require.Contains(t, cfg.sh.GetUnschedulableJobs(), "shop.report")

	reload(original)
	assert.Same(t, old, cfg.sh.GetAnyJob("shop.report"))
	assert.Empty(t, cfg.sh.GetUnschedulableJobs(), "the report outlives the refused change")
}

// A job the scheduler refused at boot keeps its report when a later reload
// takes a second refused change back to the first one: nothing runs.
func TestReload_RefusedAtBootKeepsTheReportWhenItComesBack(t *testing.T) {
	t.Parallel()
	first := "[job-local \"shop.report\"]\nschedule = not-a-schedule\ncommand = echo old\n"
	cfg, reload := iniReloadConfig(t, first)
	require.Nil(t, cfg.sh.GetAnyJob("shop.report"), "precondition: boot refuses the job")

	reload("[job-local \"shop.report\"]\nschedule = still-not-a-schedule\ncommand = echo old\n")
	reload(first)

	assert.Nil(t, cfg.sh.GetAnyJob("shop.report"))
	assert.Contains(t, cfg.sh.GetUnschedulableJobs(), "shop.report", "the report was dropped while nothing runs")
}

// A refused label change is logged once, not on every container event; a
// definition refused again after a successful change is logged again.
func TestReload_RefusedLabelChangeIsLoggedOnce(t *testing.T) {
	t.Parallel()
	handler := test.NewHandler()
	logger := slog.New(handler)
	cfg := NewConfig(logger)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.WebhookConfigs = NewWebhookConfigs()
	containers := func(schedule string) []DockerContainerInfo {
		return []DockerContainerInfo{{
			Name:  "shop",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                  "true",
				"ofelia.job-exec.report.schedule": schedule,
				"ofelia.job-exec.report.command":  "id",
			},
		}}
	}
	refusals := func() int {
		n := 0
		for _, m := range handler.GetMessages() {
			if m.Level == "ERROR" && strings.Contains(m.Message, "the changed job is refused") {
				n++
			}
		}
		return n
	}
	cfg.dockerContainersUpdate(containers("@daily"))
	cfg.dockerContainersUpdate(containers("not-a-schedule"))
	cfg.dockerContainersUpdate(containers("not-a-schedule"))
	assert.Equal(t, 1, refusals(), "the same refused definition is logged on every event")

	cfg.dockerContainersUpdate(containers("@hourly"))
	cfg.dockerContainersUpdate(containers("not-a-schedule"))
	assert.Equal(t, 2, refusals(), "a definition refused again after a successful change is not logged")
}
