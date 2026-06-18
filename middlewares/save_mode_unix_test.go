// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build unix

// These tests assert on-disk Unix permission bits and neutralize the process
// umask via syscall.Umask, which only exists on unix GOOS. Ofelia ships a
// Windows binary (see .goreleaser.yml), so keep them out of the Windows build.
package middlewares

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAppliesConfiguredModes(t *testing.T) {
	// Not parallel: neutralizes the process umask so mode assertions are
	// deterministic regardless of the CI environment's umask.
	defer syscall.Umask(syscall.Umask(0))
	ctx, job := setupSaveTestContext(t)

	dir := filepath.Join(t.TempDir(), "logs")

	ctx.Start()
	ctx.Stop(nil)

	job.Name = testNameFoo
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir, SaveMode: "0644", SaveFolderMode: "0755"})
	require.NoError(t, m.Run(ctx))

	di, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), di.Mode().Perm(), "folder uses save-folder-mode")

	fi, err := os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".stdout.log"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), fi.Mode().Perm(), "log file uses save-mode")
}

func TestSaveDefaultModesAre0600And0750(t *testing.T) {
	// Not parallel: neutralizes the process umask (see TestSaveAppliesConfiguredModes).
	defer syscall.Umask(syscall.Umask(0))
	ctx, job := setupSaveTestContext(t)

	dir := filepath.Join(t.TempDir(), "logs")

	ctx.Start()
	ctx.Stop(nil)

	job.Name = testNameFoo
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	require.NoError(t, m.Run(ctx))

	di, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), di.Mode().Perm())

	fi, err := os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".stdout.log"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}
