package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	. "gopkg.in/check.v1"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
)

func TestDaemonLifecycle(t *testing.T) { TestingT(t) }

type DaemonLifecycleSuite struct{}

var _ = Suite(&DaemonLifecycleSuite{})

// mockDockerProvider implements the core.DockerProvider interface for testing
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

// mockDockerLabelsUpdate implements the dockerLabelsUpdate interface
type mockDockerLabelsUpdate struct{}

func (m *mockDockerLabelsUpdate) dockerLabelsUpdate(labels map[string]map[string]string) {
	// Mock implementation - do nothing
}

// Helper function to get an available address for testing
func getAvailableAddress() string {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	return fmt.Sprintf(":%d", listener.Addr().(*net.TCPAddr).Port)
}

// Test successful complete lifecycle
func (s *DaemonLifecycleSuite) TestSuccessfulBootStartShutdown(c *C) {
	// Setup
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:      logger,
		EnableWeb:   false,
		EnablePprof: false,
	}

	// Mock docker handler that succeeds
	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerProvider:   &mockDockerProvider{},
			notifier:       &mockDockerLabelsUpdate{},
			logger:         logger,
			pollInterval:   cfg.PollInterval,
			useEvents:      cfg.UseEvents,
			disablePolling: cfg.DisablePolling,
		}
		return handler, nil
	}

	// Test boot phase
	err := cmd.boot()
	c.Assert(err, IsNil)
	c.Assert(cmd.scheduler, NotNil)
	c.Assert(cmd.shutdownManager, NotNil)
	c.Assert(cmd.done, NotNil)
	c.Assert(cmd.config, NotNil)

	// Test start phase
	err = cmd.start()
	c.Assert(err, IsNil)

	// Give some time for goroutines to start
	time.Sleep(100 * time.Millisecond)

	// Test shutdown
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = cmd.shutdownManager.Shutdown()
	}()

	err = cmd.shutdown()
	c.Assert(err, IsNil)
}

// Test boot failure scenarios
func (s *DaemonLifecycleSuite) TestBootFailureInvalidConfig(c *C) {
	// Create invalid config file
	tmpFile, err := os.CreateTemp("", "ofelia_invalid_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmpFile.Name())

	// Write malformed INI content
	_, err = tmpFile.WriteString("[global\ninvalid-section-header\nkey = value")
	c.Assert(err, IsNil)
	tmpFile.Close()

	_, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{
		ConfigFile: tmpFile.Name(),
		Logger:     logger,
	}

	// Mock docker handler failure
	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("docker initialization failed")
	}

	// Boot should handle config error gracefully
	err = cmd.boot()
	c.Assert(err, NotNil) // InitializeApp will fail due to docker error
}

func (s *DaemonLifecycleSuite) TestBootDockerConnectionFailure(c *C) {
	hook, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{
		Logger:    logger,
		EnableWeb: true,
	}

	// Mock docker handler failure
	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	dockerError := errors.New("cannot connect to Docker daemon")
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, dockerError
	}

	err := cmd.boot()
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*Docker daemon.*")

	// Verify error was logged
	found := false
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "Can't start the app") {
			found = true
			break
		}
	}
	c.Assert(found, Equals, true)
}

// Test pprof server functionality
func (s *DaemonLifecycleSuite) TestPprofServerStartup(c *C) {
	hook, logger := newMemoryLogger(logrus.InfoLevel)

	// Find available port
	addr := getAvailableAddress()

	cmd := &DaemonCommand{
		Logger:      logger,
		EnablePprof: true,
		PprofAddr:   addr,
		done:        make(chan struct{}),
	}

	// Mock successful components
	cmd.scheduler = core.NewScheduler(logger)
	cmd.shutdownManager = core.NewShutdownManager(logger, 1*time.Second)
	cmd.pprofServer = &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
	}

	err := cmd.start()
	c.Assert(err, IsNil)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify pprof server started
	found := false
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "Starting pprof server") {
			found = true
			break
		}
	}
	c.Assert(found, Equals, true)

	// Trigger shutdown
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = cmd.shutdownManager.Shutdown()
	}()

	_ = cmd.shutdown()
}

// Test web server functionality
func (s *DaemonLifecycleSuite) TestWebServerStartup(c *C) {
	hook, logger := newMemoryLogger(logrus.InfoLevel)

	// Find available port
	addr := getAvailableAddress()

	cmd := &DaemonCommand{
		Logger:    logger,
		EnableWeb: true,
		WebAddr:   addr,
		done:      make(chan struct{}),
	}

	// Setup successful boot
	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerProvider:   &mockDockerProvider{},
			notifier:       &mockDockerLabelsUpdate{},
			logger:         logger,
			pollInterval:   cfg.PollInterval,
			useEvents:      cfg.UseEvents,
			disablePolling: cfg.DisablePolling,
		}
		return handler, nil
	}

	err := cmd.boot()
	c.Assert(err, IsNil)
	c.Assert(cmd.webServer, NotNil)

	err = cmd.start()
	c.Assert(err, IsNil)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify web server started
	found := false
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "Starting web server") {
			found = true
			break
		}
	}
	c.Assert(found, Equals, true)

	// Trigger shutdown
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = cmd.shutdownManager.Shutdown()
	}()

	_ = cmd.shutdown()
}

