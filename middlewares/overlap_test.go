package middlewares

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
