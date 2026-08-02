// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The daemon ends the process when it sees shutdown finish. It used to watch
// ShutdownChan, which is closed *before* the first hook runs, so everything
// after the first priority group was killed mid-flight — the web server was
// never stopped gracefully even though a hook was registered to do it. Done is
// the signal that actually means finished, and these pin that meaning.

func TestShutdownDoneClosesOnlyAfterEveryHookRan(t *testing.T) {
	t.Parallel()

	sm := NewShutdownManager(newDiscardLogger(), 5*time.Second)

	var mu sync.Mutex
	var ran []string
	for _, h := range []struct {
		name     string
		priority int
	}{{"first", 10}, {"second", 20}} {
		sm.RegisterHook(ShutdownHook{
			Name:     h.name,
			Priority: h.priority,
			Hook: func(context.Context) error {
				mu.Lock()
				defer mu.Unlock()
				ran = append(ran, h.name)
				return nil
			},
		})
	}

	select {
	case <-sm.Done():
		t.Fatal("Done was closed before shutdown had even started")
	default:
	}

	require.NoError(t, sm.Shutdown())

	select {
	case <-sm.Done():
	default:
		t.Fatal("Done was not closed once Shutdown returned")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"first", "second"}, ran,
		"every priority group has to run, in order, before Done closes")
}

// TestShutdownDoneClosesWhenAHookTimesOut pins the other half: a hook that
// overruns the deadline must not leave whoever waits on Done blocked forever.
func TestShutdownDoneClosesWhenAHookTimesOut(t *testing.T) {
	t.Parallel()

	sm := NewShutdownManager(newDiscardLogger(), 50*time.Millisecond)
	sm.RegisterHook(ShutdownHook{
		Name:     "overruns",
		Priority: 10,
		Hook: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})

	require.Error(t, sm.Shutdown())

	select {
	case <-sm.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done stayed open after a hook overran the shutdown timeout")
	}
}
