// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

func TestComposeProject_DuplicateCanonicalIdentity(t *testing.T) {
	t.Parallel()
	for _, sameFile := range []bool{false, true} {
		t.Run(fmt.Sprintf("same_file_%t", sameFile), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			a := writeFile(t, dir, "a.yaml", shopCompose)
			b := a
			if !sameFile {
				b = writeFile(t, dir, "b.yaml", shopCompose+"  exclusive:\n    image: alpine:3.22\n"+
					"    triggers:\n      schedule:\n        - cron: \"0 4 * * *\"\n          missed_fires: skip\n")
			}
			unrelated := writeFile(t, dir, "other.yaml", shopProdCompose)
			logger, handler := test.NewTestLoggerWithHandler()
			ini := fmt.Sprintf("[compose-project \"a\"]\nfile = %s\n[compose-project \"b\"]\nfile = %s\n"+
				"project-name = shop\n[compose-project \"other\"]\nfile = %s\n", a, b, unrelated)

			c, err := BuildFromString(ini, logger)

			require.NoError(t, err)
			assert.NotContains(t, c.ComposeJobs, "a.backup")
			assert.NotContains(t, c.ComposeJobs, "a.cleanup", "the whole colliding registration is rejected")
			assert.NotContains(t, c.ComposeJobs, "b.backup")
			assert.NotContains(t, c.ComposeJobs, "b.cleanup")
			assert.NotContains(t, c.ComposeJobs, "b.exclusive", "non-conflicting jobs in the same registration are rejected too")
			assert.Contains(t, c.ComposeJobs, "other.nightly", "unrelated projects keep their jobs")
			assert.Equal(t, []string{"a", "b"}, c.rejectedComposeProjects())
			assert.True(t, handler.HasError("duplicate compose job identity"))
			assert.False(t, c.composeProjectsChanged(), "the same invalid registration must not trigger every poll")
		})
	}
}

func TestComposeProject_CanonicalIdentityAllowsDistinctProjectsAndJobs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		second    string
		selection string
		job       string
	}{
		{name: "distinct project", second: shopCompose, selection: "project-name = other\n", job: "backup"},
		{name: "distinct job", second: strings.ReplaceAll(shopCompose, "backup:", "archive:"), job: "archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			// Leave cleanup inactive so the same project has distinct jobs.
			first := strings.Replace(shopCompose, "  cleanup:", "  cleanup:\n    profiles: [unused]", 1)
			second := strings.Replace(tc.second, "  cleanup:", "  cleanup:\n    profiles: [unused]", 1)
			a := writeFile(t, dir, "a.yaml", first)
			b := writeFile(t, dir, "b.yaml", second)
			ini := fmt.Sprintf("[compose-project \"a\"]\nfile = %s\n[compose-project \"b\"]\nfile = %s\n%s",
				a, b, tc.selection)

			c, err := BuildFromString(ini, test.NewTestLogger())

			require.NoError(t, err)
			assert.Contains(t, c.ComposeJobs, "a.backup")
			assert.Contains(t, c.ComposeJobs, "b."+tc.job)
			assert.Empty(t, c.rejectedComposeProjects())
		})
	}
}

func TestComposeProject_ReloadChecksRetainedCanonicalIdentity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := writeFile(t, dir, "a.yaml", shopCompose)
	b := writeFile(t, dir, "b.yaml", strings.Replace(shopCompose, "name: shop", "name: other", 1))
	ini := fmt.Sprintf("[compose-project \"a\"]\nfile = %s\n[compose-project \"b\"]\nfile = %s\n", a, b)
	iniPath := writeFile(t, dir, "ofelia.ini", ini)
	c := prepareReload(t, iniPath)
	require.Len(t, c.ComposeJobs, 4)

	// A cannot load, so its old shop jobs are retained. B now also targets
	// shop: its otherwise valid new model must not create a second scheduler.
	touchFile(t, a, brokenShopCompose, time.Minute)
	touchFile(t, b, shopCompose, time.Minute)
	require.NoError(t, c.iniConfigUpdate())

	assert.Empty(t, c.ComposeJobs, "colliding retained and newly loaded projects must both be rejected")
	assert.Empty(t, c.sh.GetActiveJobs(), "the rejected jobs must also leave the scheduler")
	assert.Equal(t, []string{"a", "b"}, c.rejectedComposeProjects())
	assert.False(t, c.composeProjectsChanged(), "a duplicate rejection must not cause a reload loop")

	// A later valid, distinct selection recovers both registrations.
	touchFile(t, a, shopCompose, 2*time.Minute)
	touchFile(t, b, strings.Replace(shopCompose, "name: shop", "name: other", 1), 2*time.Minute)
	require.NoError(t, c.iniConfigUpdate())
	assert.Len(t, c.ComposeJobs, 4)
	assert.Empty(t, c.rejectedComposeProjects())
}

func TestComposeProject_DuplicateReloadKeepsDisjointLastLoadedJobs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := writeFile(t, dir, "a.yaml", shopCompose)
	b := writeFile(t, dir, "b.yaml", strings.Replace(shopCompose, "name: shop", "name: other", 1))
	iniPath := writeFile(t, dir, "ofelia.ini", fmt.Sprintf(
		"[compose-project \"a\"]\nfile = %s\n[compose-project \"b\"]\nfile = %s\n", a, b))
	c := prepareReload(t, iniPath)
	oldA, oldB := c.ComposeJobs["a.backup"], c.ComposeJobs["b.backup"]

	touchFile(t, b, shopCompose, time.Minute)
	require.NoError(t, c.iniConfigUpdate())

	assert.Same(t, oldA, c.ComposeJobs["a.backup"])
	assert.Same(t, oldB, c.ComposeJobs["b.backup"], "old disjoint project identities remain safe to keep")
	assert.Equal(t, []string{"a", "b"}, c.rejectedComposeProjects())
}
