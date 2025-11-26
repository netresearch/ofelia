package domain

import (
	"errors"
	"fmt"
)

// Common domain errors.
var (
	// ErrNotFound indicates a resource was not found.
	ErrNotFound = errors.New("resource not found")

	// ErrConflict indicates a resource conflict (e.g., name already exists).
	ErrConflict = errors.New("resource conflict")

	// ErrUnauthorized indicates authentication failure.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden indicates permission denied.
	ErrForbidden = errors.New("forbidden")

	// ErrTimeout indicates an operation timed out.
	ErrTimeout = errors.New("operation timed out")

	// ErrCancelled indicates an operation was cancelled.
	ErrCancelled = errors.New("operation cancelled")

	// ErrConnectionFailed indicates a connection failure.
	ErrConnectionFailed = errors.New("connection failed")

	// ErrMaxTimeRunning indicates a job exceeded its maximum runtime.
	ErrMaxTimeRunning = errors.New("maximum time running exceeded")
)

// ContainerNotFoundError indicates a container was not found.
type ContainerNotFoundError struct {
	ID string
}

func (e *ContainerNotFoundError) Error() string {
	return fmt.Sprintf("container not found: %s", e.ID)
}

// Is implements error matching.
func (e *ContainerNotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

// ImageNotFoundError indicates an image was not found.
type ImageNotFoundError struct {
	Image string
}

func (e *ImageNotFoundError) Error() string {
	return fmt.Sprintf("image not found: %s", e.Image)
}

// Is implements error matching.
func (e *ImageNotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

// NetworkNotFoundError indicates a network was not found.
type NetworkNotFoundError struct {
	Network string
}

func (e *NetworkNotFoundError) Error() string {
	return fmt.Sprintf("network not found: %s", e.Network)
}

// Is implements error matching.
func (e *NetworkNotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

// ServiceNotFoundError indicates a service was not found.
type ServiceNotFoundError struct {
	ID string
}

func (e *ServiceNotFoundError) Error() string {
	return fmt.Sprintf("service not found: %s", e.ID)
}

// Is implements error matching.
func (e *ServiceNotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

// ExecNotFoundError indicates an exec instance was not found.
type ExecNotFoundError struct {
	ID string
}

func (e *ExecNotFoundError) Error() string {
	return fmt.Sprintf("exec not found: %s", e.ID)
}

// Is implements error matching.
func (e *ExecNotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

// IsNotFound returns true if the error indicates a resource was not found.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsConflict returns true if the error indicates a resource conflict.
func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}

// IsTimeout returns true if the error indicates a timeout.
func IsTimeout(err error) bool {
	return errors.Is(err, ErrTimeout)
}

// IsCancelled returns true if the error indicates cancellation.
func IsCancelled(err error) bool {
	return errors.Is(err, ErrCancelled)
}
