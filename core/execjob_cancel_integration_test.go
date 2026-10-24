//go:build integration

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

// TestExecJobIntegration_CancelReportsStillRunning runs a long command through
// job-exec against the real daemon, cancels it, and checks that Run returns
// promptly with ErrExecStillRunning naming the exec — the Engine API cannot
// stop an exec'd process, so the command is in fact still running.
func TestExecJobIntegration_CancelReportsStillRunning(t *testing.T) {
	provider := newIntegrationProvider(t)
	name := "ofelia-it-exec-" + strings.ReplaceAll(t.Name(), "/", "-")[len("TestExecJobIntegration_"):]
	name = strings.ToLower(name)
	out, err := exec.Command("docker", "run", "-d", "--rm", "--name", name, "alpine:3.22", "sleep", "120").CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	job := &ExecJob{
		BareJob:   BareJob{Name: "exec-cancel-it", Command: "sleep 30"},
		Provider:  provider,
		Container: name,
		User:      "root",
	}
	execution, err := NewExecution()
	require.NoError(t, err)
	execution.Start()
	runCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx := NewContextWithContext(runCtx, NewScheduler(test.NewTestLogger()), job, execution)

	start := time.Now()
	err = job.Run(ctx)

	require.ErrorIs(t, err, ErrMaxTimeRunning)
	require.ErrorIs(t, err, ErrExecStillRunning)
	assert.Less(t, time.Since(start), 15*time.Second, "Run must return soon after the deadline")

	top, topErr := exec.Command("docker", "top", name).CombinedOutput()
	require.NoError(t, topErr, string(top))
	assert.Contains(t, string(top), "sleep 30", "the exec'd process keeps running, which is what the error reports")
}
