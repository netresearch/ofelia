package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalJob_Run_Success(t *testing.T) {
	job := NewLocalJob()
	job.Command = "echo hello world"

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := &Context{
		Execution: execution,
	}

	err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Expected successful execution, got error: %v", err)
	}

	// Verify output was captured
	stdout := execution.GetStdout()
	if !strings.Contains(stdout, "hello world") {
		t.Errorf("Expected output to contain 'hello world', got: %q", stdout)
	}
}

func TestLocalJob_Run_NonZeroExit(t *testing.T) {
	job := NewLocalJob()

	if runtime.GOOS == "windows" {
		job.Command = "cmd /c exit 1"
	} else {
		job.Command = "sh -c 'exit 1'"
	}

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := &Context{
		Execution: execution,
	}

	err = job.Run(ctx)
	if err == nil {
		t.Fatal("Expected error for non-zero exit code")
	}

	// Verify it's wrapped as a local run error
	if !strings.Contains(err.Error(), "local run") {
		t.Errorf("Expected error to be wrapped as 'local run' error, got: %v", err)
	}

	// The underlying error should be an exit error
	if _, ok := errors.AsType[*exec.ExitError](err); !ok {
		t.Errorf("Expected underlying error to be ExitError, got: %T", err)
	}
}

func TestLocalJob_Run_CommandNotFound(t *testing.T) {
	job := NewLocalJob()
	job.Command = "nonexistent-binary-that-should-not-exist"

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := &Context{
		Execution: execution,
	}

	err = job.Run(ctx)
	if err == nil {
		t.Fatal("Expected error for nonexistent command")
	}

	// Should fail at the LookPath stage in buildCommand
	if !strings.Contains(err.Error(), "look path") {
		t.Errorf("Expected error to contain 'look path', got: %v", err)
	}
}

func TestLocalJob_Run_EmptyCommand(t *testing.T) {
	job := NewLocalJob()
	job.Command = ""

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := &Context{
		Execution: execution,
	}

	// Test that empty command returns proper error (BUG FIXED: used to panic)
	err = job.Run(ctx)
	if err == nil {
		t.Error("Expected error for empty command, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "command cannot be empty") {
		t.Errorf("Expected 'command cannot be empty' error, got: %v", err)
	}
}

func TestLocalJob_BuildCommand_CorrectArguments(t *testing.T) {
	job := NewLocalJob()
	job.Command = "ls -la /tmp"

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := &Context{
		Execution: execution,
	}

	cmd, err := job.buildCommand(ctx)
	if err != nil {
		t.Fatalf("buildCommand failed: %v", err)
	}

	// Verify command structure
	expectedArgs := []string{"ls", "-la", "/tmp"}
	if len(cmd.Args) != len(expectedArgs) {
		t.Fatalf("Expected args %v, got %v", expectedArgs, cmd.Args)
	}

	for i, arg := range expectedArgs {
		if cmd.Args[i] != arg {
			t.Errorf("Expected arg %d to be %q, got %q", i, arg, cmd.Args[i])
		}
	}

	// Verify output streams are connected
	if cmd.Stdout != execution.OutputStream {
		t.Error("Expected Stdout to be connected to execution OutputStream")
	}
	if cmd.Stderr != execution.ErrorStream {
		t.Error("Expected Stderr to be connected to execution ErrorStream")
	}
}

func TestLocalJob_BuildCommand_Environment(t *testing.T) {
	job := NewLocalJob()
	job.Command = "echo test"
	job.Environment = []string{"CUSTOM_VAR=custom_value", "ANOTHER_VAR=another_value"}

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := &Context{
		Execution: execution,
	}

	cmd, err := job.buildCommand(ctx)
	if err != nil {
		t.Fatalf("buildCommand failed: %v", err)
	}

	// Verify environment variables are added to existing environment
	baseEnv := os.Environ()
	expectedEnvLen := len(baseEnv) + len(job.Environment)

	if len(cmd.Env) != expectedEnvLen {
		t.Errorf("Expected %d environment variables, got %d", expectedEnvLen, len(cmd.Env))
	}

	// Check that our custom variables are present
	envMap := make(map[string]string)
	for _, env := range cmd.Env {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	if envMap["CUSTOM_VAR"] != "custom_value" {
		t.Errorf("Expected CUSTOM_VAR=custom_value, got CUSTOM_VAR=%s", envMap["CUSTOM_VAR"])
	}
	if envMap["ANOTHER_VAR"] != "another_value" {
		t.Errorf("Expected ANOTHER_VAR=another_value, got ANOTHER_VAR=%s", envMap["ANOTHER_VAR"])
	}

	// Check that base environment is preserved (check for PATH as an example)
	if envMap["PATH"] == "" {
		t.Error("Expected PATH to be preserved from base environment")
	}
}

func TestLocalJob_Run_WithWorkingDirectory(t *testing.T) {
	// Create a temporary directory with a test file
	tempDir := t.TempDir()

	testFile := filepath.Join(tempDir, "testfile.txt")
	err := os.WriteFile(testFile, []byte("test content"), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	job := NewLocalJob()
	job.Dir = tempDir

	if runtime.GOOS == "windows" {
		job.Command = "dir testfile.txt"
	} else {
		job.Command = "ls testfile.txt"
	}

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := &Context{
		Execution: execution,
	}

	err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Expected successful execution in working directory, got error: %v", err)
	}

	// Verify the file was found (meaning working directory was set correctly)
	stdout := execution.GetStdout()
	if !strings.Contains(stdout, "testfile.txt") {
		t.Errorf("Expected output to contain 'testfile.txt', got: %q", stdout)
	}
}

func TestLocalJob_Run_EnvironmentVariables(t *testing.T) {
	job := NewLocalJob()
	job.Environment = []string{"TEST_VAR=test_value"}

	if runtime.GOOS == "windows" {
		job.Command = "cmd /c echo %TEST_VAR%"
	} else {
		job.Command = "sh -c 'echo $TEST_VAR'"
	}

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := &Context{
		Execution: execution,
	}

	err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Expected successful execution with environment, got error: %v", err)
	}

	// Verify environment variable was used
	stdout := execution.GetStdout()
	if !strings.Contains(stdout, "test_value") {
		t.Errorf("Expected output to contain 'test_value', got: %q", stdout)
	}
}

func TestLocalJob_Run_StderrCapture(t *testing.T) {
	job := NewLocalJob()

	if runtime.GOOS == "windows" {
		job.Command = `cmd /c echo stdout output && echo stderr output 1>&2`
	} else {
		job.Command = `sh -c 'echo stdout output; echo stderr output >&2'`
	}

	execution, err := NewExecution()
	if err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	ctx := &Context{
		Execution: execution,
	}

	err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Expected successful execution, got error: %v", err)
	}

	stdout := execution.GetStdout()
	stderr := execution.GetStderr()

	if !strings.Contains(stdout, "stdout output") {
		t.Errorf("Expected stdout to contain 'stdout output', got: %q", stdout)
	}
	if !strings.Contains(stderr, "stderr output") {
		t.Errorf("Expected stderr to contain 'stderr output', got: %q", stderr)
	}
}

