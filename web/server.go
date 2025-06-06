package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/static"
)

type Server struct {
	addr      string
	scheduler *core.Scheduler
	config    interface{}
	srv       *http.Server
	origins   map[string]string
}

func NewServer(addr string, s *core.Scheduler, cfg interface{}) *Server {
	server := &Server{addr: addr, scheduler: s, config: cfg, origins: make(map[string]string)}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/jobs/removed", server.removedJobsHandler)
	mux.HandleFunc("/api/jobs/disabled", server.disabledJobsHandler)
	mux.HandleFunc("/api/jobs/run", server.runJobHandler)
	mux.HandleFunc("/api/jobs/disable", server.disableJobHandler)
	mux.HandleFunc("/api/jobs/enable", server.enableJobHandler)
	mux.HandleFunc("/api/jobs/create", server.createJobHandler)
	mux.HandleFunc("/api/jobs/update", server.updateJobHandler)
	mux.HandleFunc("/api/jobs/delete", server.deleteJobHandler)
	mux.HandleFunc("/api/jobs/", server.historyHandler)
	mux.HandleFunc("/api/jobs", server.jobsHandler)
	mux.HandleFunc("/api/config", server.configHandler)
	uiFS, err := fs.Sub(static.UI, "ui")
	if err != nil {
		panic("failed to load UI subdirectory: " + err.Error())
	}
	mux.Handle("/", http.FileServer(http.FS(uiFS)))
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
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Schedule string          `json:"schedule"`
	Command  string          `json:"command"`
	LastRun  *apiExecution   `json:"last_run,omitempty"`
	Origin   string          `json:"origin"`
	Config   json.RawMessage `json:"config"`
}

func jobOrigin(cfg interface{}, name string) string {
	if cfg == nil {
		return ""
	}
	v := reflect.ValueOf(cfg)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	runJobs := v.FieldByName("RunJobs")
	if runJobs.IsValid() && runJobs.Kind() == reflect.Map {
		if runJobs.MapIndex(reflect.ValueOf(name)).IsValid() {
			return "ini"
		}
	}
	labelRunJobs := v.FieldByName("LabelRunJobs")
	if labelRunJobs.IsValid() && labelRunJobs.Kind() == reflect.Map {
		if labelRunJobs.MapIndex(reflect.ValueOf(name)).IsValid() {
			return "label"
		}
	}
	labelExecJobs := v.FieldByName("LabelExecJobs")
	if labelExecJobs.IsValid() && labelExecJobs.Kind() == reflect.Map {
		if labelExecJobs.MapIndex(reflect.ValueOf(name)).IsValid() {
			return "label"
		}
	}
	return ""
}

func (s *Server) jobOrigin(name string) string {
	if o, ok := s.origins[name]; ok {
		return o
	}
	return jobOrigin(s.config, name)
}

func jobType(j core.Job) string {
	switch j.(type) {
	case *core.RunJob:
		return "run"
	case *core.ExecJob:
		return "exec"
	case *core.LocalJob:
		return "local"
	case *core.RunServiceJob:
		return "service"
	default:
		t := reflect.TypeOf(j)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		return strings.ToLower(t.Name())
	}
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
		origin := s.jobOrigin(job.GetName())
		cfgBytes, _ := json.Marshal(job)
		jobs = append(jobs, apiJob{
			Name:     job.GetName(),
			Type:     jobType(job),
			Schedule: job.GetSchedule(),
			Command:  job.GetCommand(),
			LastRun:  execInfo,
			Origin:   origin,
			Config:   cfgBytes,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) removedJobsHandler(w http.ResponseWriter, r *http.Request) {
	removed := s.scheduler.GetRemovedJobs()
	jobs := make([]apiJob, 0, len(removed))
	for _, job := range removed {
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
		origin := s.jobOrigin(job.GetName())
		cfgBytes, _ := json.Marshal(job)
		jobs = append(jobs, apiJob{
			Name:     job.GetName(),
			Type:     jobType(job),
			Schedule: job.GetSchedule(),
			Command:  job.GetCommand(),
			LastRun:  execInfo,
			Origin:   origin,
			Config:   cfgBytes,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) disabledJobsHandler(w http.ResponseWriter, r *http.Request) {
	disabled := s.scheduler.GetDisabledJobs()
	jobs := make([]apiJob, 0, len(disabled))
	for _, job := range disabled {
		origin := s.jobOrigin(job.GetName())
		cfgBytes, _ := json.Marshal(job)
		jobs = append(jobs, apiJob{
			Name:     job.GetName(),
			Type:     jobType(job),
			Schedule: job.GetSchedule(),
			Command:  job.GetCommand(),
			Origin:   origin,
			Config:   cfgBytes,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

type jobRequest struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule,omitempty"`
	Command  string `json:"command,omitempty"`
}

func (s *Server) runJobHandler(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.scheduler.RunJob(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) disableJobHandler(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.scheduler.DisableJob(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enableJobHandler(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.scheduler.EnableJob(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createJobHandler(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	job := &core.LocalJob{BareJob: core.BareJob{Schedule: req.Schedule, Name: req.Name, Command: req.Command}}
	if err := s.scheduler.AddJob(job); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	origin := r.Header.Get("X-Origin")
	if origin == "" {
		origin = "api"
	}
	s.origins[req.Name] = origin
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) updateJobHandler(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = s.scheduler.DisableJob(req.Name)
	job := &core.LocalJob{BareJob: core.BareJob{Schedule: req.Schedule, Name: req.Name, Command: req.Command}}
	if err := s.scheduler.AddJob(job); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	origin := r.Header.Get("X-Origin")
	if origin == "" {
		origin = "api"
	}
	s.origins[req.Name] = origin
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) deleteJobHandler(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	j := s.scheduler.GetJob(req.Name)
	if j == nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	_ = s.scheduler.RemoveJob(j)
	delete(s.origins, req.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) configHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	cfg := stripJobs(s.config)
	_ = json.NewEncoder(w).Encode(cfg)
}

func stripJobs(cfg interface{}) interface{} {
	if cfg == nil {
		return nil
	}
	v := reflect.ValueOf(cfg)
	isPtr := false
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
		isPtr = true
	}
	if v.Kind() != reflect.Struct {
		return cfg
	}
	out := reflect.New(v.Type()).Elem()
	out.Set(v)
	fields := []string{"RunJobs", "LabelRunJobs", "LabelExecJobs", "ExecJobs", "ServiceJobs", "LocalJobs"}
	for _, f := range fields {
		if fv := out.FieldByName(f); fv.IsValid() && fv.CanSet() {
			fv.Set(reflect.Zero(fv.Type()))
		}
	}
	if isPtr {
		p := reflect.New(out.Type())
		p.Elem().Set(out)
		return p.Interface()
	}
	return out.Interface()
}

func (s *Server) historyHandler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/history") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/history")
	var target core.Job
	for _, job := range s.scheduler.Jobs {
		if job.GetName() == name {
			target = job
			break
		}
	}
	if target == nil {
		http.NotFound(w, r)
		return
	}
	hJob, ok := target.(interface{ GetHistory() []*core.Execution })
	if !ok {
		http.NotFound(w, r)
		return
	}
	hist := hJob.GetHistory()
	out := make([]apiExecution, 0, len(hist))
	for _, e := range hist {
		errStr := ""
		if e.Error != nil {
			errStr = e.Error.Error()
		}
		out = append(out, apiExecution{
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
	_ = json.NewEncoder(w).Encode(out)
}
