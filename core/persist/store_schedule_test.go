// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package persist

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleSchedule() Schedule {
	at := time.Date(2026, 10, 25, 2, 0, 0, 0, time.UTC)
	return Schedule{
		Revision:  "rev-1",
		Activated: at.Add(-24 * time.Hour),
		Cursor:    at,
		Dispatch: &Dispatch{
			Occurrence: Occurrence{Scheduled: at, Trigger: "scheduled"},
			Execution:  "exec-1",
			State:      "started",
		},
		Pending:  &Occurrence{Scheduled: at.Add(time.Hour), Trigger: "scheduled"},
		Workload: "exec-1",
	}
}

func TestStore_Schedule_PersistsAcrossInstances(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.PutSchedule("shop.backup", sampleSchedule()))

	reloaded := NewStore(path)
	require.NoError(t, reloaded.Load())
	got, ok := reloaded.GetSchedule("shop.backup")
	require.True(t, ok)
	assert.Equal(t, sampleSchedule(), got)
}

// Every other mutator saves the snapshot; a snapshot without the schedule
// records would drop them on the next PutJob, SetDisabled or ClearDisabled.
func TestStore_Schedule_SurvivesOtherMutators(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.PutSchedule("shop.backup", sampleSchedule()))
	require.NoError(t, s.PutJob("api-job", Job{Type: JobTypeLocal, Schedule: "@daily", Command: "true"}))
	require.NoError(t, s.SetDisabled("api-job"))
	require.NoError(t, s.ClearDisabled("api-job"))
	require.NoError(t, s.RemoveJob("api-job"))

	reloaded := NewStore(path)
	require.NoError(t, reloaded.Load())
	got, ok := reloaded.GetSchedule("shop.backup")
	require.True(t, ok, "the schedule record was dropped by another mutator")
	assert.Equal(t, sampleSchedule(), got)
}

// A record handed out or passed in shares no pointer with the stored one.
func TestStore_Schedule_CopiesAreIsolated(t *testing.T) {
	t.Parallel()
	s := NewStore(filepath.Join(t.TempDir(), "state.json"))
	in := sampleSchedule()
	require.NoError(t, s.PutSchedule("job", in))
	in.Dispatch.State = "ended"
	in.Pending.Trigger = "manual"

	got, _ := s.GetSchedule("job")
	assert.Equal(t, "started", got.Dispatch.State, "PutSchedule kept the caller's Dispatch pointer")
	assert.Equal(t, "scheduled", got.Pending.Trigger, "PutSchedule kept the caller's Pending pointer")

	got.Dispatch.Outcome = "failed"
	again, _ := s.GetSchedule("job")
	assert.Empty(t, again.Dispatch.Outcome, "GetSchedule handed out the stored Dispatch pointer")

	snap := s.Snapshot()
	snap.Schedules["job"].Dispatch.Execution = "changed"
	again, _ = s.GetSchedule("job")
	assert.Equal(t, "exec-1", again.Dispatch.Execution, "Snapshot handed out the stored Dispatch pointer")
}

func TestStore_RemoveSchedules(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	for _, name := range []string{"c", "a", "b"} {
		require.NoError(t, s.PutSchedule(name, Schedule{Revision: name}))
	}
	assert.Equal(t, []string{"a", "b", "c"}, s.ScheduleNames())

	require.NoError(t, s.RemoveSchedules("a", "missing", "c"))
	assert.Equal(t, []string{"b"}, s.ScheduleNames())

	reloaded := NewStore(path)
	require.NoError(t, reloaded.Load())
	assert.Equal(t, []string{"b"}, reloaded.ScheduleNames())
}

// Removing names without records writes nothing.
func TestStore_RemoveSchedules_NothingToRemove(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(path)
	require.NoError(t, s.RemoveSchedules("missing"))
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "a no-op removal created the state file")
}

func TestStore_Schedule_NullValueRejected(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":1,"schedules":{"x":null}}`), 0o600))
	err := NewStore(path).Load()
	require.ErrorIs(t, err, ErrNullJobValue)
}

func TestStore_Schedule_DisabledStoreIsNoOp(t *testing.T) {
	t.Parallel()
	s := NewStore("")
	require.NoError(t, s.PutSchedule("job", sampleSchedule()))
	require.NoError(t, s.RemoveSchedules("job"))
	_, ok := s.GetSchedule("job")
	assert.False(t, ok)
}
