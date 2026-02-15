package test

import (
	"fmt"
	"strings"
	"sync"
)

// TestLogger is a shared test logger implementation for use across test suites.
// It implements the core.Logger interface and provides methods to capture and verify log output.
type Logger struct {
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
func NewTestLogger(verbose ...bool) *Logger {
	v := false
	if len(verbose) > 0 {
		v = verbose[0]
	}
	return &Logger{
		messages: make([]LogEntry, 0),
		verbose:  v,
	}
}

// Criticalf logs a critical message
func (l *Logger) Criticalf(s string, v ...any) {
	l.log("CRITICAL", s, v...)
}

// Errorf logs an error message
func (l *Logger) Errorf(s string, v ...any) {
	l.log("ERROR", s, v...)
}

// Warningf logs a warning message
func (l *Logger) Warningf(s string, v ...any) {
	l.log("WARN", s, v...)
}

// Noticef logs a notice message
func (l *Logger) Noticef(s string, v ...any) {
	l.log("NOTICE", s, v...)
}

// Infof logs an info message
func (l *Logger) Infof(s string, v ...any) {
	l.log("INFO", s, v...)
}

// Debugf logs a debug message
func (l *Logger) Debugf(s string, v ...any) {
	l.log("DEBUG", s, v...)
}

// Deprecated methods for backward compatibility
func (l *Logger) Error(s string)   { l.Errorf("%s", s) }
func (l *Logger) Warning(s string) { l.Warningf("%s", s) }
func (l *Logger) Notice(s string)  { l.Noticef("%s", s) }
func (l *Logger) Info(s string)    { l.Infof("%s", s) }
func (l *Logger) Debug(s string)   { l.Debugf("%s", s) }

// Shortened names for brevity
func (l *Logger) Err(s string)  { l.Errorf("%s", s) }
func (l *Logger) Warn(s string) { l.Warningf("%s", s) }
func (l *Logger) Log(s string)  { l.Infof("%s", s) }

// log is the internal logging method
func (l *Logger) log(level, format string, v ...any) {
	msg := fmt.Sprintf(format, v...)

	l.mu.Lock()
	l.messages = append(l.messages, LogEntry{
		Level:   level,
		Message: msg,
	})
	l.mu.Unlock()

	// Verbose output is disabled to avoid using forbidden fmt.Print functions
}

// GetMessages returns all logged messages
func (l *Logger) GetMessages() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]LogEntry, len(l.messages))
	copy(result, l.messages)
	return result
}

// HasMessage checks if a message containing the substring was logged
func (l *Logger) HasMessage(substr string) bool {
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
func (l *Logger) HasError(substr string) bool {
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
func (l *Logger) HasWarning(substr string) bool {
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
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = l.messages[:0]
}

// MessageCount returns the number of logged messages
func (l *Logger) MessageCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.messages)
}

// ErrorCount returns the number of error messages
func (l *Logger) ErrorCount() int {
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

// WarningCount returns the number of warning messages
func (l *Logger) WarningCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	count := 0
	for _, entry := range l.messages {
		if entry.Level == "WARN" {
			count++
		}
	}
	return count
}
