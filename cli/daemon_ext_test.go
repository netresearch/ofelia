// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

func TestRestoreJobHistory_Disabled(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	cmd := &DaemonCommand{
		Logger: logger,
	}

	// Create a config with RestoreHistory disabled (default: empty SaveFolder)
	config := NewConfig(logger)
	// SaveConfig.SaveFolder is empty, so RestoreHistoryEnabled() returns false

	sched := core.NewScheduler(logger)
	cmd.scheduler = sched

	// Should return early without doing anything
	cmd.restoreJobHistory(config)

	// If we got here without panic, the disabled path works correctly
}

func TestRestoreJobHistory_EnabledButNoFiles(t *testing.T) {
	t.Parallel()

	logger, handler := test.NewTestLoggerWithHandler()
	cmd := &DaemonCommand{
		Logger: logger,
	}

	config := NewConfig(logger)
	config.Global.SaveConfig = middlewares.SaveConfig{
		SaveFolder:           t.TempDir(),
		RestoreHistoryMaxAge: 24 * time.Hour,
	}

	sched := core.NewScheduler(logger)
	cmd.scheduler = sched

	// Should attempt restore but find no files - may warn or succeed silently
	cmd.restoreJobHistory(config)

	// Verify no error-level messages were logged
	assert.Equal(t, 0, handler.ErrorCount(), "expected no errors when no history files exist")
}

func TestRestoreJobHistory_NonexistentFolder(t *testing.T) {
	t.Parallel()

	logger, handler := test.NewTestLoggerWithHandler()
	cmd := &DaemonCommand{
		Logger: logger,
	}

	config := NewConfig(logger)
	config.Global.SaveConfig = middlewares.SaveConfig{
		SaveFolder:           "/nonexistent/path/that/should/not/exist",
		RestoreHistoryMaxAge: 24 * time.Hour,
	}

	sched := core.NewScheduler(logger)
	cmd.scheduler = sched

	// RestoreHistory silently skips nonexistent folders (returns nil)
	cmd.restoreJobHistory(config)

	// Verify no error-level messages (silent skip is expected behavior)
	assert.Equal(t, 0, handler.ErrorCount(), "expected no errors for nonexistent save folder")
}

