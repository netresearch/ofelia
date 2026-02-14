package cli

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
)

type mockDockerProviderForInit struct {
	containers []domain.Container
}

func (m *mockDockerProviderForInit) CreateContainer(ctx context.Context, config *domain.ContainerConfig, name string) (string, error) {
	return "test-container", nil
}

func (m *mockDockerProviderForInit) StartContainer(ctx context.Context, containerID string) error {
	return nil
}

func (m *mockDockerProviderForInit) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	return nil
}

func (m *mockDockerProviderForInit) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	return nil
}

func (m *mockDockerProviderForInit) InspectContainer(ctx context.Context, containerID string) (*domain.Container, error) {
	return &domain.Container{ID: containerID}, nil
}

func (m *mockDockerProviderForInit) ListContainers(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error) {
	return m.containers, nil
}

func (m *mockDockerProviderForInit) WaitContainer(ctx context.Context, containerID string) (int64, error) {
	return 0, nil
}

func (m *mockDockerProviderForInit) GetContainerLogs(ctx context.Context, containerID string, opts core.ContainerLogsOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockDockerProviderForInit) CreateExec(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
	return "exec-id", nil
}

func (m *mockDockerProviderForInit) StartExec(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	return nil, nil
}

func (m *mockDockerProviderForInit) InspectExec(ctx context.Context, execID string) (*domain.ExecInspect, error) {
	return &domain.ExecInspect{ExitCode: 0}, nil
}

func (m *mockDockerProviderForInit) RunExec(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
	return 0, nil
}

func (m *mockDockerProviderForInit) PullImage(ctx context.Context, image string) error {
	return nil
}

func (m *mockDockerProviderForInit) HasImageLocally(ctx context.Context, image string) (bool, error) {
	return true, nil
}

func (m *mockDockerProviderForInit) EnsureImage(ctx context.Context, image string, forcePull bool) error {
	return nil
}

func (m *mockDockerProviderForInit) ConnectNetwork(ctx context.Context, networkID, containerID string) error {
	return nil
}

func (m *mockDockerProviderForInit) FindNetworkByName(ctx context.Context, networkName string) ([]domain.Network, error) {
	return nil, nil
}

func (m *mockDockerProviderForInit) SubscribeEvents(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
	eventCh := make(chan domain.Event)
	errCh := make(chan error)
	return eventCh, errCh
}

func (m *mockDockerProviderForInit) CreateService(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
	return "service-id", nil
}

func (m *mockDockerProviderForInit) InspectService(ctx context.Context, serviceID string) (*domain.Service, error) {
	return nil, nil
}

func (m *mockDockerProviderForInit) ListTasks(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
	return nil, nil
}

func (m *mockDockerProviderForInit) RemoveService(ctx context.Context, serviceID string) error {
	return nil
}

func (m *mockDockerProviderForInit) WaitForServiceTasks(ctx context.Context, serviceID string, timeout time.Duration) ([]domain.Task, error) {
	return nil, nil
}

func (m *mockDockerProviderForInit) Info(ctx context.Context) (*domain.SystemInfo, error) {
	return &domain.SystemInfo{}, nil
}

func (m *mockDockerProviderForInit) Ping(ctx context.Context) error {
	return nil
}

func (m *mockDockerProviderForInit) Close() error {
	return nil
}

func TestInitializeAppSuccess(t *testing.T) {
	// Note: Not parallel - modifies global newDockerHandler
	origFactory := newDockerHandler
	defer func() { newDockerHandler = origFactory }()
	newDockerHandler = func(ctx context.Context, notifier dockerContainersUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return &DockerHandler{
			ctx:                ctx,
			filters:            cfg.Filters,
			notifier:           notifier,
			logger:             logger,
			dockerProvider:     &mockDockerProviderForInit{},
			configPollInterval: cfg.ConfigPollInterval,
			useEvents:          cfg.UseEvents,
			dockerPollInterval: cfg.DockerPollInterval,
			pollingFallback:    cfg.PollingFallback,
		}, nil
	}

	cfg := NewConfig(&TestLogger{})
	cfg.Docker.Filters = []string{}
	err := cfg.InitializeApp()
	require.NoError(t, err)
	assert.NotNil(t, cfg.sh)
	assert.NotNil(t, cfg.dockerHandler)
}

func TestInitializeAppLabelConflict(t *testing.T) {
	// Note: Not parallel - modifies global newDockerHandler
	const iniStr = "[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo ini\n"
	cfg, err := BuildFromString(iniStr, &TestLogger{})
	require.NoError(t, err)

	mockProvider := &mockDockerProviderForInit{
		containers: []domain.Container{
			{
				Name: "cont1",
				Labels: map[string]string{
					"ofelia.enabled":              "true",
					"ofelia.job-run.foo.schedule": "@every 10s",
					"ofelia.job-run.foo.image":    "busybox",
					"ofelia.job-run.foo.command":  "echo label",
				},
			},
		},
	}

	origFactory := newDockerHandler
	defer func() { newDockerHandler = origFactory }()
	newDockerHandler = func(ctx context.Context, notifier dockerContainersUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return &DockerHandler{
			ctx:                ctx,
			filters:            cfg.Filters,
			notifier:           notifier,
			logger:             logger,
			dockerProvider:     mockProvider,
			configPollInterval: 0,
		}, nil
	}

	cfg.logger = &TestLogger{}
	err = cfg.InitializeApp()
	require.NoError(t, err)
	assert.Len(t, cfg.RunJobs, 1)
	j, ok := cfg.RunJobs["foo"]
	assert.True(t, ok)
	assert.Equal(t, "@every 5s", j.GetSchedule())
	assert.Equal(t, JobSourceINI, j.JobSource)
}

func TestInitializeAppComposeConflict(t *testing.T) {
	// Note: Not parallel - modifies global newDockerHandler
	iniStr := "[job-compose \"foo\"]\nschedule = @daily\nfile = docker-compose.yml\n"
	cfg, err := BuildFromString(iniStr, &TestLogger{})
	require.NoError(t, err)

	mockProvider := &mockDockerProviderForInit{
		containers: []domain.Container{
			{
				Name: "cont1",
				Labels: map[string]string{
					"ofelia.enabled":                  "true",
					"ofelia.job-compose.foo.schedule": "@hourly",
					"ofelia.job-compose.foo.file":     "override.yml",
				},
			},
		},
	}

	origFactory := newDockerHandler
	defer func() { newDockerHandler = origFactory }()
	newDockerHandler = func(ctx context.Context, notifier dockerContainersUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return &DockerHandler{ctx: ctx, filters: cfg.Filters, notifier: notifier, logger: logger, dockerProvider: mockProvider, configPollInterval: 0}, nil
	}

	cfg.logger = &TestLogger{}
	err = cfg.InitializeApp()
	require.NoError(t, err)
	j, ok := cfg.ComposeJobs["foo"]
	assert.True(t, ok)
	assert.Equal(t, "docker-compose.yml", j.File)
	assert.Equal(t, JobSourceINI, j.JobSource)
}
