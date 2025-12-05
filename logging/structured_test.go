package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogLevels(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(false)

	// Test different log levels
	logger.SetLevel(InfoLevel)

	logger.Debug("debug message")
	if buf.Len() > 0 {
		t.Error("Debug message should not be logged at Info level")
	}

	buf.Reset()
	logger.Info("info message")
	if !strings.Contains(buf.String(), "info message") {
		t.Error("Info message should be logged")
	}

	buf.Reset()
	logger.Warn("warning message")
	if !strings.Contains(buf.String(), "warning message") {
		t.Error("Warning message should be logged")
	}

	buf.Reset()
	logger.Error("error message")
	if !strings.Contains(buf.String(), "error message") {
		t.Error("Error message should be logged")
	}

	t.Log("Log level filtering test passed")
}

func TestStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(true)

	// Log with fields
	logger.InfoWithFields("test message", map[string]interface{}{
		"user_id": 123,
		"action":  "login",
		"success": true,
	})

	// Parse JSON output
	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}

	// Check fields
	if entry.Message != "test message" {
		t.Errorf("Expected message 'test message', got '%s'", entry.Message)
	}

	if entry.Fields["user_id"] != float64(123) { // JSON unmarshals numbers as float64
		t.Errorf("Expected user_id 123, got %v", entry.Fields["user_id"])
	}

	if entry.Fields["action"] != "login" {
		t.Errorf("Expected action 'login', got %v", entry.Fields["action"])
	}

	if entry.Fields["success"] != true {
		t.Errorf("Expected success true, got %v", entry.Fields["success"])
	}

	t.Log("Structured fields test passed")
}

func TestLoggerChaining(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(true)

	// Create chained logger with fields
	jobLogger := logger.
		WithField("service", "ofelia").
		WithField("version", "1.0.0").
		WithFields(map[string]interface{}{
			"environment": "production",
			"region":      "us-east-1",
		})

	jobLogger.Info("deployment started")

	// Parse output
	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Check all fields are present
	expectedFields := map[string]interface{}{
		"service":     "ofelia",
		"version":     "1.0.0",
		"environment": "production",
		"region":      "us-east-1",
	}

	for key, expected := range expectedFields {
		if entry.Fields[key] != expected {
			t.Errorf("Field %s: expected %v, got %v", key, expected, entry.Fields[key])
		}
	}

	t.Log("Logger chaining test passed")
}

func TestCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(true)

	// Create logger with correlation ID
	correlatedLogger := logger.WithCorrelationID("req-123-456")
	correlatedLogger.Info("processing request")

	// Parse output
	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.CorrelationID != "req-123-456" {
		t.Errorf("Expected correlation ID 'req-123-456', got '%s'", entry.CorrelationID)
	}

	t.Log("Correlation ID test passed")
}

func TestJobLogger(t *testing.T) {
	var buf bytes.Buffer
	jobLogger := NewJobLogger("job-001", "backup-task")
	jobLogger.SetOutput(&buf)
	jobLogger.SetJSONFormat(true)

	// Log job start
	jobLogger.LogStart()

	// Check start event
	var entry LogEntry
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("Failed to parse start event: %v", err)
	}

	if entry.Fields["event"] != "job_start" {
		t.Error("Expected job_start event")
	}
	if entry.Fields["job_id"] != "job-001" {
		t.Error("Expected job_id in fields")
	}

	// Log progress
	buf.Reset()
	jobLogger.LogProgress("Processing items", 50.0)

	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse progress event: %v", err)
	}

	if entry.Fields["event"] != "job_progress" {
		t.Error("Expected job_progress event")
	}
	if entry.Fields["progress"] != float64(50.0) {
		t.Errorf("Expected progress 50.0, got %v", entry.Fields["progress"])
	}

	// Log completion
	buf.Reset()
	jobLogger.LogComplete(5*time.Second, true)

	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse complete event: %v", err)
	}

	if entry.Fields["event"] != "job_complete" {
		t.Error("Expected job_complete event")
	}
	if entry.Fields["success"] != true {
		t.Error("Expected success true")
	}
	if entry.Fields["duration"] != float64(5) {
		t.Errorf("Expected duration 5, got %v", entry.Fields["duration"])
	}

	t.Log("Job logger test passed")
}

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(false) // Use text format

	logger.InfoWithFields("user login", map[string]interface{}{
		"user": "admin",
		"ip":   "192.168.1.1",
	})

	output := buf.String()

	// Check text format contains expected elements
	if !strings.Contains(output, "[INFO]") {
		t.Error("Text format should contain log level")
	}
	if !strings.Contains(output, "user login") {
		t.Error("Text format should contain message")
	}
	if !strings.Contains(output, "admin") {
		t.Error("Text format should contain field values")
	}

	t.Log("Text format test passed")
}

