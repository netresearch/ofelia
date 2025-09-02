package core

import (
	"errors"
	"fmt"
)

// Common errors used across the package
var (
	// Container errors
	ErrContainerNotFound     = errors.New("container not found")
	ErrContainerStartFailed  = errors.New("failed to start container")
	ErrContainerCreateFailed = errors.New("failed to create container")
	ErrContainerStopFailed   = errors.New("failed to stop container")
	ErrContainerRemoveFailed = errors.New("failed to remove container")

	// Image errors
	ErrImageNotFound      = errors.New("image not found")
	ErrImagePullFailed    = errors.New("failed to pull image")
	ErrLocalImageNotFound = errors.New("local image not found")

	// Service errors
	ErrServiceNotFound     = errors.New("service not found")
	ErrServiceCreateFailed = errors.New("failed to create service")
	ErrServiceStartFailed  = errors.New("failed to start service")
	ErrServiceRemoveFailed = errors.New("failed to remove service")

	// Job errors
	ErrJobNotFound      = errors.New("job not found")
	ErrJobAlreadyExists = errors.New("job already exists")
	ErrJobExecution     = errors.New("job execution failed")
	ErrMaxTimeRunning   = errors.New("max runtime exceeded")
	ErrUnexpected       = errors.New("unexpected error")

	// Workflow errors
	ErrCircularDependency = errors.New("circular dependency detected")
	ErrDependencyNotMet   = errors.New("job dependencies not met")
	ErrWorkflowInvalid    = errors.New("invalid workflow configuration")
)

// WrapContainerError wraps a container-related error with context
func WrapContainerError(op string, containerID string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s container %q: %w", op, containerID, err)
}

// WrapImageError wraps an image-related error with context
func WrapImageError(op string, image string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s image %q: %w", op, image, err)
}

// WrapServiceError wraps a service-related error with context
func WrapServiceError(op string, serviceID string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s service %q: %w", op, serviceID, err)
}

// WrapJobError wraps a job-related error with context
func WrapJobError(op string, jobName string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s job %q: %w", op, jobName, err)
}

// IsRetryableError checks if an error should trigger a retry
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific retryable errors
	return errors.Is(err, ErrContainerStartFailed) ||
		errors.Is(err, ErrImagePullFailed) ||
		errors.Is(err, ErrServiceStartFailed) ||
		// Network errors are usually retryable
		containsNetworkError(err)
}

// containsNetworkError checks if the error is network-related
func containsNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Common network error patterns
	networkErrors := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"temporary failure",
		"no such host",
		"network unreachable",
	}

	for _, pattern := range networkErrors {
		if stringContains(errStr, pattern) {
			return true
		}
	}
	return false
}

// stringContains checks if a string contains a substring (case-insensitive)
func stringContains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			containsIgnoreCase(s, substr))
}

// containsIgnoreCase performs case-insensitive substring search
func containsIgnoreCase(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}

	// Simple case-insensitive contains
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// toLower converts a byte to lowercase
func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// NonZeroExitError represents a container exit with non-zero code
type NonZeroExitError struct {
	ExitCode int
}

func (e NonZeroExitError) Error() string {
	return fmt.Sprintf("non-zero exit code: %d", e.ExitCode)
}

// IsNonZeroExitError checks if the error is a non-zero exit code error
func IsNonZeroExitError(err error) bool {
	var exitErr NonZeroExitError
	return errors.As(err, &exitErr)
}
