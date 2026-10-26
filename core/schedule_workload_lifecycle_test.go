// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A run can report an unconfirmed cleanup after Stop has already closed the
// watcher channel. Starting the same Scheduler again must resume monitoring.
func TestScheduleState_UnconfirmedShutdownResumesWatcherOnRestart(t *testing.T) {
	t.Parallel()
	h := newDurableHarness(t, at(23, 1))
	h.s.orphanPollInterval = 5 * time.Millisecond
	j := &unconfirmedDurableJob{durableJob: newDurableJob("shop.backup", true, true)}
	var gone atomic.Bool
	j.setRunning(func(string) (bool, error) { return !gone.Load(), nil })
	release := j.hold()
	t.Cleanup(release)
	require.NoError(t, h.s.AddJob(j))
	h.start()
	h.fire(j.Name, at(23, 2))
	nextRun(t, j.durableJob)

	require.NoError(t, h.s.StopWithTimeout(time.Second))
	h.start()
	require.ErrorIs(t, h.s.RunJob(context.Background(), j.Name), ErrJobBusy)
	gone.Store(true)
	require.Eventually(t, func() bool { return !h.s.durableBusy(j.Name) }, time.Second, 5*time.Millisecond,
		"the restarted scheduler must release the job after its workload ends")
}
