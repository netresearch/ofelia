package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestWrapContainerError(t *testing.T) {
	baseErr := errors.New("base error")
	wrapped := WrapContainerError("start", "test-container", baseErr)

	expectedMsg := `start container "test-container": base error`
	if wrapped.Error() != expectedMsg {
		t.Errorf("expected %q, got %q", expectedMsg, wrapped.Error())
	}

	// Test nil error
	if WrapContainerError("start", "test", nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestWrapImageError(t *testing.T) {
	baseErr := errors.New("pull failed")
	wrapped := WrapImageError("pull", "nginx:latest", baseErr)

	expectedMsg := `pull image "nginx:latest": pull failed`
	if wrapped.Error() != expectedMsg {
		t.Errorf("expected %q, got %q", expectedMsg, wrapped.Error())
	}
}

func TestWrapServiceError(t *testing.T) {
	baseErr := errors.New("service error")
	wrapped := WrapServiceError("create", "web-service", baseErr)

	expectedMsg := `create service "web-service": service error`
	if wrapped.Error() != expectedMsg {
		t.Errorf("expected %q, got %q", expectedMsg, wrapped.Error())
	}
}

func TestWrapJobError(t *testing.T) {
	baseErr := errors.New("execution failed")
	wrapped := WrapJobError("execute", "backup-job", baseErr)

	expectedMsg := `execute job "backup-job": execution failed`
	if wrapped.Error() != expectedMsg {
		t.Errorf("expected %q, got %q", expectedMsg, wrapped.Error())
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"container start failed", ErrContainerStartFailed, true},
		{"image pull failed", ErrImagePullFailed, true},
		{"service start failed", ErrServiceStartFailed, true},
		{"connection refused", errors.New("connection refused"), true},
		{"timeout error", errors.New("operation timeout"), true},
		{"network unreachable", errors.New("network unreachable"), true},
		{"non-retryable error", errors.New("invalid configuration"), false},
		{"wrapped retryable", fmt.Errorf("failed: %w", ErrImagePullFailed), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryableError(tt.err)
			if result != tt.expected {
				t.Errorf("IsRetryableError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestContainsNetworkError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"connection reset", errors.New("read: connection reset by peer"), true},
		{"timeout", errors.New("i/o timeout"), true},
		{"temporary failure", errors.New("temporary failure in name resolution"), true},
		{"no such host", errors.New("no such host"), true},
		{"network unreachable", errors.New("network unreachable"), true},
		{"non-network error", errors.New("file not found"), false},
		{"mixed case", errors.New("Connection Refused"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsNetworkError(tt.err)
			if result != tt.expected {
				t.Errorf("containsNetworkError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestNonZeroExitError(t *testing.T) {
	err := NonZeroExitError{ExitCode: 127}

	expectedMsg := "non-zero exit code: 127"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %q", expectedMsg, err.Error())
	}

	// Test IsNonZeroExitError
	if !IsNonZeroExitError(err) {
		t.Error("expected IsNonZeroExitError to return true")
	}

	if IsNonZeroExitError(errors.New("other error")) {
		t.Error("expected IsNonZeroExitError to return false for other errors")
	}
}

func TestStringContains(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"hello world", "world", true},
		{"hello world", "World", true}, // case insensitive
		{"hello", "hello world", false},
		{"", "test", false},
		{"test", "", true},
		{"exact", "exact", true},
		{"Connection Refused", "connection refused", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s contains %s", tt.s, tt.substr), func(t *testing.T) {
			result := stringContains(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("stringContains(%q, %q) = %v, expected %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}

func TestToLower(t *testing.T) {
	tests := []struct {
		input    byte
		expected byte
	}{
		{'A', 'a'},
		{'Z', 'z'},
		{'a', 'a'},
		{'z', 'z'},
		{'0', '0'},
		{'!', '!'},
	}

	for _, tt := range tests {
		result := toLower(tt.input)
		if result != tt.expected {
			t.Errorf("toLower(%c) = %c, expected %c", tt.input, result, tt.expected)
		}
	}
}
