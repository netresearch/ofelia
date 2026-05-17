// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
)

// hangingHandlerProvider implements core.DockerProvider where every
// context-bound call blocks until ctx is canceled. Used to assert that
// NewDockerHandler / buildSDKProvider wrap their sanity Ping in a bounded
// context (#614).
type hangingHandlerProvider struct {
	pingCalls int
}

func (h *hangingHandlerProvider) blockUntilCancelled(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (h *hangingHandlerProvider) Ping(ctx context.Context) error {
	h.pingCalls++
	return h.blockUntilCancelled(ctx)
}

func (h *hangingHandlerProvider) Info(ctx context.Context) (*domain.SystemInfo, error) {
	return nil, h.blockUntilCancelled(ctx)
}
func (h *hangingHandlerProvider) Close() error { return nil }

// Stubs for the rest of the interface.
func (h *hangingHandlerProvider) CreateContainer(_ context.Context, _ *domain.ContainerConfig, _ string) (string, error) {
	return "", nil
}
func (h *hangingHandlerProvider) StartContainer(_ context.Context, _ string) error { return nil }
func (h *hangingHandlerProvider) StopContainer(_ context.Context, _ string, _ domain.StopOptions) error {
	return nil
}

func (h *hangingHandlerProvider) RemoveContainer(_ context.Context, _ string, _ bool) error {
	return nil
}

func (h *hangingHandlerProvider) InspectContainer(_ context.Context, _ string) (*domain.Container, error) {
	return nil, nil
}

func (h *hangingHandlerProvider) ListContainers(_ context.Context, _ domain.ListOptions) ([]domain.Container, error) {
	return nil, nil
}

func (h *hangingHandlerProvider) WaitContainer(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (h *hangingHandlerProvider) GetContainerLogs(_ context.Context, _ string, _ core.ContainerLogsOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (h *hangingHandlerProvider) CreateExec(_ context.Context, _ string, _ *domain.ExecConfig) (string, error) {
	return "", nil
}

func (h *hangingHandlerProvider) StartExec(_ context.Context, _ string, _ domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	return nil, nil
}

func (h *hangingHandlerProvider) InspectExec(_ context.Context, _ string) (*domain.ExecInspect, error) {
	return nil, nil
}

func (h *hangingHandlerProvider) RunExec(_ context.Context, _ string, _ *domain.ExecConfig, _, _ io.Writer) (int, error) {
	return 0, nil
}
func (h *hangingHandlerProvider) PullImage(_ context.Context, _ string) error { return nil }
func (h *hangingHandlerProvider) HasImageLocally(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (h *hangingHandlerProvider) EnsureImage(_ context.Context, _ string, _ bool) error { return nil }

func (h *hangingHandlerProvider) ConnectNetwork(_ context.Context, _, _ string) error { return nil }

func (h *hangingHandlerProvider) FindNetworkByName(_ context.Context, _ string) ([]domain.Network, error) {
	return nil, nil
}

func (h *hangingHandlerProvider) SubscribeEvents(_ context.Context, _ domain.EventFilter) (<-chan domain.Event, <-chan error) {
	return nil, nil
}

func (h *hangingHandlerProvider) CreateService(_ context.Context, _ domain.ServiceSpec, _ domain.ServiceCreateOptions) (string, error) {
	return "", nil
}

func (h *hangingHandlerProvider) InspectService(_ context.Context, _ string) (*domain.Service, error) {
	return nil, nil
}

func (h *hangingHandlerProvider) ListTasks(_ context.Context, _ domain.TaskListOptions) ([]domain.Task, error) {
	return nil, nil
}
func (h *hangingHandlerProvider) RemoveService(_ context.Context, _ string) error { return nil }
func (h *hangingHandlerProvider) WaitForServiceTasks(_ context.Context, _ string, _ time.Duration) ([]domain.Task, error) {
	return nil, nil
}

var _ core.DockerProvider = (*hangingHandlerProvider)(nil)

// TestNewDockerHandler_PostConstructSanityPing_BoundedTimeout asserts the
// post-construction sanity Ping in NewDockerHandler returns within the
// configured timeout when the daemon is wedged. Without the bounded context,
// NewDockerHandler would hang indefinitely at startup. See #614.
func TestNewDockerHandler_PostConstructSanityPing_BoundedTimeout(t *testing.T) {
	provider := &hangingHandlerProvider{}

	cfg := &DockerConfig{
		ConfigPollInterval: 0, // do not start the watcher goroutine
		UseEvents:          false,
		DockerPollInterval: 0,
		PollingFallback:    0,
	}

	const hardDeadline = 30 * time.Second
	const expectWithin = 20 * time.Second // 2x dockerStartupPingTimeout (10s) for CI headroom // dockerStartupPingTimeout is 10s

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		// Pass a non-nil provider to skip buildSDKProvider; we are
		// exercising the second sanity Ping at NewDockerHandler itself.
		_, err := NewDockerHandler(context.Background(), &dummyNotifier{}, slog.Default(), cfg, provider)
		done <- err
	}()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if elapsed > expectWithin {
			t.Fatalf("NewDockerHandler returned after %v, want <= %v", elapsed, expectWithin)
		}
		if err == nil {
			t.Fatal("NewDockerHandler should have returned an error from the wedged Ping, got nil")
		}
		if provider.pingCalls == 0 {
			t.Fatal("hanging provider was never pinged - test did not exercise the sanity check")
		}
	case <-time.After(hardDeadline):
		t.Fatalf("NewDockerHandler did not return within %v - the sanity Ping at the construction site is unbounded", hardDeadline)
	}
}

// TestBuildSDKProvider_PostConstructPing_BoundedTimeout asserts the Ping
// inside buildSDKProvider is bounded. Swaps newSDKDockerProvider for a hanging
// stub so we can exercise the wrapping logic without running a fake Docker
// daemon. See #614.
func TestBuildSDKProvider_PostConstructPing_BoundedTimeout(t *testing.T) {
	stub := &hangingHandlerProvider{}
	original := newSDKDockerProvider
	t.Cleanup(func() { newSDKDockerProvider = original })
	newSDKDockerProvider = func() (core.DockerProvider, error) { return stub, nil }

	cfg := &DockerConfig{
		ConfigPollInterval: 0,
		UseEvents:          false,
		DockerPollInterval: 0,
		PollingFallback:    0,
	}

	const hardDeadline = 30 * time.Second
	const expectWithin = 20 * time.Second // 2x dockerStartupPingTimeout (10s) for CI headroom

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		// Pass nil provider so buildSDKProvider runs.
		_, err := NewDockerHandler(context.Background(), &dummyNotifier{}, slog.Default(), cfg, nil)
		done <- err
	}()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if elapsed > expectWithin {
			t.Fatalf("NewDockerHandler returned after %v, want <= %v", elapsed, expectWithin)
		}
		if err == nil {
			t.Fatal("NewDockerHandler should have returned an error from the wedged provider, got nil")
		}
		if stub.pingCalls == 0 {
			t.Fatal("stub provider was never pinged - the buildSDKProvider sanity check did not run")
		}
	case <-time.After(hardDeadline):
		t.Fatalf("NewDockerHandler did not return within %v - the buildSDKProvider Ping is unbounded", hardDeadline)
	}
}
