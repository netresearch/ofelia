// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// The config the daemon falls back to when the file does not load at boot
// loads the file once it is fixed.
func TestFallbackConfig_LoadsTheFixedFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.ini")
	require.NoError(t, os.WriteFile(path, []byte("[job-local \"report\"\nschedule = @daily\n"), 0o644))
	logger := test.NewTestLogger()
	_, err := BuildFromFile(path, logger)
	require.Error(t, err, "precondition: the file does not load")

	cfg := fallbackConfig(logger, path)
	cfg.sh = core.NewScheduler(logger)
	cfg.buildSchedulerMiddlewares(cfg.sh)
	cfg.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	cfg.levelVar = &slog.LevelVar{}
	require.NoError(t, os.WriteFile(path, []byte("[job-local \"report\"]\nschedule = @daily\ncommand = echo ok\n"), 0o644))
	require.NoError(t, cfg.iniConfigUpdate())

	assert.NotNil(t, cfg.sh.GetAnyJob("report"), "the fixed file was not loaded")
}

// A deployment that configures jobs through labels only has no config file.
// The poller skips a not-exist error silently, so keeping the path costs a
// stat per poll and no log line.
func TestFallbackConfig_MissingFileIsNotExist(t *testing.T) {
	t.Parallel()
	cfg := fallbackConfig(test.NewTestLogger(), filepath.Join(t.TempDir(), "none.ini"))
	err := cfg.iniConfigUpdate()
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist, "the poller would log this error on every tick")
}
