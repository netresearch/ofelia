// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// TestInitPersistStore_Disabled pins the no-op path: empty StateFile
// must construct a disabled Store, skip Load, and return nil. Mirrors
// the documented contract for the pre-#593 default.
func TestInitPersistStore_Disabled(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	c.StateFile = ""
	require.NoError(t, c.initPersistStore())
	require.NotNil(t, c.persistStore)
	assert.False(t, c.persistStore.Enabled(),
		"empty StateFile must produce a disabled Store so the rest of boot can call mutators without nil-checks")
}

// TestInitPersistStore_AppliesPersistedLocalJob pins the end-to-end
// boot contract: a pre-written state file with a local job lands in
// the scheduler. This is the entire load-half of the feature; gap
// flagged by test-engineer review.
func TestInitPersistStore_AppliesPersistedLocalJob(t *testing.T) {
	t.Parallel()
	path := writePersistFile(t, persist.State{
		Version: persist.CurrentVersion,
		Jobs: map[string]*persist.Job{
			"persisted-local": {Type: persist.JobTypeLocal, Schedule: "@hourly", Command: "echo hi"},
		},
	})

	c := newDaemonForPersistTest(t)
	c.StateFile = path
	require.NoError(t, c.initPersistStore())

	got := c.scheduler.GetJob("persisted-local")
	require.NotNil(t, got, "initPersistStore must apply persisted jobs to the live scheduler")
	lj, ok := got.(*core.LocalJob)
	require.True(t, ok, "persisted JobTypeLocal must materialize as *core.LocalJob, got %T", got)
	assert.Equal(t, "@hourly", lj.Schedule)
	assert.Equal(t, "echo hi", lj.Command)
}

// TestInitPersistStore_AppliesDisabledFlagToINIJob pins the
// cross-origin disable semantics: a name in the persisted Disabled
// list must result in that scheduler job being disabled, even if it
// came from INI/labels and not from the API.
func TestInitPersistStore_AppliesDisabledFlagToINIJob(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	// Pre-seed an "INI" job directly on the scheduler.
	ini := &core.LocalJob{}
	ini.Name = "ini-paused"
	ini.Schedule = "@hourly"
	require.NoError(t, c.scheduler.AddJob(ini))

	c.StateFile = writePersistFile(t, persist.State{
		Version:  persist.CurrentVersion,
		Disabled: []string{"ini-paused"},
	})
	require.NoError(t, c.initPersistStore())

	disabled := c.scheduler.GetDisabledJobs()
	require.NotEmpty(t, disabled, "DisableJob must have run for the persisted name")
	found := false
	for _, j := range disabled {
		if j.GetName() == "ini-paused" {
			found = true
			break
		}
	}
	assert.True(t, found,
		"persisted Disabled list must apply regardless of origin so an INI-defined job paused via the UI stays paused")
}

// TestInitPersistStore_SkipsRunJob_WhenDockerProviderMissing pins
// the graceful-degradation path: a persisted job-run cannot
// materialize without a Docker provider, but boot must NOT fail.
// The job is skipped+logged; the rest of the persist apply continues.
func TestInitPersistStore_SkipsRunJob_WhenDockerProviderMissing(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	// dockerHandler stays nil → provider unavailable.
	c.StateFile = writePersistFile(t, persist.State{
		Version: persist.CurrentVersion,
		Jobs: map[string]*persist.Job{
			"needs-docker": {Type: persist.JobTypeRun, Schedule: "@hourly", Image: "alpine"},
			"works-anyway": {Type: persist.JobTypeLocal, Schedule: "@daily", Command: "true"},
		},
	})
	require.NoError(t, c.initPersistStore(),
		"missing docker provider must not abort boot — bad persisted entries are skipped+logged")

	assert.Nil(t, c.scheduler.GetJob("needs-docker"),
		"run job must be skipped when provider unavailable")
	assert.NotNil(t, c.scheduler.GetJob("works-anyway"),
		"local job must still apply — one bad entry doesn't poison the whole load")
}

// TestInitPersistStore_FailsBoot_OnMalformedState pins fail-closed
// boot: a corrupt state file aborts boot rather than silently
// dropping the persisted state. Operators see the decode error and
// know to recover from backup or hand-fix the file.
func TestInitPersistStore_FailsBoot_OnMalformedState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid"), 0o600))

	c := newDaemonForPersistTest(t)
	c.StateFile = path
	err := c.initPersistStore()
	require.Error(t, err, "malformed state file must abort boot, not silently empty the in-memory store")
	assert.Contains(t, err.Error(), "persist load")
}

// TestPersistedJobToScheduler_RejectsInvalidName pins the validation
// parity: a hand-edited state file with an invalid job name (control
// chars, empty, oversize) must surface a clear error so the loop in
// initPersistStore skips it rather than passing it to the scheduler.
func TestPersistedJobToScheduler_RejectsInvalidName(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	cases := []struct {
		name string
		err  string
	}{
		{"", "must not be empty"},
		{"with\x00null", "control character"},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.err, func(t *testing.T) {
			t.Parallel()
			_, err := c.persistedJobToScheduler(tc.name,
				&persist.Job{Type: persist.JobTypeLocal, Schedule: "@hourly"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.err)
		})
	}
}

// TestPersistedJobToScheduler_ValidatesComposeFields pins the
// defense-in-depth that the persist load runs the same
// config.CommandValidator as the API path. Bypassing this on load
// would let a hand-edited state file inject shell-meta into a
// ComposeJob.File / .Service / .Command that the API would have
// rejected. Uses a command-substitution pattern (rejected by the
// dangerousPatterns regex) because plain "../" is permitted —
// validator parity is the contract; the validator's specific
// ruleset is its own.
func TestPersistedJobToScheduler_ValidatesComposeFields(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)

	t.Run("file_with_shell_injection", func(t *testing.T) {
		t.Parallel()
		_, err := c.persistedJobToScheduler("bad-compose-file", &persist.Job{
			Type: persist.JobTypeCompose, Schedule: "@hourly",
			Service: "web", File: "compose.yaml;rm",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid compose file path",
			"validator parity: shell-meta in compose file must be rejected at load")
	})

	t.Run("service_with_shell_injection", func(t *testing.T) {
		t.Parallel()
		_, err := c.persistedJobToScheduler("bad-svc", &persist.Job{
			Type: persist.JobTypeCompose, Schedule: "@hourly",
			Service: "web;rm -rf /", File: "compose.yaml",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid service name")
	})

	t.Run("local_command_with_null_byte", func(t *testing.T) {
		t.Parallel()
		_, err := c.persistedJobToScheduler("bad-local", &persist.Job{
			Type: persist.JobTypeLocal, Schedule: "@hourly",
			Command: "echo \x00 injection",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid command arguments")
	})
}

// helpers ---------------------------------------------------------------

// newDaemonForPersistTest builds a minimal *DaemonCommand wired with
// a real scheduler and a no-op logger. Sufficient for the persist
// boot tests because they call initPersistStore directly rather than
// driving the full boot() workflow.
func newDaemonForPersistTest(t *testing.T) *DaemonCommand {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return &DaemonCommand{
		Logger:    logger,
		scheduler: core.NewScheduler(logger),
	}
}

// writePersistFile marshals the given State into JSON and writes it
// to a temp file. Returns the path. Saves callers from boilerplate
// MarshalIndent + WriteFile pairs in every test.
func writePersistFile(t *testing.T, state persist.State) string {
	t.Helper()
	if state.Version == 0 {
		state.Version = persist.CurrentVersion
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	return path
}
