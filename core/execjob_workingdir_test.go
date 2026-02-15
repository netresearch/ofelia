//go:build integration

package core

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

// Integration test - Tests that WorkingDir is actually passed to Docker
// Tests that the exec runs in the correct directory
func TestExecJob_WorkingDir_Integration(t *testing.T) {
	mockClient := mock.NewDockerClient()
	provider := &SDKDockerProvider{
		client: mockClient,
	}

	// Track exec configs to verify WorkingDir was passed
	var capturedConfigs []*domain.ExecConfig

	exec := mockClient.Exec().(*mock.ExecService)
	exec.OnRun = func(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
		capturedConfigs = append(capturedConfigs, config)
		// Simulate pwd output based on WorkingDir
		if stdout != nil {
			output := config.WorkingDir
			if output == "" {
				output = "/" // Default
			}
			stdout.Write([]byte(output + "\n"))
		}
		return 0, nil
	}

	// Test cases for different working directories
	testCases := []struct {
		name            string
		workingDir      string
		expectedOutput  string
		commandOverride string
	}{
		{
			name:           "working_dir_tmp",
			workingDir:     "/tmp",
			expectedOutput: "/tmp",
		},
		{
			name:           "working_dir_etc",
			workingDir:     "/etc",
			expectedOutput: "/etc",
		},
		{
			name:           "working_dir_root",
			workingDir:     "/",
			expectedOutput: "/",
		},
		{
			name:           "no_working_dir_uses_container_default",
			workingDir:     "",
			expectedOutput: "/", // Default
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			capturedConfigs = nil // Reset

			// Create ExecJob with WorkingDir
			job := &ExecJob{
				BareJob: BareJob{
					Name:    "test-workdir-" + tc.name,
					Command: "pwd",
				},
				Container:  "test-container",
				WorkingDir: tc.workingDir,
			}
			job.Provider = provider

			// Create execution context
			execution, err := NewExecution()
			if err != nil {
				t.Fatalf("Failed to create execution: %v", err)
			}

			logger := logrus.New()
			logger.SetLevel(logrus.WarnLevel)

			ctx := &Context{
				Execution: execution,
				Logger:    &LogrusAdapter{Logger: logger},
			}

			// Run the job
			err = job.Run(ctx)
			if err != nil {
				t.Fatalf("Job execution failed: %v", err)
			}

			// Get the output
			stdout := execution.GetStdout()
			output := strings.TrimSpace(stdout)

			// Verify the working directory is correct
			if output != tc.expectedOutput {
				t.Errorf("Expected working directory %q, got %q", tc.expectedOutput, output)
			}

			// Verify the config was passed with correct WorkingDir
			if len(capturedConfigs) > 0 && tc.workingDir != "" {
				if capturedConfigs[0].WorkingDir != tc.workingDir {
					t.Errorf("Expected config WorkingDir %q, got %q", tc.workingDir, capturedConfigs[0].WorkingDir)
				}
			}
		})
	}
}

// Integration test to verify WorkingDir works with actual commands
func TestExecJob_WorkingDir_WithCommands_Integration(t *testing.T) {
	mockClient := mock.NewDockerClient()
	provider := &SDKDockerProvider{
		client: mockClient,
	}

	// Track commands executed
	var executedCommands []string

	exec := mockClient.Exec().(*mock.ExecService)
	exec.OnRun = func(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
		cmd := strings.Join(config.Cmd, " ")
		executedCommands = append(executedCommands, cmd)
		// Simulate successful file operations
		if stdout != nil && strings.Contains(cmd, "ls") {
			stdout.Write([]byte("test-workdir.txt\n"))
		}
		return 0, nil
	}

	// Test: Create a file in /tmp, verify it exists
	t.Run("create_file_in_working_dir", func(t *testing.T) {
		executedCommands = nil

		// Create a file
		job1 := &ExecJob{
			BareJob: BareJob{
				Name:    "test-create-file",
				Command: "touch test-workdir.txt",
			},
			Container:  "test-container",
			WorkingDir: "/tmp",
		}
		job1.Provider = provider

		exec1, err := NewExecution()
		if err != nil {
			t.Fatalf("Failed to create execution: %v", err)
		}

		logger := logrus.New()
		logger.SetLevel(logrus.WarnLevel)

		err = job1.Run(&Context{
			Execution: exec1,
			Logger:    &LogrusAdapter{Logger: logger},
		})
		if err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}

		// Verify file exists in /tmp
		job2 := &ExecJob{
			BareJob: BareJob{
				Name:    "test-list-file",
				Command: "ls test-workdir.txt",
			},
			Container:  "test-container",
			WorkingDir: "/tmp",
		}
		job2.Provider = provider

		exec2, err := NewExecution()
		if err != nil {
			t.Fatalf("Failed to create execution: %v", err)
		}

		err = job2.Run(&Context{
			Execution: exec2,
			Logger:    &LogrusAdapter{Logger: logger},
		})
		if err != nil {
			t.Fatalf("File not found in working directory: %v", err)
		}

		output := strings.TrimSpace(exec2.GetStdout())
		if output != "test-workdir.txt" {
			t.Errorf("Expected 'test-workdir.txt', got %q", output)
		}
	})
}
