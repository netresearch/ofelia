package test

import (
	"fmt"
	"strings"
	"sync"
)

// TestLogger is a shared test logger implementation for use across test suites.
// It implements the core.Logger interface and provides methods to capture and verify log output.
type TestLogger struct {
	mu       sync.RWMutex
	messages []LogEntry
	verbose  bool // If true, print logs to stdout during tests
}

// LogEntry represents a single log message with its level
type LogEntry struct {
	Level   string
	Message string
}

// NewTestLogger creates a new test logger
func NewTestLogger(verbose ...bool) *TestLogger {
	v := false
	if len(verbose) > 0 {
		v = verbose[0]
	}
	return &TestLogger{
		messages: make([]LogEntry, 0),
		verbose:  v,
	}
}

// Criticalf logs a critical message
func (l *TestLogger) Criticalf(s string, v ...interface{}) {
	l.log("CRITICAL", s, v...)
}

// Errorf logs an error message
func (l *TestLogger) Errorf(s string, v ...interface{}) {
	l.log("ERROR", s, v...)
}

// Warningf logs a warning message
func (l *TestLogger) Warningf(s string, v ...interface{}) {
	l.log("WARN", s, v...)
}

// Noticef logs a notice message
func (l *TestLogger) Noticef(s string, v ...interface{}) {
	l.log("NOTICE", s, v...)
}

// Infof logs an info message
func (l *TestLogger) Infof(s string, v ...interface{}) {
	l.log("INFO", s, v...)
}

// Debugf logs a debug message
func (l *TestLogger) Debugf(s string, v ...interface{}) {
	l.log("DEBUG", s, v...)
}

// Deprecated methods for backward compatibility
func (l *TestLogger) Error(s string)   { l.Errorf("%s", s) }
func (l *TestLogger) Warning(s string) { l.Warningf("%s", s) }
func (l *TestLogger) Notice(s string)  { l.Noticef("%s", s) }
func (l *TestLogger) Info(s string)    { l.Infof("%s", s) }
func (l *TestLogger) Debug(s string)   { l.Debugf("%s", s) }

// Shortened names for brevity
func (l *TestLogger) Err(s string)  { l.Errorf("%s", s) }
func (l *TestLogger) Warn(s string) { l.Warningf("%s", s) }
func (l *TestLogger) Log(s string)  { l.Infof("%s", s) }

// log is the internal logging method
func (l *TestLogger) log(level, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)

	l.mu.Lock()
	l.messages = append(l.messages, LogEntry{
		Level:   level,
		Message: msg,
	})
	l.mu.Unlock()

	if l.verbose {
		fmt.Printf("[%s] %s\n", level, msg)
	}
}

// GetMessages returns all logged messages
func (l *TestLogger) GetMessages() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]LogEntry, len(l.messages))
	copy(result, l.messages)
	return result
}

// HasMessage checks if a message containing the substring was logged
func (l *TestLogger) HasMessage(substr string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, entry := range l.messages {
		if strings.Contains(entry.Message, substr) {
			return true
		}
	}
	return false
}

// HasError checks if an error containing the substring was logged
func (l *TestLogger) HasError(substr string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, entry := range l.messages {
		if entry.Level == "ERROR" && strings.Contains(entry.Message, substr) {
			return true
		}
	}
	return false
}

// HasWarning checks if a warning containing the substring was logged
func (l *TestLogger) HasWarning(substr string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, entry := range l.messages {
		if entry.Level == "WARN" && strings.Contains(entry.Message, substr) {
			return true
		}
	}
	return false
}

// Clear clears all logged messages
func (l *TestLogger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = l.messages[:0]
}

// MessageCount returns the number of logged messages
func (l *TestLogger) MessageCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.messages)
}

// ErrorCount returns the number of error messages
func (l *TestLogger) ErrorCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	count := 0
	for _, entry := range l.messages {
		if entry.Level == "ERROR" {
			count++
		}
	}
	return count
}
