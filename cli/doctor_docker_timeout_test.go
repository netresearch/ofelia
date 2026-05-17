// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// hangingDoctorProvider implements core.DockerProvider where Ping and
// HasImageLocally block on ctx.Done(). Models a wedged Docker daemon - the
// very failure mode `ofelia doctor` is supposed to surface. Without a bounded
// context wrapper, the diagnostic command would hang forever.
type hangingDoctorProvider struct{}

func (h *hangingDoctorProvider) blockUntilCancelled(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (h *hangingDoctorProvider) Ping(ctx context.Context) error {
	return h.blockUntilCancelled(ctx)
}

func (h *hangingDoctorProvider) HasImageLocally(ctx context.Context, _ string) (bool, error) {
	return false, h.blockUntilCancelled(ctx)
}

func (h *hangingDoctorProvider) Info(ctx context.Context) (*domain.SystemInfo, error) {
	return nil, h.blockUntilCancelled(ctx)
}

func (h *hangingDoctorProvider) Close() error { return nil }

// Remaining methods are stubs so the type satisfies core.DockerProvider.
func (h *hangingDoctorProvider) CreateContainer(_ context.Context, _ *domain.ContainerConfig, _ string) (string, error) {
	return "", nil
}
func (h *hangingDoctorProvider) StartContainer(_ context.Context, _ string) error { return nil }
func (h *hangingDoctorProvider) StopContainer(_ context.Context, _ string, _ domain.StopOptions) error {
	return nil
}

func (h *hangingDoctorProvider) RemoveContainer(_ context.Context, _ string, _ bool) error {
	return nil
}

func (h *hangingDoctorProvider) InspectContainer(_ context.Context, _ string) (*domain.Container, error) {
	return nil, nil
}

func (h *hangingDoctorProvider) ListContainers(_ context.Context, _ domain.ListOptions) ([]domain.Container, error) {
	return nil, nil
}

func (h *hangingDoctorProvider) WaitContainer(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (h *hangingDoctorProvider) GetContainerLogs(_ context.Context, _ string, _ core.ContainerLogsOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (h *hangingDoctorProvider) CreateExec(_ context.Context, _ string, _ *domain.ExecConfig) (string, error) {
	return "", nil
}

func (h *hangingDoctorProvider) StartExec(_ context.Context, _ string, _ domain.ExecStartOptions) (*domain.HijackedResponse, error) {
	return nil, nil
}

func (h *hangingDoctorProvider) InspectExec(_ context.Context, _ string) (*domain.ExecInspect, error) {
	return nil, nil
}

func (h *hangingDoctorProvider) RunExec(_ context.Context, _ string, _ *domain.ExecConfig, _, _ io.Writer) (int, error) {
	return 0, nil
}
func (h *hangingDoctorProvider) PullImage(_ context.Context, _ string) error           { return nil }
func (h *hangingDoctorProvider) EnsureImage(_ context.Context, _ string, _ bool) error { return nil }
func (h *hangingDoctorProvider) ConnectNetwork(_ context.Context, _, _ string) error   { return nil }
func (h *hangingDoctorProvider) FindNetworkByName(_ context.Context, _ string) ([]domain.Network, error) {
	return nil, nil
}

func (h *hangingDoctorProvider) SubscribeEvents(_ context.Context, _ domain.EventFilter) (<-chan domain.Event, <-chan error) {
	return nil, nil
}

func (h *hangingDoctorProvider) CreateService(_ context.Context, _ domain.ServiceSpec, _ domain.ServiceCreateOptions) (string, error) {
	return "", nil
}

func (h *hangingDoctorProvider) InspectService(_ context.Context, _ string) (*domain.Service, error) {
	return nil, nil
}

func (h *hangingDoctorProvider) ListTasks(_ context.Context, _ domain.TaskListOptions) ([]domain.Task, error) {
	return nil, nil
}
func (h *hangingDoctorProvider) RemoveService(_ context.Context, _ string) error { return nil }
func (h *hangingDoctorProvider) WaitForServiceTasks(_ context.Context, _ string, _ time.Duration) ([]domain.Task, error) {
	return nil, nil
}

var _ core.DockerProvider = (*hangingDoctorProvider)(nil)

// installHangingDockerHandler swaps the package-level newDockerHandler factory
// to return a DockerHandler whose dockerProvider hangs on every context-bound
// call. The override is reverted via t.Cleanup. Tests using this helper cannot
// run in parallel because newDockerHandler is a package-global.
func installHangingDockerHandler(t *testing.T) {
	t.Helper()
	original := newDockerHandler
	t.Cleanup(func() { newDockerHandler = original })
	newDockerHandler = func(ctx context.Context, notifier dockerContainersUpdate, logger *slog.Logger, cfg *DockerConfig, _ core.DockerProvider) (*DockerHandler, error) {
		return &DockerHandler{
			ctx:                ctx,
			dockerProvider:     &hangingDoctorProvider{},
			notifier:           notifier,
			logger:             logger,
			configPollInterval: cfg.ConfigPollInterval,
			useEvents:          cfg.UseEvents,
			dockerPollInterval: cfg.DockerPollInterval,
			pollingFallback:    cfg.PollingFallback,
		}, nil
	}
}

// writeDoctorConfigWithRunJob writes a minimal config that defines a single
// job-run pinned to a specific image so checkDocker / checkDockerImages do
// real work (they early-return when no docker-based jobs exist).
func writeDoctorConfigWithRunJob(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.ini")
	content := `[global]

[job-run "wedged-daemon-test"]
schedule = @daily
image = alpine:latest
command = echo hi
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

// runWithDeadline runs fn in a goroutine and returns true if it completed
// before the deadline. Failing tests are reported via t.Fatalf so the caller
// can chain assertions on completion timing.
func runWithDeadline(t *testing.T, deadline time.Duration, fn func()) time.Duration {
	t.Helper()
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return time.Since(start)
	case <-time.After(deadline):
		t.Fatalf("operation did not complete within %v - the Docker call appears unbounded", deadline)
		return 0
	}
}

// TestDoctor_CheckDocker_BoundedTimeout asserts checkDocker returns within
// the configured doctor timeout when the Docker daemon is wedged. Without
// the bounded context, this would hang and ofelia doctor would never report
// the failure - the very behavior that prompted #614.
func TestDoctor_CheckDocker_BoundedTimeout(t *testing.T) {
	installHangingDockerHandler(t)

	cmd := &DoctorCommand{
		ConfigFile: writeDoctorConfigWithRunJob(t),
		Logger:     test.NewTestLogger(),
		JSON:       true,
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}

	const hardDeadline = 30 * time.Second
	const expectWithin = 15 * time.Second

	elapsed := runWithDeadline(t, hardDeadline, func() {
		cmd.checkDocker(report)
	})

	if elapsed > expectWithin {
		t.Fatalf("checkDocker returned after %v, want <= %v", elapsed, expectWithin)
	}

	// Should have recorded a Connectivity check with status fail.
	var found bool
	for _, c := range report.Checks {
		if c.Category == categoryDocker && c.Name == checkNameConnectivity {
			found = true
			if c.Status != statusFail {
				t.Errorf("expected fail status for wedged daemon, got %q (msg=%q)", c.Status, c.Message)
			}
			break
		}
	}
	if !found {
		t.Errorf("checkDocker did not record a Connectivity check; got: %+v", report.Checks)
	}
}

// TestDoctor_CheckDockerImages_BoundedTimeout asserts checkDockerImages
// returns within the configured doctor timeout when HasImageLocally hangs.
// Mirrors the per-call bounded-context fix at cli/doctor.go:553.
func TestDoctor_CheckDockerImages_BoundedTimeout(t *testing.T) {
	installHangingDockerHandler(t)

	cmd := &DoctorCommand{
		ConfigFile: writeDoctorConfigWithRunJob(t),
		Logger:     test.NewTestLogger(),
		JSON:       true,
	}
	report := &DoctorReport{Healthy: true, Checks: []CheckResult{}}

	const hardDeadline = 30 * time.Second
	const expectWithin = 15 * time.Second

	elapsed := runWithDeadline(t, hardDeadline, func() {
		cmd.checkDockerImages(report)
	})

	if elapsed > expectWithin {
		t.Fatalf("checkDockerImages returned after %v, want <= %v", elapsed, expectWithin)
	}
}
