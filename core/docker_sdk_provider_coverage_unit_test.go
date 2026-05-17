// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// ---------------------------------------------------------------------------
// StopContainer error path (66.7% → 100%)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_StopContainer_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	containers := mc.Containers().(*mock.ContainerService)
	containers.OnStop = func(_ context.Context, _ string, _ domain.StopOptions) error {
		return errors.New("stop failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	timeout := 5 * time.Second
	err := provider.StopContainer(context.Background(), "c1", domain.StopOptions{Timeout: &timeout})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stop")
}

// ---------------------------------------------------------------------------
// ListContainers (0% → 100%)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_ListContainers_Success(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	containers, err := provider.ListContainers(context.Background(), domain.ListOptions{})
	require.NoError(t, err)
	assert.NotNil(t, containers)
}

func TestSDKDockerProvider_ListContainers_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	cs := mc.Containers().(*mock.ContainerService)
	cs.OnList = func(_ context.Context, _ domain.ListOptions) ([]domain.Container, error) {
		return nil, errors.New("list failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	containers, err := provider.ListContainers(context.Background(), domain.ListOptions{})
	require.Error(t, err)
	assert.Nil(t, containers)
}

// ---------------------------------------------------------------------------
// WaitContainer edge cases (77.8% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_WaitContainer_ContextCanceled(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	cs := mc.Containers().(*mock.ContainerService)
	cs.OnWait = func(_ context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse) // block forever
		errCh := make(chan error)
		return respCh, errCh
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	exitCode, err := provider.WaitContainer(ctx, "c1")
	require.Error(t, err)
	assert.Equal(t, int64(-1), exitCode)
}

func TestSDKDockerProvider_WaitContainer_ErrorChannel(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	cs := mc.Containers().(*mock.ContainerService)
	cs.OnWait = func(_ context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse)
		errCh := make(chan error, 1)
		errCh <- errors.New("container error")
		return respCh, errCh
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	exitCode, err := provider.WaitContainer(context.Background(), "c1")
	require.Error(t, err)
	assert.Equal(t, int64(-1), exitCode)
}

func TestSDKDockerProvider_WaitContainer_RespChannelClosed(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	cs := mc.Containers().(*mock.ContainerService)
	cs.OnWait = func(_ context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse)
		errCh := make(chan error)
		go func() {
			close(respCh)
			close(errCh)
		}()
		return respCh, errCh
	}
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	exitCode, err := provider.WaitContainer(context.Background(), "c1")
	require.Error(t, err)
	assert.Equal(t, int64(-1), exitCode)
}

func TestSDKDockerProvider_WaitContainer_ResponseWithError(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	cs := mc.Containers().(*mock.ContainerService)
	cs.OnWait = func(_ context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error, 1)
		respCh <- domain.WaitResponse{
			StatusCode: 1,
			Error:      &domain.WaitError{Message: "OOM killed"},
		}
		close(respCh)
		close(errCh)
		return respCh, errCh
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	exitCode, err := provider.WaitContainer(context.Background(), "c1")
	require.Error(t, err)
	assert.Equal(t, int64(1), exitCode)
}

func TestSDKDockerProvider_WaitContainer_ErrorChannelClosed(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	cs := mc.Containers().(*mock.ContainerService)
	cs.OnWait = func(_ context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error)
		// Close errCh immediately, then send response
		go func() {
			close(errCh)
			respCh <- domain.WaitResponse{StatusCode: 0}
			close(respCh)
		}()
		return respCh, errCh
	}
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	exitCode, err := provider.WaitContainer(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), exitCode)
}

