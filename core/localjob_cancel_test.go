// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build !windows

package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

// runLocalWithCancel runs a local job running command and cancels its run
// context after delay. It returns the log handler, the error and the time
// Run took.
func runLocalWithCancel(t *testing.T, command string, delay time.Duration) (*test.Handler, error, time.Duration) {
	t.Helper()
	job := NewLocalJob()
	job.BareJob = BareJob{Name: "local-cancel", Command: command}
	logger, handler := test.NewTestLoggerWithHandler()
	exec, err := NewExecution()
	require.NoError(t, err)
	exec.Start()
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := NewContextWithContext(runCtx, NewScheduler(logger), job, exec)
	time.AfterFunc(delay, cancel)
	start := time.Now()
	err = job.Run(ctx)
	return handler, err, time.Since(start)
}

// readPID waits for path to hold a PID written by the job and returns it.
func readPID(t *testing.T, path string) int {
	t.Helper()
	var data []byte
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(path) //nolint:gosec // test-owned temp file
		data = b
		return err == nil && strings.TrimSpace(string(b)) != ""
	}, 5*time.Second, 20*time.Millisecond)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	return pid
}

// processGone reports whether pid no longer exists (orphans are reaped by
// init, so a short wait covers the zombie window).
func processGone(pid int) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func setLocalTimings(t *testing.T, grace, waitDelay time.Duration) {
	t.Helper()
	oldGrace, oldDelay := localJobStopGrace, localJobWaitDelay
	localJobStopGrace, localJobWaitDelay = grace, waitDelay
	t.Cleanup(func() { localJobStopGrace, localJobWaitDelay = oldGrace, oldDelay })
}

func TestLocalJob_Cancel_TerminatesWholeProcessGroup(t *testing.T) {
	setLocalTimings(t, 2*time.Second, 2*time.Second)
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	// The shell starts a background child, records its PID, then waits.
	_, err, took := runLocalWithCancel(t,
		`sh -c "sleep 30 & echo $! > `+pidFile+`; sleep 30"`, 500*time.Millisecond)

	require.ErrorIs(t, err, ErrMaxTimeRunning)
	assert.Less(t, took, 5*time.Second, "Run must return soon after cancellation")
	child := readPID(t, pidFile)
	assert.True(t, processGone(child), "the background child %d must be terminated with the group", child)
}

func TestLocalJob_Cancel_KillsAfterGraceWhenTermIgnored(t *testing.T) {
	setLocalTimings(t, 300*time.Millisecond, 2*time.Second)
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	// SIGTERM is ignored by the shell and, inherited, by its child.
	_, err, took := runLocalWithCancel(t,
		`sh -c "trap '' TERM; sleep 30 & echo $! > `+pidFile+`; wait"`, 500*time.Millisecond)

	require.ErrorIs(t, err, ErrMaxTimeRunning)
	assert.Less(t, took, 5*time.Second, "SIGKILL must follow the grace period")
	child := readPID(t, pidFile)
	assert.True(t, processGone(child), "the child %d ignoring SIGTERM must be killed", child)
}

func TestLocalJob_ChildHoldingOutputDoesNotBlockRun(t *testing.T) {
	setLocalTimings(t, 2*time.Second, 200*time.Millisecond)

	// The shell exits at once; its background child keeps stdout open.
	handler, err, took := runLocalWithCancel(t, `sh -c "sleep 3 & exit 0"`, time.Hour)

	require.NoError(t, err)
	assert.Less(t, took, 2*time.Second, "Run must not wait for the pipe holder to exit")
	assert.True(t, handler.HasWarning("output after the process exited was not collected"))
}

func TestLocalJob_CompletedRunKeepsItsResult(t *testing.T) {
	setLocalTimings(t, 2*time.Second, 2*time.Second)

	_, err, _ := runLocalWithCancel(t, `sh -c "exit 3"`, time.Hour)

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrMaxTimeRunning)
	assert.Contains(t, err.Error(), "exit status 3")
}