func TestErrorStackTrace(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(true)
	logger.includeCaller = true

	// Log an error
	logger.Error("database connection failed")

	// Parse output
	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Check stack trace is included for errors
	if entry.StackTrace == "" {
		t.Error("Stack trace should be included for error level logs")
	}

	// Check caller information
	if entry.Caller == "" {
		t.Error("Caller information should be included")
	}

	t.Log("Error stack trace test passed")
}

func TestFormattedLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(false)

	// Test formatted methods
	logger.Infof("User %s logged in from %s", "alice", "192.168.1.1")

	output := buf.String()
	if !strings.Contains(output, "User alice logged in from 192.168.1.1") {
		t.Error("Formatted logging not working correctly")
	}

	buf.Reset()
	logger.Debugf("Processing %d items", 42)
	logger.SetLevel(DebugLevel)
	logger.Debugf("Processing %d items", 42)

	if !strings.Contains(buf.String(), "Processing 42 items") {
		t.Error("Formatted debug logging not working")
	}

	t.Log("Formatted logging test passed")
}

// New comprehensive tests for missing coverage

func TestLogLevelString(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{DebugLevel, "DEBUG"},
		{InfoLevel, "INFO"},
		{WarnLevel, "WARN"},
		{ErrorLevel, "ERROR"},
		{FatalLevel, "FATAL"},
		{LogLevel(99), "UNKNOWN"}, // Test default case
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("LogLevel.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAllLogLevelsWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(true)
	logger.SetLevel(DebugLevel) // Enable all levels

	testFields := map[string]interface{}{
		"test_key": "test_value",
		"count":    42,
	}

	tests := []struct {
		name     string
		logFunc  func()
		level    string
		checkMsg string
	}{
		{
			name: "DebugWithFields",
			logFunc: func() {
				logger.DebugWithFields("debug message", testFields)
			},
			level:    "DEBUG",
			checkMsg: "debug message",
		},
		{
			name: "WarnWithFields",
			logFunc: func() {
				logger.WarnWithFields("warning message", testFields)
			},
			level:    "WARN",
			checkMsg: "warning message",
		},
		{
			name: "ErrorWithFields",
			logFunc: func() {
				logger.ErrorWithFields("error message", testFields)
			},
			level:    "ERROR",
			checkMsg: "error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc()

			var entry LogEntry
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			if entry.Level != tt.level {
				t.Errorf("Expected level %s, got %s", tt.level, entry.Level)
			}

			if entry.Message != tt.checkMsg {
				t.Errorf("Expected message %s, got %s", tt.checkMsg, entry.Message)
			}

			if entry.Fields["test_key"] != "test_value" {
				t.Error("Expected test_key field to be present")
			}

			if entry.Fields["count"] != float64(42) {
				t.Error("Expected count field to be 42")
			}
		})
	}
}

func TestFormattedWarnAndError(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(true)

	tests := []struct {
		name     string
		logFunc  func()
		level    string
		contains string
	}{
		{
			name: "Warnf",
			logFunc: func() {
				logger.Warnf("Warning: %s has %d issues", "system", 3)
			},
			level:    "WARN",
			contains: "Warning: system has 3 issues",
		},
		{
			name: "Errorf",
			logFunc: func() {
				logger.Errorf("Error in %s: code %d", "module", 500)
			},
			level:    "ERROR",
			contains: "Error in module: code 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc()

			var entry LogEntry
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			if entry.Level != tt.level {
				t.Errorf("Expected level %s, got %s", tt.level, entry.Level)
			}

			if entry.Message != tt.contains {
				t.Errorf("Expected message '%s', got '%s'", tt.contains, entry.Message)
			}
		})
	}
}

func TestFatalLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(true)

	tests := []struct {
		name     string
		logFunc  func()
		expected string
	}{
		{
			name: "Fatal",
			logFunc: func() {
				logger.Fatal("critical system failure")
			},
			expected: "critical system failure",
		},
		{
			name: "Fatalf",
			logFunc: func() {
				logger.Fatalf("Fatal error in %s: %d", "database", 1001)
			},
			expected: "Fatal error in database: 1001",
		},
		{
			name: "FatalWithFields",
			logFunc: func() {
				logger.FatalWithFields("system crash", map[string]interface{}{
					"error_code": 500,
					"component":  "core",
				})
			},
			expected: "system crash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc()

			var entry LogEntry
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			if entry.Level != "FATAL" {
				t.Errorf("Expected level FATAL, got %s", entry.Level)
			}

			if entry.Message != tt.expected {
				t.Errorf("Expected message '%s', got '%s'", tt.expected, entry.Message)
			}

			// Fatal should include stack trace
			if entry.StackTrace == "" {
				t.Error("Stack trace should be included for fatal level logs")
			}
		})
	}
}

func TestJobLoggerWithMetrics(t *testing.T) {
	var buf bytes.Buffer
	jobLogger := NewJobLogger("job-002", "test-job")
	jobLogger.SetOutput(&buf)
	jobLogger.SetJSONFormat(true)

	// Create mock metrics collector
	metrics := &MockMetricsCollector{
		counters:   make(map[string]float64),
		gauges:     make(map[string]float64),
		histograms: make(map[string][]float64),
	}

	// Set metrics collector
	jobLogger.SetMetricsCollector(metrics)

	// Test LogStart with metrics
	jobLogger.LogStart()
	if metrics.counters["jobs_started_total"] != 1 {
		t.Errorf("Expected jobs_started_total counter to be 1, got %f", metrics.counters["jobs_started_total"])
	}
	if metrics.gauges["jobs_running"] != 1 {
		t.Errorf("Expected jobs_running gauge to be 1, got %f", metrics.gauges["jobs_running"])
	}

	// Test LogComplete success with metrics
	buf.Reset()
	jobLogger.LogComplete(3*time.Second, true)
	if metrics.counters["jobs_success_total"] != 1 {
		t.Error("Expected jobs_success_total counter to be incremented")
	}
	if len(metrics.histograms["job_duration_seconds"]) != 1 {
		t.Error("Expected job duration to be recorded in histogram")
	}

	// Test LogComplete failure with metrics
	buf.Reset()
	jobLogger.LogComplete(2*time.Second, false)
	if metrics.counters["jobs_failed_total"] != 1 {
		t.Error("Expected jobs_failed_total counter to be incremented")
	}

	// Test LogProgress with metrics
	buf.Reset()
	jobLogger.LogProgress("halfway done", 50.0)
	if metrics.gauges["job_progress_percent"] != 50.0 {
		t.Errorf("Expected job_progress_percent gauge to be 50.0, got %f", metrics.gauges["job_progress_percent"])
	}

	// Test LogError with metrics
	buf.Reset()
	testErr := errors.New("test error")
	jobLogger.LogError(testErr, "during processing")

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.Fields["event"] != "job_error" {
		t.Error("Expected job_error event")
	}
	if entry.Fields["error"] != "test error" {
		t.Error("Expected error message in fields")
	}
	if entry.Fields["context"] != "during processing" {
		t.Error("Expected context in fields")
	}
	if metrics.counters["job_errors_total"] != 1 {
		t.Error("Expected job_errors_total counter to be incremented")
	}

	// Test LogRetry with metrics
	buf.Reset()
	retryErr := errors.New("connection timeout")
	jobLogger.LogRetry(2, 5, retryErr)

	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.Fields["event"] != "job_retry" {
		t.Error("Expected job_retry event")
	}
	if entry.Fields["attempt"] != float64(2) {
		t.Error("Expected attempt number in fields")
	}
	if entry.Fields["max_attempts"] != float64(5) {
		t.Error("Expected max_attempts in fields")
	}
	if entry.Fields["error"] != "connection timeout" {
		t.Error("Expected error message in fields")
	}
	if metrics.counters["job_retries_total"] != 1 {
		t.Error("Expected job_retries_total counter to be incremented")
	}
}

