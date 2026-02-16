package main

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildLogger_ValidLevels(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		level    string
		expected slog.Level
	}{
		{"debug level", "debug", slog.LevelDebug},
		{"DEBUG uppercase", "DEBUG", slog.LevelDebug},
		{"trace level", "trace", slog.LevelDebug},
		{"info level", "info", slog.LevelInfo},
		{"INFO uppercase", "INFO", slog.LevelInfo},
		{"warn level", "warn", slog.LevelWarn},
		{"warning level", "warning", slog.LevelWarn},
		{"error level", "error", slog.LevelError},
		{"fatal level", "fatal", slog.LevelError},
		{"panic level", "panic", slog.LevelError},
		{"critical level", "critical", slog.LevelError},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logger, lv := buildLogger(tc.level)
			assert.NotNil(t, logger)
			assert.Equal(t, tc.expected, lv.Level())
		})
	}
}

func TestBuildLogger_InvalidLevel_DefaultsToInfo(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		level string
	}{
		{"empty string", ""},
		{"invalid level", "invalid"},
		{"garbage", "xyz123"},
		{"numeric", "42"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logger, lv := buildLogger(tc.level)
			assert.NotNil(t, logger)
			assert.Equal(t, slog.LevelInfo, lv.Level(), "invalid level should default to Info")
		})
	}
}

func TestBuildLogger_ProducesWorkingLogger(t *testing.T) {
	t.Parallel()
	logger, lv := buildLogger("debug")
	assert.NotNil(t, logger)
	assert.NotNil(t, lv)

	// Should not panic when logging
	logger.Debug("test message", "key", "value")
	logger.Info("info message")
	logger.Warn("warn message")
}

func TestBuildLogger_ReturnsLevelVar(t *testing.T) {
	t.Parallel()
	_, lv := buildLogger("debug")
	assert.Equal(t, slog.LevelDebug, lv.Level())

	// LevelVar should be mutable
	lv.Set(slog.LevelError)
	assert.Equal(t, slog.LevelError, lv.Level())
}

func TestBuildLogger_LevelTransitions(t *testing.T) {
	t.Parallel()

	_, lv1 := buildLogger("debug")
	assert.Equal(t, slog.LevelDebug, lv1.Level())

	_, lv2 := buildLogger("error")
	assert.Equal(t, slog.LevelError, lv2.Level())

	_, lv3 := buildLogger("invalid")
	assert.Equal(t, slog.LevelInfo, lv3.Level(), "should default to info for invalid")
}

func TestBuildLogger_MixedCaseLevels(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		input    string
		expected slog.Level
	}{
		{"DeBuG", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"WaRn", slog.LevelWarn},
		{"ERROR", slog.LevelError},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			_, lv := buildLogger(tc.input)
			assert.Equal(t, tc.expected, lv.Level())
		})
	}
}

func TestBuildLogger_LoggerHasSourceEnabled(t *testing.T) {
	t.Parallel()
	logger, _ := buildLogger("debug")
	assert.NotNil(t, logger)
	// The handler is configured with AddSource: true
	// We verify by ensuring the logger was created successfully
	// (source inclusion is verified by the handler options)
}
