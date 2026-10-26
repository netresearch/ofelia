// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// installFakeDocker puts a `docker` script running body first on PATH, so
// ComposeJob starts it instead of the real CLI.
func installFakeDocker(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake docker CLI is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700)) //nolint:gosec // test executable
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func newComposeCancelJob(provider DockerProvider) *ComposeJob {
	job := NewComposeJob()
	job.BareJob = BareJob{Name: "compose-cancel"}
	job.File = "compose.yml"
	job.Service = "task"
	job.Provider = provider
	return job
}

// runComposeWithCancel runs job and cancels its run context after delay.
func runComposeWithCancel(t *testing.T, job *ComposeJob, delay time.Duration) (*Context, error) {
	t.Helper()
	exec, err := NewExecution()
	require.NoError(t, err)
	exec.Start()
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := NewContextWithContext(runCtx, NewScheduler(test.NewTestLogger()), job, exec)
	time.AfterFunc(delay, cancel)
	return ctx, job.Run(ctx)
}

func TestComposeJob_Cancel_StopsLabeledContainer(t *testing.T) {
	installFakeDocker(t, "exec sleep 30")

	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)

	var mu sync.Mutex
	var filters []string
	var stopped, removed []string
	var stopTimeout time.Duration
	live := true
	containers.OnList = func(_ context.Context, opts domain.ListOptions) ([]domain.Container, error) {
		mu.Lock()
		defer mu.Unlock()
		filters = append(filters, opts.Filters["label"]...)
		if !live {
			return nil, nil
		}
		return []domain.Container{{ID: "run-container"}}, nil
	}
	containers.OnStop = func(_ context.Context, id string, opts domain.StopOptions) error {
		mu.Lock()
		defer mu.Unlock()
		stopped = append(stopped, id)
		if opts.Timeout != nil {
			stopTimeout = *opts.Timeout
		}
		return nil
	}
	containers.OnRemove = func(_ context.Context, id string, _ domain.RemoveOptions) error {
		mu.Lock()
		defer mu.Unlock()
		removed = append(removed, id)
		live = false
		return nil
	}

	job := newComposeCancelJob(NewSDKDockerProviderFromClient(client, nil, nil))
	start := time.Now()
	ctx, err := runComposeWithCancel(t, job, 200*time.Millisecond)

	require.ErrorIs(t, err, ErrMaxTimeRunning)
	require.NotErrorIs(t, err, ErrComposeTerminationUnconfirmed)
	assert.Less(t, time.Since(start), 5*time.Second, "SIGTERM must end the CLI well before its exit timeout")

	mu.Lock()
	defer mu.Unlock()
	wantLabel := ComposeExecutionLabel + "=" + ctx.Execution.ID
	require.NotEmpty(t, filters)
	for _, f := range filters {
		assert.Equal(t, wantLabel, f, "only containers of this execution may be touched")
	}
	assert.Contains(t, stopped, "run-container")
	assert.Equal(t, composeStopTimeout, stopTimeout)
	assert.Contains(t, removed, "run-container")
}

func TestComposeJob_Cancel_DaemonUnreachableIsUnconfirmed(t *testing.T) {
	installFakeDocker(t, "exec sleep 30")

	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	containers.OnList = func(context.Context, domain.ListOptions) ([]domain.Container, error) {
		return nil, errors.New("connection refused")
	}

	job := newComposeCancelJob(NewSDKDockerProviderFromClient(client, nil, nil))
	_, err := runComposeWithCancel(t, job, 100*time.Millisecond)

	require.ErrorIs(t, err, ErrMaxTimeRunning)
	require.ErrorIs(t, err, ErrComposeTerminationUnconfirmed)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestComposeJob_Cancel_NoProviderIsUnconfirmed(t *testing.T) {
	installFakeDocker(t, "exec sleep 30")

	job := newComposeCancelJob(nil)
	_, err := runComposeWithCancel(t, job, 100*time.Millisecond)

	require.ErrorIs(t, err, ErrComposeTerminationUnconfirmed)
}

func TestComposeJob_Cancel_ContainerThatStaysIsUnconfirmed(t *testing.T) {
	installFakeDocker(t, "exec sleep 30")

	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	containers.OnList = func(context.Context, domain.ListOptions) ([]domain.Container, error) {
		return []domain.Container{{ID: "stuck"}}, nil
	}
	containers.OnRemove = func(context.Context, string, domain.RemoveOptions) error {
		return errors.New("removal of container stuck is already in progress")
	}

	job := newComposeCancelJob(NewSDKDockerProviderFromClient(client, nil, nil))
	_, err := runComposeWithCancel(t, job, 100*time.Millisecond)

	require.ErrorIs(t, err, ErrComposeTerminationUnconfirmed)
	assert.Contains(t, err.Error(), "stuck")
}

func TestComposeJob_CompletedRunKeepsItsResult(t *testing.T) {
	installFakeDocker(t, "exit 0")

	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	listed := false
	containers.OnList = func(context.Context, domain.ListOptions) ([]domain.Container, error) {
		listed = true
		return nil, nil
	}

	job := newComposeCancelJob(NewSDKDockerProviderFromClient(client, nil, nil))
	_, err := runComposeWithCancel(t, job, time.Hour)

	require.NoError(t, err)
	assert.False(t, listed, "a run that finished on its own needs no cleanup")
}

func TestComposeJob_FailedRunReportsExitError(t *testing.T) {
	installFakeDocker(t, "exit 3")

	job := newComposeCancelJob(nil)
	_, err := runComposeWithCancel(t, job, time.Hour)

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrMaxTimeRunning)
	assert.Contains(t, err.Error(), "exit status 3")
}

