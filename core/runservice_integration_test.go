//go:build integration
// +build integration

package core

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/sirupsen/logrus"
	. "gopkg.in/check.v1"
)

const ServiceImageFixture = "test-image"

type SuiteRunServiceJob struct {
	mockClient *mock.DockerClient
	provider   *SDKDockerProvider
}

var _ = Suite(&SuiteRunServiceJob{})

const logFormat = "%{color}%{shortfile} ▶ %{level}%{color:reset} %{message}"

var logger Logger

func (s *SuiteRunServiceJob) SetUpTest(c *C) {
	l := logrus.New()
	l.Formatter = &logrus.TextFormatter{DisableTimestamp: true}
	logger = &LogrusAdapter{Logger: l}

	s.mockClient = mock.NewDockerClient()
	s.provider = &SDKDockerProvider{
		client: s.mockClient,
	}

	s.setupMockBehaviors()
}

func (s *SuiteRunServiceJob) setupMockBehaviors() {
	services := s.mockClient.Services().(*mock.SwarmService)
	images := s.mockClient.Images().(*mock.ImageService)

	// Track created services
	createdServices := make(map[string]*domain.Service)
	serviceCounter := 0

	services.OnCreate = func(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
		serviceCounter++
		serviceID := "service-" + string(rune('0'+serviceCounter))
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

func (s *SuiteRunServiceJob) TestRun(c *C) {
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
	job.Provider = s.provider

	e, err := NewExecution()
	c.Assert(err, IsNil)

	err = job.Run(&Context{Execution: e, Logger: logger})
	c.Assert(err, IsNil)

	// Verify service was created
	services := s.mockClient.Services().(*mock.SwarmService)
	c.Assert(len(services.CreateCalls) > 0, Equals, true)
}

// TestParseRepositoryTag tests the domain.ParseRepositoryTag function
func (s *SuiteRunServiceJob) TestParseRepositoryTagBareImage(c *C) {
	ref := domain.ParseRepositoryTag("foo")
	c.Assert(ref.Repository, Equals, "foo")
	c.Assert(ref.Tag, Equals, "latest")
}

func (s *SuiteRunServiceJob) TestParseRepositoryTagVersion(c *C) {
	ref := domain.ParseRepositoryTag("foo:qux")
	c.Assert(ref.Repository, Equals, "foo")
	c.Assert(ref.Tag, Equals, "qux")
}

func (s *SuiteRunServiceJob) TestParseRepositoryTagRegistry(c *C) {
	ref := domain.ParseRepositoryTag("quay.io/srcd/rest:qux")
	c.Assert(ref.Repository, Equals, "quay.io/srcd/rest")
	c.Assert(ref.Tag, Equals, "qux")
}

// Hook up gocheck into the "go test" runner
func TestRunServiceJobIntegration(t *testing.T) { TestingT(t) }
