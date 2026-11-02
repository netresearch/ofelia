// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

func composeJobFor(t *testing.T, cfg any, req *jobRequest) (*core.ComposeJob, error) {
	t.Helper()
	srv := &Server{config: cfg}
	req.Name, req.Type, req.Service = "guard", jobTypeCompose, "task"
	job, err := srv.newComposeJobFromRequest(req)
	if err != nil {
		return nil, err
	}
	cj, ok := job.(*core.ComposeJob)
	if !ok {
		t.Fatalf("got %T, want *core.ComposeJob", job)
	}
	return cj, nil
}

// An API-created compose job follows the same max-runtime ladder as an API
// run job and an INI compose job: its own value, else the scheduler's global
// at run time. So an omitted value stays 0 on the job, and only a run-mode
// job inherits.
func TestComposeJob_APIMaxRuntime(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		req      jobRequest
		want     time.Duration
		inherits bool
	}{
		"omitted inherits the global": {req: jobRequest{}, want: 0, inherits: true},
		"explicit value wins":         {req: jobRequest{MaxRuntime: "30m"}, want: 30 * time.Minute, inherits: true},
		"exec mode inherits nothing":  {req: jobRequest{ExecFlag: true}, want: 0, inherits: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := tc.req
			cj, err := composeJobFor(t, nil, &req)
			if err != nil {
				t.Fatalf("newComposeJobFromRequest: %v", err)
			}
			if cj.MaxRuntime != tc.want {
				t.Errorf("MaxRuntime = %v, want %v", cj.MaxRuntime, tc.want)
			}
			if got := cj.InheritsGlobalMaxRuntime(); got != tc.inherits {
				t.Errorf("InheritsGlobalMaxRuntime() = %v, want %v", got, tc.inherits)
			}
		})
	}
}

// The compose job's maxRuntime must reach the state file through the real
// create handler, or a restart drops the override: the API test above stops
// at the job, the cli test starts from a hand-built persist.Job.
func TestPersist_CreateComposeJob_WritesMaxRuntime(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer("", core.NewScheduler(logger), nil, newHangingDockerProvider())
	srv.SetPersistStore(persist.NewStore(path))

	body := `{"name":"bounded","type":"compose","schedule":"@hourly","service":"task","maxRuntime":"30m"}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create returned %d, want 201: %s", w.Code, w.Body.String())
	}

	reader := persist.NewStore(path)
	if err := reader.Load(); err != nil {
		t.Fatalf("reload the state file: %v", err)
	}
	got, ok := reader.Snapshot().Jobs["bounded"]
	if !ok {
		t.Fatal("the created job is not in the state file")
	}
	if got.MaxRuntime != "30m" {
		t.Fatalf("MaxRuntime = %q in the state file, want %q", got.MaxRuntime, "30m")
	}
}

func TestComposeJob_APIRejectsInvalidMaxRuntime(t *testing.T) {
	t.Parallel()
	_, err := composeJobFor(t, nil, &jobRequest{MaxRuntime: "-5m"})
	if !errors.Is(err, core.ErrMaxRuntimeNegative) {
		t.Fatalf("err = %v, want ErrMaxRuntimeNegative", err)
	}
}

func TestComposeJob_APIRejectsMaxRuntimeInExecMode(t *testing.T) {
	t.Parallel()
	_, err := composeJobFor(t, nil, &jobRequest{ExecFlag: true, MaxRuntime: "5m"})
	if !errors.Is(err, core.ErrComposeExecMaxRuntime) {
		t.Fatalf("err = %v, want ErrComposeExecMaxRuntime", err)
	}
}
