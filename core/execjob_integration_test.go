//go:build integration
// +build integration

package core

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/sirupsen/logrus"
	. "gopkg.in/check.v1"
)

const ContainerFixture = "test-container"

type SuiteExecJob struct {
	mockClient *mock.DockerClient
	provider   *SDKDockerProvider
}

var _ = Suite(&SuiteExecJob{})

func (s *SuiteExecJob) SetUpTest(c *C) {
	s.mockClient = mock.NewDockerClient()
	s.provider = &SDKDockerProvider{
		client: s.mockClient,
	}

	s.setupMockBehaviors()
}

func (s *SuiteExecJob) setupMockBehaviors() {
	containers := s.mockClient.Containers().(*mock.ContainerService)
	exec := s.mockClient.Exec().(*mock.ExecService)

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
		exec.OnStart(ctx, execID, domain.ExecStartOptions{})
		// Return success
		return 0, nil
	}
}

func (s *SuiteExecJob) TestRun(c *C) {
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
	job.Provider = s.provider

	e, err := NewExecution()
	c.Assert(err, IsNil)

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	err = job.Run(&Context{Execution: e, Logger: &LogrusAdapter{Logger: logger}})
	c.Assert(err, IsNil)

	// Verify exec was run
	exec := s.mockClient.Exec().(*mock.ExecService)
	c.Assert(len(exec.RunCalls) > 0, Equals, true)
}

func (s *SuiteExecJob) TestRunStartExecError(c *C) {
	// Set up mock to return error on start
	exec := s.mockClient.Exec().(*mock.ExecService)
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
	job.Provider = s.provider

	e, err := NewExecution()
	c.Assert(err, IsNil)

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	ctx := &Context{Execution: e, Job: job, Logger: &LogrusAdapter{Logger: logger}}

	ctx.Start()
	err = job.Run(ctx)
	ctx.Stop(err)

	c.Assert(err, NotNil)
	c.Assert(e.Failed, Equals, true)
}

// Hook up gocheck into the "go test" runner
func TestExecJobIntegration(t *testing.T) { TestingT(t) }
