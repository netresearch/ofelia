// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfgvalidator "github.com/netresearch/ofelia/config"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// A compose job restored from the state file gets max-runtime the way the API
// applied it at creation: its stored value, else the current global.
func TestPersistedComposeJob_MaxRuntime(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	cfg.Global.MaxRuntime = 2 * time.Hour
	c := &DaemonCommand{config: cfg}

	build := func(t *testing.T, j *persist.Job) (*core.ComposeJob, error) {
		t.Helper()
		j.Type, j.Service = persist.JobTypeCompose, "task"
		job, err := c.buildPersistedComposeJob("restored", j, cfgvalidator.NewCommandValidator(), nil)
		if err != nil {
			return nil, err
		}
		cj, ok := job.(*core.ComposeJob)
		require.True(t, ok, "got %T", job)
		return cj, nil
	}

	cj, err := build(t, &persist.Job{})
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, cj.MaxRuntime, "no stored value inherits the global")

	cj, err = build(t, &persist.Job{MaxRuntime: "45m"})
	require.NoError(t, err)
	assert.Equal(t, 45*time.Minute, cj.MaxRuntime)

	cj, err = build(t, &persist.Job{Exec: true})
	require.NoError(t, err)
	assert.Zero(t, cj.MaxRuntime, "exec mode inherits nothing")

	_, err = build(t, &persist.Job{Exec: true, MaxRuntime: "5m"})
	require.ErrorIs(t, err, core.ErrComposeExecMaxRuntime)
}
