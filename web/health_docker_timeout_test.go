// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
)

// hangingDockerProvider implements core.DockerProvider where every method that
// accepts a context blocks until the context is canceled or the provider is
// closed. It models a Docker daemon that is reachable but wedged - every call
// would otherwise hang forever. Used to verify that callers wrap the call with
// a bounded context.
type hangingDockerProvider struct {
	closed chan struct{}
}

func newHangingDockerProvider() *hangingDockerProvider {
	return &hangingDockerProvider{closed: make(chan struct{})}
}

// blockUntilCancelled returns context.DeadlineExceeded / Canceled when ctx
// fires, or a synthetic error when the provider is closed. Either way the
// goroutine never spins; this models a wedged TCP socket that swallows the
// request.
func (h *hangingDockerProvider) blockUntilCancelled(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.closed:
		return context.Canceled
	}
}

func (h *hangingDockerProvider) Ping(ctx context.Context) error {
	return h.blockUntilCancelled(ctx)
}

func (h *hangingDockerProvider) Info(ctx context.Context) (*domain.SystemInfo, error) {
	return nil, h.blockUntilCancelled(ctx)
}

func (h *hangingDockerProvider) HasImageLocally(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (h *hangingDockerProvider) Close() error {
	select {
	case <-h.closed:
		// already closed
	default:
		close(h.closed)
	}
	return nil
}

// The remaining methods are unused by the health-check path; return zero
// values so the type satisfies core.DockerProvider.
func (h *hangingDockerProvider) CreateContainer(_ context.Context, _ *domain.ContainerConfig, _ string) (string, error) {
	return "", nil
}

func (h *hangingDockerProvider) StartContainer(_ context.Context, _ string) error { return nil }

func (h *hangingDockerProvider) StopContainer(_ context.Context, _ string, _ domain.StopOptions) error {
	return nil
}

func (h *hangingDockerProvider) RemoveContainer(_ context.Context, _ string, _ bool) error {
	return nil
}

func (h *hangingDockerProvider) InspectContainer(_ context.Context, _ string) (*domain.Container, error) {
	return nil, nil
}

func (h *hangingDockerProvider) ListContainers(_ context.Context, _ domain.ListOptions) ([]domain.Container, error) {
	return nil, nil
}

func (h *hangingDockerProvider) WaitContainer(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (h *hangingDockerProvider) GetContainerLogs(_ context.Context, _ string, _ core.ContainerLogsOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (h *hangingDockerProvider) CreateExec(_ context.Context, _ string, _ *domain.ExecConfig) (string, error) {
	return "", nil
}

func (h *hangingDockerProvider) StartExec(_ context.Context, _ string, _ domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	return nil, nil
}

func (h *hangingDockerProvider) InspectExec(_ context.Context, _ string) (*domain.ExecInspect, error) {
	return nil, nil
}

func (h *hangingDockerProvider) RunExec(_ context.Context, _ string, _ *domain.ExecConfig, _, _ io.Writer) (int, error) {
	return 0, nil
}

func (h *hangingDockerProvider) PullImage(_ context.Context, _ string) error { return nil }

func (h *hangingDockerProvider) EnsureImage(_ context.Context, _ string, _ bool) error { return nil }

func (h *hangingDockerProvider) ConnectNetwork(_ context.Context, _, _ string) error { return nil }

func (h *hangingDockerProvider) FindNetworkByName(_ context.Context, _ string) ([]domain.Network, error) {
	return nil, nil
}

func (h *hangingDockerProvider) SubscribeEvents(_ context.Context, _ domain.EventFilter) (<-chan domain.Event, <-chan error) {
	return nil, nil
}

func (h *hangingDockerProvider) CreateService(_ context.Context, _ domain.ServiceSpec, _ domain.ServiceCreateOptions) (string, error) {
	return "", nil
}

func (h *hangingDockerProvider) InspectService(_ context.Context, _ string) (*domain.Service, error) {
	return nil, nil
}

func (h *hangingDockerProvider) ListTasks(_ context.Context, _ domain.TaskListOptions) ([]domain.Task, error) {
	return nil, nil
}

func (h *hangingDockerProvider) RemoveService(_ context.Context, _ string) error { return nil }

func (h *hangingDockerProvider) WaitForServiceTasks(_ context.Context, _ string, _ time.Duration) ([]domain.Task, error) {
	return nil, nil
}

// Compile-time assertion that hangingDockerProvider satisfies the interface.
var _ core.DockerProvider = (*hangingDockerProvider)(nil)

// TestCheckDocker_BoundedTimeout asserts checkDocker returns within the
// configured health-check timeout when the Docker provider is wedged. Without
// the bounded context, this test would hang until the test timeout fires.
//
// Regression for https://github.com/netresearch/ofelia/issues/614. checkDocker
// runs from the periodic ticker (not the HTTP handler), so the parent context
// is the synthesized background context with a short timeout - operators
// monitoring /health expect a non-2xx response within a single scrape interval.
func TestCheckDocker_BoundedTimeout(t *testing.T) {
	t.Parallel()

	provider := newHangingDockerProvider()
	t.Cleanup(func() { _ = provider.Close() })

	// Build the HealthChecker directly so the background goroutine spawned by
	// NewHealthChecker does not race with the assertion below.
	hc := &HealthChecker{
		startTime:      time.Now(),
		dockerProvider: provider,
		version:        "test",
		checks:         make(map[string]HealthCheck),
		checkInterval:  time.Hour, // arbitrary; the goroutine is not started
	}

	// The bounded check must return well within the worst-case scrape
	// interval. checkDocker performs at most two sequential blocking calls
	// (Ping + Info), so the upper bound is roughly 2 * health-check timeout.
	const returnWithin = 15 * time.Second
	// Hard safety net: if checkDocker hangs anyway, fail this test rather
	// than the whole suite at the package -timeout.
	const hardDeadline = 30 * time.Second

	done := make(chan struct{})
	start := time.Now()
	go func() {
		hc.checkDocker()
		close(done)
	}()

	select {
	case <-done:
		elapsed := time.Since(start)
		if elapsed > returnWithin {
			t.Fatalf("checkDocker returned after %v, want <= %v", elapsed, returnWithin)
		}
		// Verify the check was recorded as unhealthy (Ping should have
		// failed with a context error).
		hc.mu.RLock()
		check, ok := hc.checks["docker"]
		hc.mu.RUnlock()
		if !ok {
			t.Fatal("checkDocker did not record a docker check")
		}
		if check.Status != HealthStatusUnhealthy && check.Status != HealthStatusDegraded {
			t.Fatalf("expected unhealthy/degraded status after wedged daemon, got %q (msg=%q)", check.Status, check.Message)
		}
	case <-time.After(hardDeadline):
		t.Fatalf("checkDocker did not return within %v - the Docker call is unbounded", hardDeadline)
	}
}
