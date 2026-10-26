// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

func TestComposeProject_RejectedReloadRunsAcceptedDefinition(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake docker CLI is a shell script")
	}
	dir := t.TempDir()
	// The fake CLI reads the configuration the adapter actually supplies.
	// Both the original file path and the immutable stdin path are supported.
	script := "#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do\n" +
		"  if [ \"$1\" = '-f' ]; then shift; if [ \"$1\" = '-' ]; then cat; else cat \"$1\"; fi; exit; fi\n" +
		"  shift\ndone\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700)) //nolint:gosec // test executable
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	accepted := strings.Replace(shopCompose, "  backup:\n", "  backup:\n    command: [echo, accepted-command]\n", 1)
	composePath := writeFile(t, dir, "compose.yaml", accepted)
	c := prepareReload(t, writeFile(t, dir, "ofelia.ini", "[compose-project \"shop\"]\nfile = "+composePath+"\n"))
	old := c.ComposeJobs["shop.backup"]
	rejected := strings.Replace(accepted, "accepted-command", "rejected-command", 1)
	rejected = strings.Replace(rejected, "      schedule:\n", "      schedule:\n        - cron: \"1 1 * * *\"\n", 1)
	touchFile(t, composePath, rejected, time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	require.Same(t, old, c.ComposeJobs["shop.backup"])

	execution, err := core.NewExecution()
	require.NoError(t, err)
	ctx := core.NewContextWithContext(context.Background(), core.NewScheduler(test.NewTestLogger()), old, execution)
	require.NoError(t, old.Run(ctx))
	assert.Contains(t, execution.OutputStream.String(), "accepted-command")
	assert.NotContains(t, execution.OutputStream.String(), "rejected-command")
}