// A failed CLI must retain its exit status while cleanup either confirms the
// workload ended or exposes the uncertainty needed to keep admission blocked.
func TestComposeJob_FailedCLIReconcilesItsWorkload(t *testing.T) {
	installFakeDocker(t, "exit 7")
	for _, unreachable := range []bool{false, true} {
		name := "cleanup confirmed"
		if unreachable {
			name = "Docker unreachable"
		}
		t.Run(name, func(t *testing.T) {
			client := mock.NewDockerClient()
			containers := client.Containers().(*mock.ContainerService)
			live, stopped := true, false
			containers.OnList = func(context.Context, domain.ListOptions) ([]domain.Container, error) {
				if unreachable {
					return nil, errors.New("daemon disconnected")
				}
				if live {
					return []domain.Container{{ID: "orphaned-run"}}, nil
				}
				return nil, nil
			}
			containers.OnStop = func(context.Context, string, domain.StopOptions) error {
				stopped = true
				return nil
			}
			containers.OnRemove = func(context.Context, string, domain.RemoveOptions) error {
				live = false
				return nil
			}
			job := newComposeCancelJob(NewSDKDockerProviderFromClient(client, nil, nil))
			execution, err := NewExecution()
			require.NoError(t, err)
			defer execution.Cleanup()
			ctx := NewContextWithContext(context.Background(), NewScheduler(test.NewTestLogger()), job, execution)
			err = job.Run(ctx)
			require.Error(t, err)
			require.NoError(t, ctx.RunContext().Err(), "a CLI failure is not a run cancellation")
			require.NotErrorIs(t, err, ErrMaxTimeRunning)
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, 7, exitErr.ExitCode(), "preserve the original CLI exit status")
			if unreachable {
				require.ErrorIs(t, err, ErrComposeTerminationUnconfirmed)
				assert.Contains(t, err.Error(), "daemon disconnected")
			} else {
				require.NotErrorIs(t, err, ErrComposeTerminationUnconfirmed)
				assert.True(t, stopped)
				assert.False(t, live, "a failed CLI must not leave its run workload alive")
			}
		})
	}
}

func TestCliDockerHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in       string
		wantHost string
		wantTLS  bool
	}{
		{"unix:///var/run/docker.sock", "unix:///var/run/docker.sock", false},
		{"tcp://10.0.0.1:2375", "tcp://10.0.0.1:2375", false},
		{"npipe:////./pipe/docker_engine", "npipe:////./pipe/docker_engine", false},
		{"http://10.0.0.1:2375", "tcp://10.0.0.1:2375", false},
		{"https://10.0.0.1:2376", "tcp://10.0.0.1:2376", true},
		{"tcp+tls://10.0.0.1:2376", "tcp://10.0.0.1:2376", true},
	}
	for _, tt := range tests {
		host, tls := cliDockerHost(tt.in)
		assert.Equal(t, tt.wantHost, host, tt.in)
		assert.Equal(t, tt.wantTLS, tls, tt.in)
	}
}

func TestComposeChildEnv_PinsEndpointAndDropsContext(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	base := []string{"PATH=/usr/bin", "DOCKER_CONTEXT=remote", "DOCKER_HOST=tcp://elsewhere:2375"}

	env, err := composeChildEnv(base)
	require.NoError(t, err)

	assert.Contains(t, env, "PATH=/usr/bin")
	assert.False(t, slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "DOCKER_CONTEXT=") }))
	hosts := slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return !strings.HasPrefix(kv, "DOCKER_HOST=") })
	assert.Equal(t, []string{"DOCKER_HOST=unix:///var/run/docker.sock"}, hosts,
		"the child must use the endpoint Ofelia's own client resolves")
}

