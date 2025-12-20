package cli

// SPDX-License-Identifier: MIT

import (
	// dummyNotifier implements dockerLabelsUpdate for testing
	"context"
	"io"
	"os"
	"testing"
	"time"

	defaults "github.com/creasty/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
)

// dummyNotifier implements dockerLabelsUpdate
type dummyNotifier struct{}

func (d *dummyNotifier) dockerLabelsUpdate(labels map[string]map[string]string) {}

// mockDockerProviderForHandler implements core.DockerProvider for handler tests
type mockDockerProviderForHandler struct {
	containers []domain.Container
	pingErr    error
}

func (m *mockDockerProviderForHandler) CreateContainer(ctx context.Context, config *domain.ContainerConfig, name string) (string, error) {
	return "test-container", nil
}

func (m *mockDockerProviderForHandler) StartContainer(ctx context.Context, containerID string) error {
	return nil
}

func (m *mockDockerProviderForHandler) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	return nil
}

func (m *mockDockerProviderForHandler) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	return nil
}

func (m *mockDockerProviderForHandler) InspectContainer(ctx context.Context, containerID string) (*domain.Container, error) {
	return &domain.Container{ID: containerID}, nil
}

func (m *mockDockerProviderForHandler) ListContainers(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error) {
	return m.containers, nil
}

func (m *mockDockerProviderForHandler) WaitContainer(ctx context.Context, containerID string) (int64, error) {
	return 0, nil
}

func (m *mockDockerProviderForHandler) GetContainerLogs(ctx context.Context, containerID string, opts core.ContainerLogsOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockDockerProviderForHandler) CreateExec(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
	return "exec-id", nil
}

func (m *mockDockerProviderForHandler) StartExec(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	return nil, nil
}

func (m *mockDockerProviderForHandler) InspectExec(ctx context.Context, execID string) (*domain.ExecInspect, error) {
	return &domain.ExecInspect{ExitCode: 0}, nil
}

func (m *mockDockerProviderForHandler) RunExec(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
	return 0, nil
}

func (m *mockDockerProviderForHandler) PullImage(ctx context.Context, image string) error {
	return nil
}

func (m *mockDockerProviderForHandler) HasImageLocally(ctx context.Context, image string) (bool, error) {
	return true, nil
}

func (m *mockDockerProviderForHandler) EnsureImage(ctx context.Context, image string, forcePull bool) error {
	return nil
}

func (m *mockDockerProviderForHandler) ConnectNetwork(ctx context.Context, networkID, containerID string) error {
	return nil
}

func (m *mockDockerProviderForHandler) FindNetworkByName(ctx context.Context, networkName string) ([]domain.Network, error) {
	return nil, nil
}

func (m *mockDockerProviderForHandler) SubscribeEvents(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
	eventCh := make(chan domain.Event)
	errCh := make(chan error)
	return eventCh, errCh
}

func (m *mockDockerProviderForHandler) CreateService(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
	return "service-id", nil
}

func (m *mockDockerProviderForHandler) InspectService(ctx context.Context, serviceID string) (*domain.Service, error) {
	return nil, nil
}

func (m *mockDockerProviderForHandler) ListTasks(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
	return nil, nil
}

func (m *mockDockerProviderForHandler) RemoveService(ctx context.Context, serviceID string) error {
	return nil
}

func (m *mockDockerProviderForHandler) WaitForServiceTasks(ctx context.Context, serviceID string, timeout time.Duration) ([]domain.Task, error) {
	return nil, nil
}

func (m *mockDockerProviderForHandler) Info(ctx context.Context) (*domain.SystemInfo, error) {
	return &domain.SystemInfo{}, nil
}

func (m *mockDockerProviderForHandler) Ping(ctx context.Context) error {
	return m.pingErr
}

func (m *mockDockerProviderForHandler) Close() error {
	return nil
}

// newBaseConfig creates a Config with logger, docker handler, and scheduler ready
func newBaseConfig() *Config {
	cfg := NewConfig(&TestLogger{})
	cfg.logger = &TestLogger{}
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(&TestLogger{})
	cfg.buildSchedulerMiddlewares(cfg.sh)
	return cfg
}

func addRunJobsToScheduler(cfg *Config) {
	for name, j := range cfg.RunJobs {
		_ = defaults.Set(j)
		j.Name = name
		_ = cfg.sh.AddJob(j)
	}
}

func addExecJobsToScheduler(cfg *Config) {
	for name, j := range cfg.ExecJobs {
		_ = defaults.Set(j)
		j.Name = name
		_ = cfg.sh.AddJob(j)
	}
}

