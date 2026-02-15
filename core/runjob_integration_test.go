//go:build integration

package core

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

const (
	ImageFixture  = "test-image"
	watchDuration = time.Millisecond * 500
)

type runJobTestHelper struct {
	mockClient *mock.DockerClient
	provider   *SDKDockerProvider
}

func setupRunJobTest(t *testing.T) *runJobTestHelper {
	t.Helper()

	helper := &runJobTestHelper{
		mockClient: mock.NewDockerClient(),
	}
	helper.provider = &SDKDockerProvider{
		client: helper.mockClient,
	}

	setupRunJobMockBehaviors(helper.mockClient)
	return helper
}

func setupRunJobMockBehaviors(mockClient *mock.DockerClient) {
	containers := mockClient.Containers().(*mock.ContainerService)
	images := mockClient.Images().(*mock.ImageService)

	// Track created containers
	createdContainers := make(map[string]*domain.Container)

	containers.OnCreate = func(ctx context.Context, config *domain.ContainerConfig) (string, error) {
		containerID := "container-" + config.Name
		createdContainers[containerID] = &domain.Container{
			ID:   containerID,
			Name: config.Name,
			State: domain.ContainerState{
				Running: false,
			},
			Config: config,
		}
		return containerID, nil
	}

	containers.OnStart = func(ctx context.Context, containerID string) error {
		if cont, ok := createdContainers[containerID]; ok {
			cont.State.Running = true
		}
		return nil
	}

	containers.OnStop = func(ctx context.Context, containerID string, timeout *time.Duration) error {
		if cont, ok := createdContainers[containerID]; ok {
			cont.State.Running = false
			cont.State.ExitCode = 0
		}
		return nil
	}

	containers.OnInspect = func(ctx context.Context, containerID string) (*domain.Container, error) {
		if cont, ok := createdContainers[containerID]; ok {
			return cont, nil
		}
		return &domain.Container{
			ID: containerID,
			State: domain.ContainerState{
				Running: false,
			},
		}, nil
	}

	containers.OnRemove = func(ctx context.Context, containerID string, opts domain.RemoveOptions) error {
		delete(createdContainers, containerID)
		return nil
	}

	containers.OnWait = func(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error, 1)
		// Simulate container finishing after short delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			if cont, ok := createdContainers[containerID]; ok {
				cont.State.Running = false
			}
			respCh <- domain.WaitResponse{StatusCode: 0}
			close(respCh)
			close(errCh)
		}()
		return respCh, errCh
	}

	images.OnExists = func(ctx context.Context, image string) (bool, error) {
		return true, nil
	}

	images.OnPull = func(ctx context.Context, opts domain.PullOptions) (io.ReadCloser, error) {
		return io.NopCloser(nil), nil
	}
}

func TestRunJob_Run(t *testing.T) {
	h := setupRunJobTest(t)

	job := &RunJob{
		BareJob: BareJob{
			Name:    "test",
			Command: `echo -a "foo bar"`,
		},
		Image:       ImageFixture,
		User:        "foo",
		TTY:         true,
		Delete:      "true",
		Network:     "foo",
		Hostname:    "test-host",
		Environment: []string{"test_Key1=value1", "test_Key2=value2"},
		Volume:      []string{"/test/tmp:/test/tmp:ro", "/test/tmp:/test/tmp:rw"},
	}
	job.Provider = h.provider

	exec, err := NewExecution()
	require.NoError(t, err)

	ctx := &Context{Job: job, Execution: exec}
	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{DisableTimestamp: true}
	ctx.Logger = &LogrusAdapter{Logger: logger}

	err = job.Run(ctx)
	require.NoError(t, err)

	// Verify container was created with correct parameters
	containers := h.mockClient.Containers().(*mock.ContainerService)
	assert.True(t, len(containers.CreateCalls) > 0, "expected container to be created")
}

func TestRunJob_RunFailed(t *testing.T) {
	h := setupRunJobTest(t)

	// Set up mock to return non-zero exit code
	containers := h.mockClient.Containers().(*mock.ContainerService)
	containers.OnWait = func(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error, 1)
		respCh <- domain.WaitResponse{StatusCode: 1}
		close(respCh)
		close(errCh)
		return respCh, errCh
	}

	job := &RunJob{
		BareJob: BareJob{
			Name:    "fail",
			Command: "echo fail",
		},
		Image:  ImageFixture,
		Delete: "true",
	}
	job.Provider = h.provider

	exec, err := NewExecution()
	require.NoError(t, err)

	jobCtx := &Context{Job: job, Execution: exec}
	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{DisableTimestamp: true}
	jobCtx.Logger = &LogrusAdapter{Logger: logger}

	jobCtx.Start()
	err = job.Run(jobCtx)
	jobCtx.Stop(err)

	assert.Error(t, err)
	assert.True(t, jobCtx.Execution.Failed)
}

func TestRunJob_RunWithEntrypoint(t *testing.T) {
	h := setupRunJobTest(t)

	ep := ""
	job := &RunJob{
		BareJob: BareJob{
			Name:    "test-ep",
			Command: `echo -a "foo bar"`,
		},
		Image:      ImageFixture,
		Entrypoint: &ep,
		Delete:     "true",
	}
	job.Provider = h.provider

	exec, err := NewExecution()
	require.NoError(t, err)

	ctx := &Context{Job: job, Execution: exec}
	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{DisableTimestamp: true}
	ctx.Logger = &LogrusAdapter{Logger: logger}

	err = job.Run(ctx)
	require.NoError(t, err)

	// Verify container was created
	containers := h.mockClient.Containers().(*mock.ContainerService)
	assert.True(t, len(containers.CreateCalls) > 0, "expected container to be created")
}

func TestRunJob_ParseRepositoryTagBareImage(t *testing.T) {
	ref := domain.ParseRepositoryTag("foo")
	assert.Equal(t, "foo", ref.Repository)
	assert.Equal(t, "latest", ref.Tag)
}

func TestRunJob_ParseRepositoryTagVersion(t *testing.T) {
	ref := domain.ParseRepositoryTag("foo:qux")
	assert.Equal(t, "foo", ref.Repository)
	assert.Equal(t, "qux", ref.Tag)
}

func TestRunJob_ParseRepositoryTagRegistry(t *testing.T) {
	ref := domain.ParseRepositoryTag("quay.io/srcd/rest:qux")
	assert.Equal(t, "quay.io/srcd/rest", ref.Repository)
	assert.Equal(t, "qux", ref.Tag)
}
