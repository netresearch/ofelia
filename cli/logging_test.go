package cli

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected slog.Level
		wantErr  bool
	}{
		{name: "debug", input: "debug", expected: slog.LevelDebug},
		{name: "info", input: "info", expected: slog.LevelInfo},
		{name: "warn", input: "warn", expected: slog.LevelWarn},
		{name: "warning", input: "warning", expected: slog.LevelWarn},
		{name: "error", input: "error", expected: slog.LevelError},
		{name: "empty is noop", input: "", expected: slog.LevelInfo},
		{name: "invalid", input: "bogus", wantErr: true},
		{name: "notice maps to info", input: "notice", expected: slog.LevelInfo},
		{name: "trace maps to debug", input: "trace", expected: slog.LevelDebug},
		{name: "fatal maps to error", input: "fatal", expected: slog.LevelError},
		{name: "panic maps to error", input: "panic", expected: slog.LevelError},
		{name: "critical maps to error", input: "critical", expected: slog.LevelError},
		{name: "case insensitive DEBUG", input: "DEBUG", expected: slog.LevelDebug},
		{name: "typo in debug", input: "degub", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lv := &slog.LevelVar{}
			lv.Set(slog.LevelInfo)
			err := ApplyLogLevel(tc.input, lv)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.input != "" {
				assert.Equal(t, tc.expected, lv.Level())
			}
		})
	}
}
