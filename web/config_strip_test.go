// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
)

// TestConfigEndpoint_StripsUnknownJobCollection pins that /api/config strips
// job collections by shape rather than by a hand-written field-name list.
// The list carried five names, so adding a sixth collection to cli.Config
// shipped every one of its jobs — commands and any credential-bearing fields —
// inside the config payload of /api/config and /api/dashboard. The name list
// had already drifted once, which is what makes the structural rule load-bearing
// rather than cosmetic.
func TestConfigEndpoint_StripsUnknownJobCollection(t *testing.T) {
	t.Parallel()

	type stubJob struct {
		Command  string
		Password string
	}
	cfg := &struct {
		Global            struct{ LogLevel string }
		RunJobs           map[string]*stubJob
		CronJobs          map[string]*stubJob
		WebTrustedProxies []string
	}{
		RunJobs:           map[string]*stubJob{"known": {Command: "echo known", Password: "run-secret"}},
		CronJobs:          map[string]*stubJob{"novel": {Command: "echo novel", Password: "cron-secret"}},
		WebTrustedProxies: []string{"10.0.0.1"},
	}
	cfg.Global.LogLevel = "info"

	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	resp := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
	body := resp.Body.String()

	assert.NotContains(t, body, "run-secret",
		"a known job collection must not reach the wire")
	assert.NotContains(t, body, "cron-secret",
		"a job collection absent from the old name list must be stripped too")
	assert.NotContains(t, body, "novel",
		"job names of an unlisted collection must not leak either")

	// Non-job configuration must survive: stripping by shape must not turn
	// /api/config into an empty document.
	assert.Contains(t, body, "info", "ordinary config must stay visible")
	assert.Contains(t, body, "10.0.0.1", "slice config must stay visible")
}
