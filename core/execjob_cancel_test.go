// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// runExecJobCanceled runs an ExecJob whose provider waits for the run context
// to end and then answers with code and err, as the SDK adapter does after
// inspecting the exec.
func runExecJobCanceled(t *testing.T, code int, err error) error {
	t.Helper()
	mc := mock.NewDockerClient()
	execSvc := mc.Exec().(*mock.ExecService)
	execSvc.OnRun = func(ctx context.Context, _ string, _ *domain.ExecConfig, _, _ io.Writer) (int, error) {
		<-ctx.Done()
		return code, err
	}
	job := &ExecJob{
		BareJob:   BareJob{Name: "exec-cancel", Command: "sleep 30"},
		Provider:  NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil),
		Container: "c1",
	}
	exec, nerr := NewExecution()
	require.NoError(t, nerr)
	exec.Start()
	runCtx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := NewContextWithContext(runCtx, NewScheduler(test.NewTestLogger()), job, exec)
	return job.Run(ctx)
}

func TestExecJob_Canceled_StillRunning(t *testing.T) {
	err := runExecJobCanceled(t, -1, fmt.Errorf("%w: exec exec-123 (pid 42)", domain.ErrExecStillRunning))

	require.ErrorIs(t, err, ErrMaxTimeRunning)
	require.ErrorIs(t, err, ErrExecStillRunning)
	assert.Contains(t, err.Error(), "exec-123")
}

func TestExecJob_Canceled_OutcomeUnknown(t *testing.T) {
	err := runExecJobCanceled(t, -1, fmt.Errorf("%w: exec exec-123: boom", domain.ErrExecOutcomeUnknown))

	require.ErrorIs(t, err, ErrMaxTimeRunning)
	require.ErrorIs(t, err, ErrExecOutcomeUnknown)
}

func TestExecJob_Canceled_ExitedKeepsExitCode(t *testing.T) {
	err := runExecJobCanceled(t, 3, nil)

	require.NotErrorIs(t, err, ErrMaxTimeRunning)
	exitErr, ok := errors.AsType[NonZeroExitError](err)
	require.True(t, ok, "expected NonZeroExitError, got %T: %v", err, err)
	assert.Equal(t, 3, exitErr.ExitCode)
}
