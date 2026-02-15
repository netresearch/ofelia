package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test/testutil"
)

type mockDockerProvider struct{}

func (m *mockDockerProvider) CreateContainer(ctx context.Context, config *domain.ContainerConfig, name string) (string, error) {
	return "test-container", nil
}

func (m *mockDockerProvider) StartContainer(ctx context.Context, containerID string) error {
	return nil
}

func (m *mockDockerProvider) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	return nil
}

func (m *mockDockerProvider) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	return nil
}

func (m *mockDockerProvider) InspectContainer(ctx context.Context, containerID string) (*domain.Container, error) {
	return &domain.Container{ID: containerID}, nil
}

func (m *mockDockerProvider) ListContainers(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error) {
	return []domain.Container{}, nil
}

func (m *mockDockerProvider) WaitContainer(ctx context.Context, containerID string) (int64, error) {
	return 0, nil
}

func (m *mockDockerProvider) GetContainerLogs(ctx context.Context, containerID string, opts core.ContainerLogsOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockDockerProvider) CreateExec(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
	return "exec-id", nil
}

func (m *mockDockerProvider) StartExec(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	return nil, nil
}

func (m *mockDockerProvider) InspectExec(ctx context.Context, execID string) (*domain.ExecInspect, error) {
	return &domain.ExecInspect{ExitCode: 0}, nil
}

func (m *mockDockerProvider) RunExec(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
	return 0, nil
}

func (m *mockDockerProvider) PullImage(ctx context.Context, image string) error {
	return nil
}

func (m *mockDockerProvider) HasImageLocally(ctx context.Context, image string) (bool, error) {
	return true, nil
}

func (m *mockDockerProvider) EnsureImage(ctx context.Context, image string, forcePull bool) error {
	return nil
}

func (m *mockDockerProvider) ConnectNetwork(ctx context.Context, networkID, containerID string) error {
	return nil
}

func (m *mockDockerProvider) FindNetworkByName(ctx context.Context, networkName string) ([]domain.Network, error) {
	return nil, nil
}

func (m *mockDockerProvider) SubscribeEvents(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
	eventCh := make(chan domain.Event)
	errCh := make(chan error)
	return eventCh, errCh
}

func (m *mockDockerProvider) CreateService(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
	return "service-id", nil
}

func (m *mockDockerProvider) InspectService(ctx context.Context, serviceID string) (*domain.Service, error) {
	return nil, nil
}

func (m *mockDockerProvider) ListTasks(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
	return nil, nil
}

func (m *mockDockerProvider) RemoveService(ctx context.Context, serviceID string) error {
	return nil
}

func (m *mockDockerProvider) WaitForServiceTasks(ctx context.Context, serviceID string, timeout time.Duration) ([]domain.Task, error) {
	return nil, nil
}

func (m *mockDockerProvider) Info(ctx context.Context) (*domain.SystemInfo, error) {
	return &domain.SystemInfo{}, nil
}

func (m *mockDockerProvider) Ping(ctx context.Context) error {
	return nil
}

func (m *mockDockerProvider) Close() error {
	return nil
}

type mockDockerLabelsUpdate struct{}

func (m *mockDockerLabelsUpdate) dockerLabelsUpdate(labels map[string]map[string]string) {}

func getAvailableAddress() string {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	return fmt.Sprintf(":%d", listener.Addr().(*net.TCPAddr).Port)
}

func TestSuccessfulBootStartShutdown(t *testing.T) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:      logger,
		EnableWeb:   false,
		EnablePprof: false,
	}

	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:                ctx,
			dockerProvider:     &mockDockerProvider{},
			notifier:           &mockDockerLabelsUpdate{},
			logger:             logger,
			configPollInterval: cfg.ConfigPollInterval,
			useEvents:          cfg.UseEvents,
			dockerPollInterval: cfg.DockerPollInterval,
			pollingFallback:    cfg.PollingFallback,
		}
		return handler, nil
	}

	err := cmd.boot()
	require.NoError(t, err)
	assert.NotNil(t, cmd.scheduler)
	assert.NotNil(t, cmd.shutdownManager)
	assert.NotNil(t, cmd.done)
	assert.NotNil(t, cmd.config)

	err = cmd.start()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = cmd.shutdownManager.Shutdown()
	}()

	err = cmd.shutdown()
	require.NoError(t, err)
}

func TestBootFailureInvalidConfig(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.ini")
	err := os.WriteFile(configFile, []byte("[global\ninvalid-section-header\nkey = value"), 0o644)
	require.NoError(t, err)

	_, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{
		ConfigFile: configFile,
		Logger:     logger,
	}

	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("docker initialization failed")
	}

	err = cmd.boot()
	assert.Error(t, err)
}

