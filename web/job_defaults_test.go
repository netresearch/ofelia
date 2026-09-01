// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
)

// historyLimitOf reads the retention limit off the concrete job types the
// API can build. BareJob exposes the field, not an accessor.
func historyLimitOf(t *testing.T, j core.Job) int {
	t.Helper()
	switch j := j.(type) {
	case *core.LocalJob:
		return j.HistoryLimit
	case *core.ComposeJob:
		return j.HistoryLimit
	case *core.ExecJob:
		return j.HistoryLimit
	case *core.RunJob:
		return j.HistoryLimit
	default:
		t.Fatalf("unexpected job type %T", j)
		return 0
	}
}

// TestCreatedJobsCarryStructTagDefaults pins that a job built from an API
// request gets the same struct-tag defaults the config decoder applies.
//
// Only the run-job constructor called defaults.Set, so exec, compose and
// local jobs came out with HistoryLimit 0 and BareJob.GetHistory never
// trimmed: every Execution stayed pinned, each holding up to two 10 MB
// output buffers, and the 5s /api/dashboard poll copied the whole slice
// each tick. A frequently-run API-created job exhausted daemon memory.
func TestCreatedJobsCarryStructTagDefaults(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body string }{
		{"local", `{"name":"d-local","type":"local","schedule":"@daily","command":"echo hi"}`},
		{"compose", `{"name":"d-compose","type":"compose","schedule":"@daily","service":"web","command":"echo hi"}`},
		{"empty-type", `{"name":"d-default","type":"","schedule":"@daily","command":"echo hi"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sched := core.NewScheduler(stubDiscardLogger())
			srv := webpkg.NewServer("", sched, nil, nil)

			resp := postJSON(t, srv, "/api/jobs/create", tc.body)
			require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())

			jobs := sched.GetActiveJobs()
			require.Len(t, jobs, 1)
			assert.Positive(t, historyLimitOf(t, jobs[0]),
				"HistoryLimit 0 keeps every Execution forever")
		})
	}
}
