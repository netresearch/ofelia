// Copyright (c) 2026 Netresearch DTT GmbH
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

// A create the scheduler refuses is a rejected request, not a configured job
// that is not scheduled: the health check stays operational.
func TestCreateJob_RefusedCreateLeavesNoUnschedulableJob(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"name taken":   `{"name":"api-owned","type":"local","schedule":"@hourly","command":"echo twice"}`,
		"bad schedule": `{"name":"api-other","type":"local","schedule":"not-a-schedule","command":"echo hi"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sched := core.NewScheduler(stubDiscardLogger())
			srv := withJobBuilder(webpkg.NewServer("", sched, nil, nil))
			resp := postJSON(t, srv, "/api/jobs/create",
				`{"name":"api-owned","type":"local","schedule":"@daily","command":"echo hi"}`)
			require.Equal(t, http.StatusCreated, resp.Code)

			resp = postJSON(t, srv, "/api/jobs/create", body)
			require.Equal(t, http.StatusBadRequest, resp.Code)

			assert.Empty(t, sched.GetUnschedulableJobs())
			assert.NotNil(t, sched.GetAnyJob("api-owned"), "the existing job stays")
		})
	}
}
