// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
)

// An INI reload that changes [global] and a label reload run on their own
// goroutines; the label reload reads the globals under the config lock.
// go test -race reports the unlocked read.
func TestReload_INIAndLabelReloadDoNotRace(t *testing.T) {
	t.Parallel()
	cfg, reload := iniReloadConfig(t, "[global]\nnotification-cooldown = 1m\n")
	cfg.WebhookConfigs = NewWebhookConfigs()
	containers := []DockerContainerInfo{{
		Name:  "shop",
		State: domain.ContainerState{Running: true},
		Labels: map[string]string{
			"ofelia.enabled":                  "true",
			"ofelia.job-exec.report.schedule": "@daily",
			"ofelia.job-exec.report.command":  "id",
		},
	}}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 20 {
			cooldown := "1m"
			if i%2 == 0 {
				cooldown = "2m"
			}
			reload("[global]\nnotification-cooldown = " + cooldown + "\n")
		}
	}()
	go func() {
		defer wg.Done()
		for range 20 {
			cfg.dockerContainersUpdate(containers)
		}
	}()
	wg.Wait()
}

// The scheduler's middlewares are replaced by a reload while a web update
// reads them; go test -race reports an unlocked access.
func TestScheduler_MiddlewaresResetDuringUpdateDoNotRace(t *testing.T) {
	t.Parallel()
	cfg, _ := iniReloadConfig(t, "[job-local \"shop.report\"]\nschedule = @daily\ncommand = true\n")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 50 {
			cfg.sh.ResetMiddlewares()
		}
	}()
	go func() {
		defer wg.Done()
		for range 50 {
			_ = cfg.sh.UpdateJob("shop.report", "@daily", &core.LocalJob{BareJob: core.BareJob{Name: "shop.report", Schedule: "@daily", Command: "true"}})
		}
	}()
	wg.Wait()
	require.NotNil(t, cfg.sh.GetAnyJob("shop.report"))
}