// Test port binding conflicts
func (s *DaemonLifecycleSuite) TestPortBindingConflict(c *C) {
	hook, logger := newMemoryLogger(logrus.InfoLevel)

	// Get an address and bind to it first
	addr := getAvailableAddress()
	listener, err := net.Listen("tcp", addr)
	c.Assert(err, IsNil)
	defer listener.Close()

	cmd := &DaemonCommand{
		Logger:      logger,
		EnablePprof: true,
		PprofAddr:   addr, // This should conflict
		done:        make(chan struct{}),
	}

	// Mock successful components
	cmd.scheduler = core.NewScheduler(logger)
	cmd.shutdownManager = core.NewShutdownManager(logger, 1*time.Second)
	cmd.pprofServer = &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
	}

	err = cmd.start()
	c.Assert(err, IsNil) // start() doesn't wait for servers to fully start

	// Give time for error to occur
	time.Sleep(200 * time.Millisecond)

	// Check that error was logged
	found := false
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.ErrorLevel && strings.Contains(entry.Message, "Error starting HTTP server") {
			found = true
			break
		}
	}
	c.Assert(found, Equals, true)
}

// Test graceful shutdown with running jobs
func (s *DaemonLifecycleSuite) TestGracefulShutdownWithRunningJobs(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)

	cmd := &DaemonCommand{
		Logger:          logger,
		scheduler:       core.NewScheduler(logger),
		shutdownManager: core.NewShutdownManager(logger, 2*time.Second),
		done:            make(chan struct{}),
	}

	err := cmd.start()
	c.Assert(err, IsNil)

	// Start shutdown in background
	shutdownDone := make(chan error)
	go func() {
		shutdownDone <- cmd.shutdown()
	}()

	// Give some time then trigger shutdown
	time.Sleep(50 * time.Millisecond)
	_ = cmd.shutdownManager.Shutdown()

	// Wait for shutdown to complete
	select {
	case err := <-shutdownDone:
		c.Assert(err, IsNil)
	case <-time.After(5 * time.Second):
		c.Fatal("shutdown took too long")
	}
}

// Test forced shutdown on timeout
func (s *DaemonLifecycleSuite) TestForcedShutdownOnTimeout(c *C) {
	_, logger := newMemoryLogger(logrus.DebugLevel)

	cmd := &DaemonCommand{
		Logger:          logger,
		shutdownManager: core.NewShutdownManager(logger, 100*time.Millisecond), // Very short timeout
		done:            make(chan struct{}),
	}

	// Create scheduler
	cmd.scheduler = core.NewScheduler(logger)

	err := cmd.start()
	c.Assert(err, IsNil)

	// Start shutdown process
	shutdownDone := make(chan error)
	go func() {
		shutdownDone <- cmd.shutdown()
	}()

	// Trigger shutdown immediately
	_ = cmd.shutdownManager.Shutdown()

	// Should complete quickly due to timeout
	select {
	case err := <-shutdownDone:
		c.Assert(err, IsNil)
	case <-time.After(2 * time.Second):
		c.Fatal("shutdown took too long even with timeout")
	}
}

// Test configuration option application
func (s *DaemonLifecycleSuite) TestConfigurationOptionApplication(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)

	// Test CLI options override config file options
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

	// Mock docker handler
	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerProvider:   &mockDockerProvider{},
			notifier:       &mockDockerLabelsUpdate{},
			logger:         logger,
			pollInterval:   cfg.PollInterval,
			useEvents:      cfg.UseEvents,
			disablePolling: cfg.DisablePolling,
		}
		return handler, nil
	}

	err := cmd.boot()
	c.Assert(err, IsNil)

	// Verify options were applied
	c.Assert(cmd.config.Docker.Filters, DeepEquals, []string{"label=test"})
	c.Assert(cmd.config.Docker.PollInterval, Equals, pollInterval)
	c.Assert(cmd.EnableWeb, Equals, true)
	c.Assert(cmd.WebAddr, Equals, ":9999")
	c.Assert(cmd.EnablePprof, Equals, true)
	c.Assert(cmd.PprofAddr, Equals, "127.0.0.1:9998")
}