func TestBootDockerConnectionFailure(t *testing.T) {
	hook, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{
		Logger:    logger,
		EnableWeb: true,
	}

	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	dockerError := errors.New("cannot connect to Docker daemon")
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, dockerError
	}

	err := cmd.boot()
	require.Error(t, err)
	assert.Regexp(t, ".*Docker daemon.*", err.Error())

	found := false
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "Can't start the app") {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestPprofServerStartup(t *testing.T) {
	hook, logger := newMemoryLogger(logrus.InfoLevel)
	addr := getAvailableAddress()

	cmd := &DaemonCommand{
		Logger:      logger,
		EnablePprof: true,
		PprofAddr:   addr,
		done:        make(chan struct{}),
	}

	cmd.scheduler = core.NewScheduler(logger)
	cmd.shutdownManager = core.NewShutdownManager(logger, 1*time.Second)
	cmd.pprofServer = &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
	}

	err := cmd.start()
	require.NoError(t, err)

	// Poll for log message instead of fixed sleep
	testutil.Eventually(t, func() bool {
		for _, entry := range hook.AllEntries() {
			if strings.Contains(entry.Message, "Starting pprof server") {
				return true
			}
		}
		return false
	}, testutil.WithTimeout(500*time.Millisecond), testutil.WithInterval(10*time.Millisecond))

	// Trigger shutdown asynchronously
	go func() { _ = cmd.shutdownManager.Shutdown() }()
	_ = cmd.shutdown()
}

func TestWebServerStartup(t *testing.T) {
	hook, logger := newMemoryLogger(logrus.InfoLevel)
	addr := getAvailableAddress()

	cmd := &DaemonCommand{
		Logger:    logger,
		EnableWeb: true,
		WebAddr:   addr,
		done:      make(chan struct{}),
	}

	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:                ctx,
			dockerProvider:     &mockDockerProvider{},
			notifier:           &mockDockerLabelsUpdate{},
			logger:             logger,
			configPollInterval: cfg.ConfigPollInterval,
			useEvents:          cfg.UseEvents,
			dockerPollInterval: cfg.DockerPollInterval,
			pollingFallback:    cfg.PollingFallback,
		}
		return handler, nil
	}

	err := cmd.boot()
	require.NoError(t, err)
	assert.NotNil(t, cmd.webServer)

	err = cmd.start()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	found := false
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "Starting web server") {
			found = true
			break
		}
	}
	assert.True(t, found)

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = cmd.shutdownManager.Shutdown()
	}()

	_ = cmd.shutdown()
}

func TestPortBindingConflict(t *testing.T) {
	_, logger := newMemoryLogger(logrus.InfoLevel)

	addr := getAvailableAddress()
	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	defer listener.Close()

	cmd := &DaemonCommand{
		Logger:      logger,
		EnablePprof: true,
		PprofAddr:   addr,
		done:        make(chan struct{}),
	}

	cmd.scheduler = core.NewScheduler(logger)
	cmd.shutdownManager = core.NewShutdownManager(logger, 1*time.Second)
	cmd.pprofServer = &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
	}

	err = cmd.start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pprof server startup failed")
}

func TestGracefulShutdownWithRunningJobs(t *testing.T) {
	_, logger := newMemoryLogger(logrus.InfoLevel)

	cmd := &DaemonCommand{
		Logger:          logger,
		scheduler:       core.NewScheduler(logger),
		shutdownManager: core.NewShutdownManager(logger, 2*time.Second),
		done:            make(chan struct{}),
	}

	err := cmd.start()
	require.NoError(t, err)

	shutdownDone := make(chan error)
	go func() {
		shutdownDone <- cmd.shutdown()
	}()

	time.Sleep(50 * time.Millisecond)
	_ = cmd.shutdownManager.Shutdown()

	select {
	case err := <-shutdownDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown took too long")
	}
}

func TestForcedShutdownOnTimeout(t *testing.T) {
	_, logger := newMemoryLogger(logrus.DebugLevel)

	cmd := &DaemonCommand{
		Logger:          logger,
		shutdownManager: core.NewShutdownManager(logger, 100*time.Millisecond),
		done:            make(chan struct{}),
	}

	cmd.scheduler = core.NewScheduler(logger)

	err := cmd.start()
	require.NoError(t, err)

	shutdownDone := make(chan error)
	go func() {
		shutdownDone <- cmd.shutdown()
	}()

	_ = cmd.shutdownManager.Shutdown()

	select {
	case err := <-shutdownDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown took too long even with timeout")
	}
}

func TestConfigurationOptionApplication(t *testing.T) {
	_, logger := newMemoryLogger(logrus.InfoLevel)

	pollInterval := 30 * time.Second
	cmd := &DaemonCommand{
		Logger:             logger,
		DockerFilters:      []string{"label=test"},
		DockerPollInterval: &pollInterval,
		EnableWeb:          true,
		WebAddr:            ":9999",
		EnablePprof:        true,
		PprofAddr:          "127.0.0.1:9998",
		LogLevel:           "DEBUG",
	}

	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:                ctx,
			dockerProvider:     &mockDockerProvider{},
			notifier:           &mockDockerLabelsUpdate{},
			logger:             logger,
			configPollInterval: cfg.ConfigPollInterval,
			useEvents:          cfg.UseEvents,
			dockerPollInterval: cfg.DockerPollInterval,
			pollingFallback:    cfg.PollingFallback,
		}
		return handler, nil
	}

	err := cmd.boot()
	require.NoError(t, err)

	assert.Equal(t, []string{"label=test"}, cmd.config.Docker.Filters)
	assert.Equal(t, pollInterval, cmd.config.Docker.PollInterval)
	assert.True(t, cmd.EnableWeb)
	assert.Equal(t, ":9999", cmd.WebAddr)
	assert.True(t, cmd.EnablePprof)
	assert.Equal(t, "127.0.0.1:9998", cmd.PprofAddr)
}

