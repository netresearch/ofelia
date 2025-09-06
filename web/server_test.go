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

const (
	schedDaily   = "@daily"
	schedHourly  = "@hourly"
	cmdEcho      = "echo"
	nameJobINI   = "job-ini"
	nameJobLabel = "job-label"
	originINI    = "ini"
	originLabel  = "label"
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
	LastRun  *apiExecution   `json:"lastRun"`
	Origin   string          `json:"origin"`
	Config   json.RawMessage `json:"config"`
}

func TestHistoryEndpoint(t *testing.T) {
	job := &testJob{}
	job.Name = "job1"
	const (
		schedDaily   = "@daily"
		schedHourly  = "@hourly"
		cmdEcho      = "echo"
		nameJobINI   = "job-ini"
		nameJobLabel = "job-label"
		originINI    = "ini"
	)
	job.Schedule = schedDaily
	job.Command = cmdEcho
	e, _ := core.NewExecution()
	_, _ = e.OutputStream.Write([]byte("out"))
	_, _ = e.ErrorStream.Write([]byte("err"))
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

func TestJobsEndpointWithRuntimeData(t *testing.T) {
	// Create test job with execution output
	job := &testJob{}
	job.Name = "test-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho

	// Create execution with output
	e, err := core.NewExecution()
	if err != nil {
		t.Fatalf("NewExecution error: %v", err)
	}

	// Write test data to buffers
	stdoutData := "job completed successfully"
	stderrData := "warning: deprecated flag used"

	_, err = e.OutputStream.Write([]byte(stdoutData))
	if err != nil {
		t.Fatalf("Write stdout error: %v", err)
	}

	_, err = e.ErrorStream.Write([]byte(stderrData))
	if err != nil {
		t.Fatalf("Write stderr error: %v", err)
	}

	e.Start()
	time.Sleep(1 * time.Millisecond) // Ensure duration > 0
	e.Stop(nil)                      // Success

	job.SetLastRun(e)

	// Create scheduler and server
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: &stubLogger{}}
	srv := webpkg.NewServer("", sched, nil, nil)

	// Test with live buffers
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	rr := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var jobs []apiJob
	if err := json.Unmarshal(rr.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}

	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	job1 := jobs[0]
	if job1.LastRun == nil {
		t.Fatal("expected LastRun to be set")
	}

	// Verify runtime data is present
	if job1.LastRun.Stdout != stdoutData {
		t.Errorf("LastRun.Stdout = %q, want %q", job1.LastRun.Stdout, stdoutData)
	}
	if job1.LastRun.Stderr != stderrData {
		t.Errorf("LastRun.Stderr = %q, want %q", job1.LastRun.Stderr, stderrData)
	}
	if job1.LastRun.Duration <= 0 {
		t.Errorf("LastRun.Duration = %v, want > 0", job1.LastRun.Duration)
	}
	if job1.LastRun.Date.IsZero() {
		t.Error("LastRun.Date should not be zero")
	}
	if job1.LastRun.Failed {
		t.Error("LastRun.Failed should be false for successful execution")
	}
}

func TestJobsEndpointAfterBufferCleanup(t *testing.T) {
	// Create test job with execution output
	job := &testJob{}
	job.Name = "cleaned-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho

	// Create execution with output
	e, err := core.NewExecution()
	if err != nil {
		t.Fatalf("NewExecution error: %v", err)
	}

	// Write test data to buffers
	stdoutData := "cleanup test output"
	stderrData := "cleanup test error"

	_, err = e.OutputStream.Write([]byte(stdoutData))
	if err != nil {
		t.Fatalf("Write stdout error: %v", err)
	}

	_, err = e.ErrorStream.Write([]byte(stderrData))
	if err != nil {
		t.Fatalf("Write stderr error: %v", err)
	}

	e.Start()
	time.Sleep(1 * time.Millisecond) // Ensure duration > 0
	e.Stop(nil)                      // Success

	// Cleanup buffers to simulate real-world scenario
	e.Cleanup()

	job.SetLastRun(e)

	// Create scheduler and server
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: &stubLogger{}}
	srv := webpkg.NewServer("", sched, nil, nil)

	// Test with cleaned buffers (should use captured content)
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	rr := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var jobs []apiJob
	if err := json.Unmarshal(rr.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}

	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	job1 := jobs[0]
	if job1.LastRun == nil {
		t.Fatal("expected LastRun to be set")
	}

	// Verify runtime data is still available after cleanup
	if job1.LastRun.Stdout != stdoutData {
		t.Errorf("LastRun.Stdout after cleanup = %q, want %q", job1.LastRun.Stdout, stdoutData)
	}
	if job1.LastRun.Stderr != stderrData {
		t.Errorf("LastRun.Stderr after cleanup = %q, want %q", job1.LastRun.Stderr, stderrData)
	}
	if job1.LastRun.Duration <= 0 {
		t.Errorf("LastRun.Duration = %v, want > 0", job1.LastRun.Duration)
	}
}

