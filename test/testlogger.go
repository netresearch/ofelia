package test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

// Handler is a slog.Handler that captures log records for test assertions.
type Handler struct {
	mu      sync.RWMutex
	records []LogEntry
	level   slog.Level
}

// LogEntry represents a single log message with its level.
type LogEntry struct {
	Level   string
	Message string
}

// NewHandler creates a new test handler that captures all log levels.
func NewHandler() *Handler {
	return &Handler{
		records: make([]LogEntry, 0),
		level:   slog.LevelDebug, // capture everything
	}
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle implements slog.Handler.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.records = append(h.records, LogEntry{
		Level:   r.Level.String(),
		Message: r.Message,
	})
	return nil
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(_ string) slog.Handler {
	return h
}

// NewTestLogger creates a *slog.Logger backed by a capturing handler.
// The returned logger can be passed anywhere a *slog.Logger is expected.
// If you need to inspect captured log messages, use NewTestLoggerWithHandler instead.
func NewTestLogger() *slog.Logger {
	return slog.New(NewHandler())
}

// NewTestLoggerWithHandler creates a *slog.Logger and returns both the logger
// and its Handler, allowing tests to inspect captured log messages.
func NewTestLoggerWithHandler() (*slog.Logger, *Handler) {
	h := NewHandler()
	return slog.New(h), h
}

// GetMessages returns all logged messages.
func (h *Handler) GetMessages() []LogEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]LogEntry, len(h.records))
	copy(result, h.records)
	return result
}

// HasMessage checks if any message containing substr was logged.
func (h *Handler) HasMessage(substr string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, e := range h.records {
		if strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

// HasError checks if an error message containing substr was logged.
func (h *Handler) HasError(substr string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, e := range h.records {
		if e.Level == "ERROR" && strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

// HasWarning checks if a warning message containing substr was logged.
func (h *Handler) HasWarning(substr string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, e := range h.records {
		if e.Level == "WARN" && strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

// Clear clears all captured messages.
func (h *Handler) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = h.records[:0]
}

// MessageCount returns the total number of captured messages.
func (h *Handler) MessageCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.records)
}

// ErrorCount returns the number of error messages.
func (h *Handler) ErrorCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for _, e := range h.records {
		if e.Level == "ERROR" {
			count++
		}
	}
	return count
}

// WarningCount returns the number of warning messages.
func (h *Handler) WarningCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for _, e := range h.records {
		if e.Level == "WARN" {
			count++
		}
	}
	return count
}
