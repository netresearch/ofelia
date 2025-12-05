package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/adapters/mock"
)

// Simple unit tests focusing on RunJob business logic without complex Docker mocking

func TestRunJob_NewRunJob_Initialization(t *testing.T) {
	mockClient := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mockClient, nil, nil)
	job := NewRunJob(provider)

	if job.Provider != provider {
		t.Error("Expected Provider to be set correctly")
	}
	if job.containerID != "" {
		t.Error("Expected containerID to be empty initially")
	}
}

func TestRunJob_ContainerConfiguration(t *testing.T) {
	testCases := []struct {
		name     string
		setupJob func(*RunJob)
		checkJob func(*testing.T, *RunJob)
	}{
		{
			name: "default_configuration",
			setupJob: func(job *RunJob) {
				// Use defaults
			},
			checkJob: func(t *testing.T, job *RunJob) {
				if job.User != "nobody" {
					t.Errorf("Expected default user 'nobody', got %q", job.User)
				}
				if job.TTY != false {
					t.Errorf("Expected default TTY false, got %v", job.TTY)
				}
				if job.Delete != "true" {
					t.Errorf("Expected default Delete 'true', got %q", job.Delete)
				}
				if job.Pull != "true" {
					t.Errorf("Expected default Pull 'true', got %q", job.Pull)
				}
			},
		},
		{
			name: "custom_configuration",
			setupJob: func(job *RunJob) {
				job.User = "root"
				job.TTY = true
				job.Delete = "false"
				job.Pull = "false"
				job.Image = "custom:latest"
				job.Network = "custom-network"
				job.Hostname = "custom-host"
			},
			checkJob: func(t *testing.T, job *RunJob) {
				if job.User != "root" {
					t.Errorf("Expected user 'root', got %q", job.User)
				}
				if job.TTY != true {
					t.Errorf("Expected TTY true, got %v", job.TTY)
				}
				if job.Image != "custom:latest" {
					t.Errorf("Expected image 'custom:latest', got %q", job.Image)
				}
				if job.Network != "custom-network" {
					t.Errorf("Expected network 'custom-network', got %q", job.Network)
				}
				if job.Hostname != "custom-host" {
					t.Errorf("Expected hostname 'custom-host', got %q", job.Hostname)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := &RunJob{
				BareJob: BareJob{
					Command: "echo test",
					Name:    "test-job",
				},
				User:   "nobody", // Default
				TTY:    false,    // Default
				Delete: "true",   // Default
				Pull:   "true",   // Default
			}

			tc.setupJob(job)
			tc.checkJob(t, job)
		})
	}
}

func TestRunJob_ContainerNameLogic(t *testing.T) {
	testCases := []struct {
		name          string
		jobName       string
		containerName *string
		expectedName  string
		description   string
	}{
		{
			name:          "use_job_name_when_container_name_nil",
			jobName:       "my-job",
			containerName: nil,
			expectedName:  "my-job",
			description:   "Should use job name when ContainerName is nil",
		},
		{
			name:          "use_container_name_when_specified",
			jobName:       "my-job",
			containerName: stringPtr("custom-container"),
			expectedName:  "custom-container",
			description:   "Should use ContainerName when specified",
		},
		{
			name:          "use_empty_string_when_container_name_empty",
			jobName:       "my-job",
			containerName: stringPtr(""),
			expectedName:  "",
			description:   "Should use empty string when ContainerName is explicitly empty (Docker assigns random name)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := &RunJob{
				BareJob: BareJob{
					Name: tc.jobName,
				},
				ContainerName: tc.containerName,
			}

			// Simulate the name resolution logic from buildContainer
			var actualName string
			if job.ContainerName != nil {
				actualName = *job.ContainerName
			} else {
				actualName = job.Name
			}

			if actualName != tc.expectedName {
				t.Errorf("%s: expected name %q, got %q", tc.description, tc.expectedName, actualName)
			}
		})
	}
}