func TestHistoryEndpointWithCapturedOutput(t *testing.T) {
	job := &testJob{}
	job.Name = "history-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho

	// Create execution with output that will be cleaned up
	e, err := core.NewExecution()
	if err != nil {
		t.Fatalf("NewExecution error: %v", err)
	}

	historyStdout := "historical output"
	historyStderr := "historical error"

	_, err = e.OutputStream.Write([]byte(historyStdout))
	if err != nil {
		t.Fatalf("Write stdout error: %v", err)
	}

	_, err = e.ErrorStream.Write([]byte(historyStderr))
	if err != nil {
		t.Fatalf("Write stderr error: %v", err)
	}

	e.Start()
	time.Sleep(1 * time.Millisecond)
	e.Stop(fmt.Errorf("test error"))

	// Cleanup to simulate buffer pool return
	e.Cleanup()

	job.SetLastRun(e)
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: &stubLogger{}}
	srv := webpkg.NewServer("", sched, nil, nil)

	req := httptest.NewRequest("GET", "/api/jobs/history-job/history", nil)
	rr := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var history []apiExecution
	if err := json.Unmarshal(rr.Body.Bytes(), &history); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}

	if len(history) != 1 {
		t.Fatalf("expected 1 execution in history, got %d", len(history))
	}

	exec := history[0]

	// Verify captured output is available in history
	if exec.Stdout != historyStdout {
		t.Errorf("History Stdout = %q, want %q", exec.Stdout, historyStdout)
	}
	if exec.Stderr != historyStderr {
		t.Errorf("History Stderr = %q, want %q", exec.Stderr, historyStderr)
	}
	if !exec.Failed {
		t.Error("Execution should be marked as failed")
	}
	if exec.Error != "test error" {
		t.Errorf("Error = %q, want %q", exec.Error, "test error")
	}
}

func TestJobsHandlerIncludesOutput(t *testing.T) {
	job := &testJob{}
	job.Name = "job1"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	e, _ := core.NewExecution()
	_, _ = e.OutputStream.Write([]byte("out"))
	_, _ = e.ErrorStream.Write([]byte("err"))
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
	jobIni.Name = nameJobINI
	jobIni.Schedule = schedDaily
	jobIni.Command = cmdEcho

	jobLabel := &testJob{}
	jobLabel.Name = nameJobLabel
	jobLabel.Schedule = schedHourly
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

	if m[nameJobINI] != originINI || m[nameJobLabel] != originLabel {
		t.Fatalf("unexpected origins %v", m)
	}
}

func TestRemovedJobsHandlerOrigin(t *testing.T) {
	jobIni := &testJob{}
	jobIni.Name = nameJobINI
	jobIni.Schedule = schedDaily
	jobIni.Command = cmdEcho

	jobLabel := &testJob{}
	jobLabel.Name = nameJobLabel
	jobLabel.Schedule = schedHourly
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
	if m[nameJobINI] != originINI || m[nameJobLabel] != originLabel {
		t.Fatalf("unexpected origins %v", m)
	}
}

func TestDisabledJobsHandlerOrigin(t *testing.T) {
	jobIni := &testJob{}
	jobIni.Name = nameJobINI
	jobIni.Schedule = schedDaily
	jobIni.Command = cmdEcho

	jobLabel := &testJob{}
	jobLabel.Name = nameJobLabel
	jobLabel.Schedule = schedHourly
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
	if m[nameJobINI] != originINI || m[nameJobLabel] != originLabel {
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
