//go:build integration

package core

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

const ContainerFixture = "test-container"

type execJobTestHelper struct {
	mockClient *mock.DockerClient
	provider   *SDKDockerProvider
}

func setupExecJobTest(t *testing.T) *execJobTestHelper {
	t.Helper()

	helper := &execJobTestHelper{
		mockClient: mock.NewDockerClient(),
	}
	helper.provider = &SDKDockerProvider{
		client: helper.mockClient,
	}

	setupExecJobMockBehaviors(helper.mockClient)
	return helper
}

func setupExecJobMockBehaviors(mockClient *mock.DockerClient) {
	containers := mockClient.Containers().(*mock.ContainerService)
	exec := mockClient.Exec().(*mock.ExecService)

	// Track created execs
	createdExecs := make(map[string]*domain.ExecInspect)
	execCounter := 0

	containers.OnInspect = func(ctx context.Context, containerID string) (*domain.Container, error) {
		return &domain.Container{
			ID:   containerID,
			Name: ContainerFixture,
			State: domain.ContainerState{
				Running: true,
			},
		}, nil
	}

	exec.OnCreate = func(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
		execCounter++
		execID := "exec-" + string(rune('0'+execCounter))

		createdExecs[execID] = &domain.ExecInspect{
			ID:       execID,
			Running:  false,
			ExitCode: 0,
			ProcessConfig: &domain.ExecProcessConfig{
				Entrypoint: config.Cmd[0],
				Arguments:  config.Cmd[1:],
				User:       config.User,
				Tty:        config.Tty,
			},
		}
		return execID, nil
	}

	exec.OnStart = func(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
		if e, ok := createdExecs[execID]; ok {
			e.Running = true
		}
		return &domain.HijackedResponse{}, nil
	}

	exec.OnInspect = func(ctx context.Context, execID string) (*domain.ExecInspect, error) {
		if e, ok := createdExecs[execID]; ok {
			e.Running = false
			return e, nil
		}
		return &domain.ExecInspect{
			ID:       execID,
			Running:  false,
			ExitCode: 0,
		}, nil
	}

	exec.OnRun = func(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
		// Create exec
		execID, _ := exec.OnCreate(ctx, containerID, config)
		// Start exec
		_, _ = exec.OnStart(ctx, execID, domain.ExecStartOptions{})
		// Return success
		return 0, nil
	}
}

func TestExecJob_Run(t *testing.T) {
	h := setupExecJobTest(t)

	job := &ExecJob{
		BareJob: BareJob{
			Name:    "test-exec",
			Command: `echo -a "foo bar"`,
		},
		Container:   ContainerFixture,
		User:        "foo",
		TTY:         true,
		Environment: []string{"test_Key1=value1", "test_Key2=value2"},
	}
	job.Provider = h.provider

	e, err := NewExecution()
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	err = job.Run(&Context{Execution: e, Logger: &LogrusAdapter{Logger: logger}})
	require.NoError(t, err)

	// Verify exec was run
	exec := h.mockClient.Exec().(*mock.ExecService)
	assert.True(t, len(exec.RunCalls) > 0, "expected exec to be run")
}

func TestExecJob_RunStartExecError(t *testing.T) {
	h := setupExecJobTest(t)

	// Set up mock to return error on start
	exec := h.mockClient.Exec().(*mock.ExecService)
	exec.OnStart = func(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
		return nil, errors.New("exec start failed")
	}
	exec.OnRun = func(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
		return -1, errors.New("exec run failed")
	}

	job := &ExecJob{
		BareJob: BareJob{
			Name:    "fail-exec",
			Command: "echo foo",
		},
		Container: ContainerFixture,
	}
	job.Provider = h.provider

	e, err := NewExecution()
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	ctx := &Context{Execution: e, Job: job, Logger: &LogrusAdapter{Logger: logger}}

	ctx.Start()
	err = job.Run(ctx)
	ctx.Stop(err)

	assert.Error(t, err)
	assert.True(t, e.Failed)
}
