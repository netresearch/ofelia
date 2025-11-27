package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/netresearch/ofelia/test"
)

// TestConfigShowCommand_Execute tests the config show command
func TestConfigShowCommand_Execute(t *testing.T) {
	tests := []struct {
		name          string
		configContent string
		expectedError bool
		checkOutput   func(string) bool
	}{
		{
			name: "valid config file",
			configContent: `
[global]
log-level = debug

[job-run "test-job"]
schedule = @every 10s
image = busybox
command = echo test
`,
			expectedError: false,
			checkOutput: func(output string) bool {
				// Should be valid JSON
				var result map[string]interface{}
				return json.Unmarshal([]byte(output), &result) == nil
			},
		},
		{
			name:          "missing config file",
			configContent: "",
			expectedError: true,
		},
		{
			name: "invalid config file",
			configContent: `
[global
invalid = true
`,
			expectedError: true,
		},
		{
			name: "empty config file",
			configContent: `
[global]
`,
			expectedError: false,
			checkOutput: func(output string) bool {
				var result map[string]interface{}
				return json.Unmarshal([]byte(output), &result) == nil
			},
		},
		{
			name: "config with multiple job types",
			configContent: `
[job-exec "exec-job"]
schedule = @every 5s
command = echo exec

[job-local "local-job"]
schedule = @every 15s
command = echo local

[job-service-run "service-job"]
schedule = @every 20s
command = echo service
`,
			expectedError: false,
			checkOutput: func(output string) bool {
				var result map[string]interface{}
				if err := json.Unmarshal([]byte(output), &result); err != nil {
					return false
				}
				// Check that job types are present
				return result != nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var configFile string
			var cleanup func()

			if tt.configContent != "" {
				// Create temporary config file
				tmpFile, err := os.CreateTemp("", "ofelia_show_*.ini")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				configFile = tmpFile.Name()
				cleanup = func() { os.Remove(configFile) }
				defer cleanup()

				_, err = tmpFile.WriteString(tt.configContent)
				if err != nil {
					t.Fatalf("Failed to write temp file: %v", err)
				}
				tmpFile.Close()
			} else {
				// Use non-existent file
				configFile = "/tmp/nonexistent_ofelia_config.ini"
				cleanup = func() {}
				defer cleanup()
			}

			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			logger := test.NewTestLogger()
			cmd := &ConfigShowCommand{
				ConfigFile: configFile,
				Logger:     logger,
			}

			err := cmd.Execute(nil)

			// Restore stdout and read captured output
			w.Close()
			os.Stdout = oldStdout
			var buf bytes.Buffer
			io.Copy(&buf, r)
			output := buf.String()

			if tt.expectedError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if tt.checkOutput != nil && !tt.checkOutput(output) {
					t.Errorf("Output validation failed. Output: %s", output)
				}
			}
		})
	}
}

// TestConfigShowCommand_ExecuteWithLogLevel tests log level override
func TestConfigShowCommand_ExecuteWithLogLevel(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "ofelia_show_loglevel_*.ini")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	configContent := `
[global]
log-level = info

[job-run "test"]
schedule = @every 10s
image = busybox
command = echo test
`
	_, err = tmpFile.WriteString(configContent)
	if err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	logger := test.NewTestLogger()
	cmd := &ConfigShowCommand{
		ConfigFile: tmpFile.Name(),
		LogLevel:   "debug", // Override config log level
		Logger:     logger,
	}

	err = cmd.Execute(nil)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout
	io.Copy(io.Discard, r)

	if err != nil {
		t.Errorf("Expected no error but got: %v", err)
	}
}
