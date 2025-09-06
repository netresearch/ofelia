package logging

import (
	"bytes"
	"encoding/json"
	"strings"
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
