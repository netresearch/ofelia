// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/middlewares"
)

// The web server builds jobs on its request goroutines while a reload
// rewrites the globals assembly reads (notification settings, the dedup
// state) under c.mu. Run with -race: without BuildAPIJob's read lock the
// detector reports the two goroutines.
func TestBuildAPIJob_ConcurrentWithReload(t *testing.T) {
	t.Parallel()
	f := newAPIJobFixture(t, &mockDockerProviderForHandler{})
	cfg := f.cfg

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 200 {
			cfg.mu.Lock()
			cfg.Global.SaveFolder = fmt.Sprintf("/var/log/ofelia-%d", i)
			cfg.Global.NotificationCooldown = time.Duration(i%2) * time.Minute
			// What initNotificationDedup does to the configuration, without its
			// write to the package-wide middlewares.DefaultNotificationDedup,
			// which parallel tests share.
			cfg.notificationDedup = middlewares.NewNotificationDedup(cfg.Global.NotificationCooldown)
			cfg.mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 200 {
			_, err := cfg.BuildAPIJob(fmt.Sprintf("job-%d", i), persist.Job{Type: persist.JobTypeLocal, Command: "echo"})
			assert.NoError(t, err)
		}
	}()
	wg.Wait()
}

// An omitted type is a local job, as docs/openapi.yaml states.
func TestBuildAPIJob_OmittedTypeIsLocal(t *testing.T) {
	t.Parallel()
	job, err := buildAPIJob(t, persist.Job{Command: "echo hi"})
	require.NoError(t, err)
	_, ok := job.(*core.LocalJob)
	assert.True(t, ok, "got %T", job)
}
