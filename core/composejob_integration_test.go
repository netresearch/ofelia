//go:build integration

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// composeProject writes a compose file for a uniquely named project and
// tears the project down when the test ends.
func composeProject(t *testing.T, services string) (file, project string) {
	t.Helper()
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skipf("docker compose CLI not available: %v", err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	project = "ofelia-it-" + hex.EncodeToString(suffix)
	file = filepath.Join(t.TempDir(), "compose.yml")
	require.NoError(t, os.WriteFile(file, []byte("name: "+project+"\nservices:\n"+services), 0o600))
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-f", file, "down", "--remove-orphans", "-t", "0").Run()
	})
	return file, project
}

func newIntegrationProvider(t *testing.T) DockerProvider {
	t.Helper()
	provider, err := NewSDKDockerProviderDefault()
	if err != nil {
		t.Skipf("Docker daemon not available: %v", err)
	}
	if err := provider.Ping(context.Background()); err != nil {
		t.Skipf("Docker daemon not reachable: %v", err)
	}
	return provider
}

func listByLabel(t *testing.T, provider DockerProvider, label string, all bool) []domain.Container {
	t.Helper()
	containers, err := provider.ListContainers(context.Background(), domain.ListOptions{
		All:     all,
		Filters: map[string][]string{"label": {label}},
	})
	require.NoError(t, err)
	return containers
}

// startCompose runs job in the background and returns its execution and a
// channel with Run's result.
func startCompose(t *testing.T, job *ComposeJob, runCtx context.Context) (*Execution, <-chan error) {
	t.Helper()
	execution, err := NewExecution()
	require.NoError(t, err)
	execution.Start()
	ctx := NewContextWithContext(runCtx, NewScheduler(test.NewTestLogger()), job, execution)
	result := make(chan error, 1)
	go func() { result <- job.Run(ctx) }()
	return execution, result
}

func waitForLabeled(t *testing.T, provider DockerProvider, label string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return len(listByLabel(t, provider, label, false)) > 0
	}, 60*time.Second, 200*time.Millisecond, "run container did not start")
}

const gracefulTask = `
  task:
    image: alpine:3.22
    command: ["sh", "-c", "trap 'exit 0' TERM; sleep 120 & wait"]
`

func TestComposeJobIntegration_CancelRemovesRunContainer(t *testing.T) {
	provider := newIntegrationProvider(t)
	file, _ := composeProject(t, gracefulTask)

	job := NewComposeJob()
	job.Name = "compose-cancel-it"
	job.File = file
	job.Service = "task"
	job.Provider = provider

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	execution, result := startCompose(t, job, runCtx)
	label := ComposeExecutionLabel + "=" + execution.ID
	waitForLabeled(t, provider, label)

	cancel()
	var err error
	select {
	case err = <-result:
	case <-time.After(60 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	require.ErrorIs(t, err, ErrMaxTimeRunning)
	require.NotErrorIs(t, err, ErrComposeTerminationUnconfirmed)
	assert.Empty(t, listByLabel(t, provider, label, true), "the canceled run container must be gone")
}

func TestComposeJobIntegration_CancelKeepsSharedDependency(t *testing.T) {
	provider := newIntegrationProvider(t)
	file, project := composeProject(t, `
  dep:
    image: alpine:3.22
    init: true
    command: ["sleep", "600"]
`+gracefulTask+`    depends_on: [dep]
`)

	job := NewComposeJob()
	job.Name = "compose-dep-it"
	job.File = file
	job.Service = "task"
	job.Provider = provider

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	execution, result := startCompose(t, job, runCtx)
	label := ComposeExecutionLabel + "=" + execution.ID
	waitForLabeled(t, provider, label)

	cancel()
	err := <-result
	require.ErrorIs(t, err, ErrMaxTimeRunning)
	require.NotErrorIs(t, err, ErrComposeTerminationUnconfirmed)

	assert.Empty(t, listByLabel(t, provider, label, true))
	deps := listByLabel(t, provider, "com.docker.compose.project="+project, false)
	var depRunning bool
	for _, c := range deps {
		if c.Labels["com.docker.compose.service"] == "dep" {
			depRunning = true
		}
	}
	assert.True(t, depRunning, "the dependency service must keep running")
}

// TestComposeJobIntegration_CompletionRacingCancellation cancels around the
// moment a short run finishes on its own. Either outcome is acceptable — a
// completed run or a canceled one — but cleanup must always be confirmed and
// leave no run container behind.
func TestComposeJobIntegration_CompletionRacingCancellation(t *testing.T) {
	provider := newIntegrationProvider(t)
	file, _ := composeProject(t, `
  task:
    image: alpine:3.22
    command: ["sleep", "1"]
`)

	delays := []time.Duration{
		600 * time.Millisecond, 900 * time.Millisecond, 1100 * time.Millisecond,
		1300 * time.Millisecond, 1600 * time.Millisecond,
	}
	for _, delay := range delays {
		job := NewComposeJob()
		job.Name = "compose-race-it"
		job.File = file
		job.Service = "task"
		job.Provider = provider

		runCtx, cancel := context.WithCancel(context.Background())
		execution, result := startCompose(t, job, runCtx)
		time.AfterFunc(delay, cancel)
		err := <-result
		cancel()

		if err != nil {
			require.ErrorIs(t, err, ErrMaxTimeRunning, "delay %s", delay)
			require.NotErrorIs(t, err, ErrComposeTerminationUnconfirmed, "delay %s", delay)
		}
		assert.Empty(t, listByLabel(t, provider, ComposeExecutionLabel+"="+execution.ID, true), "delay %s", delay)
	}
}