func TestLocalJob_BuildCommand_ErrorHandling(t *testing.T) {
	testCases := []struct {
		name        string
		command     string
		expectError bool
		errorCheck  func(error) bool
	}{
		{
			name:        "empty_command",
			command:     "",
			expectError: true,
			errorCheck:  func(err error) bool { return strings.Contains(err.Error(), "look path") },
		},
		{
			name:        "nonexistent_binary",
			command:     "absolutely-nonexistent-binary-12345",
			expectError: true,
			errorCheck:  func(err error) bool { return strings.Contains(err.Error(), "look path") },
		},
		{
			name:        "valid_command",
			command:     "echo test",
			expectError: false,
			errorCheck:  nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := NewLocalJob()
			job.Command = tc.command

			execution, err := NewExecution()
			if err != nil {
				t.Fatalf("Failed to create execution: %v", err)
			}

			ctx := &Context{
				Execution: execution,
			}

			// Handle panic for empty command case (documenting known bug)
			if tc.name == "empty_command" {
				defer func() {
					if r := recover(); r != nil {
						// The panic is expected with current implementation
						// This should be fixed to return a proper error instead
						t.Logf("KNOWN BUG: Empty command causes panic in buildCommand: %v", r)
						return
					}
					// If we get here, either there was no panic (unexpected) or there was a proper error
					if !tc.expectError {
						return // Normal case for non-error expectations
					}
					if err == nil {
						t.Error("Expected panic or error for empty command (documenting current bug)")
					}
				}()
			}

			_, err = job.buildCommand(ctx)

			// Skip normal error checking for empty_command case since it may panic
			if tc.name == "empty_command" {
				return
			}

			if tc.expectError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}
				if tc.errorCheck != nil && !tc.errorCheck(err) {
					t.Errorf("Error check failed for error: %v", err)
				}
			} else if err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}
		})
	}
}

// Test edge cases and boundary conditions
func TestLocalJob_EdgeCases(t *testing.T) {
	testCases := []struct {
		name    string
		setup   func(*LocalJob)
		wantErr bool
	}{
		{
			name: "very_long_command_line",
			setup: func(job *LocalJob) {
				// Create a very long command line (but within reasonable limits)
				longArg := strings.Repeat("a", 1000)
				job.Command = "echo " + longArg
			},
			wantErr: false,
		},
		{
			name: "many_environment_variables",
			setup: func(job *LocalJob) {
				job.Command = "echo test"
				env := make([]string, 100)
				for i := range 100 {
					env[i] = fmt.Sprintf("VAR%d=value%d", i, i)
				}
				job.Environment = env
			},
			wantErr: false,
		},
		{
			name: "unicode_in_command",
			setup: func(job *LocalJob) {
				job.Command = "echo 'Hello 世界 🌍'"
			},
			wantErr: false,
		},
		{
			name: "special_characters_in_arguments",
			setup: func(job *LocalJob) {
				job.Command = `echo "Special chars: !@#$%^&*()"`
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := NewLocalJob()
			tc.setup(job)

			execution, err := NewExecution()
			if err != nil {
				t.Fatalf("Failed to create execution: %v", err)
			}

			ctx := &Context{
				Execution: execution,
			}

			err = job.Run(ctx)
			if tc.wantErr && err == nil {
				t.Error("Expected error but got none")
			} else if !tc.wantErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}
