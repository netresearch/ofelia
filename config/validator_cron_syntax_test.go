// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The validators accept the cron syntax the scheduler accepts and the docs
// describe: last day, nearest weekday, nth weekday, hash and year.
func TestValidateCronExpression_AcceptsTheSchedulersSyntax(t *testing.T) {
	t.Parallel()
	for _, expr := range []string{"0 0 L * *", "0 12 15W * *", "0 12 * * FRI#3", "0 12 * * FRI#L", "H 3 * * *", "0 0 1 1 * 2030"} {
		v := NewValidator()
		v.ValidateCronExpression("schedule", expr)
		assert.False(t, v.HasErrors(), "%s: %v", expr, v.Errors())
		require.NoError(t, NewSanitizer().ValidateCronExpression(expr), expr)
	}
	v := NewValidator()
	v.ValidateCronExpression("schedule", "61 * * * *")
	assert.True(t, v.HasErrors(), "an invalid minute passes")
	require.Error(t, NewSanitizer().ValidateCronExpression("61 * * * *"))
}
