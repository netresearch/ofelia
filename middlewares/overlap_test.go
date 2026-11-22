// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

func TestNewOverlapEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, NewOverlap(&OverlapConfig{}))
}

func TestOverlapRun(t *testing.T) {
	t.Parallel()
	ctx, _ := setupTestContext(t)

	m := &Overlap{}
	require.NoError(t, m.Run(ctx))
}

func TestOverlap_SkipsOverlapReportsNoOverlap(t *testing.T) {
	t.Parallel()
	var on, off core.OverlapGuard = &Overlap{OverlapConfig{NoOverlap: true}}, &Overlap{}
	assert.True(t, on.SkipsOverlap())
	assert.False(t, off.SkipsOverlap())
}

func TestOverlap_ContinueOnStop(t *testing.T) {
	t.Parallel()
	m := &Overlap{}
	assert.False(t, m.ContinueOnStop(), "Overlap.ContinueOnStop() should return false")
}

func TestOverlapRunOverlap(t *testing.T) {
	t.Parallel()
	ctx, _ := setupTestContext(t)

	ctx.Execution.Start()
	ctx.Job.NotifyStart()
	ctx.Job.NotifyStart()

	m := NewOverlap(&OverlapConfig{NoOverlap: true})
	require.NoError(t, m.Run(ctx))
	assert.False(t, ctx.Execution.IsRunning)
	assert.True(t, ctx.Execution.Skipped)
}
