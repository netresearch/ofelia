// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/netresearch/ofelia/core"
	dockeradapter "github.com/netresearch/ofelia/core/adapters/docker"
	"github.com/netresearch/ofelia/core/domain"
)

var ErrNoContainerWithOfeliaEnabled = errors.New("couldn't find containers with label 'ofelia.enabled=true'")

// dockerStartupPingTimeout bounds the construction-time sanity Ping calls
// (both the buildSDKProvider post-construction check and the NewDockerHandler
// post-construction check). Without this, a daemon that completes API
// negotiation but then wedges on /_ping would hang Ofelia at startup. The
// bound is generous because daemon startup is one-shot and operators expect
// a clear error within a few seconds. See https://github.com/netresearch/ofelia/issues/614.
const dockerStartupPingTimeout = 10 * time.Second

// dockerEventTypeContainer is the Docker event filter "type" value for
// container-scoped events (vs. image, network, volume, etc.). Docker's API
// transports these as plain strings.
const dockerEventTypeContainer = "container"

type DockerHandler struct {
	ctx            context.Context //nolint:containedctx // holds lifecycle for background goroutines
	cancel         context.CancelFunc
	filters        []string
	dockerProvider core.DockerProvider // SDK-based provider
	notifier       dockerContainersUpdate
	logger         *slog.Logger

	// Separated configuration options
	configPollInterval time.Duration // For INI file watching
	useEvents          bool          // For container detection via events
	dockerPollInterval time.Duration // For container polling (explicit)
	pollingFallback    time.Duration // Auto-enable polling if events fail

	// Startup retry — see DockerConfig.StartupRetryCount / .StartupRetryInterval
	// and https://github.com/netresearch/ofelia/issues/523. Honored by both
	// the buildSDKProvider post-construction ping and the NewDockerHandler
	// externally-provided-provider ping.
	startupRetryCount    int
	startupRetryInterval time.Duration

	// Runtime state for fallback mechanism
	mu                    sync.Mutex
	eventsFailed          bool
	fallbackPollingActive bool
	fallbackCancel        context.CancelFunc // To stop fallback polling when events recover

	wg sync.WaitGroup // tracks background goroutines for clean shutdown

	includeStopped bool // When true, ListContainers uses All: true so stopped containers are included
}

// DockerContainerInfo is a struct that contains the name and running state of a Docker container.
type DockerContainerInfo struct {
	// Name is the name of the Docker container.
	Name string
	// Created is the creation time of the container.
	Created time.Time
	// Running is a boolean flag that indicates if the container is running.
	State domain.ContainerState
	// Labels is a map of labels for the container.
	Labels map[string]string
}

type dockerContainersUpdate interface {
	dockerContainersUpdate([]DockerContainerInfo)
}

// GetDockerProvider returns the DockerProvider interface for SDK-based operations.
// This is the preferred method for new code using the official Docker SDK.
func (c *DockerHandler) GetDockerProvider() core.DockerProvider {
	return c.dockerProvider
}

// resolveConfig validates configuration and returns resolved values.
// Deprecation migrations are handled centrally by cli/deprecations.go during config loading.
func resolveConfig(cfg *DockerConfig, logger *slog.Logger) (configPoll, dockerPoll, fallback time.Duration, useEvents bool) {
	// Read values (already migrated by ApplyDeprecationMigrations during config load)
	configPoll = cfg.ConfigPollInterval
	dockerPoll = cfg.DockerPollInterval
	fallback = cfg.PollingFallback
	useEvents = cfg.UseEvents

	// Warn if both events and explicit container polling are enabled
	if useEvents && dockerPoll > 0 {
		logger.Warn("WARNING: Both Docker events and container polling are enabled. " +
			"This is usually wasteful. Consider disabling container polling (docker-poll-interval=0) " +
			"and relying on events with polling-fallback for resilience.")
	}

	return configPoll, dockerPoll, fallback, useEvents
}

