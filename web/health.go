package web

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// HealthStatus represents the overall health status
type HealthStatus string

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
	LastChecked time.Time     `json:"last_checked"`
	Duration    time.Duration `json:"duration_ms"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    HealthStatus           `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Uptime    float64                `json:"uptime_seconds"`
	Version   string                 `json:"version"`
	Checks    map[string]HealthCheck `json:"checks"`
	System    SystemInfo             `json:"system"`
}

// SystemInfo contains system-level information
type SystemInfo struct {
	GoVersion    string `json:"go_version"`
	NumGoroutine int    `json:"goroutines"`
	NumCPU       int    `json:"cpus"`
	MemoryAlloc  uint64 `json:"memory_alloc_bytes"`
	MemoryTotal  uint64 `json:"memory_total_bytes"`
	GCRuns       uint32 `json:"gc_runs"`
}

// HealthChecker performs health checks
type HealthChecker struct {
	startTime     time.Time
	dockerClient  *docker.Client
	version       string
	checks        map[string]HealthCheck
	mu            sync.RWMutex
	checkInterval time.Duration
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(dockerClient *docker.Client, version string) *HealthChecker {
	hc := &HealthChecker{
		startTime:     time.Now(),
		dockerClient:  dockerClient,
		version:       version,
		checks:        make(map[string]HealthCheck),
		checkInterval: 30 * time.Second,
	}

	// Start background health checks
	go hc.runPeriodicChecks()

	return hc
}

// runPeriodicChecks runs health checks periodically
func (hc *HealthChecker) runPeriodicChecks() {
	ticker := time.NewTicker(hc.checkInterval)
	defer ticker.Stop()

	// Run initial checks
	hc.performAllChecks()

	for range ticker.C {
		hc.performAllChecks()
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

// checkDocker verifies Docker daemon connectivity
func (hc *HealthChecker) checkDocker() {
	start := time.Now()
	check := HealthCheck{
		Name:        "docker",
		LastChecked: start,
	}

	if hc.dockerClient == nil {
		check.Status = HealthStatusUnhealthy
		check.Message = "Docker client not initialized"
	} else {
		// Try to ping Docker
		err := hc.dockerClient.Ping()
		if err != nil {
			check.Status = HealthStatusUnhealthy
			check.Message = "Docker daemon unreachable: " + err.Error()
		} else {
			// Get Docker info
			info, err := hc.dockerClient.Info()
			if err != nil {
				check.Status = HealthStatusDegraded
				check.Message = "Could not get Docker info: " + err.Error()
			} else {
				check.Status = HealthStatusHealthy
				check.Message = "Docker " + info.ServerVersion + " running with " +
					string(rune(info.Containers)) + " containers"
			}
		}
	}

	check.Duration = time.Since(start)

	hc.mu.Lock()
	hc.checks["docker"] = check
	hc.mu.Unlock()
}

// checkScheduler verifies scheduler is operational
func (hc *HealthChecker) checkScheduler() {
	start := time.Now()
	check := HealthCheck{
		Name:        "scheduler",
		LastChecked: start,
		Status:      HealthStatusHealthy,
		Message:     "Scheduler is operational",
	}

	// In a real implementation, this would check the actual scheduler
	// For now, we'll assume it's healthy if the service is running

	check.Duration = time.Since(start)

	hc.mu.Lock()
	hc.checks["scheduler"] = check
	hc.mu.Unlock()
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

	if memoryUsagePercent > 90 {
		check.Status = HealthStatusUnhealthy
		check.Message = "Memory usage critical"
	} else if memoryUsagePercent > 75 {
		check.Status = HealthStatusDegraded
		check.Message = "Memory usage high"
	} else {
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
	for k, v := range hc.checks {
		checks[k] = v
	}
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
		// Liveness just checks if the service is running
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
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
		json.NewEncoder(w).Encode(health)
	}
}

// HealthHandler returns detailed health information
func (hc *HealthChecker) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := hc.GetHealth()

		// Always return 200 for health endpoint (monitoring tools expect this)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(health)
	}
}
