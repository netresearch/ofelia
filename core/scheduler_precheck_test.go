// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

// Precheck refuses what AddJob refuses, records the refusal for the name and
// leaves a registered job of that name running.
func TestPrecheck(t *testing.T) {
	t.Parallel()
	s := NewScheduler(test.NewTestLogger())
	running := &BareJob{Name: "shop.report", Schedule: "@daily", Command: "true"}
	require.NoError(t, s.AddJob(running))

	require.NoError(t, s.Precheck("shop.report", "@hourly"))
	assert.Empty(t, s.GetUnschedulableJobs())

	require.ErrorIs(t, s.Precheck("shop.report", ""), ErrEmptySchedule)
	require.Error(t, s.Precheck("shop.report", "not-a-schedule"))
	assert.Contains(t, s.GetUnschedulableJobs(), "shop.report")
	assert.Same(t, running, s.GetAnyJob("shop.report"))
	assert.True(t, s.cron.EntryByName("shop.report").Valid())
}
