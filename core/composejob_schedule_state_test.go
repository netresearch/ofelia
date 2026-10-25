// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

func TestComposeJob_SchedulePolicy(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		revision, missed, concurrency string
		want                          bool
		catchUp, queue                bool
	}{
		"plain cron form":       {"r1", "one", "forbid", true, true, false},
		"queue":                 {"r1", "skip", "queue", true, false, true},
		"both":                  {"r1", "one", "queue", true, true, true},
		"skip and forbid":       {"r1", "skip", "forbid", false, false, false},
		"not from a project":    {"", "one", "queue", false, false, false},
		"job-compose (no file)": {"", "", "", false, false, false},
	}
	for name, tc := range cases {
		j := NewComposeJob()
		j.Schedule = "0 2 * * *"
		j.Project = ComposeProjectContext{Revision: tc.revision, MissedFires: tc.missed, Concurrency: tc.concurrency}
		p, ok := j.SchedulePolicy()
		require.Equal(t, tc.want, ok, name)
		if !ok {
			continue
		}
		assert.Equal(t, tc.catchUp, p.CatchUp, "%s: catch-up", name)
		assert.Equal(t, tc.queue, p.Queue, "%s: queue", name)
		assert.Equal(t, ScheduleRevision("0 2 * * *", tc.catchUp, tc.queue), p.Revision, "%s: revision", name)
	}
}

// The policies are part of the job hash, so a changed policy replaces the job
// on reload.
func TestComposeJob_PoliciesAreHashed(t *testing.T) {
	t.Parallel()
	a := NewComposeJob()
	a.Project = ComposeProjectContext{Revision: "r1", MissedFires: "one", Concurrency: "forbid"}
	b := NewComposeJob()
	b.Project = ComposeProjectContext{Revision: "r1", MissedFires: "one", Concurrency: "queue"}
	c := NewComposeJob()
	c.Project = ComposeProjectContext{Revision: "r1", MissedFires: "skip", Concurrency: "forbid"}
	ha, err := a.Hash()
	require.NoError(t, err)
	hb, err := b.Hash()
	require.NoError(t, err)
	hc, err := c.Hash()
	require.NoError(t, err)
	assert.NotEqual(t, ha, hb, "concurrency")
	assert.NotEqual(t, ha, hc, "missed_fires")
}

func TestComposeJob_WorkloadRunning(t *testing.T) {
	t.Parallel()
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	var gotOpts domain.ListOptions
	running := true
	containers.OnList = func(_ context.Context, opts domain.ListOptions) ([]domain.Container, error) {
		gotOpts = opts
		if running {
			return []domain.Container{{ID: "run-container"}}, nil
		}
		return nil, nil
	}
	j := NewComposeJob()
	j.Provider = NewSDKDockerProviderFromClient(client, nil, nil)

	ok, err := j.WorkloadRunning(context.Background(), "exec-1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{ComposeExecutionLabel + "=exec-1"}, gotOpts.Filters["label"])
	assert.False(t, gotOpts.All, "only running containers count")

	running = false
	ok, err = j.WorkloadRunning(context.Background(), "exec-1")
	require.NoError(t, err)
	assert.False(t, ok)

	containers.OnList = func(context.Context, domain.ListOptions) ([]domain.Container, error) {
		return nil, errors.New("daemon unreachable")
	}
	_, err = j.WorkloadRunning(context.Background(), "exec-1")
	require.Error(t, err)

	j.Exec = true
	ok, err = j.WorkloadRunning(context.Background(), "exec-1")
	require.NoError(t, err)
	assert.False(t, ok, "exec mode cannot see its command")

	noProvider := NewComposeJob()
	_, err = noProvider.WorkloadRunning(context.Background(), "exec-1")
	require.Error(t, err)
}
