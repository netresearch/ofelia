// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
)

// TestExecJob_ConsoleSize_NilWhenUnset pins the default behavior: when
// neither ConsoleHeight nor ConsoleWidth is configured, the *[2]uint
// passed to the Docker daemon is nil — meaning "use Docker's default
// console size". Pre-#235 ExecJob had no way to express anything else;
// the new fields preserve that default when both are zero.
func TestExecJob_ConsoleSize_NilWhenUnset(t *testing.T) {
	t.Parallel()

	j := &ExecJob{}
	assert.Nil(t, j.consoleSize(),
		"both dimensions zero must yield nil so Docker uses its default size (matches pre-#235 behavior)")
}

// TestExecJob_ConsoleSize_HeightWidthOrder pins the [height, width]
// argument order Docker's API expects. Swapping the order would produce
// silently-wrong-shaped TUIs, so the test is explicit about which
// dimension goes where.
func TestExecJob_ConsoleSize_HeightWidthOrder(t *testing.T) {
	t.Parallel()

	j := &ExecJob{ConsoleHeight: 24, ConsoleWidth: 80}
	got := j.consoleSize()
	require.NotNil(t, got)
	assert.Equal(t, uint(24), got[0],
		"Docker's ContainerExecCreate.ConsoleSize is [height, width]; index 0 must be height (rows)")
	assert.Equal(t, uint(80), got[1],
		"index 1 must be width (columns); see Docker API v1.42 spec")
}

// TestExecJob_ConsoleSize_PartialPopulates confirms that setting only
// one dimension still produces a populated *[2]uint (with the other
// dimension at 0). The daemon honors whichever dimension is non-zero
// — partial config is operator-meaningful, not an "unset" trigger.
func TestExecJob_ConsoleSize_PartialPopulates(t *testing.T) {
	t.Parallel()

	heightOnly := &ExecJob{ConsoleHeight: 40}
	require.NotNil(t, heightOnly.consoleSize(),
		"setting only height must still produce a non-nil ConsoleSize so Docker receives the value")
	assert.Equal(t, uint(40), heightOnly.consoleSize()[0])
	assert.Equal(t, uint(0), heightOnly.consoleSize()[1])

	widthOnly := &ExecJob{ConsoleWidth: 120}
	require.NotNil(t, widthOnly.consoleSize(),
		"setting only width must still produce a non-nil ConsoleSize")
	assert.Equal(t, uint(0), widthOnly.consoleSize()[0])
	assert.Equal(t, uint(120), widthOnly.consoleSize()[1])
}

// TestExecJob_RunWithStreams_PropagatesConsoleSize is the end-to-end
// integration test for #235: when an operator configures ConsoleHeight
// / ConsoleWidth on an ExecJob, the value reaches the Docker provider's
// RunExec call (and from there, ContainerExecCreate.ConsoleSize on the
// SDK). RunWithStreams is the path the unit tests exercise; the
// ConsoleSize handoff is identical in Run().
func TestExecJob_RunWithStreams_PropagatesConsoleSize(t *testing.T) {
	t.Parallel()

	k := newTestExecJobKit(t)
	k.job.TTY = true
	k.job.ConsoleHeight = 30
	k.job.ConsoleWidth = 100

	var got *[2]uint
	k.exec.OnRun = func(_ context.Context, _ string, config *domain.ExecConfig, _, _ io.Writer) (int, error) {
		got = config.ConsoleSize
		return 0, nil
	}

	_, err := k.job.RunWithStreams(context.Background(), io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, got,
		"ConsoleSize must propagate from ExecJob.ConsoleHeight/Width to domain.ExecConfig (#235)")
	assert.Equal(t, [2]uint{30, 100}, *got,
		"the [height, width] order Docker expects must survive the ExecJob → domain.ExecConfig handoff")
}

// TestExecJob_RunWithStreams_DefaultsToNilConsoleSize confirms backward
// compatibility: an ExecJob with no ConsoleHeight/Width configured
// produces a nil ConsoleSize on the Docker call — preserving the
// pre-#235 "Docker default" behavior. Without this pin, a future
// refactor that sets ConsoleSize unconditionally (e.g. &[2]uint{0,0})
// could subtly change how Docker sizes the pseudo-TTY.
func TestExecJob_RunWithStreams_DefaultsToNilConsoleSize(t *testing.T) {
	t.Parallel()

	k := newTestExecJobKit(t)
	k.job.TTY = true
	// ConsoleHeight and ConsoleWidth left at zero defaults.

	var got *[2]uint
	k.exec.OnRun = func(_ context.Context, _ string, config *domain.ExecConfig, _, _ io.Writer) (int, error) {
		got = config.ConsoleSize
		return 0, nil
	}

	_, err := k.job.RunWithStreams(context.Background(), io.Discard, io.Discard)
	require.NoError(t, err)
	assert.Nil(t, got,
		"unconfigured ConsoleHeight/Width must yield a nil ConsoleSize so Docker uses its default — the pre-#235 behavior")
}