func assertKeepsIniJobs(t *testing.T, cfg *Config, jobsCount func() int) {
	t.Helper()
	assert.Len(t, cfg.sh.Entries(), 1)
	cfg.dockerLabelsUpdate(map[string]map[string]string{})
	assert.Equal(t, 1, jobsCount())
	assert.Len(t, cfg.sh.Entries(), 1)
}

// TestBuildSDKProviderError verifies that buildSDKProvider returns an error when DOCKER_HOST is invalid
func TestBuildSDKProviderError(t *testing.T) {
	orig := os.Getenv("DOCKER_HOST")
	defer os.Setenv("DOCKER_HOST", orig)
	os.Setenv("DOCKER_HOST", "=")

	h := &DockerHandler{ctx: context.Background(), logger: &TestLogger{}}
	_, err := h.buildSDKProvider()
	assert.Error(t, err)
}

// TestNewDockerHandlerErrorPing verifies that NewDockerHandler returns an error when Ping fails
func TestNewDockerHandlerErrorPing(t *testing.T) {
	// Create a mock provider that fails Ping
	mockProvider := &mockDockerProviderForHandler{
		pingErr: ErrNoContainerWithOfeliaEnabled, // Use any error
	}

	notifier := &dummyNotifier{}
	handler, err := NewDockerHandler(context.Background(), notifier, &TestLogger{}, &DockerConfig{}, mockProvider)
	assert.Nil(t, handler)
	assert.Error(t, err)
}

// TestGetDockerLabelsInvalidFilter verifies that GetDockerLabels returns an error on invalid filter strings
func TestGetDockerLabelsInvalidFilter(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{}
	h := &DockerHandler{filters: []string{"invalidfilter"}, logger: &TestLogger{}, ctx: context.Background(), dockerProvider: mockProvider}
	_, err := h.GetDockerLabels()
	require.Error(t, err)
	assert.Regexp(t, `(?s)invalid docker filter "invalidfilter".*key=value format.*`, err.Error())
}

// TestGetDockerLabelsNoContainers verifies that GetDockerLabels returns ErrNoContainerWithOfeliaEnabled when no containers match
func TestGetDockerLabelsNoContainers(t *testing.T) {
	t.Parallel()
	// Mock provider returning empty container list
	mockProvider := &mockDockerProviderForHandler{containers: []domain.Container{}}

	h := &DockerHandler{filters: []string{}, logger: &TestLogger{}, ctx: context.Background(), dockerProvider: mockProvider}
	_, err := h.GetDockerLabels()
	assert.Equal(t, ErrNoContainerWithOfeliaEnabled, err)
}

// TestGetDockerLabelsValid verifies that GetDockerLabels filters and returns only ofelia-prefixed labels
func TestGetDockerLabelsValid(t *testing.T) {
	t.Parallel()
	// Mock provider returning one container with mixed labels
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{
			{
				Name: "cont1",
				Labels: map[string]string{
					"ofelia.enabled":               "true",
					"ofelia.job-exec.foo.schedule": "@every 1s",
					"ofelia.job-run.bar.schedule":  "@every 2s",
					"other.label":                  "ignore",
				},
			},
		},
	}

	h := &DockerHandler{filters: []string{}, logger: &TestLogger{}, ctx: context.Background(), dockerProvider: mockProvider}
	labels, err := h.GetDockerLabels()
	require.NoError(t, err)

	expected := map[string]map[string]string{
		"cont1": {
			"ofelia.enabled":               "true",
			"ofelia.job-exec.foo.schedule": "@every 1s",
			"ofelia.job-run.bar.schedule":  "@every 2s",
		},
	}
	assert.Equal(t, expected, labels)
}

// TestWatchConfigInvalidInterval verifies that watchConfig exits immediately when
// configPollInterval is zero or negative.
func TestWatchConfigInvalidInterval(t *testing.T) {
	t.Parallel()
	h := &DockerHandler{configPollInterval: 0, notifier: &dummyNotifier{}, logger: &TestLogger{}, ctx: context.Background(), cancel: func() {}}
	done := make(chan struct{})
	go func() {
		h.watchConfig()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(time.Millisecond * 50):
		t.Error("watchConfig did not return for zero interval")
	}

	h = &DockerHandler{configPollInterval: -time.Second, notifier: &dummyNotifier{}, logger: &TestLogger{}, ctx: context.Background(), cancel: func() {}}
	done = make(chan struct{})
	go func() {
		h.watchConfig()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(time.Millisecond * 50):
		t.Error("watchConfig did not return for negative interval")
	}
}

