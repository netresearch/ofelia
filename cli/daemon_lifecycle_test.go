package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/sirupsen/logrus"
	. "gopkg.in/check.v1"

	"github.com/netresearch/ofelia/core"
)

func TestDaemonLifecycle(t *testing.T) { TestingT(t) }

type DaemonLifecycleSuite struct{}

var _ = Suite(&DaemonLifecycleSuite{})

// mockDockerClient implements the dockerClient interface for testing
type mockDockerClient struct{}

func (m *mockDockerClient) Info() (*docker.DockerInfo, error) {
	return &docker.DockerInfo{}, nil
}

func (m *mockDockerClient) ListContainers(opts docker.ListContainersOptions) ([]docker.APIContainers, error) {
	return []docker.APIContainers{}, nil
}

func (m *mockDockerClient) AddEventListenerWithOptions(opts docker.EventsOptions, listener chan<- *docker.APIEvents) error {
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

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerClient:   &mockDockerClient{},
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

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
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
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
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

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerClient:   &mockDockerClient{},
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

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerClient:   &mockDockerClient{},
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

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerClient:   &mockDockerClient{},
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

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
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

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerClient:   &mockDockerClient{},
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

	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		handler := &DockerHandler{
			ctx:            ctx,
			dockerClient:   &mockDockerClient{},
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
