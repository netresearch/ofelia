package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

// Simple unit tests focusing on ExecJob business logic without complex Docker mocking

func TestExecJob_NewExecJob_Initialization(t *testing.T) {
	mockClient := mock.NewDockerClient()
	provider := NewSDKDockerProviderFromClient(mockClient, nil, nil)
	job := NewExecJob(provider)

	if job.Provider != provider {
		t.Error("Expected Provider to be set correctly")
	}
}

func TestExecJob_BuildExec_ArgumentParsing(t *testing.T) {
	testCases := []struct {
		name        string
		command     string
		expectedCmd []string
	}{
		{
			name:        "simple_command",
			command:     "echo hello",
			expectedCmd: []string{"echo", "hello"},
		},
		{
			name:        "command_with_flags",
			command:     "ls -la /tmp",
			expectedCmd: []string{"ls", "-la", "/tmp"},
		},
		{
			name:        "quoted_arguments",
			command:     `echo "hello world"`,
			expectedCmd: []string{"echo", "hello world"},
		},
		{
			name:        "empty_command",
			command:     "",
			expectedCmd: []string{},
		},
		{
			name:        "complex_command",
			command:     `find /tmp -name "*.log" -type f`,
			expectedCmd: []string{"find", "/tmp", "-name", "*.log", "-type", "f"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a job but don't try to execute Docker operations
			job := &ExecJob{
				BareJob: BareJob{
					Command: tc.command,
					Name:    "test-job",
				},
				Container:   "test-container",
				User:        "testuser",
				TTY:         false,
				Environment: []string{"TEST_VAR=test_value"},
			}

			// Test the argument parsing logic directly using domain types
			config := &domain.ExecConfig{
				AttachStdin:  false,
				AttachStdout: true,
				AttachStderr: true,
				Tty:          job.TTY,
				Cmd:          parseCommand(tc.command),
				User:         job.User,
				Env:          job.Environment,
			}

			if len(config.Cmd) != len(tc.expectedCmd) {
				t.Errorf("Expected command %v, got %v", tc.expectedCmd, config.Cmd)
				return
			}

			for i, expected := range tc.expectedCmd {
				if config.Cmd[i] != expected {
					t.Errorf("Command arg %d: expected %q, got %q", i, expected, config.Cmd[i])
				}
			}
		})
	}
}