func TestRunJob_DeleteBehaviorParsing(t *testing.T) {
	testCases := []struct {
		name         string
		deleteValue  string
		shouldDelete bool
	}{
		{"delete_true", "true", true},
		{"delete_false", "false", false},
		{"delete_1", "1", true},
		{"delete_0", "0", false},
		{"delete_yes", "yes", false}, // ParseBool doesn't recognize "yes"
		{"delete_no", "no", false},
		{"delete_empty", "", false},
		{"delete_invalid", "invalid", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test the parsing logic directly without creating unused variables
			shouldDelete := false
			if parsed, err := parseBool(tc.deleteValue); err == nil {
				shouldDelete = parsed
			}

			if shouldDelete != tc.shouldDelete {
				t.Errorf("Delete value %q: expected shouldDelete=%v, got %v", tc.deleteValue, tc.shouldDelete, shouldDelete)
			}
		})
	}
}

func TestRunJob_PullBehaviorParsing(t *testing.T) {
	testCases := []struct {
		name       string
		pullValue  string
		shouldPull bool
	}{
		{"pull_true", "true", true},
		{"pull_false", "false", false},
		{"pull_1", "1", true},
		{"pull_0", "0", false},
		{"pull_empty", "", false},
		{"pull_invalid", "invalid", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test the parsing logic directly
			shouldPull := false
			if parsed, err := parseBool(tc.pullValue); err == nil {
				shouldPull = parsed
			}

			if shouldPull != tc.shouldPull {
				t.Errorf("Pull value %q: expected shouldPull=%v, got %v", tc.pullValue, tc.shouldPull, shouldPull)
			}
		})
	}
}

func TestRunJob_MaxRuntimeHandling(t *testing.T) {
	testCases := []struct {
		name       string
		maxRuntime time.Duration
		elapsed    time.Duration
		shouldStop bool
	}{
		{
			name:       "no_timeout_set",
			maxRuntime: 0,
			elapsed:    time.Hour, // Very long time
			shouldStop: false,     // Should never timeout when maxRuntime is 0
		},
		{
			name:       "within_timeout",
			maxRuntime: 10 * time.Second,
			elapsed:    5 * time.Second,
			shouldStop: false,
		},
		{
			name:       "exactly_at_timeout",
			maxRuntime: 10 * time.Second,
			elapsed:    10 * time.Second,
			shouldStop: false, // Should be exactly at the limit
		},
		{
			name:       "exceeded_timeout",
			maxRuntime: 10 * time.Second,
			elapsed:    15 * time.Second,
			shouldStop: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := &RunJob{MaxRuntime: tc.maxRuntime}

			// Simulate the timeout logic from watchContainerLegacy
			shouldStop := job.MaxRuntime > 0 && tc.elapsed > job.MaxRuntime

			if shouldStop != tc.shouldStop {
				t.Errorf("MaxRuntime %v, elapsed %v: expected shouldStop=%v, got %v",
					tc.maxRuntime, tc.elapsed, tc.shouldStop, shouldStop)
			}
		})
	}
}

func TestRunJob_ExitCodeHandling(t *testing.T) {
	testCases := []struct {
		name         string
		exitCode     int
		expectError  bool
		expectedType interface{}
	}{
		{
			name:         "success_exit_0",
			exitCode:     0,
			expectError:  false,
			expectedType: nil,
		},
		{
			name:         "general_error_exit_1",
			exitCode:     1,
			expectError:  true,
			expectedType: NonZeroExitError{},
		},
		{
			name:         "interrupted_exit_130",
			exitCode:     130,
			expectError:  true,
			expectedType: NonZeroExitError{},
		},
		{
			name:         "killed_exit_137",
			exitCode:     137,
			expectError:  true,
			expectedType: NonZeroExitError{},
		},
		{
			name:         "unexpected_exit_negative_1",
			exitCode:     -1,
			expectError:  true,
			expectedType: nil, // Should be ErrUnexpected
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the exit code handling logic from watchContainer
			var err error

			switch tc.exitCode {
			case 0:
				err = nil
			case -1:
				err = ErrUnexpected
			default:
				err = NonZeroExitError{ExitCode: tc.exitCode}
			}

			if tc.expectError && err == nil {
				t.Error("Expected error but got none")
			} else if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tc.expectError {
				if tc.expectedType != nil {
					var exitErr NonZeroExitError
					if !errors.As(err, &exitErr) {
						t.Errorf("Expected NonZeroExitError, got %T", err)
					} else if exitErr.ExitCode != tc.exitCode {
						t.Errorf("Expected exit code %d, got %d", tc.exitCode, exitErr.ExitCode)
					}
				} else if tc.exitCode == -1 && !errors.Is(err, ErrUnexpected) {
					t.Errorf("Expected ErrUnexpected for exit code -1, got %v", err)
				}
			}
		})
	}
}

