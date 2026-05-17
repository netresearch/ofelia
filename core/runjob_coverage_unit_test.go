// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// ---------------------------------------------------------------------------
// InitializeRuntimeFields (0% → 100%)
// ---------------------------------------------------------------------------

func TestRunJob_InitializeRuntimeFields(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)
	job := NewRunJob(provider)

	// Should not panic and is a no-op
	job.InitializeRuntimeFields()
}

// ---------------------------------------------------------------------------
// stopContainer (0% → 100%)
// ---------------------------------------------------------------------------

func TestRunJob_stopContainer_Success(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)
	job := NewRunJob(provider)
	job.setContainerID("test-container-123")

	timeout := 10 * time.Second
	err := job.stopContainer(context.Background(), timeout)
	assert.NoError(t, err)
}

func TestRunJob_stopContainer_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	containers.OnStop = func(_ context.Context, _ string, _ domain.StopOptions) error {
		return errors.New("stop failed")
	}
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)
	job := NewRunJob(provider)
	job.setContainerID("test-container-456")

	timeout := 5 * time.Second
	err := job.stopContainer(context.Background(), timeout)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopping container")
}

// ---------------------------------------------------------------------------
// getContainer (0% → 100%)
// ---------------------------------------------------------------------------

func TestRunJob_getContainer_Success(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)
	job := NewRunJob(provider)
	job.setContainerID("inspect-me")

	container, err := job.getContainer(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "inspect-me", container.ID)
}

func TestRunJob_getContainer_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	containers.OnInspect = func(_ context.Context, _ string) (*domain.Container, error) {
		return nil, errors.New("inspect failed")
	}
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)
	job := NewRunJob(provider)
	job.setContainerID("fail-inspect")

	container, err := job.getContainer(context.Background())
	require.Error(t, err)
	assert.Nil(t, container)
	assert.Contains(t, err.Error(), "getting container")
}

// ---------------------------------------------------------------------------
// watchContainer edge cases
// ---------------------------------------------------------------------------

func TestRunJob_watchContainer_ContextCanceled(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	containers.OnWait = func(ctx context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse) // block forever
		errCh := make(chan error, 1)
		go func() {
			<-ctx.Done()
			errCh <- ctx.Err()
		}()
		return respCh, errCh
	}
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)
	job := NewRunJob(provider)
	job.setContainerID("timeout-container")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := job.watchContainer(ctx)
	assert.ErrorIs(t, err, ErrMaxTimeRunning)
}

func TestRunJob_watchContainer_UnexpectedExit(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	containers.OnWait = func(_ context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error, 1)
		respCh <- domain.WaitResponse{StatusCode: -1}
		close(respCh)
		close(errCh)
		return respCh, errCh
	}
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)
	job := NewRunJob(provider)
	job.setContainerID("unexpected-container")

	err := job.watchContainer(context.Background())
	assert.ErrorIs(t, err, ErrUnexpected)
}

func TestRunJob_watchContainer_NonZeroExit(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	containers.OnWait = func(_ context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error, 1)
		respCh <- domain.WaitResponse{StatusCode: 42}
		close(respCh)
		close(errCh)
		return respCh, errCh
	}
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)
	job := NewRunJob(provider)
	job.setContainerID("nonzero-container")

	err := job.watchContainer(context.Background())
	var nzErr NonZeroExitError
	require.ErrorAs(t, err, &nzErr)
	assert.Equal(t, 42, nzErr.ExitCode)
}