func TestComposeChildEnv_FollowsDockerHostAndRequestsTLS(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp+tls://daemon:2376")

	env, err := composeChildEnv([]string{"PATH=/usr/bin"})
	require.NoError(t, err)

	assert.Contains(t, env, "DOCKER_HOST=tcp://daemon:2376")
	assert.Contains(t, env, "DOCKER_TLS_VERIFY=1")
}

func TestComposeChildEnv_KeepsExplicitTLSVerify(t *testing.T) {
	t.Setenv("DOCKER_HOST", "https://daemon:2376")

	env, err := composeChildEnv([]string{"DOCKER_TLS_VERIFY=yes"})
	require.NoError(t, err)

	assert.Equal(t, []string{"DOCKER_TLS_VERIFY=yes", "DOCKER_HOST=tcp://daemon:2376"}, env)
}

func TestComposeChildEnv_InvalidHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "ftp://daemon")

	_, err := composeChildEnv(nil)
	require.Error(t, err)
}

func TestIgnoredDockerContext(t *testing.T) {
	cfgDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"currentContext":"rootless"}`), 0o600))
	defaultDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(defaultDir, "config.json"), []byte(`{"currentContext":"default"}`), 0o600))
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".docker"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".docker", "config.json"), []byte(`{"currentContext":"remote"}`), 0o600))

	tests := []struct {
		name string
		env  []string
		want string
	}{
		{"DOCKER_CONTEXT is followed", []string{"DOCKER_CONTEXT=remote"}, "remote"},
		{"default context changes nothing", []string{"DOCKER_CONTEXT=default"}, ""},
		{"DOCKER_HOST already wins over DOCKER_CONTEXT", []string{"DOCKER_HOST=unix:///x.sock", "DOCKER_CONTEXT=remote"}, ""},
		{"currentContext from DOCKER_CONFIG", []string{"DOCKER_CONFIG=" + cfgDir}, "rootless"},
		{"currentContext default", []string{"DOCKER_CONFIG=" + defaultDir}, ""},
		{"currentContext from HOME", []string{"HOME=" + home}, "remote"},
		{"no config file", []string{"DOCKER_CONFIG=" + t.TempDir()}, ""},
		{"DOCKER_CONTEXT beats the config file", []string{"DOCKER_CONTEXT=other", "DOCKER_CONFIG=" + cfgDir}, "other"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ignoredDockerContext(tt.env), tt.name)
	}
}

// runComposeWithLogger runs job to completion and returns the log handler.
func runComposeWithLogger(t *testing.T, job *ComposeJob) *test.Handler {
	t.Helper()
	logger, handler := test.NewTestLoggerWithHandler()
	exec, err := NewExecution()
	require.NoError(t, err)
	exec.Start()
	ctx := NewContextWithContext(context.Background(), NewScheduler(logger), job, exec)
	require.NoError(t, job.Run(ctx))
	return handler
}

func TestComposeJob_WarnsAboutIgnoredDockerContext(t *testing.T) {
	installFakeDocker(t, "exit 0")
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "remote")

	handler := runComposeWithLogger(t, newComposeCancelJob(nil))

	assert.True(t, handler.HasWarning(`ignores Docker context "remote"`),
		"a context the CLI no longer follows must be reported")
}

func TestComposeJob_ChildHoldingOutputDoesNotBlockRun(t *testing.T) {
	// The CLI exits at once, but a process it started keeps stdout open.
	installFakeDocker(t, "sleep 3 &\nexit 0")
	old := composeWaitDelay
	composeWaitDelay = 200 * time.Millisecond
	t.Cleanup(func() { composeWaitDelay = old })

	start := time.Now()
	handler := runComposeWithLogger(t, newComposeCancelJob(nil))

	assert.Less(t, time.Since(start), 2*time.Second, "Run must not wait for the pipe holder to exit")
	assert.True(t, handler.HasWarning("output after the CLI exited was not collected"))
}

func TestComposeJob_NoWarningWithoutContext(t *testing.T) {
	installFakeDocker(t, "exit 0")
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	handler := runComposeWithLogger(t, newComposeCancelJob(nil))

	assert.False(t, handler.HasWarning("ignores Docker context"))
}