func NewDockerHandler(
	ctx context.Context, //nolint:contextcheck // external callers provide base context; we derive cancelable child
	notifier dockerContainersUpdate,
	logger *slog.Logger,
	cfg *DockerConfig,
	provider core.DockerProvider,
) (*DockerHandler, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	// Resolve configuration with deprecation handling
	configPoll, dockerPoll, fallback, useEvents := resolveConfig(cfg, logger)

	c := &DockerHandler{
		ctx:                  ctx,
		cancel:               cancel,
		filters:              cfg.Filters,
		notifier:             notifier,
		logger:               logger,
		configPollInterval:   configPoll,
		useEvents:            useEvents,
		dockerPollInterval:   dockerPoll,
		pollingFallback:      fallback,
		includeStopped:       cfg.IncludeStopped,
		startupRetryCount:    cfg.StartupRetryCount,
		startupRetryInterval: cfg.StartupRetryInterval,
	}

	var err error
	if provider == nil {
		c.dockerProvider, err = c.buildSDKProvider()
		if err != nil {
			cancel()
			return nil, err
		}
	} else {
		c.dockerProvider = provider
	}

	// Do a sanity check on docker. Bound each attempt so a wedged daemon
	// cannot hang Ofelia at startup; see issue #614. Retry with exponential
	// backoff when the operator opted in via StartupRetryCount > 0; see #523.
	if err = pingWithRetry(ctx, c.dockerProvider, c.startupRetryCount, c.startupRetryInterval, logger); err != nil {
		cancel()
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return nil, fmt.Errorf("failed to connect to Docker daemon: %w\n  → Check Docker daemon is running: systemctl status docker\n  → Verify Docker API is accessible: docker info\n  → Check for Docker daemon errors: journalctl -u docker -n 50", err)
	}

	// Start config file watcher (separate from container detection)
	if c.configPollInterval > 0 {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.watchConfig()
		}()
	}

	// Start container detection
	if c.useEvents {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.watchEvents()
		}()
	}

	// Start explicit container polling (if enabled, separate from events)
	if c.dockerPollInterval > 0 {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.watchContainerPolling()
		}()
	}

	return c, nil
}

// newSDKDockerProvider builds a real SDK-backed core.DockerProvider. It is a
// package-level variable so that tests can swap in a stub provider without
// having to spin up a fake Docker daemon. Mirrors the existing newDockerHandler
// seam used elsewhere in this package.
var newSDKDockerProvider = func() (core.DockerProvider, error) {
	authProvider := dockeradapter.NewConfigAuthProvider()
	return core.NewSDKDockerProvider(&core.SDKDockerProviderConfig{
		AuthProvider: authProvider,
	})
}

// buildSDKProvider creates the new SDK-based Docker provider.
func (c *DockerHandler) buildSDKProvider() (core.DockerProvider, error) {
	provider, err := newSDKDockerProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create SDK Docker provider: %w", err)
	}

	// Verify connection; each attempt is bounded by dockerStartupPingTimeout
	// (see issue #614) and the parent ctx (so SIGINT during startup cancels).
	// Retries with exponential backoff are honored only when the operator
	// opted in via StartupRetryCount > 0; see #523.
	if err = pingWithRetry(c.ctx, provider, c.startupRetryCount, c.startupRetryInterval, c.logger); err != nil {
		_ = provider.Close()
		return nil, fmt.Errorf("SDK provider failed to connect to Docker: %w", err)
	}

	return provider, nil
}

// pingWithRetry calls provider.Ping with exponential backoff. The total
// attempt budget is count+1 (the initial attempt plus `count` retries),
// so count=0 collapses to a single ping — the pre-#523 behavior.
// baseInterval × 2^(attempt-1) is the backoff before the n-th retry
// (1s → 2s → 4s → ...). Each individual attempt is bounded by
// dockerStartupPingTimeout. The backoff observes ctx cancellation via a
// select over time.After / ctx.Done so SIGTERM during startup drains
// promptly instead of blocking the full retry budget — same shape as
// the retry-loop fixes in #685 (webhook) and #687 (job retries).
//
// Returns the last attempt's Ping error on exhaustion; returns a
// wrapped context error if canceled during a backoff window.
// See https://github.com/netresearch/ofelia/issues/523.
func pingWithRetry(ctx context.Context, provider core.DockerProvider, count int, baseInterval time.Duration, logger *slog.Logger) error {
	const maxBackoffStep = 5 * time.Minute
	var lastErr error
	for attempt := 0; attempt <= count; attempt++ {
		pingCtx, pingCancel := context.WithTimeout(ctx, dockerStartupPingTimeout)
		err := provider.Ping(pingCtx)
		pingCancel()
		if err == nil {
			if attempt > 0 {
				logger.Info(fmt.Sprintf("Docker reachable after %d retry attempt(s)", attempt))
			}
			return nil
		}
		lastErr = err
		if attempt == count {
			break // exhausted; fall through to return lastErr
		}
		// Exponential backoff: baseInterval × 2^attempt, capped per step.
		backoff := baseInterval << attempt //nolint:gosec // attempt bounded by StartupRetryCount validation (<=20)
		if backoff > maxBackoffStep || backoff <= 0 {
			backoff = maxBackoffStep
		}
		logger.Warn(fmt.Sprintf("Docker ping failed (attempt %d/%d), retrying in %v",
			attempt+1, count+1, backoff), "error", err)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return fmt.Errorf("docker startup retry canceled: %w", ctx.Err())
		}
	}
	return lastErr
}

