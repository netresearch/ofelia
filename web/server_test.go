package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/ofelia/cli"
	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
)

type stubLogger struct{}

func (stubLogger) Criticalf(string, ...interface{}) {}
func (stubLogger) Debugf(string, ...interface{})    {}
func (stubLogger) Errorf(string, ...interface{})    {}
func (stubLogger) Noticef(string, ...interface{})   {}
func (stubLogger) Warningf(string, ...interface{})  {}

type testJob struct{ core.BareJob }

func (j *testJob) Run(*core.Context) error { return nil }

type apiExecution struct {
	Date     time.Time     `json:"date"`
	Duration time.Duration `json:"duration"`
	Failed   bool          `json:"failed"`
	Skipped  bool          `json:"skipped"`
	Error    string        `json:"error"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
}

type apiJob struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Schedule string          `json:"schedule"`
	Command  string          `json:"command"`
	LastRun  *apiExecution   `json:"last_run"`
	Origin   string          `json:"origin"`
	Config   json.RawMessage `json:"config"`
}

func TestHistoryEndpoint(t *testing.T) {
	job := &testJob{}
	job.Name = "job1"
	job.Schedule = "@daily"
	job.Command = "echo"
	e, _ := core.NewExecution()
	e.OutputStream.Write([]byte("out"))
	e.ErrorStream.Write([]byte("err"))
	e.Error = fmt.Errorf("boom")
	e.Failed = true
	job.SetLastRun(e)
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: &stubLogger{}}
	srv := webpkg.NewServer("", sched, nil, nil)

	req := httptest.NewRequest("GET", "/api/jobs/job1/history", nil)
	w := httptest.NewRecorder()
	httpSrv := srv.HTTPServer()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("unexpected status %d", w.Code)
	}
	var data []apiExecution
	if err := json.NewDecoder(w.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(data))
	}
	if data[0].Stdout != "out" || data[0].Stderr != "err" || data[0].Error != "boom" {
		t.Fatalf("unexpected output %v", data[0])
	}
}

func TestJobsHandlerIncludesOutput(t *testing.T) {
	job := &testJob{}
	job.Name = "job1"
	job.Schedule = "@daily"
	job.Command = "echo"
	e, _ := core.NewExecution()
	e.OutputStream.Write([]byte("out"))
	e.ErrorStream.Write([]byte("err"))
	e.Error = fmt.Errorf("boom")
	e.Failed = true
	job.SetLastRun(e)
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: &stubLogger{}}
	srv := webpkg.NewServer("", sched, nil, nil)

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	httpSrv := srv.HTTPServer()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("unexpected status %d", w.Code)
	}
	var jobs []apiJob
	if err := json.NewDecoder(w.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(jobs) != 1 || jobs[0].LastRun == nil {
		t.Fatalf("unexpected jobs %v", jobs)
	}
	if jobs[0].LastRun.Stdout != "out" || jobs[0].LastRun.Stderr != "err" || jobs[0].LastRun.Error != "boom" {
		t.Fatalf("stdout/stderr/error not included")
	}
}

func TestJobsHandlerOrigin(t *testing.T) {
	jobIni := &testJob{}
	jobIni.Name = "job-ini"
	jobIni.Schedule = "@daily"
	jobIni.Command = "echo"

	jobLabel := &testJob{}
	jobLabel.Name = "job-label"
	jobLabel.Schedule = "@hourly"
	jobLabel.Command = "ls"

	sched := &core.Scheduler{Jobs: []core.Job{jobIni, jobLabel}, Logger: &stubLogger{}}

	type originConfig struct {
		RunJobs map[string]*struct{ JobSource cli.JobSource }
	}
	cfg := &originConfig{
		RunJobs: map[string]*struct{ JobSource cli.JobSource }{
			"job-ini":   {JobSource: cli.JobSourceINI},
			"job-label": {JobSource: cli.JobSourceLabel},
		},
	}

	srv := webpkg.NewServer("", sched, cfg, nil)

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	httpSrv := srv.HTTPServer()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("unexpected status %d", w.Code)
	}

	var jobs []apiJob
	if err := json.NewDecoder(w.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs")
	}

	m := map[string]string{}
	for _, j := range jobs {
		m[j.Name] = j.Origin
	}

	if m["job-ini"] != "ini" || m["job-label"] != "label" {
		t.Fatalf("unexpected origins %v", m)
	}
}
func TestRemovedJobsHandlerOrigin(t *testing.T) {
	jobIni := &testJob{}
	jobIni.Name = "job-ini"
	jobIni.Schedule = "@daily"
	jobIni.Command = "echo"

	jobLabel := &testJob{}
	jobLabel.Name = "job-label"
	jobLabel.Schedule = "@hourly"
	jobLabel.Command = "ls"

	sched := core.NewScheduler(&stubLogger{})
	_ = sched.AddJob(jobIni)
	_ = sched.AddJob(jobLabel)
	_ = sched.RemoveJob(jobIni)
	_ = sched.RemoveJob(jobLabel)

	type originConfig struct {
		RunJobs map[string]*struct{ JobSource cli.JobSource }
	}
	cfg := &originConfig{
		RunJobs: map[string]*struct{ JobSource cli.JobSource }{
			"job-ini":   {JobSource: cli.JobSourceINI},
			"job-label": {JobSource: cli.JobSourceLabel},
		},
	}

	srv := webpkg.NewServer("", sched, cfg, nil)

	req := httptest.NewRequest("GET", "/api/jobs/removed", nil)
	w := httptest.NewRecorder()
	httpSrv := srv.HTTPServer()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("unexpected status %d", w.Code)
	}

	var jobs []apiJob
	if err := json.NewDecoder(w.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	m := map[string]string{}
	for _, j := range jobs {
		m[j.Name] = j.Origin
	}
	if m["job-ini"] != "ini" || m["job-label"] != "label" {
		t.Fatalf("unexpected origins %v", m)
	}
}

func TestDisabledJobsHandlerOrigin(t *testing.T) {
	jobIni := &testJob{}
	jobIni.Name = "job-ini"
	jobIni.Schedule = "@daily"
	jobIni.Command = "echo"

	jobLabel := &testJob{}
	jobLabel.Name = "job-label"
	jobLabel.Schedule = "@hourly"
	jobLabel.Command = "ls"

	sched := core.NewScheduler(&stubLogger{})
	_ = sched.AddJob(jobIni)
	_ = sched.AddJob(jobLabel)
	_ = sched.DisableJob("job-ini")
	_ = sched.DisableJob("job-label")

	type originConfig struct {
		RunJobs map[string]*struct{ JobSource cli.JobSource }
	}
	cfg := &originConfig{
		RunJobs: map[string]*struct{ JobSource cli.JobSource }{
			"job-ini":   {JobSource: cli.JobSourceINI},
			"job-label": {JobSource: cli.JobSourceLabel},
		},
	}

	srv := webpkg.NewServer("", sched, cfg, nil)

	req := httptest.NewRequest("GET", "/api/jobs/disabled", nil)
	w := httptest.NewRecorder()
	httpSrv := srv.HTTPServer()
	httpSrv.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("unexpected status %d", w.Code)
	}

	var jobs []apiJob
	if err := json.NewDecoder(w.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	m := map[string]string{}
	for _, j := range jobs {
		m[j.Name] = j.Origin
	}
	if m["job-ini"] != "ini" || m["job-label"] != "label" {
		t.Fatalf("unexpected origins %v", m)
	}
}

func TestCreateJobTypes(t *testing.T) {
	sched := core.NewScheduler(&stubLogger{})
	srv := webpkg.NewServer("", sched, nil, nil)
	httpSrv := srv.HTTPServer()

	cases := []struct {
		name   string
		body   string
		status int
		check  func(core.Job) bool
	}{
		{"run1", `{"name":"run1","type":"run","schedule":"@hourly","image":"busybox"}`, http.StatusBadRequest, func(j core.Job) bool { return j == nil }},
		{"exec1", `{"name":"exec1","type":"exec","schedule":"@hourly","container":"c1"}`, http.StatusBadRequest, func(j core.Job) bool { return j == nil }},
		{"comp1", `{"name":"comp1","type":"compose","schedule":"@hourly","service":"db"}`, http.StatusCreated, func(j core.Job) bool { _, ok := j.(*core.ComposeJob); return ok }},
		{"local1", `{"name":"local1","type":"local","schedule":"@hourly"}`, http.StatusCreated, func(j core.Job) bool { _, ok := j.(*core.LocalJob); return ok }},
	}

	for _, c := range cases {
		req := httptest.NewRequest("POST", "/api/jobs/create", strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)
		if w.Code != c.status {
			t.Fatalf("%s: unexpected status %d", c.name, w.Code)
		}
		j := sched.GetJob(c.name)
		if !c.check(j) {
			t.Fatalf("%s: job check failed: %T", c.name, j)
		}
		if j != nil {
			_ = sched.RemoveJob(j)
		}
	}
}