func TestPackageLevelFunctions(t *testing.T) {
	var buf bytes.Buffer
	DefaultLogger.SetOutput(&buf)
	DefaultLogger.SetJSONFormat(true)
	DefaultLogger.SetLevel(DebugLevel)

	tests := []struct {
		name    string
		logFunc func()
		level   string
		message string
	}{
		{
			name:    "PackageDebug",
			logFunc: func() { Debug("package debug message") },
			level:   "DEBUG",
			message: "package debug message",
		},
		{
			name:    "PackageInfo",
			logFunc: func() { Info("package info message") },
			level:   "INFO",
			message: "package info message",
		},
		{
			name:    "PackageWarn",
			logFunc: func() { Warn("package warn message") },
			level:   "WARN",
			message: "package warn message",
		},
		{
			name:    "PackageError",
			logFunc: func() { Error("package error message") },
			level:   "ERROR",
			message: "package error message",
		},
		{
			name:    "PackageFatal",
			logFunc: func() { Fatal("package fatal message") },
			level:   "FATAL",
			message: "package fatal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc()

			var entry LogEntry
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			if entry.Level != tt.level {
				t.Errorf("Expected level %s, got %s", tt.level, entry.Level)
			}

			if entry.Message != tt.message {
				t.Errorf("Expected message '%s', got '%s'", tt.message, entry.Message)
			}
		})
	}
}

func TestTextFormatWithCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStructuredLogger()
	logger.SetOutput(&buf)
	logger.SetJSONFormat(false)

	correlatedLogger := logger.WithCorrelationID("corr-123")
	correlatedLogger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "[corr-123]") {
		t.Error("Text format should include correlation ID")
	}
}

func TestJobLoggerWithoutMetrics(t *testing.T) {
	var buf bytes.Buffer
	jobLogger := NewJobLogger("job-003", "no-metrics-job")
	jobLogger.SetOutput(&buf)
	jobLogger.SetJSONFormat(true)

	// Test all methods without metrics collector (should not panic)
	jobLogger.LogStart()
	jobLogger.LogProgress("testing", 25.0)
	jobLogger.LogComplete(1*time.Second, true)
	jobLogger.LogError(errors.New("test"), "context")
	jobLogger.LogRetry(1, 3, errors.New("retry"))

	// Should have logged without errors
	if buf.Len() == 0 {
		t.Error("Expected log output even without metrics collector")
	}
}

func TestConcurrentLogging(t *testing.T) {
	// Use thread-safe writer
	sw := &safeWriter{buf: &bytes.Buffer{}}
	logger := NewStructuredLogger()
	logger.SetOutput(sw)
	logger.SetJSONFormat(true)

	// Test concurrent writes don't cause races in logger
	const testTimeout = 10 * time.Second // Timeout for mutation testing
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			logger.Infof("concurrent message %d", id)
			done <- true
		}(i)
	}

	// Wait for all goroutines with timeout
	timeout := time.After(testTimeout)
	for i := 0; i < 10; i++ {
		select {
		case <-done:
			// goroutine completed
		case <-timeout:
			t.Fatalf("Test timed out waiting for goroutine %d", i)
		}
	}

	// Should have 10 log entries
	sw.mu.Lock()
	lines := strings.Split(strings.TrimSpace(sw.buf.String()), "\n")
	sw.mu.Unlock()

	if len(lines) != 10 {
		t.Errorf("Expected 10 log lines, got %d", len(lines))
	}
}

// safeWriter is a thread-safe writer for testing concurrent logging
type safeWriter struct {
	mu  sync.Mutex
	buf *bytes.Buffer
}

func (sw *safeWriter) Write(p []byte) (n int, err error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.buf.Write(p)
}

// MockMetricsCollector for testing
type MockMetricsCollector struct {
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string][]float64
}

func (m *MockMetricsCollector) IncrementCounter(name string, value float64) {
	m.counters[name] += value
}

func (m *MockMetricsCollector) SetGauge(name string, value float64) {
	m.gauges[name] = value
}

func (m *MockMetricsCollector) ObserveHistogram(name string, value float64) {
	m.histograms[name] = append(m.histograms[name], value)
}