// watchConfig handles INI configuration file polling (separate from container detection).
func (c *DockerHandler) watchConfig() {
	if c.configPollInterval <= 0 {
		return
	}

	ticker := time.NewTicker(c.configPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if cfg, ok := c.notifier.(*Config); ok {
				cfg.logger.Debug(fmt.Sprintf("checking config file %s", cfg.configPath))
				if err := cfg.iniConfigUpdate(); err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						c.logger.Warn(fmt.Sprintf("%v", err))
					}
				}
			}
		}
	}
}

// watchContainerPolling handles explicit container polling (fallback method).
func (c *DockerHandler) watchContainerPolling() {
	if c.dockerPollInterval <= 0 {
		return
	}

	ticker := time.NewTicker(c.dockerPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.refreshContainerLabels()
		}
	}
}

// startFallbackPolling starts container polling as a fallback when events fail.
// The polling is stopped when events recover (via stopFallbackPolling).
func (c *DockerHandler) startFallbackPolling() {
	c.mu.Lock()
	if c.fallbackPollingActive {
		c.mu.Unlock()
		return
	}
	c.fallbackPollingActive = true
	// Create a cancellable context for this fallback polling goroutine
	fallbackCtx, fallbackCancel := context.WithCancel(c.ctx)
	c.fallbackCancel = fallbackCancel
	c.mu.Unlock()

	c.logger.Warn(fmt.Sprintf("Starting fallback container polling at %v interval due to event stream failure", c.pollingFallback))

	ticker := time.NewTicker(c.pollingFallback)
	defer ticker.Stop()

	for {
		select {
		case <-fallbackCtx.Done():
			c.mu.Lock()
			c.fallbackPollingActive = false
			c.fallbackCancel = nil
			c.mu.Unlock()
			c.logger.Info("Stopped fallback container polling (events recovered)")
			return
		case <-ticker.C:
			c.refreshContainerLabels()
		}
	}
}

// refreshContainerLabels fetches and updates container labels.
func (c *DockerHandler) refreshContainerLabels() {
	labels, err := c.GetDockerContainers()
	if err != nil && !errors.Is(err, ErrNoContainerWithOfeliaEnabled) {
		c.logger.Debug(fmt.Sprintf("%v", err))
	}
	c.notifier.dockerContainersUpdate(labels)
}

func (c *DockerHandler) GetDockerContainers() ([]DockerContainerInfo, error) {
	filters := map[string][]string{
		"label": {requiredLabelFilter},
	}
	for _, f := range c.filters {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) != 2 {
			//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
			return nil, fmt.Errorf("invalid docker filter %q\n  → Filters must use key=value format (e.g., 'label=app=web')\n  → Valid filter keys: label, name, id, status, network\n  → Example: --docker-filter='label=environment=production'\n  → Check your OFELIA_DOCKER_FILTER environment variable or config file", f)
		}
		key, value := parts[0], parts[1]
		values, ok := filters[key]
		if ok {
			filters[key] = append(values, value)
		} else {
			filters[key] = []string{value}
		}
	}

	conts, err := c.dockerProvider.ListContainers(c.ctx, domain.ListOptions{
		Filters: filters,
		All:     c.includeStopped,
	})
	if err != nil {
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return nil, fmt.Errorf("failed to list Docker containers: %w\n  → Check Docker daemon is running: docker ps\n  → Verify user has Docker permissions: groups | grep docker\n  → Check Docker filters are valid: %v\n  → Try listing containers manually: docker ps -a", err, filters)
	}

	if len(conts) == 0 {
		return nil, ErrNoContainerWithOfeliaEnabled
	}

	containers := make([]DockerContainerInfo, 0, len(conts))

	for _, cont := range conts {
		if cont.Name == "" || len(cont.Labels) == 0 {
			continue
		}
		ofeliaLabels := make(map[string]string)
		for k, v := range cont.Labels {
			if strings.HasPrefix(k, labelPrefix) || k == dockerComposeServiceLabel {
				ofeliaLabels[k] = v
			}
		}
		if len(ofeliaLabels) == 0 {
			continue
		}
		name := cont.Name
		containerInfo := DockerContainerInfo{
			Name:    name,
			Created: cont.Created,
			State:   cont.State,
			Labels:  ofeliaLabels,
		}
		containers = append(containers, containerInfo)
	}

	return containers, nil
}

