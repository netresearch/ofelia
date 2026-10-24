// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// A compose run job with max-runtime is stopped when the bound expires: the
// scheduler bounds the run context through GetMaxRuntime (boundJobContext),
// and the expired context takes the cancellation path that stops the labeled
// container. Before max-runtime applied to compose jobs, the bound was the
// fixed 24h default, so a hung job blocked its schedule for a day.
func TestComposeJob_MaxRuntimeStopsRun(t *testing.T) {
	installFakeDocker(t, "exec sleep 30")

	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	var mu sync.Mutex
	var stopped []string
	live := true
	containers.OnList = func(context.Context, domain.ListOptions) ([]domain.Container, error) {
		mu.Lock()
		defer mu.Unlock()
		if !live {
			return nil, nil
		}
		return []domain.Container{{ID: "run-container"}}, nil
	}
	containers.OnStop = func(_ context.Context, id string, _ domain.StopOptions) error {
		mu.Lock()
		defer mu.Unlock()
		stopped = append(stopped, id)
		return nil
	}
	containers.OnRemove = func(context.Context, string, domain.RemoveOptions) error {
		mu.Lock()
		defer mu.Unlock()
		live = false
		return nil
	}

	job := newComposeCancelJob(NewSDKDockerProviderFromClient(client, nil, nil))
	job.MaxRuntime = 200 * time.Millisecond

	exec, err := NewExecution()
	require.NoError(t, err)
	exec.Start()
	// The job's own 200ms must win over the default the scheduler passes.
	bounded, cancel := boundJobContext(context.Background(), job, time.Hour)
	defer cancel()
	ctx := NewContextWithContext(bounded, NewScheduler(test.NewTestLogger()), job, exec)

	done := make(chan error, 1)
	go func() { done <- job.Run(ctx) }()
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("max-runtime did not stop the compose run")
	}

	require.ErrorIs(t, err, ErrMaxTimeRunning)
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, stopped, "run-container")
}

func TestComposeJob_GetMaxRuntime(t *testing.T) {
	t.Parallel()
	job := NewComposeJob()
	assert.Zero(t, job.GetMaxRuntime())
	job.MaxRuntime = time.Minute
	assert.Equal(t, time.Minute, job.GetMaxRuntime())
}

func TestComposeJob_ValidateMaxRuntime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		exec    bool
		max     time.Duration
		wantErr error
	}{
		{name: "run mode with bound", max: time.Minute},
		{name: "run mode without bound"},
		{name: "exec mode without bound", exec: true},
		{name: "exec mode with bound", exec: true, max: time.Minute, wantErr: ErrComposeExecMaxRuntime},
		{name: "negative bound", max: -time.Second, wantErr: ErrMaxRuntimeNegative},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			job := NewComposeJob()
			job.Exec = tc.exec
			job.MaxRuntime = tc.max
			err := job.Validate()
			if tc.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}