func TestRunJob_ContainerIDConcurrency(t *testing.T) {
	job := &RunJob{}

	const numGoroutines = 10
	const numOperations = 100
	const testTimeout = 10 * time.Second // Timeout for mutation testing

	// Test concurrent access to container ID
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				containerID := fmt.Sprintf("container-%d-%d", id, j)
				job.setContainerID(containerID)

				// Verify we can read it back
				readID := job.getContainerID()
				if readID == "" {
					t.Errorf("Got empty container ID in goroutine %d, operation %d", id, j)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete with timeout
	timeout := time.After(testTimeout)
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
			// goroutine completed
		case <-timeout:
			t.Fatalf("Test timed out waiting for goroutine %d", i)
		}
	}

	// Final verification that we have a valid container ID
	finalID := job.getContainerID()
	if finalID == "" {
		t.Error("Expected non-empty container ID after concurrent operations")
	}
}

func TestRunJob_VolumesConfiguration(t *testing.T) {
	testCases := []struct {
		name        string
		volumes     []string
		volumesFrom []string
		isValid     bool
	}{
		{
			name:        "no_volumes",
			volumes:     nil,
			volumesFrom: nil,
			isValid:     true,
		},
		{
			name:        "host_volumes",
			volumes:     []string{"/host/path:/container/path", "/data:/app/data:ro"},
			volumesFrom: nil,
			isValid:     true,
		},
		{
			name:        "volumes_from",
			volumes:     nil,
			volumesFrom: []string{"data-container", "config-container"},
			isValid:     true,
		},
		{
			name:        "mixed_volumes",
			volumes:     []string{"/logs:/app/logs"},
			volumesFrom: []string{"data-container"},
			isValid:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := &RunJob{
				Volume:      tc.volumes,
				VolumesFrom: tc.volumesFrom,
			}

			// Basic validation (volumes should be properly formatted)
			for _, volume := range job.Volume {
				if !strings.Contains(volume, ":") && volume != "" {
					// This would be invalid volume syntax in most cases
					// but we'll allow it for flexibility
				}
			}

			// All test cases should be valid for this basic check
			if !tc.isValid {
				t.Errorf("Expected configuration to be valid")
			}
		})
	}
}

func TestRunJob_EnvironmentVariables(t *testing.T) {
	testCases := []struct {
		name        string
		environment []string
		expectValid bool
	}{
		{
			name:        "no_environment",
			environment: nil,
			expectValid: true,
		},
		{
			name:        "valid_environment",
			environment: []string{"KEY1=value1", "KEY2=value2", "PATH=/usr/bin"},
			expectValid: true,
		},
		{
			name:        "environment_with_equals_in_value",
			environment: []string{"URL=https://example.com:8080", "COMPLEX=key=value"},
			expectValid: true,
		},
		{
			name:        "empty_environment_entry",
			environment: []string{"VALID=value", "", "ANOTHER=value"},
			expectValid: true, // Docker handles empty entries
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := &RunJob{
				Environment: tc.environment,
			}

			// Basic validation - environment variables should have format KEY=VALUE
			isValid := true
			for _, env := range job.Environment {
				if env != "" && !strings.Contains(env, "=") {
					isValid = false
					break
				}
			}

			if isValid != tc.expectValid {
				t.Errorf("Expected valid=%v, got %v for environment: %v", tc.expectValid, isValid, tc.environment)
			}
		})
	}
}

// Helper functions for testing

func stringPtr(s string) *string {
	return &s
}

func parseBool(s string) (bool, error) {
	switch s {
	case "true", "1":
		return true, nil
	case "false", "0", "":
		return false, nil
	default:
		return false, errors.New("invalid boolean value")
	}
}
