// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

// The daemon's web server builds API jobs through the configuration's job
// assembly. Without the SetJobBuilder call in setupWebServer every create
// would be refused, while every test that wires the builder itself passed.
func TestSetupWebServer_CreatesJobsThroughJobAssembly(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	c.WebAddr = "127.0.0.1:0"
	c.shutdownManager = core.NewShutdownManager(c.Logger, time.Second)
	require.NoError(t, c.setupWebServer())

	body := `{"name":"wired","type":"exec","schedule":"@hourly","container":"c1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c.webServer.HTTPServer().Handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	user, ok := userOfCoreJob(c.scheduler.GetJob("wired"))
	require.True(t, ok)
	assert.Equal(t, "nobody", user, "the job went through job assembly")
}
