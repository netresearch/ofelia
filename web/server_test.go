package web

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/netresearch/ofelia/core"
)

type stubLogger struct{}

func (stubLogger) Criticalf(string, ...interface{}) {}
func (stubLogger) Debugf(string, ...interface{})    {}
func (stubLogger) Errorf(string, ...interface{})    {}
func (stubLogger) Noticef(string, ...interface{})   {}
func (stubLogger) Warningf(string, ...interface{})  {}

type testJob struct{ core.BareJob }

func (j *testJob) Run(*core.Context) error { return nil }

func TestHistoryEndpoint(t *testing.T) {
	job := &testJob{}
	job.Name = "job1"
	job.Schedule = "@daily"
	job.Command = "echo"
	e, _ := core.NewExecution()
	e.OutputStream.Write([]byte("out"))
	e.ErrorStream.Write([]byte("err"))
	job.SetLastRun(e)
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: &stubLogger{}}
	srv := NewServer("", sched, nil)

	req := httptest.NewRequest("GET", "/api/jobs/job1/history", nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)
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
	if data[0].Stdout != "out" || data[0].Stderr != "err" {
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
	job.SetLastRun(e)
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: &stubLogger{}}
	srv := NewServer("", sched, nil)

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)
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
	if jobs[0].LastRun.Stdout != "out" || jobs[0].LastRun.Stderr != "err" {
		t.Fatalf("stdout/stderr not included")
	}
}