func TestConcurrentServerStartup(t *testing.T) {
	hook, logger := newMemoryLogger(logrus.InfoLevel)

	pprofAddr := getAvailableAddress()
	webAddr := getAvailableAddress()

	cmd := &DaemonCommand{
		Logger:      logger,
		EnableWeb:   true,
		WebAddr:     webAddr,
		EnablePprof: true,
		PprofAddr:   pprofAddr,
		done:        make(chan struct{}),
	}

	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:                ctx,
			dockerProvider:     &mockDockerProvider{},
			notifier:           &mockDockerLabelsUpdate{},
			logger:             logger,
			configPollInterval: cfg.ConfigPollInterval,
			useEvents:          cfg.UseEvents,
			dockerPollInterval: cfg.DockerPollInterval,
			pollingFallback:    cfg.PollingFallback,
		}
		return handler, nil
	}

	err := cmd.boot()
	require.NoError(t, err)

	err = cmd.start()
	require.NoError(t, err)

	testutil.Eventually(t, func() bool {
		pprofFound := false
		webFound := false
		for _, entry := range hook.AllEntries() {
			if strings.Contains(entry.Message, "Starting pprof server") {
				pprofFound = true
			}
			if strings.Contains(entry.Message, "Starting web server") {
				webFound = true
			}
		}
		return pprofFound && webFound
	}, testutil.WithTimeout(500*time.Millisecond), testutil.WithInterval(10*time.Millisecond))

	go func() { _ = cmd.shutdownManager.Shutdown() }()
	_ = cmd.shutdown()
}

func TestResourceCleanupOnFailure(t *testing.T) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger: logger,
	}

	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("docker init failed")
	}

	err := cmd.boot()
	require.Error(t, err)

	assert.NotNil(t, cmd.done)
	assert.NotNil(t, cmd.shutdownManager)
}

func TestHealthCheckerInitialization(t *testing.T) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:    logger,
		EnableWeb: true,
	}

	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:                ctx,
			dockerProvider:     &mockDockerProvider{},
			notifier:           &mockDockerLabelsUpdate{},
			logger:             logger,
			configPollInterval: cfg.ConfigPollInterval,
			useEvents:          cfg.UseEvents,
			dockerPollInterval: cfg.DockerPollInterval,
			pollingFallback:    cfg.PollingFallback,
		}
		return handler, nil
	}

	err := cmd.boot()
	require.NoError(t, err)
	assert.NotNil(t, cmd.healthChecker)
}

func TestServerErrorHandlingDuringStartup(t *testing.T) {
	hook, logger := newMemoryLogger(logrus.InfoLevel)

	cmd := &DaemonCommand{
		Logger:      logger,
		EnablePprof: true,
		PprofAddr:   "invalid:address:9999",
		done:        make(chan struct{}),
	}

	cmd.scheduler = core.NewScheduler(logger)
	cmd.shutdownManager = core.NewShutdownManager(logger, 1*time.Second)
	cmd.pprofServer = &http.Server{
		Addr:              "invalid:address:9999",
		ReadHeaderTimeout: 5 * time.Second,
	}

	err := cmd.start()
	require.Error(t, err)
	assert.Regexp(t, ".*pprof server startup failed.*", err.Error())

	foundError := false
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.ErrorLevel &&
			(strings.Contains(entry.Message, "pprof server failed to start") ||
				strings.Contains(entry.Message, "Error starting HTTP server")) {
			foundError = true
			break
		}
	}
	assert.True(t, foundError)
}

func TestCompleteExecuteWorkflow(t *testing.T) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:      logger,
		EnableWeb:   false,
		EnablePprof: false,
	}

	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:                ctx,
			dockerProvider:     &mockDockerProvider{},
			notifier:           &mockDockerLabelsUpdate{},
			logger:             logger,
			configPollInterval: cfg.ConfigPollInterval,
			useEvents:          cfg.UseEvents,
			dockerPollInterval: cfg.DockerPollInterval,
			pollingFallback:    cfg.PollingFallback,
		}
		return handler, nil
	}

	done := make(chan error)
	go func() {
		if err := cmd.boot(); err != nil {
			done <- err
			return
		}
		if err := cmd.start(); err != nil {
			done <- err
			return
		}

		time.Sleep(100 * time.Millisecond)

		_ = cmd.shutdownManager.Shutdown()

		done <- cmd.shutdown()
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("execute workflow took too long")
	}
}
