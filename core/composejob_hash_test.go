// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComposeJobHash_StableAcrossDaylightSaving: the description of the local
// zone changes at every daylight-saving switch; the job hash must not, or the
// next reload replaces every job without an explicit timezone.
func TestComposeJobHash_StableAcrossDaylightSaving(t *testing.T) {
	t.Parallel()
	job := func(zone string) *ComposeJob {
		j := NewComposeJob()
		j.Name = "shop.cleanup"
		j.Schedule = "30 3 * * *"
		j.Service = "cleanup"
		j.Project = ComposeProjectContext{
			Files: []string{"compose.yaml"}, ProjectName: "shop", Revision: "abc", TimeZone: zone,
		}
		return j
	}
	summer, err := job("local (CEST, UTC+02:00)").Hash()
	require.NoError(t, err)
	winter, err := job("local (CET, UTC+01:00)").Hash()
	require.NoError(t, err)

	assert.Equal(t, summer, winter)
}

// TestComposeJobHash_ChangesWithTheDefinition keeps the hash sensitive to what
// does select the run: the job revision and the project context.
func TestComposeJobHash_ChangesWithTheDefinition(t *testing.T) {
	t.Parallel()
	base := NewComposeJob()
	base.Project = ComposeProjectContext{Files: []string{"compose.yaml"}, Revision: "abc"}
	changed := NewComposeJob()
	changed.Project = ComposeProjectContext{Files: []string{"compose.yaml"}, Revision: "def"}

	a, err := base.Hash()
	require.NoError(t, err)
	b, err := changed.Hash()
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
}
