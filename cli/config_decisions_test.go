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

// A # or ; inside an INI value is part of the value; only one after a space
// starts a comment.
func TestBuildFromString_InlineCommentNeedsASpace(t *testing.T) {
	t.Parallel()
	cfg, err := BuildFromString("[job-local \"report\"]\n"+
		"schedule = 0 12 * * FRI#3\n"+
		"command = echo a;echo b\n"+
		"[job-local \"noted\"]\n"+
		"schedule = @daily # once a day\n"+
		"command = true ; nothing\n", test.NewTestLogger())
	require.NoError(t, err)
	assert.Equal(t, "0 12 * * FRI#3", cfg.LocalJobs["report"].Schedule, "the third Friday became every Friday")
	assert.Equal(t, "echo a;echo b", cfg.LocalJobs["report"].Command)
	assert.Equal(t, "@daily", cfg.LocalJobs["noted"].Schedule)
	assert.Equal(t, "true", cfg.LocalJobs["noted"].Command)
}

// A job that can never run is refused at load and reported, and the other
// jobs run.
func TestRegisterAllJobs_RefusesJobsThatCannotRun(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	cfg, err := BuildFromString("[job-exec \"no-container\"]\nschedule = @daily\ncommand = id\n"+
		"[job-exec \"no-command\"]\nschedule = @daily\ncontainer = app\n"+
		"[job-local \"no-local-command\"]\nschedule = @daily\n"+
		"[job-compose \"etc-file\"]\nschedule = @daily\nfile = /etc/compose.yml\nservice = db\n"+
		"[job-compose \"bad-service\"]\nschedule = @daily\nservice = bad name\n"+
		"[job-local \"ok\"]\nschedule = @daily\ncommand = true\n", logger)
	require.NoError(t, err)
	cfg.sh = core.NewScheduler(logger)
	cfg.registerAllJobs()

	refused := cfg.sh.GetUnschedulableJobs()
	for _, name := range []string{"no-container", "no-command", "no-local-command", "etc-file", "bad-service"} {
		assert.Nil(t, cfg.sh.GetAnyJob(name), "%s was registered", name)
		assert.Contains(t, refused, name, "%s is not reported", name)
	}
	assert.NotNil(t, cfg.sh.GetAnyJob("ok"))
}
