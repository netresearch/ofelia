//go:build integration
// +build integration

package core

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/sirupsen/logrus"
	. "gopkg.in/check.v1"
)

const (
	ImageFixture  = "test-image"
	watchDuration = time.Millisecond * 500
)

type SuiteRunJob struct {
	mockClient *mock.DockerClient
	provider   *SDKDockerProvider
}

var _ = Suite(&SuiteRunJob{})

func (s *SuiteRunJob) SetUpTest(c *C) {
	s.mockClient = mock.NewDockerClient()
	s.provider = &SDKDockerProvider{
		client: s.mockClient,
	}

	// Set up mock behaviors
	s.setupMockBehaviors()
}

func (s *SuiteRunJob) setupMockBehaviors() {
	containers := s.mockClient.Containers().(*mock.ContainerService)
	images := s.mockClient.Images().(*mock.ImageService)

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

func (s *SuiteRunJob) TestRun(c *C) {
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
	job.Provider = s.provider

	exec, err := NewExecution()
	if err != nil {
		c.Fatal(err)
	}
	ctx := &Context{Job: job, Execution: exec}
	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{DisableTimestamp: true}
	ctx.Logger = &LogrusAdapter{Logger: logger}

	err = job.Run(ctx)
	c.Assert(err, IsNil)

	// Verify container was created with correct parameters
	containers := s.mockClient.Containers().(*mock.ContainerService)
	c.Assert(len(containers.CreateCalls) > 0, Equals, true)
}

func (s *SuiteRunJob) TestRunFailed(c *C) {
	// Set up mock to return non-zero exit code
	containers := s.mockClient.Containers().(*mock.ContainerService)
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
	job.Provider = s.provider

	exec, err := NewExecution()
	if err != nil {
		c.Fatal(err)
	}
	ctx := &Context{Job: job, Execution: exec}
	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{DisableTimestamp: true}
	ctx.Logger = &LogrusAdapter{Logger: logger}

	ctx.Start()
	err = job.Run(ctx)
	ctx.Stop(err)

	c.Assert(err, NotNil)
	c.Assert(ctx.Execution.Failed, Equals, true)
}

func (s *SuiteRunJob) TestRunWithEntrypoint(c *C) {
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
	job.Provider = s.provider

	exec, err := NewExecution()
	if err != nil {
		c.Fatal(err)
	}
	ctx := &Context{Job: job, Execution: exec}
	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{DisableTimestamp: true}
	ctx.Logger = &LogrusAdapter{Logger: logger}

	err = job.Run(ctx)
	c.Assert(err, IsNil)

	// Verify container was created
	containers := s.mockClient.Containers().(*mock.ContainerService)
	c.Assert(len(containers.CreateCalls) > 0, Equals, true)
}

// TestParseRepositoryTag tests the domain.ParseRepositoryTag function
func (s *SuiteRunJob) TestParseRepositoryTagBareImage(c *C) {
	ref := domain.ParseRepositoryTag("foo")
	c.Assert(ref.Repository, Equals, "foo")
	c.Assert(ref.Tag, Equals, "latest")
}

func (s *SuiteRunJob) TestParseRepositoryTagVersion(c *C) {
	ref := domain.ParseRepositoryTag("foo:qux")
	c.Assert(ref.Repository, Equals, "foo")
	c.Assert(ref.Tag, Equals, "qux")
}

func (s *SuiteRunJob) TestParseRepositoryTagRegistry(c *C) {
	ref := domain.ParseRepositoryTag("quay.io/srcd/rest:qux")
	c.Assert(ref.Repository, Equals, "quay.io/srcd/rest")
	c.Assert(ref.Tag, Equals, "qux")
}

// Hook up gocheck into the "go test" runner
func TestRunJobIntegration(t *testing.T) { TestingT(t) }