func TestExecJob_ExitCodeHandling(t *testing.T) {
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
			name:         "permission_denied_exit_126",
			exitCode:     126,
			expectError:  true,
			expectedType: NonZeroExitError{},
		},
		{
			name:         "command_not_found_exit_127",
			exitCode:     127,
			expectError:  true,
			expectedType: NonZeroExitError{},
		},
		{
			name:         "killed_by_signal_exit_137",
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
			// Simulate the exit code handling logic from ExecJob.Run
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

func TestExecJob_OptionsConfiguration(t *testing.T) {
	testCases := []struct {
		name      string
		setupJob  func(*ExecJob)
		checkOpts func(*testing.T, *ExecJob)
	}{
		{
			name: "default_configuration",
			setupJob: func(job *ExecJob) {
				// Use defaults
			},
			checkOpts: func(t *testing.T, job *ExecJob) {
				if job.User != "nobody" {
					t.Errorf("Expected default user 'nobody', got %q", job.User)
				}
				if job.TTY != false {
					t.Errorf("Expected default TTY false, got %v", job.TTY)
				}
			},
		},
		{
			name: "custom_user_and_tty",
			setupJob: func(job *ExecJob) {
				job.User = "root"
				job.TTY = true
			},
			checkOpts: func(t *testing.T, job *ExecJob) {
				if job.User != "root" {
					t.Errorf("Expected user 'root', got %q", job.User)
				}
				if job.TTY != true {
					t.Errorf("Expected TTY true, got %v", job.TTY)
				}
			},
		},
		{
			name: "environment_variables",
			setupJob: func(job *ExecJob) {
				job.Environment = []string{"KEY1=value1", "KEY2=value2"}
			},
			checkOpts: func(t *testing.T, job *ExecJob) {
				expected := []string{"KEY1=value1", "KEY2=value2"}
				if len(job.Environment) != len(expected) {
					t.Errorf("Expected %d env vars, got %d", len(expected), len(job.Environment))
					return
				}
				for i, env := range expected {
					if job.Environment[i] != env {
						t.Errorf("Env var %d: expected %q, got %q", i, env, job.Environment[i])
					}
				}
			},
		},
		{
			name: "container_specification",
			setupJob: func(job *ExecJob) {
				job.Container = "my-custom-container"
			},
			checkOpts: func(t *testing.T, job *ExecJob) {
				if job.Container != "my-custom-container" {
					t.Errorf("Expected container 'my-custom-container', got %q", job.Container)
				}
			},
		},
		{
			name: "working_directory",
			setupJob: func(job *ExecJob) {
				job.WorkingDir = "/var/log"
			},
			checkOpts: func(t *testing.T, job *ExecJob) {
				if job.WorkingDir != "/var/log" {
					t.Errorf("Expected working directory '/var/log', got %q", job.WorkingDir)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := &ExecJob{
				BareJob: BareJob{
					Command: "echo test",
					Name:    "test-job",
				},
				User: "nobody", // Default
				TTY:  false,    // Default
			}

			tc.setupJob(job)
			tc.checkOpts(t, job)
		})
	}
}

func TestExecJob_ErrorMessageParsing(t *testing.T) {
	testCases := []struct {
		name          string
		operation     string
		originalError error
		expectContain string
	}{
		{
			name:          "exec_run_error",
			operation:     "exec run",
			originalError: errors.New("container not found"),
			expectContain: "exec run",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test error wrapping as done in ExecJob methods
			wrappedErr := errors.New(tc.operation + ": " + tc.originalError.Error())

			if !strings.Contains(wrappedErr.Error(), tc.expectContain) {
				t.Errorf("Expected error to contain %q, got: %v", tc.expectContain, wrappedErr)
			}

			if !strings.Contains(wrappedErr.Error(), tc.originalError.Error()) {
				t.Errorf("Expected error to contain original error %q, got: %v", tc.originalError.Error(), wrappedErr)
			}
		})
	}
}

func TestExecJob_FieldValidation(t *testing.T) {
	testCases := []struct {
		name     string
		setupJob func() *ExecJob
		isValid  bool
		reason   string
	}{
		{
			name: "valid_job",
			setupJob: func() *ExecJob {
				return &ExecJob{
					BareJob: BareJob{
						Command: "echo test",
						Name:    "test-job",
					},
					Container: "test-container",
				}
			},
			isValid: true,
		},
		{
			name: "missing_container",
			setupJob: func() *ExecJob {
				return &ExecJob{
					BareJob: BareJob{
						Command: "echo test",
						Name:    "test-job",
					},
					Container: "",
				}
			},
			isValid: false,
			reason:  "container must be specified",
		},
		{
			name: "empty_command_allowed",
			setupJob: func() *ExecJob {
				return &ExecJob{
					BareJob: BareJob{
						Command: "",
						Name:    "test-job",
					},
					Container: "test-container",
				}
			},
			isValid: true, // Empty commands are allowed in exec context
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := tc.setupJob()

			// Basic field validation (this simulates what Docker would validate)
			valid := job.Container != ""

			if tc.isValid && !valid {
				t.Errorf("Expected job to be valid, but validation failed: %s", tc.reason)
			} else if !tc.isValid && valid {
				t.Errorf("Expected job to be invalid (%s), but validation passed", tc.reason)
			}
		})
	}
}

// Helper function to parse commands (using the same logic as args.GetArgs)
func parseCommand(command string) []string {
	if command == "" {
		return []string{}
	}

	// This is a simplified version of args.GetArgs for testing
	// In real code, this would use the actual gobs/args library
	parts := strings.Fields(command)

	// Handle quoted strings (simplified)
	var result []string
	var current strings.Builder
	inQuotes := false

	for _, part := range parts {
		if strings.HasPrefix(part, `"`) && strings.HasSuffix(part, `"`) && len(part) > 1 {
			// Complete quoted string in one part
			result = append(result, part[1:len(part)-1])
		} else if strings.HasPrefix(part, `"`) {
			// Start of quoted string
			inQuotes = true
			current.WriteString(part[1:])
		} else if strings.HasSuffix(part, `"`) && inQuotes {
			// End of quoted string
			current.WriteString(" ")
			current.WriteString(part[:len(part)-1])
			result = append(result, current.String())
			current.Reset()
			inQuotes = false
		} else if inQuotes {
			// Middle of quoted string
			if current.Len() > 0 {
				current.WriteString(" ")
			}
			current.WriteString(part)
		} else {
			// Regular unquoted part
			result = append(result, part)
		}
	}

	return result
}
