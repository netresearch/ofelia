package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netresearch/ofelia/core"
)

func TestJobTypeCoversKnown(t *testing.T) {
	cases := []struct {
		j    core.Job
		want string
	}{
		{&core.RunJob{}, "run"},
		{&core.ExecJob{}, "exec"},
		{&core.LocalJob{}, "local"},
		{&core.RunServiceJob{}, "service"},
		{&core.ComposeJob{}, "compose"},
	}
	for _, c := range cases {
		if got := jobType(c.j); got != c.want {
			t.Fatalf("jobType(%T)=%q want %q", c.j, got, c.want)
		}
	}
}

func TestJobFromRequestLocal(t *testing.T) {
	s := &Server{}
	req := &jobRequest{Name: "n", Type: "local", Schedule: "@daily", Command: "echo"}
	j, err := s.jobFromRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := j.(*core.LocalJob); !ok {
		t.Fatalf("expected LocalJob, got %T", j)
	}
}

// --- Additional coverage for HTTP handlers ---

type testLogger struct{}

func (testLogger) Criticalf(string, ...interface{}) {}
func (testLogger) Debugf(string, ...interface{})    {}
func (testLogger) Errorf(string, ...interface{})    {}
func (testLogger) Noticef(string, ...interface{})   {}
func (testLogger) Warningf(string, ...interface{})  {}

type simpleJob struct{ core.BareJob }

func (j *simpleJob) Run(*core.Context) error { return nil }

func newSchedWithJob(name string) *core.Scheduler {
	l := &testLogger{}
	sc := core.NewScheduler(l)
	j := &simpleJob{}
	j.Name = name
	j.Schedule = "@daily"
	_ = sc.AddJob(j)
	// Ensure the scheduler is properly initialized
	_ = sc.Start()
	return sc
}

func TestRunJobHandler_OK_and_NotFound(t *testing.T) {
	sc := newSchedWithJob("job1")
	srv := NewServer("", sc, nil, nil)
	httpSrv := srv.HTTPServer()

	// OK
	body, _ := json.Marshal(map[string]string{"name": "job1"})
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/run", bytes.NewReader(body))
	w := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("run ok: unexpected status %d", w.Code)
	}

	// Not found
	body, _ = json.Marshal(map[string]string{"name": "missing"})
	req = httptest.NewRequest(http.MethodPost, "/api/jobs/run", bytes.NewReader(body))
	w = httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("run notfound: unexpected status %d", w.Code)
	}
}

func TestDisableEnableHandlers(t *testing.T) {
	sc := newSchedWithJob("job1")
	srv := NewServer("", sc, nil, nil)
	httpSrv := srv.HTTPServer()

	// disable
	body, _ := json.Marshal(map[string]string{"name": "job1"})
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/disable", bytes.NewReader(body))
	w := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("disable: unexpected %d", w.Code)
	}
	if sc.GetJob("job1") != nil {
		t.Fatalf("expected job disabled")
	}

	// enable
	req = httptest.NewRequest(http.MethodPost, "/api/jobs/enable", bytes.NewReader(body))
	w = httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("enable: unexpected %d", w.Code)
	}
	if sc.GetJob("job1") == nil {
		t.Fatalf("expected job enabled")
	}
}

func TestCreateUpdateDeleteHandlers_Local(t *testing.T) {
	sc := core.NewScheduler(&testLogger{})
	srv := NewServer("", sc, nil, nil)
	httpSrv := srv.HTTPServer()

	// create local
	body := []byte(`{"name":"loc1","type":"local","schedule":"@daily","command":"echo"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: unexpected %d", w.Code)
	}
	if sc.GetJob("loc1") == nil {
		t.Fatalf("create: job missing")
	}

	// update local
	body = []byte(`{"name":"loc1","type":"local","schedule":"@hourly","command":"echo hi"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/jobs/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("update: unexpected %d", w.Code)
	}

	// delete
	body = []byte(`{"name":"loc1"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/jobs/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: unexpected %d", w.Code)
	}
}

func TestConfigHandlerStripsJobs(t *testing.T) {
	type cfg struct {
		RunJobs map[string]*struct{ core.BareJob }
	}
	c := &cfg{RunJobs: map[string]*struct{ core.BareJob }{"n": {}}}
	sc := core.NewScheduler(&testLogger{})
	srv := NewServer("", sc, c, nil)
	httpSrv := srv.HTTPServer()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("config: unexpected %d", w.Code)
	}
	// ensure response decodes and doesn't include RunJobs
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v, ok := out["RunJobs"]; ok && v != nil {
		t.Fatalf("expected RunJobs to be null or absent, got %v", v)
	}
}

func TestCreateUpdateDeleteHandlers_ComposeAndErrors(t *testing.T) {
	sc := core.NewScheduler(&testLogger{})
	srv := NewServer("", sc, nil, nil)
	httpSrv := srv.HTTPServer()

	// bad json
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", bytes.NewReader([]byte("{")))
	w := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create bad json: %d", w.Code)
	}

	// compose create ok
	body := []byte(`{"name":"c1","type":"compose","schedule":"@hourly","file":"f.yml","service":"db"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/jobs/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("compose create: %d", w.Code)
	}

	// update unknown type -> 400
	body = []byte(`{"name":"c1","type":"unknown"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/jobs/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update unknown type: %d", w.Code)
	}

	// delete missing -> 404
	body = []byte(`{"name":"missing"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/jobs/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", w.Code)
	}
}
