// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/netresearch/ofelia/core"
)

// HealthStatus represents the overall health status
type HealthStatus string

// The statuses a single check, and the aggregate health report, can carry.
// GetHealth folds the individual checks into the worst status present, so one
// unhealthy check makes the whole report unhealthy. Only the readiness
// endpoint acts on the value, and only for Unhealthy: /ready answers 503 for
// it and 200 for the other two, so a degraded ofelia stays in rotation.
// /health and /healthz always answer 200 and put the status in the body.
const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// HealthCheck represents a single health check
type HealthCheck struct {
	Name        string        `json:"name"`
	Status      HealthStatus  `json:"status"`
	Message     string        `json:"message,omitempty"`
	LastChecked time.Time     `json:"lastChecked"`
	Duration    time.Duration `json:"durationMs"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    HealthStatus           `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Uptime    float64                `json:"uptimeSeconds"`
	Version   string                 `json:"version"`
	Checks    map[string]HealthCheck `json:"checks"`
	System    SystemInfo             `json:"system"`
}

// SystemInfo contains system-level information
type SystemInfo struct {
	GoVersion    string `json:"goVersion"`
	NumGoroutine int    `json:"goroutines"`
	NumCPU       int    `json:"cpus"`
	MemoryAlloc  uint64 `json:"memoryAllocBytes"`
	MemoryTotal  uint64 `json:"memoryTotalBytes"`
	GCRuns       uint32 `json:"gcRuns"`
}

// HealthChecker performs health checks
type HealthChecker struct {
	startTime      time.Time
	dockerProvider core.DockerProvider
	scheduler      *core.Scheduler
	version        string
	checks         map[string]HealthCheck
	mu             sync.RWMutex
	checkInterval  time.Duration
	stop           chan struct{}
	stopOnce       sync.Once
}

// NewHealthChecker creates a new health checker.
//
// The scheduler may be nil, in which case the scheduler check reports what it
// can see rather than claiming health it has not established.
func NewHealthChecker(dockerProvider core.DockerProvider, scheduler *core.Scheduler, version string) *HealthChecker {
	hc := &HealthChecker{
		startTime:      time.Now(),
		dockerProvider: dockerProvider,
		scheduler:      scheduler,
		version:        version,
		checks:         make(map[string]HealthCheck),
		checkInterval:  30 * time.Second,
		stop:           make(chan struct{}),
	}

	// Start background health checks
	go hc.runPeriodicChecks()

	return hc
}

// Stop ends the periodic check loop.
//
// Without it the loop ran until the process exited, so every checker ever
// constructed kept a goroutine alive — harmless for the daemon's single
// long-lived instance, but tests that build one per case leaked one each.
// Safe to call more than once and from several goroutines.
func (hc *HealthChecker) Stop() {
	hc.stopOnce.Do(func() { close(hc.stop) })
}

// runPeriodicChecks runs health checks periodically
func (hc *HealthChecker) runPeriodicChecks() {
	ticker := time.NewTicker(hc.checkInterval)
	defer ticker.Stop()

	// Run initial checks
	hc.performAllChecks()

	for {
		select {
		case <-hc.stop:
			return
		case <-ticker.C:
			hc.performAllChecks()
		}
	}
}

// performAllChecks executes all health checks
func (hc *HealthChecker) performAllChecks() {
	// Check Docker connectivity
	hc.checkDocker()

	// Check scheduler status
	hc.checkScheduler()

	// Check system resources
	hc.checkSystemResources()
}

// dockerHealthCheckTimeout bounds each Docker daemon call performed by the
// background health checker. Operators monitoring /health expect a non-2xx
// response within a single scrape interval, so we keep this short. The value
// covers Ping and Info independently; the worst-case caller wait is roughly
// 2x this value.
const dockerHealthCheckTimeout = 5 * time.Second

// checkDocker verifies Docker daemon connectivity. Each Docker SDK call is
// bounded by dockerHealthCheckTimeout so that a wedged daemon - reachable but
// unresponsive - cannot stall the periodic health-check goroutine and starve
// /health and /ready of fresh status. See https://github.com/netresearch/ofelia/issues/614.
func (hc *HealthChecker) checkDocker() {
	start := time.Now()
	check := HealthCheck{
		Name:        "docker",
		LastChecked: start,
	}

	if hc.dockerProvider == nil {
		check.Status = HealthStatusUnhealthy
		check.Message = "Docker provider not initialized"
	} else {
		// Try to ping Docker with a bounded context so a wedged daemon
		// cannot hang the background ticker.
		pingCtx, pingCancel := context.WithTimeout(context.Background(), dockerHealthCheckTimeout)
		err := hc.dockerProvider.Ping(pingCtx)
		pingCancel()
		if err != nil {
			check.Status = HealthStatusUnhealthy
			check.Message = "Docker daemon unreachable: " + err.Error()
		} else {
			// Get Docker info, also bounded.
			infoCtx, infoCancel := context.WithTimeout(context.Background(), dockerHealthCheckTimeout)
			info, err := hc.dockerProvider.Info(infoCtx)
			infoCancel()
			if err != nil {
				check.Status = HealthStatusDegraded
				check.Message = "Could not get Docker info: " + err.Error()
			} else {
				check.Status = HealthStatusHealthy
				check.Message = fmt.Sprintf("Docker %s running with %d containers",
					info.ServerVersion, info.ContainersRunning)
			}
		}
	}

	check.Duration = time.Since(start)

	hc.mu.Lock()
	hc.checks["docker"] = check
	hc.mu.Unlock()
}

