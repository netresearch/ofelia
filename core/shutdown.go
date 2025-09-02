package core

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ShutdownManager handles graceful shutdown of the application
type ShutdownManager struct {
	timeout        time.Duration
	hooks          []ShutdownHook
	mu             sync.Mutex
	shutdownChan   chan struct{}
	isShuttingDown bool
	logger         Logger
}

// ShutdownHook is a function to be called during shutdown
type ShutdownHook struct {
	Name     string
	Priority int // Lower values execute first
	Hook     func(context.Context) error
}

// NewShutdownManager creates a new shutdown manager
func NewShutdownManager(logger Logger, timeout time.Duration) *ShutdownManager {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &ShutdownManager{
		timeout:      timeout,
		hooks:        make([]ShutdownHook, 0),
		shutdownChan: make(chan struct{}),
		logger:       logger,
	}
}

// RegisterHook registers a shutdown hook
func (sm *ShutdownManager) RegisterHook(hook ShutdownHook) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.hooks = append(sm.hooks, hook)

	// Sort hooks by priority
	for i := len(sm.hooks) - 1; i > 0; i-- {
		if sm.hooks[i].Priority < sm.hooks[i-1].Priority {
			sm.hooks[i], sm.hooks[i-1] = sm.hooks[i-1], sm.hooks[i]
		} else {
			break
		}
	}
}

// ListenForShutdown starts listening for shutdown signals
func (sm *ShutdownManager) ListenForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	go func() {
		sig := <-sigChan
		sm.logger.Warningf("Received shutdown signal: %v", sig)
		sm.Shutdown()
	}()
}

// Shutdown initiates graceful shutdown
func (sm *ShutdownManager) Shutdown() error {
	sm.mu.Lock()
	if sm.isShuttingDown {
		sm.mu.Unlock()
		return fmt.Errorf("shutdown already in progress")
	}
	sm.isShuttingDown = true
	sm.mu.Unlock()

	sm.logger.Noticef("Starting graceful shutdown (timeout: %v)", sm.timeout)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), sm.timeout)
	defer cancel()

	// Signal that shutdown has started
	close(sm.shutdownChan)

	// Execute shutdown hooks in order
	var wg sync.WaitGroup
	errChan := make(chan error, len(sm.hooks))

	for _, hook := range sm.hooks {
		wg.Add(1)
		go func(h ShutdownHook) {
			defer wg.Done()

			sm.logger.Debugf("Executing shutdown hook: %s (priority: %d)", h.Name, h.Priority)

			if err := h.Hook(ctx); err != nil {
				sm.logger.Errorf("Shutdown hook '%s' failed: %v", h.Name, err)
				errChan <- fmt.Errorf("hook %s: %w", h.Name, err)
			} else {
				sm.logger.Debugf("Shutdown hook '%s' completed successfully", h.Name)
			}
		}(hook)
	}

	// Wait for all hooks to complete or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		sm.logger.Noticef("Graceful shutdown completed successfully")
	case <-ctx.Done():
		sm.logger.Errorf("Graceful shutdown timed out after %v", sm.timeout)
		return fmt.Errorf("shutdown timed out")
	}

	// Check for errors
	close(errChan)
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("shutdown completed with %d errors", len(errors))
	}

	return nil
}

// ShutdownChan returns a channel that's closed when shutdown starts
func (sm *ShutdownManager) ShutdownChan() <-chan struct{} {
	return sm.shutdownChan
}

// IsShuttingDown returns true if shutdown is in progress
func (sm *ShutdownManager) IsShuttingDown() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.isShuttingDown
}

// GracefulScheduler wraps a scheduler with graceful shutdown
type GracefulScheduler struct {
	*Scheduler
	shutdownManager *ShutdownManager
	activeJobs      sync.WaitGroup
	mu              sync.RWMutex
}

// NewGracefulScheduler creates a scheduler with graceful shutdown support
func NewGracefulScheduler(scheduler *Scheduler, shutdownManager *ShutdownManager) *GracefulScheduler {
	gs := &GracefulScheduler{
		Scheduler:       scheduler,
		shutdownManager: shutdownManager,
	}

	// Register scheduler shutdown hook
	shutdownManager.RegisterHook(ShutdownHook{
		Name:     "scheduler",
		Priority: 10,
		Hook:     gs.gracefulStop,
	})

	return gs
}

// RunJobWithTracking runs a job with shutdown tracking
func (gs *GracefulScheduler) RunJobWithTracking(job Job, ctx *Context) error {
	// Check if shutting down
	if gs.shutdownManager.IsShuttingDown() {
		return fmt.Errorf("cannot start job during shutdown")
	}

	gs.activeJobs.Add(1)
	defer gs.activeJobs.Done()

	// Create a context that can be cancelled during shutdown
	jobCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Monitor for shutdown
	go func() {
		<-gs.shutdownManager.ShutdownChan()
		cancel()
	}()

	// Run the job with cancellation support
	done := make(chan error, 1)
	go func() {
		done <- job.Run(ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-jobCtx.Done():
		gs.Scheduler.Logger.Warningf("Job %s cancelled due to shutdown", job.GetName())
		return fmt.Errorf("job cancelled: shutdown in progress")
	}
}

// gracefulStop stops the scheduler gracefully
func (gs *GracefulScheduler) gracefulStop(ctx context.Context) error {
	gs.Scheduler.Logger.Noticef("Stopping scheduler gracefully")

	// Stop accepting new jobs
	gs.Scheduler.Stop()

	// Wait for active jobs to complete
	done := make(chan struct{})
	go func() {
		gs.activeJobs.Wait()
		close(done)
	}()

	select {
	case <-done:
		gs.Scheduler.Logger.Noticef("All jobs completed successfully")
		return nil
	case <-ctx.Done():
		// Count remaining jobs
		gs.Scheduler.Logger.Warningf("Forcing shutdown with active jobs")
		return fmt.Errorf("timeout waiting for jobs to complete")
	}
}

// GracefulServer wraps an HTTP server with graceful shutdown
type GracefulServer struct {
	server          *http.Server
	shutdownManager *ShutdownManager
	logger          Logger
}

// NewGracefulServer creates a server with graceful shutdown support
func NewGracefulServer(server *http.Server, shutdownManager *ShutdownManager, logger Logger) *GracefulServer {
	gs := &GracefulServer{
		server:          server,
		shutdownManager: shutdownManager,
		logger:          logger,
	}

	// Register server shutdown hook
	shutdownManager.RegisterHook(ShutdownHook{
		Name:     "http-server",
		Priority: 20, // After scheduler
		Hook:     gs.gracefulStop,
	})

	return gs
}

// gracefulStop stops the HTTP server gracefully
func (gs *GracefulServer) gracefulStop(ctx context.Context) error {
	gs.logger.Noticef("Stopping HTTP server gracefully")

	// Stop accepting new connections
	if err := gs.server.Shutdown(ctx); err != nil {
		gs.logger.Errorf("HTTP server shutdown error: %v", err)
		return err
	}

	gs.logger.Noticef("HTTP server stopped successfully")
	return nil
}