func TestWaitForServerWithErrChan_ErrChanError(t *testing.T) {
	t.Parallel()

	addr := getUnusedAddr(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	errChan <- errors.New("startup failed")

	err := waitForServerWithErrChan(ctx, addr, errChan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server failed to start")
}

func TestApplyOptions_NilConfigNoOp(t *testing.T) {
	t.Parallel()

	cmd := &DaemonCommand{}
	// Should not panic with nil config
	cmd.applyOptions(nil)
}

func TestApplyOptions_AllFields(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	config := NewConfig(logger)

	pollInterval := 5 * time.Minute
	useEvents := true
	noPoll := false
	includeStopped := true

	cmd := &DaemonCommand{
		DockerFilters:        []string{"label=ofelia.enabled=true"},
		DockerPollInterval:   &pollInterval,
		DockerUseEvents:      &useEvents,
		DockerNoPoll:         &noPoll,
		DockerIncludeStopped: &includeStopped,
		EnableWeb:            true,
		WebAddr:              ":9090",
		WebAuthEnabled:       true,
		WebUsername:          "admin",
		WebPasswordHash:      "hash",
		WebSecretKey:         "secret",
		WebTokenExpiry:       48,
		WebMaxLoginAttempts:  10,
		EnablePprof:          true,
		PprofAddr:            "127.0.0.1:6060",
		LogLevel:             "debug",
	}

	cmd.applyOptions(config)

	assert.Equal(t, []string{"label=ofelia.enabled=true"}, config.Docker.Filters)
	assert.Equal(t, 5*time.Minute, config.Docker.PollInterval)
	assert.True(t, config.Docker.UseEvents)
	assert.False(t, config.Docker.DisablePolling)
	assert.True(t, config.Docker.IncludeStopped)
	assert.True(t, config.Global.EnableWeb)
	assert.Equal(t, ":9090", config.Global.WebAddr)
	assert.True(t, config.Global.WebAuthEnabled)
	assert.Equal(t, "admin", config.Global.WebUsername)
	assert.Equal(t, "hash", config.Global.WebPasswordHash)
	assert.Equal(t, "secret", config.Global.WebSecretKey)
	assert.Equal(t, 48, config.Global.WebTokenExpiry)
	assert.Equal(t, 10, config.Global.WebMaxLoginAttempts)
	assert.True(t, config.Global.EnablePprof)
	assert.Equal(t, "127.0.0.1:6060", config.Global.PprofAddr)
	assert.Equal(t, "debug", config.Global.LogLevel)
}

// TestApplyOptions_DockerStartupRetryFlags pins the CLI/env wiring for
// the new --docker-startup-retry-count / --docker-startup-retry-interval
// flags introduced in PR #699 / #523. Two parity tests: pointer-nil
// (no override) and pointer-set (override applied). Mirrors the existing
// DockerPollInterval/DockerIncludeStopped patterns above so the
// applyOptions seam stays mutation-test-covered.
func TestApplyOptions_DockerStartupRetryFlags(t *testing.T) {
	t.Parallel()

	t.Run("nil-flags-preserve-config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(test.NewTestLogger())
		config.Docker.StartupRetryCount = 5
		config.Docker.StartupRetryInterval = 2 * time.Second

		// All flag pointers nil — applyOptions must NOT touch the
		// pre-existing config values (operator set them via INI).
		cmd := &DaemonCommand{}
		cmd.applyOptions(config)

		assert.Equal(t, 5, config.Docker.StartupRetryCount,
			"nil CLI flag must NOT clobber INI-supplied StartupRetryCount")
		assert.Equal(t, 2*time.Second, config.Docker.StartupRetryInterval,
			"nil CLI flag must NOT clobber INI-supplied StartupRetryInterval")
	})

	t.Run("flags-override-config", func(t *testing.T) {
		t.Parallel()
		config := NewConfig(test.NewTestLogger())
		config.Docker.StartupRetryCount = 1
		config.Docker.StartupRetryInterval = 100 * time.Millisecond

		count := 7
		interval := 3 * time.Second
		cmd := &DaemonCommand{
			DockerStartupRetryCount:    &count,
			DockerStartupRetryInterval: &interval,
		}
		cmd.applyOptions(config)

		assert.Equal(t, 7, config.Docker.StartupRetryCount,
			"non-nil CLI flag must override INI-supplied StartupRetryCount")
		assert.Equal(t, 3*time.Second, config.Docker.StartupRetryInterval,
			"non-nil CLI flag must override INI-supplied StartupRetryInterval")
	})
}

func TestApplyConfigDefaults(t *testing.T) {
	t.Parallel()

	logger := test.NewTestLogger()
	config := NewConfig(logger)
	config.Global.EnableWeb = true
	config.Global.WebAddr = ":7070"
	config.Global.WebAuthEnabled = true
	config.Global.WebUsername = "configuser"
	config.Global.WebPasswordHash = "confighash"
	config.Global.WebSecretKey = "configsecret"
	config.Global.WebTokenExpiry = 12
	config.Global.WebMaxLoginAttempts = 3
	config.Global.EnablePprof = true
	config.Global.PprofAddr = "0.0.0.0:9090"

	cmd := &DaemonCommand{
		// Use default CLI values (should be overridden by config values)
		WebAddr:             ":8081",
		WebTokenExpiry:      24,
		WebMaxLoginAttempts: 5,
		PprofAddr:           "127.0.0.1:8080",
	}

	cmd.applyConfigDefaults(config)

	assert.True(t, cmd.EnableWeb)
	assert.Equal(t, ":7070", cmd.WebAddr)
	assert.True(t, cmd.WebAuthEnabled)
	assert.Equal(t, "configuser", cmd.WebUsername)
	assert.Equal(t, "confighash", cmd.WebPasswordHash)
	assert.Equal(t, "configsecret", cmd.WebSecretKey)
	assert.Equal(t, 12, cmd.WebTokenExpiry)
	assert.Equal(t, 3, cmd.WebMaxLoginAttempts)
	assert.True(t, cmd.EnablePprof)
	assert.Equal(t, "0.0.0.0:9090", cmd.PprofAddr)
}

func TestDaemonCommand_ConfigNilAndSet(t *testing.T) {
	t.Parallel()

	cmd := &DaemonCommand{}

	// Initially nil
	assert.Nil(t, cmd.Config())

	// After setting config
	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cmd.config = cfg
	assert.Equal(t, cfg, cmd.Config())
}