// TestDockerLabelsUpdateKeepsIniRunJobs verifies that RunJobs defined via INI
// remain when dockerLabelsUpdate receives no labeled containers.
func TestDockerLabelsUpdateKeepsIniRunJobs(t *testing.T) {
	cfg := newBaseConfig()

	cfg.RunJobs["ini-job"] = &RunJobConfig{RunJob: core.RunJob{BareJob: core.BareJob{Schedule: "@hourly", Command: "echo"}}, JobSource: JobSourceINI}

	addRunJobsToScheduler(cfg)

	assertKeepsIniJobs(t, cfg, func() int { return len(cfg.RunJobs) })
}

// TestDockerLabelsUpdateKeepsIniExecJobs verifies that ExecJobs defined via INI
// remain when dockerLabelsUpdate receives no labeled containers.
func TestDockerLabelsUpdateKeepsIniExecJobs(t *testing.T) {
	cfg := newBaseConfig()

	cfg.ExecJobs["ini-exec"] = &ExecJobConfig{ExecJob: core.ExecJob{BareJob: core.BareJob{Schedule: "@hourly", Command: "echo"}}, JobSource: JobSourceINI}

	addExecJobsToScheduler(cfg)

	assertKeepsIniJobs(t, cfg, func() int { return len(cfg.ExecJobs) })
}

// TestResolveConfigDefaults verifies that resolveConfig returns correct defaults
func TestResolveConfigDefaults(t *testing.T) {
	t.Parallel()
	logger := &TestLogger{}
	cfg := &DockerConfig{
		ConfigPollInterval: 10 * time.Second,
		DockerPollInterval: 0,
		PollingFallback:    10 * time.Second,
		UseEvents:          true,
	}

	configPoll, dockerPoll, fallback, useEvents := resolveConfig(cfg, logger)

	assert.Equal(t, 10*time.Second, configPoll)
	assert.Equal(t, time.Duration(0), dockerPoll)
	assert.Equal(t, 10*time.Second, fallback)
	assert.True(t, useEvents)
}

// TestResolveConfigDeprecatedPollInterval verifies backward compatibility migration
func TestResolveConfigDeprecatedPollInterval(t *testing.T) {
	t.Parallel()
	logger := &TestLogger{}
	cfg := &DockerConfig{
		PollInterval:       30 * time.Second, // deprecated, explicitly set
		ConfigPollInterval: 10 * time.Second, // default
		DockerPollInterval: 0,
		PollingFallback:    10 * time.Second, // default
		UseEvents:          false,            // events disabled
	}

	configPoll, dockerPoll, fallback, useEvents := resolveConfig(cfg, logger)

	// With deprecated poll-interval and default values, should migrate
	assert.Equal(t, 30*time.Second, configPoll) // migrated from poll-interval
	assert.Equal(t, 30*time.Second, dockerPoll) // migrated when events disabled
	assert.Equal(t, 30*time.Second, fallback)   // migrated from poll-interval
	assert.False(t, useEvents)
}

// TestResolveConfigDeprecatedPollIntervalExplicitOverride verifies explicit options override deprecated
func TestResolveConfigDeprecatedPollIntervalExplicitOverride(t *testing.T) {
	t.Parallel()
	logger := &TestLogger{}
	cfg := &DockerConfig{
		PollInterval:       30 * time.Second, // deprecated
		ConfigPollInterval: 20 * time.Second, // explicitly set (not default)
		DockerPollInterval: 15 * time.Second, // explicitly set
		PollingFallback:    5 * time.Second,  // explicitly set (not default)
		UseEvents:          true,
	}

	configPoll, dockerPoll, fallback, useEvents := resolveConfig(cfg, logger)

	// Explicit values should take precedence
	assert.Equal(t, 20*time.Second, configPoll) // kept explicit value
	assert.Equal(t, 15*time.Second, dockerPoll) // kept explicit value
	assert.Equal(t, 5*time.Second, fallback)    // kept explicit value
	assert.True(t, useEvents)
}

// TestResolveConfigDeprecatedDisablePolling verifies no-poll migration
func TestResolveConfigDeprecatedDisablePolling(t *testing.T) {
	t.Parallel()
	logger := &TestLogger{}
	cfg := &DockerConfig{
		DisablePolling:     true, // deprecated no-poll
		ConfigPollInterval: 10 * time.Second,
		DockerPollInterval: 30 * time.Second,
		PollingFallback:    10 * time.Second,
		UseEvents:          true,
	}

	configPoll, dockerPoll, fallback, useEvents := resolveConfig(cfg, logger)

	assert.Equal(t, 10*time.Second, configPoll)
	assert.Equal(t, time.Duration(0), dockerPoll) // disabled by no-poll
	assert.Equal(t, time.Duration(0), fallback)   // also disabled
	assert.True(t, useEvents)
}

