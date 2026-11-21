// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"testing"

	"github.com/netresearch/ofelia/core"
)

// lockingConfig is a configuration with a read lock, like cli.Config. It
// counts how often the lock was taken and released.
type lockingConfig struct {
	RunJobs map[string]*struct {
		core.BareJob
		JobSource string
	}
	held           bool
	locks, unlocks int
}

func (c *lockingConfig) RLock()   { c.held = true; c.locks++ }
func (c *lockingConfig) RUnlock() { c.held = false; c.unlocks++ }

// The web server reads a configuration that has a read lock only while it
// holds that lock, and releases it again.
func TestConfigReadsHoldTheConfigLock(t *testing.T) {
	t.Parallel()
	cfg := &lockingConfig{}
	cfg.RunJobs = map[string]*struct {
		core.BareJob
		JobSource string
	}{"j": {JobSource: "label"}}

	if got := jobOrigin(cfg, "j"); got != "label" {
		t.Fatalf("jobOrigin = %q, want label", got)
	}
	if cfg.locks != 1 || cfg.unlocks != 1 || cfg.held {
		t.Fatalf("jobOrigin: locks=%d unlocks=%d held=%v, want one lock released", cfg.locks, cfg.unlocks, cfg.held)
	}

	if stripJobs(cfg) == nil {
		t.Fatal("stripJobs returned nil")
	}
	if cfg.locks != 2 || cfg.unlocks != 2 || cfg.held {
		t.Fatalf("stripJobs: locks=%d unlocks=%d held=%v, want one more lock released", cfg.locks, cfg.unlocks, cfg.held)
	}
}
