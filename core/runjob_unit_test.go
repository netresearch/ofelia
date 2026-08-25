// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// testRunJobKit holds the mock objects needed for RunJob unit tests.
type testRunJobKit struct {
	job        *RunJob
	client     *mock.DockerClient
	containers *mock.ContainerService
	images     *mock.ImageService
	networks   *mock.NetworkService
	handler    *test.Handler
}

// newTestRunJobKit creates a RunJob wired to a mock DockerClient for unit testing.
func newTestRunJobKit(t *testing.T) *testRunJobKit {
	t.Helper()
	mc := mock.NewDockerClient()
	logger, handler := test.NewTestLoggerWithHandler()
	provider := NewSDKDockerProviderFromClient(mc, logger, nil)
	job := NewRunJob(provider)
	job.BareJob = BareJob{Name: "unit-run", Command: "echo hello"}
	job.Image = "alpine:latest"
	job.Delete = "true"
	job.Pull = "true"

	return &testRunJobKit{
		job:        job,
		client:     mc,
		containers: mc.Containers().(*mock.ContainerService),
		images:     mc.Images().(*mock.ImageService),
		networks:   mc.Networks().(*mock.NetworkService),
		handler:    handler,
	}
}

// newRunJobContext creates a Context suitable for RunJob unit tests.
func newRunJobContext(t *testing.T, job *RunJob) *Context {
	t.Helper()
	logger := test.NewTestLogger()
	scheduler := NewScheduler(logger)
	exec, err := NewExecution()
	if err != nil {
		t.Fatalf("NewExecution: %v", err)
	}
	exec.Start()
	return NewContext(scheduler, job, exec)
}

// ---------------------------------------------------------------------------
// Run() tests
// ---------------------------------------------------------------------------

