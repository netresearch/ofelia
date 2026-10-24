// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"os"
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
