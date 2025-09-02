package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
	"time"

	dockerclient "github.com/fsouza/go-dockerclient"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/static"
)

type Server struct {
	addr      string
	scheduler *core.Scheduler
	config    interface{}
	srv       *http.Server
	origins   map[string]string
	client    *dockerclient.Client
}

// HTTPServer returns the underlying http.Server used by the web interface. It
// is exposed for tests and may change if the Server struct evolves.
func (s *Server) HTTPServer() *http.Server { return s.srv }

// GetHTTPServer returns the underlying http.Server for graceful shutdown support
func (s *Server) GetHTTPServer() *http.Server { return s.srv }

func NewServer(addr string, s *core.Scheduler, cfg interface{}, client *dockerclient.Client) *Server {
	server := &Server{addr: addr, scheduler: s, config: cfg, origins: make(map[string]string), client: client}
	mux := http.NewServeMux()
	
	// Create rate limiter: 100 requests per minute per IP
	rl := newRateLimiter(100, time.Minute)
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
		// Return error gracefully instead of panic
		// The caller should handle this error appropriately
		server.scheduler.Logger.Errorf("failed to load UI subdirectory: %v", err)
		return nil
	}
	mux.Handle("/", http.FileServer(http.FS(uiFS)))
	
	// Apply security middlewares
	var handler http.Handler = mux
	handler = securityHeaders(handler)
	handler = rl.middleware(handler)
	
	server.srv = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return server
}

func (s *Server) Start() error { go func() { _ = s.srv.ListenAndServe() }(); return nil }

func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	return nil
}

// RegisterHealthEndpoints registers health check endpoints on the server
func (s *Server) RegisterHealthEndpoints(hc *HealthChecker) {
	if s.srv == nil || s.srv.Handler == nil {
		return
	}
	
	// Get the existing mux from the handler chain
	// We need to add the health endpoints to the underlying mux
	// before the middleware chain
	mux := http.NewServeMux()
	
	// Re-register all existing endpoints
	mux.HandleFunc("/api/jobs/removed", s.removedJobsHandler)
	mux.HandleFunc("/api/jobs/disabled", s.disabledJobsHandler)
	mux.HandleFunc("/api/jobs/run", s.runJobHandler)
	mux.HandleFunc("/api/jobs/disable", s.disableJobHandler)
	mux.HandleFunc("/api/jobs/enable", s.enableJobHandler)
	mux.HandleFunc("/api/jobs/create", s.createJobHandler)
	mux.HandleFunc("/api/jobs/update", s.updateJobHandler)
	mux.HandleFunc("/api/jobs/delete", s.deleteJobHandler)
	mux.HandleFunc("/api/jobs/", s.historyHandler)
	mux.HandleFunc("/api/jobs", s.jobsHandler)
	mux.HandleFunc("/api/config", s.configHandler)
	
	// Add health endpoints
	mux.HandleFunc("/health", hc.HealthHandler())
	mux.HandleFunc("/healthz", hc.HealthHandler())
	mux.HandleFunc("/ready", hc.ReadinessHandler())
	mux.HandleFunc("/live", hc.LivenessHandler())
	
	// Add UI handler
	uiFS, err := fs.Sub(static.UI, "ui")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(uiFS)))
	}
	
	// Re-apply middleware chain
	rl := newRateLimiter(100, time.Minute)
	var handler http.Handler = mux
	handler = securityHeaders(handler)
	handler = rl.middleware(handler)
	
	// Update the server handler
	s.srv.Handler = handler
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
	LastRun  *apiExecution   `json:"lastRun,omitempty"`
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
	fields := []string{"RunJobs", "ExecJobs", "ServiceJobs", "LocalJobs", "ComposeJobs"}
	for _, f := range fields {
		m := v.FieldByName(f)
		if m.IsValid() && m.Kind() == reflect.Map {
			jv := m.MapIndex(reflect.ValueOf(name))
			if jv.IsValid() {
				if jv.Kind() == reflect.Ptr {
					jv = jv.Elem()
				}
				src := jv.FieldByName("JobSource")
				if src.IsValid() {
					return src.String()
				}
			}
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
	case *core.ComposeJob:
		return "compose"
	default:
		t := reflect.TypeOf(j)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		return strings.ToLower(t.Name())
	}
}

// buildAPIJobs converts a slice of core.Job into apiJob payloads.
func (s *Server) buildAPIJobs(list []core.Job) []apiJob {
	jobs := make([]apiJob, 0, len(list))
	for _, job := range list {
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
	return jobs
}

func (s *Server) jobsHandler(w http.ResponseWriter, _ *http.Request) {
	jobs := s.buildAPIJobs(s.scheduler.Jobs)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) removedJobsHandler(w http.ResponseWriter, _ *http.Request) {
	jobs := s.buildAPIJobs(s.scheduler.GetRemovedJobs())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) disabledJobsHandler(w http.ResponseWriter, _ *http.Request) {
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
	Name      string `json:"name"`
	Type      string `json:"type"`
	Schedule  string `json:"schedule,omitempty"`
	Command   string `json:"command,omitempty"`
	Image     string `json:"image,omitempty"`
	Container string `json:"container,omitempty"`
	File      string `json:"file,omitempty"`
	Service   string `json:"service,omitempty"`
	ExecFlag  bool   `json:"exec,omitempty"`
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
	job, err := s.jobFromRequest(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
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
	job, err := s.jobFromRequest(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
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

func (s *Server) jobFromRequest(req *jobRequest) (core.Job, error) {
	switch req.Type {
	case "run":
		if s.client == nil {
			return nil, fmt.Errorf("docker client unavailable for run job")
		}
		j := &core.RunJob{Client: s.client}
		j.Name = req.Name
		j.Schedule = req.Schedule
		j.Command = req.Command
		j.Image = req.Image
		j.Container = req.Container
		return j, nil
	case "exec":
		if s.client == nil {
			return nil, fmt.Errorf("docker client unavailable for exec job")
		}
		j := &core.ExecJob{Client: s.client}
		j.Name = req.Name
		j.Schedule = req.Schedule
		j.Command = req.Command
		j.Container = req.Container
		return j, nil
	case "compose":
		j := &core.ComposeJob{}
		j.Name = req.Name
		j.Schedule = req.Schedule
		j.Command = req.Command
		j.File = req.File
		j.Service = req.Service
		j.Exec = req.ExecFlag
		return j, nil
	case "", "local":
		j := &core.LocalJob{}
		j.Name = req.Name
		j.Schedule = req.Schedule
		j.Command = req.Command
		return j, nil
	default:
		return nil, fmt.Errorf("unknown job type %q", req.Type)
	}
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

func (s *Server) configHandler(w http.ResponseWriter, _ *http.Request) {
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
	fields := []string{"RunJobs", "ExecJobs", "ServiceJobs", "LocalJobs", "ComposeJobs"}
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
