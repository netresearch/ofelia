//go:build integration
// +build integration

package core

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

const ServiceImageFixture = "test-image"

type runServiceJobTestHelper struct {
	mockClient *mock.DockerClient
	provider   *SDKDockerProvider
	logger     Logger
}

func setupRunServiceJobTest(t *testing.T) *runServiceJobTestHelper {
	t.Helper()

	l := logrus.New()
	l.Formatter = &logrus.TextFormatter{DisableTimestamp: true}

	helper := &runServiceJobTestHelper{
		mockClient: mock.NewDockerClient(),
		logger:     &LogrusAdapter{Logger: l},
	}
	helper.provider = &SDKDockerProvider{
		client: helper.mockClient,
	}

	setupRunServiceMockBehaviors(helper.mockClient)
	return helper
}

func setupRunServiceMockBehaviors(mockClient *mock.DockerClient) {
	services := mockClient.Services().(*mock.SwarmService)
	images := mockClient.Images().(*mock.ImageService)

	// Track created services
	createdServices := make(map[string]*domain.Service)
	serviceCounter := 0

	services.OnCreate = func(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
		serviceCounter++
		serviceID := fmt.Sprintf("service-%d", serviceCounter)
		createdServices[serviceID] = &domain.Service{
			ID:   serviceID,
			Spec: spec,
		}
		return serviceID, nil
	}

	services.OnInspect = func(ctx context.Context, serviceID string) (*domain.Service, error) {
		if svc, ok := createdServices[serviceID]; ok {
			return svc, nil
		}
		return &domain.Service{ID: serviceID}, nil
	}

	services.OnRemove = func(ctx context.Context, serviceID string) error {
		delete(createdServices, serviceID)
		return nil
	}

	services.OnListTasks = func(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
		tasks := make([]domain.Task, 0)
		for _, svc := range createdServices {
			task := domain.Task{
				ID:        "task-" + svc.ID,
				ServiceID: svc.ID,
				Status: domain.TaskStatus{
					State: domain.TaskStateComplete,
					ContainerStatus: &domain.ContainerStatus{
						ExitCode: 0,
					},
				},
				Spec: domain.TaskSpec{
					ContainerSpec: domain.ContainerSpec{
						Command: svc.Spec.TaskTemplate.ContainerSpec.Command,
					},
				},
			}
			tasks = append(tasks, task)
		}
		return tasks, nil
	}

	images.OnExists = func(ctx context.Context, image string) (bool, error) {
		return true, nil
	}

	images.OnPull = func(ctx context.Context, opts domain.PullOptions) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("")), nil
	}
}

func TestRunServiceJob_Run(t *testing.T) {
	h := setupRunServiceJobTest(t)

	job := &RunServiceJob{
		BareJob: BareJob{
			Name:    "test-service",
			Command: `echo -a foo bar`,
		},
		Image:   ServiceImageFixture,
		User:    "foo",
		TTY:     true,
		Delete:  "true",
		Network: "foo",
	}
	job.Provider = h.provider

	e, err := NewExecution()
	require.NoError(t, err)

	err = job.Run(&Context{Execution: e, Logger: h.logger})
	require.NoError(t, err)

	// Verify service was created
	services := h.mockClient.Services().(*mock.SwarmService)
	assert.True(t, len(services.CreateCalls) > 0, "expected service to be created")
}

func TestRunServiceJob_ParseRepositoryTagBareImage(t *testing.T) {
	ref := domain.ParseRepositoryTag("foo")
	assert.Equal(t, "foo", ref.Repository)
	assert.Equal(t, "latest", ref.Tag)
}

func TestRunServiceJob_ParseRepositoryTagVersion(t *testing.T) {
	ref := domain.ParseRepositoryTag("foo:qux")
	assert.Equal(t, "foo", ref.Repository)
	assert.Equal(t, "qux", ref.Tag)
}

func TestRunServiceJob_ParseRepositoryTagRegistry(t *testing.T) {
	ref := domain.ParseRepositoryTag("quay.io/srcd/rest:qux")
	assert.Equal(t, "quay.io/srcd/rest", ref.Repository)
	assert.Equal(t, "qux", ref.Tag)
}
