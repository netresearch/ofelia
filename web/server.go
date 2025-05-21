package web

import (
	"context"
	"encoding/json"
	"net/http"
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