func TestRunJobUnit_Run_HappyPath(t *testing.T) {
	t.Parallel()
	k := newTestRunJobKit(t)
	ctx := newRunJobContext(t, k.job)

	err := k.job.Run(ctx)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestRunJobUnit_Run_ExistingContainer(t *testing.T) {
	t.Parallel()
	k := newTestRunJobKit(t)
	k.job.Image = ""
	k.job.Container = "existing-ctr"
	ctx := newRunJobContext(t, k.job)

	k.containers.OnInspect = func(_ context.Context, _ string) (*domain.Container, error) {
		return &domain.Container{ID: "ctr-123"}, nil
	}
	k.containers.OnWait = func(_ context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		r := make(chan domain.WaitResponse, 1)
		e := make(chan error, 1)
		r <- domain.WaitResponse{StatusCode: 0}
		close(r)
		close(e)
		return r, e
	}

	if err := k.job.Run(ctx); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(k.containers.RemoveCalls) != 0 {
		t.Error("existing container should not be removed")
	}
}

func TestRunJobUnit_Run_DeleteFalse(t *testing.T) {
	t.Parallel()
	k := newTestRunJobKit(t)
	k.job.Delete = "false"
	ctx := newRunJobContext(t, k.job)

	if err := k.job.Run(ctx); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(k.containers.RemoveCalls) != 0 {
		t.Error("container should not be removed when Delete=false")
	}
}

// ---------------------------------------------------------------------------
// ensureImageAvailable()
// ---------------------------------------------------------------------------

func TestRunJobUnit_EnsureImageAvailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pullError error
		wantErr   bool
	}{
		{"success", nil, false},
		{"error", errors.New("registry unreachable"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k := newTestRunJobKit(t)
			ctx := newRunJobContext(t, k.job)

			if tc.pullError != nil {
				k.images.OnPullAndWait = func(_ context.Context, _ domain.PullOptions) error {
					return tc.pullError
				}
				k.images.SetExistsResult(false)
			}

			err := k.job.ensureImageAvailable(context.Background(), ctx, true)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// createOrInspectContainer()
// ---------------------------------------------------------------------------

func TestRunJobUnit_CreateOrInspectContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		image      string
		container  string
		inspectErr error
		wantErr    bool
		wantNew    bool
	}{
		{"new_container_from_image", "alpine:latest", "", nil, false, true},
		{"existing_container", "", "existing", nil, false, false},
		{"inspect_error", "", "missing", errors.New("not found"), true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k := newTestRunJobKit(t)
			k.job.Image = tc.image
			k.job.Container = tc.container

			if tc.inspectErr != nil {
				k.containers.OnInspect = func(_ context.Context, _ string) (*domain.Container, error) {
					return nil, tc.inspectErr
				}
			}

			id, err := k.job.createOrInspectContainer(context.Background())
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id == "" {
				t.Error("expected non-empty container ID")
			}
			if tc.wantNew && len(k.containers.CreateCalls) == 0 {
				t.Error("expected Create to be called")
			}
			if !tc.wantNew && len(k.containers.InspectCalls) == 0 {
				t.Error("expected Inspect to be called")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// startAndWait()
// ---------------------------------------------------------------------------

func TestRunJobUnit_StartAndWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		exitCode int64
		startErr error
		wantErr  bool
		errType  string
	}{
		{"success", 0, nil, false, ""},
		{"non_zero_exit", 42, nil, true, "nonzero"},
		{"start_error", 0, errors.New("cannot start"), true, "start"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k := newTestRunJobKit(t)
			k.job.setContainerID("ctr-abc")
			ctx := newRunJobContext(t, k.job)

			if tc.startErr != nil {
				k.containers.OnStart = func(_ context.Context, _ string) error {
					return tc.startErr
				}
			}
			k.containers.OnWait = func(_ context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
				r := make(chan domain.WaitResponse, 1)
				e := make(chan error, 1)
				r <- domain.WaitResponse{StatusCode: tc.exitCode}
				close(r)
				close(e)
				return r, e
			}

			err := k.job.startAndWait(context.Background(), ctx)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.errType == "nonzero" {
				var nze NonZeroExitError
				if !errors.As(err, &nze) {
					t.Fatalf("expected NonZeroExitError, got %T", err)
				}
				if nze.ExitCode != int(tc.exitCode) {
					t.Errorf("expected exit code %d, got %d", tc.exitCode, nze.ExitCode)
				}
			}
		})
	}
}

func TestRunJobUnit_StartAndWait_MaxRuntimeTimeout(t *testing.T) {
	t.Parallel()
	k := newTestRunJobKit(t)
	k.job.MaxRuntime = 50 * time.Millisecond
	k.job.setContainerID("ctr-timeout")
	ctx := newRunJobContext(t, k.job)

	k.containers.OnWait = func(ctx context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
		r := make(chan domain.WaitResponse, 1)
		e := make(chan error, 1)
		go func() {
			<-ctx.Done()
			e <- ctx.Err()
			close(r)
			close(e)
		}()
		return r, e
	}

	err := k.job.startAndWait(context.Background(), ctx)
	if !errors.Is(err, ErrMaxTimeRunning) {
		t.Fatalf("expected ErrMaxTimeRunning, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// buildContainer()
// ---------------------------------------------------------------------------

func TestRunJobUnit_BuildContainer(t *testing.T) {
	t.Parallel()

	customName := "custom-ctr"
	tests := []struct {
		name          string
		containerName *string
		network       string
		annotations   []string
		wantNetwork   bool
	}{
		{"uses_job_name", nil, "", nil, false},
		{"uses_custom_name", &customName, "", nil, false},
		{"with_network", nil, "my-net", nil, true},
		{"with_annotations", nil, "", []string{"team=backend", "env=staging"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k := newTestRunJobKit(t)
			k.job.ContainerName = tc.containerName
			k.job.Network = tc.network
			k.job.Annotations = tc.annotations

			if tc.wantNetwork {
				k.networks.SetNetworks([]domain.Network{
					{ID: "net-id-1", Name: tc.network},
				})
			}

			id, err := k.job.buildContainer(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id == "" {
				t.Error("expected non-empty container ID")
			}

			if len(k.containers.CreateCalls) == 0 {
				t.Fatal("expected Create to be called")
			}
			cfg := k.containers.CreateCalls[0].Config

			if tc.containerName != nil {
				if cfg.Name != *tc.containerName {
					t.Errorf("expected container name %q, got %q", *tc.containerName, cfg.Name)
				}
			} else {
				if cfg.Name != k.job.Name {
					t.Errorf("expected container name %q (job name), got %q", k.job.Name, cfg.Name)
				}
			}

			if _, ok := cfg.Labels["ofelia.job.name"]; !ok {
				t.Error("expected default annotation ofelia.job.name")
			}

			for _, ann := range tc.annotations {
				parts := splitAnnotation(ann)
				if v, ok := cfg.Labels[parts[0]]; !ok || v != parts[1] {
					t.Errorf("expected annotation %s=%s in labels", parts[0], parts[1])
				}
			}

			if tc.wantNetwork && len(k.networks.ConnectCalls) == 0 {
				t.Error("expected ConnectNetwork to be called")
			}
		})
	}
}

// splitAnnotation splits "key=value" into [key, value].
func splitAnnotation(ann string) [2]string {
	for i, c := range ann {
		if c == '=' {
			return [2]string{ann[:i], ann[i+1:]}
		}
	}
	return [2]string{ann, ""}
}

// ---------------------------------------------------------------------------
// watchContainer()
// ---------------------------------------------------------------------------

func TestRunJobUnit_WatchContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		exitCode int64
		ctxErr   bool
		wantErr  error
	}{
		{"exit_0", 0, false, nil},
		{"non_zero_exit", 1, false, NonZeroExitError{ExitCode: 1}},
		{"unexpected_exit", -1, false, ErrUnexpected},
		{"context_timeout", 0, true, ErrMaxTimeRunning},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k := newTestRunJobKit(t)
			k.job.setContainerID("ctr-watch")

			k.containers.OnWait = func(ctx context.Context, _ string) (<-chan domain.WaitResponse, <-chan error) {
				r := make(chan domain.WaitResponse, 1)
				e := make(chan error, 1)
				if tc.ctxErr {
					go func() {
						<-ctx.Done()
						e <- ctx.Err()
						close(r)
						close(e)
					}()
				} else {
					r <- domain.WaitResponse{StatusCode: tc.exitCode}
					close(r)
					close(e)
				}
				return r, e
			}

			ctx := context.Background()
			if tc.ctxErr {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
			}

			err := k.job.watchContainer(ctx)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tc.ctxErr {
				if !errors.Is(err, ErrMaxTimeRunning) {
					t.Errorf("expected ErrMaxTimeRunning, got %v", err)
				}
			} else if errors.Is(tc.wantErr, ErrUnexpected) {
				if !errors.Is(err, ErrUnexpected) {
					t.Errorf("expected ErrUnexpected, got %v", err)
				}
			} else {
				if _, ok := errors.AsType[NonZeroExitError](err); !ok {
					t.Errorf("expected NonZeroExitError, got %T: %v", err, err)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// deleteContainer()
// ---------------------------------------------------------------------------

func TestRunJobUnit_DeleteContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		deleteFlag string
		removeErr  error
		wantRemove bool
		wantErr    bool
	}{
		{"delete_true", "true", nil, true, false},
		{"delete_false", "false", nil, false, false},
		{"delete_true_with_error", "true", errors.New("cannot remove"), true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k := newTestRunJobKit(t)
			k.job.Delete = tc.deleteFlag
			k.job.setContainerID("ctr-del")

			if tc.removeErr != nil {
				k.containers.OnRemove = func(_ context.Context, _ string, _ domain.RemoveOptions) error {
					return tc.removeErr
				}
			}

			err := k.job.deleteContainer(context.Background())
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			removed := len(k.containers.RemoveCalls) > 0
			if removed != tc.wantRemove {
				t.Errorf("expected remove=%v, got %v", tc.wantRemove, removed)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Run() with logs streaming
// ---------------------------------------------------------------------------

func TestRunJobUnit_Run_LogsStreamedToOutput(t *testing.T) {
	t.Parallel()
	k := newTestRunJobKit(t)
	ctx := newRunJobContext(t, k.job)

	logPayload := "container log output\n"
	k.containers.OnLogs = func(_ context.Context, _ string, _ domain.LogOptions) (io.ReadCloser, error) {
		return io.NopCloser(newStringReader(logPayload)), nil
	}

	if err := k.job.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := ctx.Execution.OutputStream.String()
	if output != logPayload {
		t.Errorf("expected output %q, got %q", logPayload, output)
	}
}

// stringReader is a simple io.Reader wrapping a string.
type stringReader struct {
	data []byte
	pos  int
}

func newStringReader(s string) *stringReader {
	return &stringReader{data: []byte(s)}
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// ---------------------------------------------------------------------------
// Run() error propagation
// ---------------------------------------------------------------------------

func TestRunJobUnit_Run_EnsureImageError(t *testing.T) {
	t.Parallel()
	k := newTestRunJobKit(t)
	ctx := newRunJobContext(t, k.job)

	k.images.OnPullAndWait = func(_ context.Context, _ domain.PullOptions) error {
		return fmt.Errorf("pull failed")
	}
	k.images.SetExistsResult(false)

	err := k.job.Run(ctx)
	if err == nil {
		t.Fatal("expected error from ensureImageAvailable")
	}
}

func TestRunJobUnit_Run_CreateContainerError(t *testing.T) {
	t.Parallel()
	k := newTestRunJobKit(t)
	ctx := newRunJobContext(t, k.job)

	k.containers.OnCreate = func(_ context.Context, _ *domain.ContainerConfig) (string, error) {
		return "", fmt.Errorf("disk full")
	}

	err := k.job.Run(ctx)
	if err == nil {
		t.Fatal("expected error from createOrInspectContainer")
	}
}
