package cli

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestApplyLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		expectErr bool
		expected  logrus.Level
	}{
		{
			name:      "valid debug level",
			level:     "debug",
			expectErr: false,
			expected:  logrus.DebugLevel,
		},
		{
			name:      "valid info level",
			level:     "info",
			expectErr: false,
			expected:  logrus.InfoLevel,
		},
		{
			name:      "valid warning level",
			level:     "warning",
			expectErr: false,
			expected:  logrus.WarnLevel,
		},
		{
			name:      "valid error level",
			level:     "error",
			expectErr: false,
			expected:  logrus.ErrorLevel,
		},
		{
			name:      "empty level",
			level:     "",
			expectErr: false,
			expected:  logrus.InfoLevel, // Should not change current level
		},
		{
			name:      "invalid level",
			level:     "invalid",
			expectErr: true,
			expected:  logrus.InfoLevel, // Should not change current level
		},
		{
			name:      "typo in debug",
			level:     "degub",
			expectErr: true,
			expected:  logrus.InfoLevel,
		},
		{
			name:      "case insensitive",
			level:     "DEBUG",
			expectErr: false,
			expected:  logrus.DebugLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset to info level before each test
			logrus.SetLevel(logrus.InfoLevel)

			err := ApplyLogLevel(tt.level)

			if tt.expectErr && err == nil {
				t.Errorf("Expected error for level %q, but got none", tt.level)
			}

			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error for level %q: %v", tt.level, err)
			}

			if !tt.expectErr {
				currentLevel := logrus.GetLevel()
				if currentLevel != tt.expected {
					t.Errorf("Expected level %v, got %v", tt.expected, currentLevel)
				}
			}
		})
	}
}
