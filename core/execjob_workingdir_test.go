package core

import (
	"strings"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/sirupsen/logrus"
)

// Integration test - requires Docker to be running
// Tests that WorkingDir is actually passed to Docker and the exec runs in the correct directory
func TestExecJob_WorkingDir_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Connect to real Docker daemon
	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	// Verify Docker is actually reachable
	if _, err := client.Info(); err != nil {
		t.Skipf("Docker daemon not reachable: %v", err)
	}

	// Create a test container that stays running
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "30"},
		},
	})
	if err != nil {
		t.Skipf("Failed to create test container: %v (Docker may need to pull alpine:latest)", err)
	}
	defer func() {
		client.RemoveContainer(docker.RemoveContainerOptions{
			ID:    container.ID,
			Force: true,
		})
	}()

	// Start the container
	err = client.StartContainer(container.ID, nil)
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}

	// Give container a moment to be fully ready
	time.Sleep(100 * time.Millisecond)

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
			expectedOutput: "/", // Alpine container default is /
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create ExecJob with WorkingDir
			job := NewExecJob(client)
			job.Container = container.ID
			job.Command = "pwd"
			job.WorkingDir = tc.workingDir

			// Create execution context
			execution, err := NewExecution()
			if err != nil {
				t.Fatalf("Failed to create execution: %v", err)
			}

			logger := logrus.New()
			logger.SetLevel(logrus.WarnLevel) // Reduce noise in test output

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
		})
	}
}

// Integration test to verify WorkingDir works with actual commands
func TestExecJob_WorkingDir_WithCommands_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	if _, err := client.Info(); err != nil {
		t.Skipf("Docker daemon not reachable: %v", err)
	}

	// Create a test container
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "30"},
		},
	})
	if err != nil {
		t.Skipf("Failed to create test container: %v", err)
	}
	defer client.RemoveContainer(docker.RemoveContainerOptions{
		ID:    container.ID,
		Force: true,
	})

	err = client.StartContainer(container.ID, nil)
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Test: Create a file in /tmp, verify it exists
	t.Run("create_file_in_working_dir", func(t *testing.T) {
		// Create a file
		job1 := NewExecJob(client)
		job1.Container = container.ID
		job1.Command = "touch test-workdir.txt"
		job1.WorkingDir = "/tmp"

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
		job2 := NewExecJob(client)
		job2.Container = container.ID
		job2.Command = "ls test-workdir.txt"
		job2.WorkingDir = "/tmp"

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