// TestWatchContainerPollingInvalidInterval verifies watchContainerPolling exits immediately
// when dockerPollInterval is zero or negative.
func TestWatchContainerPollingInvalidInterval(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{}
	h := &DockerHandler{
		dockerPollInterval: 0,
		notifier:           &dummyNotifier{},
		logger:             &TestLogger{},
		ctx:                context.Background(),
		dockerProvider:     mockProvider,
	}
	done := make(chan struct{})
	go func() {
		h.watchContainerPolling()
		close(done)
	}()

	select {
	case <-done:
		// ok - should return immediately for zero interval
	case <-time.After(time.Millisecond * 50):
		t.Error("watchContainerPolling did not return for zero interval")
	}

	// Test negative interval
	h = &DockerHandler{
		dockerPollInterval: -time.Second,
		notifier:           &dummyNotifier{},
		logger:             &TestLogger{},
		ctx:                context.Background(),
		dockerProvider:     mockProvider,
	}
	done = make(chan struct{})
	go func() {
		h.watchContainerPolling()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(time.Millisecond * 50):
		t.Error("watchContainerPolling did not return for negative interval")
	}
}

// TestWatchContainerPollingContextCancellation verifies watchContainerPolling exits on context cancellation
func TestWatchContainerPollingContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{},
	}
	h := &DockerHandler{
		dockerPollInterval: 100 * time.Millisecond,
		notifier:           &dummyNotifier{},
		logger:             &TestLogger{},
		ctx:                ctx,
		dockerProvider:     mockProvider,
	}

	done := make(chan struct{})
	go func() {
		h.watchContainerPolling()
		close(done)
	}()

	// Cancel context after short delay
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// ok - should exit on context cancellation
	case <-time.After(time.Millisecond * 200):
		t.Error("watchContainerPolling did not exit on context cancellation")
	}
}

// TestStartFallbackPollingAlreadyActive verifies startFallbackPolling returns early if already active
func TestStartFallbackPollingAlreadyActive(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &DockerHandler{
		pollingFallback:       100 * time.Millisecond,
		notifier:              &dummyNotifier{},
		logger:                &TestLogger{},
		ctx:                   ctx,
		dockerProvider:        mockProvider,
		fallbackPollingActive: true, // Already active
	}

	done := make(chan struct{})
	go func() {
		h.startFallbackPolling()
		close(done)
	}()

	// Should return immediately since already active
	select {
	case <-done:
		// ok - returned early
	case <-time.After(time.Millisecond * 50):
		t.Error("startFallbackPolling did not return early when already active")
	}
}

// TestStartFallbackPollingCancellation verifies fallback polling stops when canceled
func TestStartFallbackPollingCancellation(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &DockerHandler{
		pollingFallback: 100 * time.Millisecond,
		notifier:        &dummyNotifier{},
		logger:          &TestLogger{},
		ctx:             ctx,
		dockerProvider:  mockProvider,
	}

	done := make(chan struct{})
	go func() {
		h.startFallbackPolling()
		close(done)
	}()

	// Wait for fallback to start
	time.Sleep(20 * time.Millisecond)

	// Verify fallbackCancel was set
	h.mu.Lock()
	assert.True(t, h.fallbackPollingActive)
	assert.NotNil(t, h.fallbackCancel)
	fallbackCancel := h.fallbackCancel
	h.mu.Unlock()

	// Cancel the fallback polling
	fallbackCancel()

	select {
	case <-done:
		// ok - should exit on cancellation
	case <-time.After(time.Millisecond * 200):
		t.Error("startFallbackPolling did not exit on cancellation")
	}

	// Verify state was reset
	h.mu.Lock()
	assert.False(t, h.fallbackPollingActive)
	assert.Nil(t, h.fallbackCancel)
	h.mu.Unlock()
}

// TestClearEventStreamErrorStopsFallback verifies that clearing event error stops fallback polling
func TestClearEventStreamErrorStopsFallback(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &DockerHandler{
		pollingFallback: 100 * time.Millisecond,
		notifier:        &dummyNotifier{},
		logger:          &TestLogger{},
		ctx:             ctx,
		dockerProvider:  mockProvider,
	}

	// Start fallback polling
	done := make(chan struct{})
	go func() {
		h.startFallbackPolling()
		close(done)
	}()

	// Wait for fallback to start
	time.Sleep(20 * time.Millisecond)

	h.mu.Lock()
	assert.True(t, h.fallbackPollingActive)
	h.mu.Unlock()

	// Simulate events recovering - this should stop fallback polling
	h.clearEventStreamError()

	select {
	case <-done:
		// ok - fallback polling should have stopped
	case <-time.After(time.Millisecond * 200):
		t.Error("clearEventStreamError did not stop fallback polling")
	}

	// Verify state
	h.mu.Lock()
	assert.False(t, h.eventsFailed)
	assert.False(t, h.fallbackPollingActive)
	h.mu.Unlock()
}
