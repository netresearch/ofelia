package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/netresearch/ofelia/test/testutil"
)

func TestHealthChecker(t *testing.T) {
	hc := NewHealthChecker(nil, "1.0.0")

	var health HealthResponse
	testutil.Eventually(t, func() bool {
		health = hc.GetHealth()
		return len(health.Checks) > 0
	}, testutil.WithTimeout(2*time.Second), testutil.WithMessage("health checks did not initialize"))

	// Check basic fields
	if health.Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", health.Version)
	}

	if health.Uptime <= 0 {
		t.Error("Uptime should be positive")
	}

	// Check system info
	if health.System.GoVersion == "" {
		t.Error("Go version should not be empty")
	}

	if health.System.NumCPU <= 0 {
		t.Error("Number of CPUs should be positive")
	}

	if health.System.NumGoroutine <= 0 {
		t.Error("Number of goroutines should be positive")
	}

	// Check that we have some health checks
	if len(health.Checks) == 0 {
		t.Error("Should have at least one health check")
	}

	t.Log("Basic health checker test passed")
}

func TestLivenessHandler(t *testing.T) {
	hc := NewHealthChecker(nil, "1.0.0")
	handler := hc.LivenessHandler()

	req := httptest.NewRequest("GET", "/live", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if w.Body.String() != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", w.Body.String())
	}

	t.Log("Liveness handler test passed")
}

func TestReadinessHandler(t *testing.T) {
	hc := NewHealthChecker(nil, "1.0.0")

	testutil.Eventually(t, func() bool {
		return len(hc.GetHealth().Checks) > 0
	}, testutil.WithTimeout(2*time.Second), testutil.WithMessage("health checks did not initialize"))

	handler := hc.ReadinessHandler()

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	// Should return JSON
	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Expected JSON content type")
	}

	// Parse response
	var health HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check response has expected fields
	if health.Status == "" {
		t.Error("Status should not be empty")
	}

	if health.Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", health.Version)
	}

	t.Log("Readiness handler test passed")
}

func TestHealthHandler(t *testing.T) {
	hc := NewHealthChecker(nil, "1.0.0")

	testutil.Eventually(t, func() bool {
		return len(hc.GetHealth().Checks) > 0
	}, testutil.WithTimeout(2*time.Second), testutil.WithMessage("health checks did not initialize"))

	handler := hc.HealthHandler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	// Health endpoint always returns 200
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Should return JSON
	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Expected JSON content type")
	}

	// Parse response
	var health HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify all expected fields are present
	if health.Status == "" {
		t.Error("Status should not be empty")
	}

	if health.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}

	if health.Uptime <= 0 {
		t.Error("Uptime should be positive")
	}

	if health.Version == "" {
		t.Error("Version should not be empty")
	}

	if health.System.GoVersion == "" {
		t.Error("Go version should not be empty")
	}

	t.Log("Health handler test passed")
}

func TestHealthStatus(t *testing.T) {
	hc := NewHealthChecker(nil, "1.0.0")

	// Manually set some check statuses
	hc.mu.Lock()
	hc.checks["test1"] = HealthCheck{
		Name:        "test1",
		Status:      HealthStatusHealthy,
		LastChecked: time.Now(),
	}
	hc.checks["test2"] = HealthCheck{
		Name:        "test2",
		Status:      HealthStatusDegraded,
		LastChecked: time.Now(),
	}
	hc.mu.Unlock()

	health := hc.GetHealth()

	// With one degraded check, overall should be degraded
	if health.Status != HealthStatusDegraded {
		t.Errorf("Expected degraded status, got %s", health.Status)
	}

	// Add unhealthy check
	hc.mu.Lock()
	hc.checks["test3"] = HealthCheck{
		Name:        "test3",
		Status:      HealthStatusUnhealthy,
		LastChecked: time.Now(),
	}
	hc.mu.Unlock()

	health = hc.GetHealth()

	// With one unhealthy check, overall should be unhealthy
	if health.Status != HealthStatusUnhealthy {
		t.Errorf("Expected unhealthy status, got %s", health.Status)
	}

	t.Log("Health status aggregation test passed")
}

func TestSystemResourceCheck(t *testing.T) {
	hc := NewHealthChecker(nil, "1.0.0")

	// Trigger system resource check
	hc.checkSystemResources()

	// Verify check was recorded
	hc.mu.RLock()
	check, exists := hc.checks["system"]
	hc.mu.RUnlock()

	if !exists {
		t.Fatal("System check not found")
	}

	if check.Name != "system" {
		t.Errorf("Expected check name 'system', got '%s'", check.Name)
	}

	if check.Status == "" {
		t.Error("Check status should not be empty")
	}

	if check.Duration <= 0 {
		t.Error("Check duration should be positive")
	}

	t.Log("System resource check test passed")
}
