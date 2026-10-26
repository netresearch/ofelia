//go:build integration && unix

// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Killing only the real Compose CLI leaves its run context healthy. The
// scheduler may release that execution only after cleanup removes its container.
func TestComposeJobIntegration_UnexpectedCLIExitCannotReleaseLiveWorkload(t *testing.T) {
	t.Parallel()
	provider := newIntegrationProvider(t)
	file, _ := composeProject(t, gracefulTask)
	h := newDurableHarness(t, at(23, 10))
	job := NewComposeJob()
	job.Name, job.Schedule, job.File, job.Service = "shop.backup", durableSchedule, file, "task"
	job.Provider = provider
	job.Project = ComposeProjectContext{Files: []string{file}, Revision: "accepted", MissedFires: "one", Concurrency: "queue"}
	require.NoError(t, h.s.AddJob(job))
	h.start()
	execution, err := NewExecution()
	require.NoError(t, err)
	defer execution.Cleanup()
	ctx := NewContextWithContext(context.Background(), h.s, job, execution)
	policy, _ := job.SchedulePolicy()
	admitted := h.s.admit(ctx.RunContext(), job, policy, execution.ID)
	require.NotNil(t, admitted)
	cmd, err := job.buildCommand(ctx)
	require.NoError(t, err)
	// Record the CLI's PID through a shell that execs Docker, avoiding a
	// shared-memory read of cmd.Process while Start populates it.
	pidFile := filepath.Join(t.TempDir(), "cli.pid")
	args := append([]string{"-c", `printf '%s' "$$" > "$1"; shift; exec "$@"`, "ofelia-cli-probe", pidFile, cmd.Path}, cmd.Args[1:]...)
	wrapper := exec.Command("sh", args...)
	wrapper.Stdout, wrapper.Stderr = cmd.Stdout, cmd.Stderr
	wrapper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	result := make(chan error, 1)
	go func() { result <- job.runWithCancellation(ctx, wrapper) }()
	label := ComposeExecutionLabel + "=" + execution.ID
	waitForLabeled(t, provider, label)
	pidBytes, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	process, err := os.FindProcess(pid)
	require.NoError(t, err)
	t.Cleanup(func() { _ = process.Release() })
	require.NoError(t, process.Kill(), "kill only the CLI, not its container or run context")
	var exitErr error
	select {
	case exitErr = <-result:
	case <-time.After(30 * time.Second):
		t.Fatal("the CLI exit did not return")
	}
	require.Error(t, exitErr)
	require.NoError(t, ctx.RunContext().Err(), "the run context must remain healthy")
	h.s.finishDispatch(ctx.RunContext(), admitted, outcomeFailed, exitErr)
	remaining := listByLabel(t, provider, label, false)
	if len(remaining) > 0 {
		require.ErrorIs(t, exitErr, ErrComposeTerminationUnconfirmed,
			"a failed CLI alone does not prove its workload ended")
		require.ErrorIs(t, h.s.RunJob(context.Background(), job.Name), ErrJobBusy)
	} else {
		assert.Empty(t, listByLabel(t, provider, label, true), "confirmed cleanup removes stopped containers too")
		assert.False(t, h.s.durableBusy(job.Name), "confirmed cleanup may release the job")
	}
}
