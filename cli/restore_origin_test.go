// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// A state-file job that cannot be restored must not hand its name to the API.
// Here an INI job and a broken API record share a name: the record is
// skipped, and the INI job must stay owned by the INI file.
func TestSetupWebServer_SkippedRecordDoesNotClaimAnINIJob(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	cfg, err := BuildFromString("[job-local \"dup\"]\nschedule = @hourly\ncommand = echo ini\n", c.Logger)
	require.NoError(t, err)
	c.config = cfg
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	cfg.sh = c.scheduler
	cfg.registerAllJobs()
	require.NotNil(t, c.scheduler.GetAnyJob("dup"), "the INI job is registered")

	// A run job with neither image nor container fails job assembly.
	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"dup": {Type: persist.JobTypeRun, Schedule: "@hourly", Command: "echo api"},
	}})
	require.NoError(t, c.initPersistStore())
	c.WebAddr = "127.0.0.1:0"
	c.shutdownManager = core.NewShutdownManager(c.Logger, time.Second)
	require.NoError(t, c.setupWebServer())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/delete", strings.NewReader(`{"name":"dup"}`))
	req.Header.Set("Content-Type", "application/json")
	c.webServer.HTTPServer().Handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "the INI job is not deletable through the API: %s", rec.Body.String())
	assert.NotNil(t, c.scheduler.GetAnyJob("dup"), "the INI job is still scheduled")
}
