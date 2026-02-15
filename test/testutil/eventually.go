// Package testutil provides common test utilities for the Ofelia project.
package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// DefaultTimeout is the default timeout for Eventually.
const DefaultTimeout = 5 * time.Second

// DefaultInterval is the default polling interval for Eventually.
const DefaultInterval = 50 * time.Millisecond

// Eventually polls a condition function until it returns true or the timeout expires.
// This replaces time.Sleep-based synchronization with event-driven waiting.
//
// Example:
//
//	testutil.Eventually(t, func() bool {
//	    return server.IsReady()
//	}, testutil.WithTimeout(2*time.Second))
func Eventually(t testing.TB, condition func() bool, opts ...Option) bool {
	t.Helper()

	cfg := &config{
		timeout:  DefaultTimeout,
		interval: DefaultInterval,
		message:  "condition was not satisfied",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	// Check immediately first
	if condition() {
		return true
	}

	for {
		select {
		case <-ctx.Done():
			t.Errorf("Eventually timed out after %v: %s", cfg.timeout, cfg.message)
			return false
		case <-ticker.C:
			if condition() {
				return true
			}
		}
	}
}

// EventuallyWithT is like Eventually but passes a *testing.T to the condition.
// This allows the condition to make assertions that won't fail the test until
// the timeout is reached.
func EventuallyWithT(t testing.TB, condition func(collect *T) bool, opts ...Option) bool {
	t.Helper()

	cfg := &config{
		timeout:  DefaultTimeout,
		interval: DefaultInterval,
		message:  "condition was not satisfied",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	var lastCollect *T

	// Check immediately first
	collect := &T{}
	if condition(collect) {
		return true
	}
	lastCollect = collect

	for {
		select {
		case <-ctx.Done():
			t.Errorf("EventuallyWithT timed out after %v: %s", cfg.timeout, cfg.message)
			if lastCollect != nil && len(lastCollect.errors) > 0 {
				for _, err := range lastCollect.errors {
					t.Errorf("  - %s", err)
				}
			}
			return false
		case <-ticker.C:
			collect := &T{}
			if condition(collect) {
				return true
			}
			lastCollect = collect
		}
	}
}

// T is a collector for errors during EventuallyWithT.
type T struct {
	errors []string
}

// Errorf records an error (does not fail immediately).
func (t *T) Errorf(format string, args ...any) {
	t.errors = append(t.errors, fmt.Sprintf(format, args...))
}

// Failed returns true if any errors were recorded.
func (t *T) Failed() bool {
	return len(t.errors) > 0
}

// config holds the configuration for Eventually.
type config struct {
	timeout  time.Duration
	interval time.Duration
	message  string
}

// Option configures Eventually behavior.
type Option func(*config)

// WithTimeout sets the maximum time to wait for the condition.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		c.timeout = d
	}
}

// WithInterval sets the polling interval.
func WithInterval(d time.Duration) Option {
	return func(c *config) {
		c.interval = d
	}
}

// WithMessage sets the error message shown on timeout.
func WithMessage(msg string) Option {
	return func(c *config) {
		c.message = msg
	}
}

// Never is the inverse of Eventually - it asserts that a condition never
// becomes true within the timeout period.
func Never(t testing.TB, condition func() bool, opts ...Option) bool {
	t.Helper()

	cfg := &config{
		timeout:  DefaultTimeout,
		interval: DefaultInterval,
		message:  "condition became true unexpectedly",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	// Check immediately first
	if condition() {
		t.Errorf("Never failed immediately: %s", cfg.message)
		return false
	}

	for {
		select {
		case <-ctx.Done():
			return true // Timeout reached, condition never became true
		case <-ticker.C:
			if condition() {
				t.Errorf("Never failed after some time: %s", cfg.message)
				return false
			}
		}
	}
}

// WaitForChan waits for a channel to receive a value or closes within the timeout.
// Returns true if the channel received/closed, false on timeout.
func WaitForChan[T any](t testing.TB, ch <-chan T, timeout time.Duration) (T, bool) {
	t.Helper()

	select {
	case v, ok := <-ch:
		if !ok {
			var zero T
			return zero, true // Channel closed
		}
		return v, true
	case <-time.After(timeout):
		var zero T
		t.Errorf("WaitForChan timed out after %v", timeout)
		return zero, false
	}
}

// WaitForClose waits for a channel to close within the timeout.
func WaitForClose[T any](t testing.TB, ch <-chan T, timeout time.Duration) bool {
	t.Helper()

	select {
	case _, ok := <-ch:
		return !ok // True if channel closed
	case <-time.After(timeout):
		t.Errorf("WaitForClose timed out after %v", timeout)
		return false
	}
}