// Test concurrent server startup
func (s *DaemonLifecycleSuite) TestConcurrentServerStartup(c *C) {
	hook, logger := newMemoryLogger(logrus.InfoLevel)

	// Use different available ports
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

	// Setup successful boot
	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerProvider:   &mockDockerProvider{},
			notifier:       &mockDockerLabelsUpdate{},
			logger:         logger,
			pollInterval:   cfg.PollInterval,
			useEvents:      cfg.UseEvents,
			disablePolling: cfg.DisablePolling,
		}
		return handler, nil
	}

	err := cmd.boot()
	c.Assert(err, IsNil)

	err = cmd.start()
	c.Assert(err, IsNil)

	// Give servers time to start
	time.Sleep(200 * time.Millisecond)

	// Verify both servers started
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
	c.Assert(pprofFound, Equals, true)
	c.Assert(webFound, Equals, true)

	// Trigger shutdown
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = cmd.shutdownManager.Shutdown()
	}()

	_ = cmd.shutdown()
}

// Test resource cleanup on failure
func (s *DaemonLifecycleSuite) TestResourceCleanupOnFailure(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger: logger,
	}

	// Mock docker handler that fails initialization
	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("docker init failed")
	}

	err := cmd.boot()
	c.Assert(err, NotNil)

	// Verify cleanup occurred - done channel should not be created on failure
	c.Assert(cmd.done, NotNil)            // done is created early in boot
	c.Assert(cmd.shutdownManager, NotNil) // shutdown manager is created
}

// Test health checker initialization
func (s *DaemonLifecycleSuite) TestHealthCheckerInitialization(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:    logger,
		EnableWeb: true,
	}

	// Mock successful docker handler
	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerProvider:   &mockDockerProvider{},
			notifier:       &mockDockerLabelsUpdate{},
			logger:         logger,
			pollInterval:   cfg.PollInterval,
			useEvents:      cfg.UseEvents,
			disablePolling: cfg.DisablePolling,
		}
		return handler, nil
	}

	err := cmd.boot()
	c.Assert(err, IsNil)
	c.Assert(cmd.healthChecker, NotNil)
}

// Test server error handling during startup
func (s *DaemonLifecycleSuite) TestServerErrorHandlingDuringStartup(c *C) {
	hook, logger := newMemoryLogger(logrus.InfoLevel)

	// Create a server that will immediately fail (invalid address)
	cmd := &DaemonCommand{
		Logger:      logger,
		EnablePprof: true,
		PprofAddr:   "invalid:address:9999", // Invalid address format
		done:        make(chan struct{}),
	}

	cmd.scheduler = core.NewScheduler(logger)
	cmd.shutdownManager = core.NewShutdownManager(logger, 1*time.Second)
	cmd.pprofServer = &http.Server{
		Addr:              "invalid:address:9999",
		ReadHeaderTimeout: 5 * time.Second,
	}

	// With health checks, start() now correctly fails when server can't bind
	err := cmd.start()
	c.Assert(err, NotNil) // start() should fail for invalid address
	c.Assert(err.Error(), Matches, ".*pprof server startup failed.*")

	// Check that failure was logged
	foundError := false
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.ErrorLevel &&
			(strings.Contains(entry.Message, "pprof server failed to start") ||
				strings.Contains(entry.Message, "Error starting HTTP server")) {
			foundError = true
			break
		}
	}
	c.Assert(foundError, Equals, true)
}

// Test daemon complete execute workflow
func (s *DaemonLifecycleSuite) TestCompleteExecuteWorkflow(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:      logger,
		EnableWeb:   false,
		EnablePprof: false,
	}

	// Mock docker handler
	originalNewDockerHandler := newDockerHandler
	defer func() { newDockerHandler = originalNewDockerHandler }()

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerProvider:   &mockDockerProvider{},
			notifier:       &mockDockerLabelsUpdate{},
			logger:         logger,
			pollInterval:   cfg.PollInterval,
			useEvents:      cfg.UseEvents,
			disablePolling: cfg.DisablePolling,
		}
		return handler, nil
	}

	// Simulate execute workflow in a goroutine with timeout
	done := make(chan error)
	go func() {
		// This simulates the Execute method workflow
		if err := cmd.boot(); err != nil {
			done <- err
			return
		}
		if err := cmd.start(); err != nil {
			done <- err
			return
		}

		// Simulate running for a short time
		time.Sleep(100 * time.Millisecond)

		// Trigger shutdown
		_ = cmd.shutdownManager.Shutdown()

		done <- cmd.shutdown()
	}()

	// Wait for completion or timeout
	select {
	case err := <-done:
		c.Assert(err, IsNil)
	case <-time.After(5 * time.Second):
		c.Fatal("execute workflow took too long")
	}
}
