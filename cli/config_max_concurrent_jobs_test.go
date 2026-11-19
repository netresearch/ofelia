// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// startConfig builds the configuration file at iniPath and registers its jobs
// the way the daemon does at start, with logger capturing the output.
func startConfig(t *testing.T, iniPath string, logger *slog.Logger) *Config {
	t.Helper()
	c, err := BuildFromFile(iniPath, logger)
	require.NoError(t, err)
	c.sh = core.NewScheduler(logger)
	c.buildSchedulerMiddlewares(c.sh)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	c.levelVar = &slog.LevelVar{}
	c.registerAllJobs()
	return c
}

func TestMaxConcurrentJobs_DefaultIsTheSchedulerDefault(t *testing.T) {
	t.Parallel()
	c := startConfig(t, writeFile(t, t.TempDir(), "ofelia.ini", "[global]\n"), test.NewTestLogger())

	assert.Equal(t, core.DefaultMaxConcurrentJobs, c.Global.MaxConcurrentJobs)
	assert.Equal(t, core.DefaultMaxConcurrentJobs, c.sh.MaxConcurrentJobs())
}

// 1 is the smallest limit that lets jobs run, so it is applied, not refused.
func TestMaxConcurrentJobs_FromINI(t *testing.T) {
	t.Parallel()
	for _, value := range []int{1, 25} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			t.Parallel()
			ini := "[global]\nmax-concurrent-jobs = " + strconv.Itoa(value) + "\n"
			c := startConfig(t, writeFile(t, t.TempDir(), "ofelia.ini", ini), test.NewTestLogger())

			assert.Equal(t, value, c.sh.MaxConcurrentJobs())
		})
	}
}

// A value below 1 would let no job run, so it is refused: the default stays
// and a warning names the key.
func TestMaxConcurrentJobs_BelowOneKeepsTheDefault(t *testing.T) {
	t.Parallel()
	for _, value := range []int{0, -3} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			t.Parallel()
			logger, handler := test.NewTestLoggerWithHandler()
			ini := "[global]\nmax-concurrent-jobs = " + strconv.Itoa(value) + "\n"
			c := startConfig(t, writeFile(t, t.TempDir(), "ofelia.ini", ini), logger)

			assert.Equal(t, core.DefaultMaxConcurrentJobs, c.sh.MaxConcurrentJobs())
			assert.True(t, handler.HasWarning("max-concurrent-jobs"), "the refused value is reported")
		})
	}
}

func TestMaxConcurrentJobs_ReloadAppliesTheNewLimit(t *testing.T) {
	t.Parallel()
	iniPath := writeFile(t, t.TempDir(), "ofelia.ini", "[global]\nmax-concurrent-jobs = 25\n")
	c := startConfig(t, iniPath, test.NewTestLogger())
	require.Equal(t, 25, c.sh.MaxConcurrentJobs())

	touchFile(t, iniPath, "[global]\nmax-concurrent-jobs = 40\n", time.Minute)
	require.NoError(t, c.iniConfigUpdate())

	assert.Equal(t, 40, c.sh.MaxConcurrentJobs())
}

// TestGlobalLabelAllowList_OmitsMaxConcurrentJobs keeps max-concurrent-jobs
// INI-only. The limit is shared by every job of the daemon: a container that
// could set it would decide how many of every other container's jobs run.
func TestGlobalLabelAllowList_OmitsMaxConcurrentJobs(t *testing.T) {
	t.Parallel()

	assert.False(t, globalLabelAllowList["max-concurrent-jobs"],
		"globalLabelAllowList must NOT contain max-concurrent-jobs — the limit is daemon-wide and stays INI-only")
}

func TestMaxConcurrentJobs_LabelIsBlocked(t *testing.T) {
	t.Parallel()
	logger, handler := test.NewTestLoggerWithHandler()
	cfg := NewConfig(logger)
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)

	cfg.dockerContainersUpdate([]DockerContainerInfo{{
		Name:  "ofelia",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			requiredLabel:                        "true",
			serviceLabel:                         "true",
			labelPrefix + ".max-concurrent-jobs": "1",
		},
	}})

	assert.Equal(t, core.DefaultMaxConcurrentJobs, cfg.Global.MaxConcurrentJobs)
	assert.Equal(t, core.DefaultMaxConcurrentJobs, cfg.sh.MaxConcurrentJobs())
	assert.True(t, handler.HasWarning(`Blocked global config key "max-concurrent-jobs"`))
}
