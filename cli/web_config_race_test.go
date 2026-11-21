// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
	"github.com/netresearch/ofelia/web"
)

// GET /api/jobs, /api/config and /api/dashboard read the configuration's
// job maps and webhooks, while a label reload rewrites them on the Docker
// handler's goroutine. Run with -race.
func TestWebJobList_ConcurrentWithLabelReload(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()
	c := NewConfig(logger)
	c.sh = core.NewScheduler(logger)
	c.dockerHandler = &DockerHandler{ctx: context.Background(), logger: logger}
	srv := web.NewServer("", c.sh, c, nil)
	handler := srv.HTTPServer().Handler

	labels := func(i int) []DockerContainerInfo {
		return []DockerContainerInfo{{
			Name:  "app",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled": "true",
				"ofelia.service": "true",
				fmt.Sprintf("ofelia.job-local.l%d.schedule", i%3): "@daily",
				fmt.Sprintf("ofelia.job-local.l%d.command", i%3):  "echo hi",
				// Webhooks come and go too: /api/config serializes them.
				fmt.Sprintf("ofelia.webhook.w%d.preset", i%3): "slack",
				fmt.Sprintf("ofelia.webhook.w%d.id", i%3):     "T00/B00",
				fmt.Sprintf("ofelia.webhook.w%d.url", i%3):    "https://hooks.slack.com/services/T00/B00/x",
			},
		}}
	}
	c.Global.AllowHostJobsFromLabels = true

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 100 {
			c.dockerContainersUpdate(labels(i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 150 {
			path := []string{"/api/jobs", "/api/config", "/api/dashboard"}[i%3]
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		}
	}()
	wg.Wait()
}