// handleEventStreamError marks the event stream as failed and starts fallback polling if configured.
func (c *DockerHandler) handleEventStreamError() {
	c.mu.Lock()
	if c.eventsFailed {
		c.mu.Unlock()
		return
	}
	c.eventsFailed = true

	// Start fallback polling if configured and not already running
	if c.pollingFallback > 0 && !c.fallbackPollingActive {
		c.mu.Unlock()
		go c.startFallbackPolling()
		return
	}
	c.mu.Unlock()

	if c.pollingFallback == 0 {
		c.logger.Error("Docker event stream failed. " +
			"Container changes will NOT be detected. " +
			"Set 'polling-fallback' or 'docker-poll-interval'.")
	}
}

// clearEventStreamError marks the event stream as healthy and stops fallback polling.
func (c *DockerHandler) clearEventStreamError() {
	c.mu.Lock()
	c.eventsFailed = false
	// Stop fallback polling if it's running - events have recovered
	if c.fallbackCancel != nil {
		c.fallbackCancel()
		// Note: fallbackPollingActive and fallbackCancel are reset in startFallbackPolling goroutine
	}
	c.mu.Unlock()
}

func (c *DockerHandler) watchEvents() {
	const (
		initialBackoff = 1 * time.Second
		maxBackoff     = 5 * time.Minute
		backoffFactor  = 2
	)

	backoff := initialBackoff

	for {
		// Check if context is canceled before attempting subscription
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		eventCh, errCh := c.dockerProvider.SubscribeEvents(c.ctx, domain.EventFilter{
			Filters: map[string][]string{
				"type":  {dockerEventTypeContainer},
				"label": {"ofelia.enabled=true"},
				"event": {
					// Lifecycle events
					domain.EventActionCreate,
					domain.EventActionStart,
					domain.EventActionRestart,
					domain.EventActionStop,
					domain.EventActionKill,
					domain.EventActionDie,
					domain.EventActionDestroy,
					// Management events
					domain.EventActionPause,
					domain.EventActionUnpause,
					domain.EventActionRename,
					domain.EventActionUpdate,
				},
			},
		})

		// Inner loop: process events until error or shutdown
	innerLoop:
		for {
			select {
			case <-c.ctx.Done():
				return
			case err, ok := <-errCh:
				if !ok {
					// Channel closed, exit inner loop to reconnect
					break innerLoop
				}
				if err != nil {
					c.logger.Warn(fmt.Sprintf("Docker event stream error, reconnecting in %v: %v", backoff, err))
					c.handleEventStreamError()
				}
				// Wait with backoff before reconnecting
				select {
				case <-c.ctx.Done():
					return
				case <-time.After(backoff):
				}
				// Increase backoff for next failure (capped at maxBackoff)
				backoff = min(time.Duration(float64(backoff)*backoffFactor), maxBackoff)
				break innerLoop // Exit inner loop to reconnect
			case _, ok := <-eventCh:
				if !ok {
					// Channel closed, exit inner loop to reconnect
					break innerLoop
				}
				// Success - reset backoff and clear failed state
				backoff = initialBackoff
				c.clearEventStreamError()
				c.refreshContainerLabels()
			}
		}
	}
}

func (c *DockerHandler) Shutdown(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for all background goroutines to finish before closing provider
	c.wg.Wait()

	// Close SDK provider if it was created
	if c.dockerProvider != nil {
		if err := c.dockerProvider.Close(); err != nil {
			c.logger.Warn(fmt.Sprintf("Error closing Docker provider: %v", err))
		}
		c.dockerProvider = nil
	}

	return nil
}