// checkScheduler reports whether every configured job is actually registered.
//
// This used to answer "healthy" unconditionally, with a note that a real
// implementation would check the scheduler. So a daemon with a mistyped
// schedule — a job that never fires — still served a green /health, which is
// the probe the integration docs tell operators to point their container
// healthcheck at. The failure it was most worth reporting was the one it could
// not see.
//
// A refused job makes this degraded rather than unhealthy on purpose: /ready
// answers 503 only for unhealthy, and taking a daemon out of rotation because
// one job of twenty has a typo would trade a silent failure for a louder one.
// Degraded shows up in the body of /health and /healthz, where an operator or
// an alert rule can act on it, without stopping the jobs that do work.
func (hc *HealthChecker) checkScheduler() {
	start := time.Now()
	check := HealthCheck{
		Name:        "scheduler",
		LastChecked: start,
		Status:      HealthStatusHealthy,
		Message:     "Scheduler is operational",
	}

	if hc.scheduler == nil {
		check.Status = HealthStatusDegraded
		check.Message = "No scheduler wired to the health checker"
	} else if refused := hc.scheduler.GetUnschedulableJobs(); len(refused) > 0 {
		check.Status = HealthStatusDegraded
		check.Message = fmt.Sprintf(
			"%d configured job(s) are not scheduled and will not run: %s",
			len(refused), strings.Join(sortedNames(refused), ", "),
		)
	}

	check.Duration = time.Since(start)

	hc.mu.Lock()
	hc.checks["scheduler"] = check
	hc.mu.Unlock()
}

// sortedNames returns the keys in a stable order, so the message does not
// change between checks purely because Go randomizes map iteration.
func sortedNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// checkSystemResources checks system resource usage
func (hc *HealthChecker) checkSystemResources() {
	start := time.Now()
	check := HealthCheck{
		Name:        "system",
		LastChecked: start,
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Check memory usage
	memoryUsagePercent := float64(m.Alloc) / float64(m.Sys) * 100

	switch {
	case memoryUsagePercent > 90:
		check.Status = HealthStatusUnhealthy
		check.Message = "Memory usage critical"
	case memoryUsagePercent > 75:
		check.Status = HealthStatusDegraded
		check.Message = "Memory usage high"
	default:
		check.Status = HealthStatusHealthy
		check.Message = "System resources normal"
	}

	check.Duration = time.Since(start)

	hc.mu.Lock()
	hc.checks["system"] = check
	hc.mu.Unlock()
}

// GetHealth returns the current health status
func (hc *HealthChecker) GetHealth() HealthResponse {
	hc.mu.RLock()
	checks := make(map[string]HealthCheck)
	maps.Copy(checks, hc.checks)
	hc.mu.RUnlock()

	// Determine overall status
	status := HealthStatusHealthy
	for _, check := range checks {
		if check.Status == HealthStatusUnhealthy {
			status = HealthStatusUnhealthy
			break
		} else if check.Status == HealthStatusDegraded && status == HealthStatusHealthy {
			status = HealthStatusDegraded
		}
	}

	// Get system info
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return HealthResponse{
		Status:    status,
		Timestamp: time.Now(),
		Uptime:    time.Since(hc.startTime).Seconds(),
		Version:   hc.version,
		Checks:    checks,
		System: SystemInfo{
			GoVersion:    runtime.Version(),
			NumGoroutine: runtime.NumGoroutine(),
			NumCPU:       runtime.NumCPU(),
			MemoryAlloc:  m.Alloc,
			MemoryTotal:  m.Sys,
			GCRuns:       m.NumGC,
		},
	}
}

// LivenessHandler returns a simple liveness check
func (hc *HealthChecker) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Liveness just checks if the service is running. Explicit
		// Content-Type: without it, a compressed response would be
		// content-sniffed on the compressed bytes and answer with the
		// codec's own type (application/x-gzip, application/zstd).
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}
}

// ReadinessHandler returns readiness status
func (hc *HealthChecker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := hc.GetHealth()

		// Set appropriate status code
		statusCode := http.StatusOK
		if health.Status == HealthStatusUnhealthy {
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(health)
	}
}

// HealthHandler returns detailed health information
func (hc *HealthChecker) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := hc.GetHealth()

		// Always return 200 for health endpoint (monitoring tools expect this)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(health)
	}
}
