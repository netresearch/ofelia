package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/netresearch/ofelia/core"
)

type Server struct {
	addr      string
	scheduler *core.Scheduler
	srv       *http.Server
}

func NewServer(addr string, s *core.Scheduler) *Server {
	server := &Server{addr: addr, scheduler: s}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/jobs", server.jobsHandler)
	mux.HandleFunc("/api/jobs/", server.jobHistoryHandler)
	mux.Handle("/", http.FileServer(http.Dir("static/ui")))
	server.srv = &http.Server{Addr: addr, Handler: mux}
	return server
}

func (s *Server) Start() error {
	go func() {
		_ = s.srv.ListenAndServe()
	}()
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

type apiExecution struct {
	Date     time.Time     `json:"date"`
	Duration time.Duration `json:"duration"`
	Failed   bool          `json:"failed"`
	Skipped  bool          `json:"skipped"`
	Error    string        `json:"error,omitempty"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
}

type apiJob struct {
	Name     string        `json:"name"`
	Schedule string        `json:"schedule"`
	Command  string        `json:"command"`
	LastRun  *apiExecution `json:"last_run,omitempty"`
}

func (s *Server) jobsHandler(w http.ResponseWriter, r *http.Request) {
	jobs := make([]apiJob, 0, len(s.scheduler.Jobs))
	for _, job := range s.scheduler.Jobs {
		var execInfo *apiExecution
		if lrGetter, ok := job.(interface{ GetLastRun() *core.Execution }); ok {
			if lr := lrGetter.GetLastRun(); lr != nil {
				errStr := ""
				if lr.Error != nil {
					errStr = lr.Error.Error()
				}
				execInfo = &apiExecution{
					Date:     lr.Date,
					Duration: lr.Duration,
					Failed:   lr.Failed,
					Skipped:  lr.Skipped,
					Error:    errStr,
					Stdout:   lr.OutputStream.String(),
					Stderr:   lr.ErrorStream.String(),
				}
			}
		}
		jobs = append(jobs, apiJob{
			Name:     job.GetName(),
			Schedule: job.GetSchedule(),
			Command:  job.GetCommand(),
			LastRun:  execInfo,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) jobHistoryHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	if !strings.HasSuffix(path, "/history") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSuffix(path, "/history")
	var target core.Job
	for _, j := range s.scheduler.Jobs {
		if j.GetName() == name {
			target = j
			break
		}
	}
	if target == nil {
		http.NotFound(w, r)
		return
	}

	execs := make([]*apiExecution, 0)
	for _, e := range target.GetHistory() {
		if e == nil {
			continue
		}
		errStr := ""
		if e.Error != nil {
			errStr = e.Error.Error()
		}
		execs = append(execs, &apiExecution{
			Date:     e.Date,
			Duration: e.Duration,
			Failed:   e.Failed,
			Skipped:  e.Skipped,
			Error:    errStr,
			Stdout:   e.OutputStream.String(),
			Stderr:   e.ErrorStream.String(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(execs)
}
