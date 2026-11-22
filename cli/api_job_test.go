// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/test"
	"github.com/netresearch/ofelia/web"
)

// apiJobFixture is a daemon as far as jobs created through the web API
// are concerned: a configuration with [global] default-user, a scheduler
// and the web server in front of both.
type apiJobFixture struct {
	cfg     *Config
	handler http.Handler
}

func newAPIJobFixture(t *testing.T, provider core.DockerProvider) *apiJobFixture {
	t.Helper()
	logger := test.NewTestLogger()
	cfg := NewConfig(logger)
	cfg.Global.DefaultUser = "nobody"
	cfg.sh = core.NewScheduler(logger)
	cfg.dockerHandler = &DockerHandler{dockerProvider: provider}
	srv := newAPIJobServer(cfg, provider)
	return &apiJobFixture{cfg: cfg, handler: srv.HTTPServer().Handler}
}

// newAPIJobServer builds the web server the way the daemon does.
func newAPIJobServer(cfg *Config, provider core.DockerProvider) *web.Server {
	srv := web.NewServer("", cfg.sh, cfg, provider)
	srv.SetJobBuilder(cfg.BuildAPIJob)
	return srv
}

func (f *apiJobFixture) create(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

// configKeys returns the keys of the "config" object GET /api/jobs shows
// for name, sorted.
func (f *apiJobFixture) configKeys(t *testing.T, name string) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var jobs []struct {
		Name   string                     `json:"name"`
		Config map[string]json.RawMessage `json:"config"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &jobs))
	for _, j := range jobs {
		if j.Name == name {
			keys := make([]string, 0, len(j.Config))
			for k := range j.Config {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return keys
		}
	}
	t.Fatalf("job %q not listed by GET /api/jobs", name)
	return nil
}

func middlewareTypes(j core.Job) []string {
	out := make([]string, 0, len(j.Middlewares()))
	for _, m := range j.Middlewares() {
		out = append(out, reflect.TypeOf(m).String())
	}
	sort.Strings(out)
	return out
}

func userOfCoreJob(j core.Job) (string, bool) {
	switch j := j.(type) {
	case *core.RunJob:
		return j.User, true
	case *core.ExecJob:
		return j.User, true
	}
	return "", false
}

// The jobs the web API creates, one per type, with what a caller sees of
// them: the Go type the scheduler holds, the middleware chain, the keys of
// the config object in GET /api/jobs and the user a container job runs as.
var apiJobCases = []struct {
	name       string
	body       map[string]any
	wantType   string
	wantUser   string
	wantConfig []string
}{
	{
		name:     "run",
		body:     map[string]any{"name": "api-run", "type": "run", "schedule": "@hourly", "image": "alpine", "command": "echo hi"},
		wantType: "*core.RunJob",
		wantUser: apiJobUser,
	},
	{
		name:     "exec",
		body:     map[string]any{"name": "api-exec", "type": "exec", "schedule": "@hourly", "container": "c1", "command": "echo hi"},
		wantType: "*core.ExecJob",
		wantUser: apiJobUser,
	},
	{
		name:     "compose",
		body:     map[string]any{"name": "api-compose", "type": "compose", "schedule": "@hourly", "service": "svc", "maxRuntime": "1h"},
		wantType: "*core.ComposeJob",
	},
	{
		name:     "local",
		body:     map[string]any{"name": "api-local", "type": "local", "schedule": "@hourly", "command": "echo hi"},
		wantType: "*core.LocalJob",
	},
}

func TestAPIJob_CreatedJobs(t *testing.T) {
	t.Parallel()
	for _, tc := range apiJobCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAPIJobFixture(t, &mockDockerProviderForHandler{})
			rec := f.create(t, tc.body)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

			name, _ := tc.body["name"].(string)
			j := f.cfg.sh.GetJob(name)
			require.NotNil(t, j)
			assert.Equal(t, tc.wantType, reflect.TypeOf(j).String())
			assert.Equal(t, 10, historyLimitOfCore(j), "struct-tag defaults")
			if user, ok := userOfCoreJob(j); ok {
				assert.Equal(t, tc.wantUser, user)
			}
			assert.Equal(t, apiJobMiddlewares, middlewareTypes(j))
			assert.NotContains(t, f.configKeys(t, name), "SlackWebhook",
				"the config object shows the core job, not the notification settings")
		})
	}
}

func historyLimitOfCore(j core.Job) int {
	v := reflect.ValueOf(j).Elem().FieldByName("HistoryLimit")
	if !v.IsValid() {
		return -1
	}
	return int(v.Int())
}

// A run job's container is removed after the run unless the job says
// otherwise.
func TestAPIJob_RunJobDeletesItsContainer(t *testing.T) {
	t.Parallel()
	f := newAPIJobFixture(t, &mockDockerProviderForHandler{})
	require.Equal(t, http.StatusCreated, f.create(t, apiJobCases[0].body).Code)
	rj, ok := f.cfg.sh.GetJob("api-run").(*core.RunJob)
	require.True(t, ok)
	assert.Equal(t, "true", rj.Delete)
}

func TestAPIJob_Rejected(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		provider core.DockerProvider
		body     map[string]any
		wantBody string
	}{
		{
			name:     "run job without a Docker provider",
			body:     map[string]any{"name": "r", "type": "run", "schedule": "@hourly", "image": "alpine"},
			wantBody: "docker provider unavailable",
		},
		{
			name:     "exec job without a Docker provider",
			body:     map[string]any{"name": "e", "type": "exec", "schedule": "@hourly", "container": "c1", "command": "true"},
			wantBody: "docker provider unavailable",
		},
		{
			name:     "compose job in exec mode with a max-runtime",
			provider: &mockDockerProviderForHandler{},
			body:     map[string]any{"name": "c", "type": "compose", "schedule": "@hourly", "service": "svc", "exec": true, "maxRuntime": "1h"},
			wantBody: "invalid maxRuntime",
		},
		{
			name:     "compose job with an invalid service name",
			provider: &mockDockerProviderForHandler{},
			body:     map[string]any{"name": "c", "type": "compose", "schedule": "@hourly", "service": "svc;rm -rf /"},
			wantBody: "invalid service name",
		},
		{
			name:     "unknown type",
			provider: &mockDockerProviderForHandler{},
			body:     map[string]any{"name": "u", "type": "service-run", "schedule": "@hourly"},
			wantBody: "unknown job type",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAPIJobFixture(t, tc.provider)
			rec := f.create(t, tc.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.wantBody)
		})
	}
}

// A job restored from the state file comes back as the job the API
// created.
func TestAPIJob_RestoredJobs(t *testing.T) {
	t.Parallel()
	c := newDaemonForPersistTest(t)
	c.StateFile = writePersistFile(t, persist.State{Jobs: map[string]*persist.Job{
		"p-run":  {Type: persist.JobTypeRun, Schedule: "@hourly", Image: "alpine"},
		"p-exec": {Type: persist.JobTypeExec, Schedule: "@hourly", Container: "c1", Command: "true"},
	}})
	attachRestoreConfig(t, c, &mockDockerProviderForHandler{})
	require.NoError(t, c.initPersistStore())
	for _, name := range []string{"p-run", "p-exec"} {
		j := c.scheduler.GetJob(name)
		require.NotNil(t, j, name)
		user, _ := userOfCoreJob(j)
		assert.Equal(t, apiJobUser, user, name)
		assert.Equal(t, apiJobMiddlewares, middlewareTypes(j), name)
	}
}
