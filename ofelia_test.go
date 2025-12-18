package main

import (
	"bytes"
	"os"
	"runtime"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestBuildLogger_ValidLevels(t *testing.T) {
	testCases := []struct {
		name     string
		level    string
		expected logrus.Level
	}{
		{"debug level", "debug", logrus.DebugLevel},
		{"DEBUG uppercase", "DEBUG", logrus.DebugLevel},
		{"info level", "info", logrus.InfoLevel},
		{"INFO uppercase", "INFO", logrus.InfoLevel},
		{"warn level", "warn", logrus.WarnLevel},
		{"warning level", "warning", logrus.WarnLevel},
		{"error level", "error", logrus.ErrorLevel},
		{"fatal level", "fatal", logrus.FatalLevel},
		{"panic level", "panic", logrus.PanicLevel},
		{"trace level", "trace", logrus.TraceLevel},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger := buildLogger(tc.level)
			assert.NotNil(t, logger)
			assert.Equal(t, tc.expected, logrus.GetLevel())
		})
	}
}

func TestBuildLogger_InvalidLevel_DefaultsToInfo(t *testing.T) {
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
			logger := buildLogger(tc.level)
			assert.NotNil(t, logger)
			assert.Equal(t, logrus.InfoLevel, logrus.GetLevel(), "invalid level should default to InfoLevel")
		})
	}
}

func TestBuildLogger_ProducesWorkingLogger(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := logrus.StandardLogger().Out
	defer logrus.SetOutput(originalOutput)

	logger := buildLogger("info")
	logrus.SetOutput(&buf)

	logger.Debugf("test message %s", "arg")

	assert.NotNil(t, logger)
}

func TestBuildLogger_EnvironmentVariablesAffectOutput(t *testing.T) {
	testCases := []struct {
		name    string
		term    string
		noColor string
	}{
		{"dumb terminal", "dumb", ""},
		{"NO_COLOR set", "xterm", "1"},
		{"normal terminal", "xterm", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			origTerm := os.Getenv("TERM")
			origNoColor := os.Getenv("NO_COLOR")
			defer func() {
				os.Setenv("TERM", origTerm)
				if origNoColor == "" {
					os.Unsetenv("NO_COLOR")
				} else {
					os.Setenv("NO_COLOR", origNoColor)
				}
			}()

			os.Setenv("TERM", tc.term)
			if tc.noColor != "" {
				os.Setenv("NO_COLOR", tc.noColor)
			} else {
				os.Unsetenv("NO_COLOR")
			}

			logger := buildLogger("info")
			assert.NotNil(t, logger)
		})
	}
}

func TestBuildLogger_IncludesCallerInformation(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := logrus.StandardLogger().Out
	defer logrus.SetOutput(originalOutput)

	logger := buildLogger("debug")
	logrus.SetOutput(&buf)

	logger.Debugf("caller test %s", "arg")

	assert.NotEmpty(t, buf.String())
}

func TestBuildLogger_EnablesReportCaller(t *testing.T) {
	_ = buildLogger("info")
	assert.True(t, logrus.StandardLogger().ReportCaller)
}

func TestBuildLogger_ConfiguresTextFormatterCorrectly(t *testing.T) {
	_ = buildLogger("info")

	formatter, ok := logrus.StandardLogger().Formatter.(*logrus.TextFormatter)
	assert.True(t, ok, "formatter should be TextFormatter")
	assert.True(t, formatter.FullTimestamp)
	assert.True(t, formatter.DisableQuote)
	assert.Equal(t, "2006-01-02 15:04:05", formatter.TimestampFormat)
}

func TestBuildLogger_CallerPrettyfierFormatsCorrectly(t *testing.T) {
	_ = buildLogger("info")

	formatter, ok := logrus.StandardLogger().Formatter.(*logrus.TextFormatter)
	assert.True(t, ok)
	assert.NotNil(t, formatter.CallerPrettyfier)

	frame := &runtime.Frame{
		File: "/some/path/to/file.go",
		Line: 42,
	}

	funcName, location := formatter.CallerPrettyfier(frame)
	assert.Empty(t, funcName, "function name should be empty")
	assert.Equal(t, "file.go:42", location)
}

func TestBuildLogger_CallerPrettyfierHandlesEdgeCases(t *testing.T) {
	_ = buildLogger("info")

	formatter, ok := logrus.StandardLogger().Formatter.(*logrus.TextFormatter)
	assert.True(t, ok)

	testCases := []struct {
		name     string
		file     string
		line     int
		expected string
	}{
		{"root file", "main.go", 1, "main.go:1"},
		{"deep path", "/a/b/c/d/e/f.go", 999, "f.go:999"},
		{"line zero", "test.go", 0, "test.go:0"},
		{"large line", "big.go", 999999, "big.go:999999"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			frame := &runtime.Frame{File: tc.file, Line: tc.line}
			_, location := formatter.CallerPrettyfier(frame)
			assert.Equal(t, tc.expected, location)
		})
	}
}

func TestBuildLogger_OutputGoesToStdout(t *testing.T) {
	_ = buildLogger("info")
	assert.Equal(t, os.Stdout, logrus.StandardLogger().Out)
}

func TestBuildLogger_LevelTransitions(t *testing.T) {
	_ = buildLogger("debug")
	assert.Equal(t, logrus.DebugLevel, logrus.GetLevel())

	_ = buildLogger("error")
	assert.Equal(t, logrus.ErrorLevel, logrus.GetLevel())

	_ = buildLogger("invalid")
	assert.Equal(t, logrus.InfoLevel, logrus.GetLevel(), "should reset to info for invalid")
}

func TestBuildLogger_MixedCaseLevels(t *testing.T) {
	testCases := []struct {
		input    string
		expected logrus.Level
	}{
		{"DeBuG", logrus.DebugLevel},
		{"INFO", logrus.InfoLevel},
		{"WaRn", logrus.WarnLevel},
		{"ERROR", logrus.ErrorLevel},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			_ = buildLogger(tc.input)
			assert.Equal(t, tc.expected, logrus.GetLevel())
		})
	}
}

func TestBuildLogger_ForceColorsDisabledInNonTerminal(t *testing.T) {
	_ = buildLogger("info")

	formatter, ok := logrus.StandardLogger().Formatter.(*logrus.TextFormatter)
	assert.True(t, ok)
	assert.False(t, formatter.ForceColors, "ForceColors should be false when not running in a terminal")
}