// ---------------------------------------------------------------------------
// GetContainerLogs error path (77.8% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_GetContainerLogs_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	cs := mc.Containers().(*mock.ContainerService)
	cs.OnLogs = func(_ context.Context, _ string, _ domain.LogOptions) (io.ReadCloser, error) {
		return nil, errors.New("logs failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	reader, err := provider.GetContainerLogs(context.Background(), "c1", ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	require.Error(t, err)
	assert.Nil(t, reader)
}

func TestSDKDockerProvider_GetContainerLogs_WithSince(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	reader, err := provider.GetContainerLogs(context.Background(), "c1", ContainerLogsOptions{
		ShowStdout: true,
		Since:      time.Now().Add(-1 * time.Hour),
	})
	require.NoError(t, err)
	if reader != nil {
		reader.Close()
	}
}

// ---------------------------------------------------------------------------
// CreateExec error path (71.4% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_CreateExec_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	es := mc.Exec().(*mock.ExecService)
	es.OnCreate = func(_ context.Context, _ string, _ *domain.ExecConfig) (string, error) {
		return "", errors.New("exec create failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	_, err := provider.CreateExec(context.Background(), "c1", &domain.ExecConfig{Cmd: []string{"ls"}})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// StartExec error path (71.4% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_StartExec_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	es := mc.Exec().(*mock.ExecService)
	es.OnStart = func(_ context.Context, _ string, _ domain.ExecStartOptions) (*domain.HijackedResponse, error) {
		return nil, errors.New("exec start failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	_, err := provider.StartExec(context.Background(), "exec-1", domain.ExecStartOptions{})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// InspectExec error path (66.7% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_InspectExec_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	es := mc.Exec().(*mock.ExecService)
	es.OnInspect = func(_ context.Context, _ string) (*domain.ExecInspect, error) {
		return nil, errors.New("exec inspect failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	_, err := provider.InspectExec(context.Background(), "exec-1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// PullImage error path (69.2% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_PullImage_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	is := mc.Images().(*mock.ImageService)
	is.OnPullAndWait = func(_ context.Context, _ domain.PullOptions) error {
		return errors.New("pull failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	err := provider.PullImage(context.Background(), "alpine:latest")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// HasImageLocally error path (66.7% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_HasImageLocally_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	is := mc.Images().(*mock.ImageService)
	is.OnExists = func(_ context.Context, _ string) (bool, error) {
		return false, errors.New("image check failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	exists, err := provider.HasImageLocally(context.Background(), "alpine:latest")
	require.Error(t, err)
	assert.False(t, exists)
}

// ---------------------------------------------------------------------------
// ConnectNetwork error path (66.7% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_ConnectNetwork_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ns := mc.Networks().(*mock.NetworkService)
	ns.OnConnect = func(_ context.Context, _, _ string, _ *domain.EndpointSettings) error {
		return errors.New("connect failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	err := provider.ConnectNetwork(context.Background(), "net-1", "c1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// FindNetworkByName error path (71.4% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_FindNetworkByName_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ns := mc.Networks().(*mock.NetworkService)
	ns.OnList = func(_ context.Context, _ domain.NetworkListOptions) ([]domain.Network, error) {
		return nil, errors.New("list failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	networks, err := provider.FindNetworkByName(context.Background(), "my-network")
	require.Error(t, err)
	assert.Nil(t, networks)
}

// ---------------------------------------------------------------------------
// Info error path (66.7% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_Info_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ss := mc.System().(*mock.SystemService)
	ss.OnInfo = func(_ context.Context) (*domain.SystemInfo, error) {
		return nil, errors.New("info failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	info, err := provider.Info(context.Background())
	require.Error(t, err)
	assert.Nil(t, info)
}

// ---------------------------------------------------------------------------
// WaitForServiceTasks (0% → 100%)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_WaitForServiceTasks_Success(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ss := mc.Services().(*mock.SwarmService)
	ss.SetTasks([]domain.Task{
		{ID: "task-1", ServiceID: "svc-1", Status: domain.TaskStatus{State: domain.TaskStateComplete}},
	})
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	tasks, err := provider.WaitForServiceTasks(context.Background(), "svc-1", 5*time.Second)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
}

func TestSDKDockerProvider_WaitForServiceTasks_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ss := mc.Services().(*mock.SwarmService)
	// Mock ListTasks to fail, which WaitForServiceTasks delegates to
	ss.OnListTasks = func(_ context.Context, _ domain.TaskListOptions) ([]domain.Task, error) {
		return nil, errors.New("list tasks failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	tasks, err := provider.WaitForServiceTasks(context.Background(), "svc-1", 5*time.Second)
	require.Error(t, err)
	assert.Nil(t, tasks)
}

// ---------------------------------------------------------------------------
// logDebug without logger (50% → higher)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_logDebug_NilLogger(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mc, nil, nil)

	// Should not panic
	provider.logDebug("test %s", "message")
	provider.logNotice("test %s", "message")
}

// ---------------------------------------------------------------------------
// recordOperation/recordError without metrics (testing nil metrics path)
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_RecordOperationWithoutMetrics(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), nil)

	// These should not panic even without metrics
	provider.recordOperation("test")
	provider.recordError("test")
}

// ---------------------------------------------------------------------------
// CreateService/InspectService/ListTasks/RemoveService error paths
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_CreateService_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ss := mc.Services().(*mock.SwarmService)
	ss.OnCreate = func(_ context.Context, _ domain.ServiceSpec, _ domain.ServiceCreateOptions) (string, error) {
		return "", errors.New("create service failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	_, err := provider.CreateService(context.Background(), domain.ServiceSpec{}, domain.ServiceCreateOptions{})
	require.Error(t, err)
}

func TestSDKDockerProvider_InspectService_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ss := mc.Services().(*mock.SwarmService)
	ss.OnInspect = func(_ context.Context, _ string) (*domain.Service, error) {
		return nil, errors.New("inspect service failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	_, err := provider.InspectService(context.Background(), "svc-1")
	require.Error(t, err)
}

func TestSDKDockerProvider_ListTasks_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ss := mc.Services().(*mock.SwarmService)
	ss.OnListTasks = func(_ context.Context, _ domain.TaskListOptions) ([]domain.Task, error) {
		return nil, errors.New("list tasks failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	_, err := provider.ListTasks(context.Background(), domain.TaskListOptions{})
	require.Error(t, err)
}

func TestSDKDockerProvider_RemoveService_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ss := mc.Services().(*mock.SwarmService)
	ss.OnRemove = func(_ context.Context, _ string) error {
		return errors.New("remove service failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	err := provider.RemoveService(context.Background(), "svc-1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Ping error path
// ---------------------------------------------------------------------------

func TestSDKDockerProvider_Ping_Error(t *testing.T) {
	t.Parallel()
	mc := mock.NewDockerClient()
	ss := mc.System().(*mock.SystemService)
	ss.OnPing = func(_ context.Context) (*domain.PingResponse, error) {
		return nil, errors.New("ping failed")
	}
	pm := NewPerformanceMetrics()
	provider := NewSDKDockerProviderFromClient(mc, test.NewTestLogger(), pm)

	err := provider.Ping(context.Background())
	require.Error(t, err)
}
