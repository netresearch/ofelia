// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// A job section that fails its own validation is left unregistered; the other
// jobs and the globals of the file load, as they do for labels and the web
// API. Before, the file failed to load and the daemon ran without it.
func TestBuildFromString_InvalidJobLeavesTheRestOfTheFile(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	cfg, err := BuildFromString("[global]\nnotification-cooldown = 2m\n"+
		"[job-run \"broken\"]\nschedule = @daily\ncommand = echo\n"+
		"[job-local \"report\"]\nschedule = @hourly\ncommand = echo ok\n", logger)
	require.NoError(t, err)
	assert.Equal(t, "2m0s", cfg.Global.NotificationCooldown.String(), "the globals were dropped")

	cfg.sh = core.NewScheduler(logger)
	cfg.registerAllJobs()
	assert.NotNil(t, cfg.sh.GetAnyJob("report"), "the valid job was dropped")
	assert.Nil(t, cfg.sh.GetAnyJob("broken"), "the invalid job was registered")
}
