// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/persist"
)

// failedWrites passes every write to the real store and counts the writes
// that failed, after each has returned.
type failedWrites struct {
	*persist.Store
	mu     sync.Mutex
	failed int
}

func (f *failedWrites) PutSchedule(name string, r persist.Schedule) error {
	err := f.Store.PutSchedule(name, r)
	if err != nil {
		f.mu.Lock()
		f.failed++
		f.mu.Unlock()
	}
	return err
}

func (f *failedWrites) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failed
}

// breakStateFile makes every write of the real store fail at its last step
// until the returned function is called: a directory takes the place of the
// state file, so the rename over it fails.
func breakStateFile(t *testing.T, h *durableHarness) (restore func()) {
	t.Helper()
	path := h.store.Path()
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Mkdir(path, 0o700))
	return func() { require.NoError(t, os.Remove(path)) }
}

// A fire whose waiting dispatch cannot be written still waits for its slot,
// and it runs when the write of its start succeeds.
func TestScheduleWriteFailure_UnrecordedWaitStillWaits(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.SetMaxConcurrentJobs(1)
	j := newDurableJob("shop.backup", true, false)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	writes := &failedWrites{Store: h.store}
	h.s.SetScheduleStore(writes)

	slot := h.s.concurrencySem.getChan()
	slot <- struct{}{}
	restore := breakStateFile(t, h)
	h.fire("shop.backup", at(23, 2))
	require.Eventually(t, func() bool { return writes.count() == 1 }, 5*time.Second, 5*time.Millisecond,
		"the waiting dispatch was not written")
	restore()
	<-slot

	run := nextRun(t, j)
	assert.True(t, run.info.ScheduledTime.Equal(at(23, 2)), "ran %v", run.info.ScheduledTime)
}

// A queued occurrence whose start cannot be written is lost, as ADR-005's at
// most once requires, but it no longer keeps the entry busy: the next fire
// runs once the state file is writable again.
func TestScheduleWriteFailure_UnrecordedQueuedStartDoesNotBlock(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.orphanPollInterval = 10 * time.Millisecond
	j := newDurableJob("shop.backup", false, true)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	writes := &failedWrites{Store: h.store}
	h.s.SetScheduleStore(writes)
	release := j.hold()

	h.fire("shop.backup", at(23, 2))
	nextRun(t, j)
	h.fire("shop.backup", at(23, 3))
	h.eventually("shop.backup", func(r persist.Schedule) bool { return r.Pending != nil }, "the second fire was not queued")
	restore := breakStateFile(t, h)
	release()
	// The end of the run, the queued occurrence's move into the dispatch and
	// its start each fail to be written.
	require.Eventually(t, func() bool { return writes.count() == 3 }, 5*time.Second, 5*time.Millisecond,
		"the queued occurrence was not attempted")
	noRun(t, j)
	restore()
	require.Eventually(t, func() bool { return !h.s.durableBusy(j.Name) }, 5*time.Second, 5*time.Millisecond,
		"the entry stays busy after the state file is writable again")

	h.fire("shop.backup", at(23, 4))
	run := nextRun(t, j)
	assert.True(t, run.info.ScheduledTime.Equal(at(23, 4)), "ran %v", run.info.ScheduledTime)
}
