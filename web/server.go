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

	"github.com/gobs/args"
	cron "github.com/netresearch/go-cron"

	"github.com/netresearch/ofelia/config"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/static"
)

type Server struct {
	addr         string
	scheduler    *core.Scheduler
	config       any
	srv          *http.Server
	origins      map[string]string
	provider     core.DockerProvider
	authConfig   *SecureAuthConfig
	tokenManager *SecureTokenManager
	loginLimiter *RateLimiter
}

// HTTPServer returns the underlying http.Server used by the web interface. It
// is exposed for tests and may change if the Server struct evolves.
func (s *Server) HTTPServer() *http.Server { return s.srv }

// GetHTTPServer returns the underlying http.Server for graceful shutdown support
func (s *Server) GetHTTPServer() *http.Server { return s.srv }

func NewServer(addr string, s *core.Scheduler, cfg any, provider core.DockerProvider) *Server {
	return NewServerWithAuth(addr, s, cfg, provider, nil)
}

func NewServerWithAuth(addr string, s *core.Scheduler, cfg any, provider core.DockerProvider, authCfg *SecureAuthConfig) *Server {
	server := &Server{
		addr:       addr,
		scheduler:  s,
		config:     cfg,
		origins:    make(map[string]string),
		provider:   provider,
		authConfig: authCfg,
	}

	if authCfg != nil && authCfg.Enabled {
		tokenExpiry := authCfg.TokenExpiry
		if tokenExpiry == 0 {
			tokenExpiry = 24
		}
		tm, err := NewSecureTokenManager(authCfg.SecretKey, tokenExpiry)
		if err != nil {
			s.Logger.Error("failed to initialize token manager", "error", err)
			return nil
		}
		server.tokenManager = tm

		maxAttempts := authCfg.MaxAttempts
		if maxAttempts == 0 {
			maxAttempts = 5
		}
		server.loginLimiter = NewRateLimiter(maxAttempts, maxAttempts)
	}

	mux := http.NewServeMux()

	rl := newRateLimiter(100, time.Minute)

	if server.authConfig != nil && server.authConfig.Enabled {
		loginHandler := NewSecureLoginHandler(server.authConfig, server.tokenManager, server.loginLimiter)
		mux.Handle("/api/login", loginHandler)
		mux.HandleFunc("/api/logout", server.logoutHandler)
		mux.HandleFunc("/api/auth/status", server.authStatusHandler)
		mux.HandleFunc("/api/csrf-token", server.csrfTokenHandler)
	}

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
		server.scheduler.Logger.Error(fmt.Sprintf("failed to load UI subdirectory: %v", err))
		return nil
	}
	mux.Handle("/", http.FileServer(http.FS(uiFS)))

	var handler http.Handler = mux
	handler = securityHeaders(handler)
	handler = rl.middleware(handler)

	if server.authConfig != nil && server.authConfig.Enabled {
		handler = server.authMiddleware(handler)
	}

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

func (s *Server) RegisterHealthEndpoints(hc *HealthChecker) {
	if s.srv == nil || s.srv.Handler == nil {
		return
	}

	mux := http.NewServeMux()

	if s.authConfig != nil && s.authConfig.Enabled {
		loginHandler := NewSecureLoginHandler(s.authConfig, s.tokenManager, s.loginLimiter)
		mux.Handle("/api/login", loginHandler)
		mux.HandleFunc("/api/logout", s.logoutHandler)
		mux.HandleFunc("/api/auth/status", s.authStatusHandler)
		mux.HandleFunc("/api/csrf-token", s.csrfTokenHandler)
	}

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

	mux.HandleFunc("/health", hc.HealthHandler())
	mux.HandleFunc("/healthz", hc.HealthHandler())
	mux.HandleFunc("/ready", hc.ReadinessHandler())
	mux.HandleFunc("/live", hc.LivenessHandler())

	uiFS, err := fs.Sub(static.UI, "ui")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(uiFS)))
	}

	rl := newRateLimiter(100, time.Minute)
	var handler http.Handler = mux
	handler = securityHeaders(handler)
	handler = rl.middleware(handler)

	if s.authConfig != nil && s.authConfig.Enabled {
		handler = s.authMiddleware(handler)
	}

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
	Running  bool            `json:"running"`
	LastRun  *apiExecution   `json:"lastRun,omitempty"`
	NextRuns []time.Time     `json:"nextRuns"`
	PrevRuns []time.Time     `json:"prevRuns"`
	Origin   string          `json:"origin"`
	Config   json.RawMessage `json:"config"`
}

func jobOrigin(cfg any, name string) string {
	if cfg == nil {
		return ""
	}
	v := reflect.ValueOf(cfg)
	if v.Kind() == reflect.Pointer {
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
				if jv.Kind() == reflect.Pointer {
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
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		return strings.ToLower(t.Name())
	}
}

// scheduleRunCount is the number of next/previous execution times returned per job.
const scheduleRunCount = 5

// buildAPIJobs converts a slice of core.Job into apiJob payloads.
func (s *Server) buildAPIJobs(list []core.Job) []apiJob {
	now := time.Now()
	jobs := make([]apiJob, 0, len(list))
	for _, job := range list {
		var execInfo *apiExecution
		if lrGetter, ok := job.(interface{ GetLastRun() *core.Execution }); ok {
			if lr := lrGetter.GetLastRun(); lr != nil {
				errStr := ""
				if lr.Error != nil {
					errStr = lr.Error.Error()
				}
				stdout := lr.GetStdout()
				stderr := lr.GetStderr()
				execInfo = &apiExecution{
					Date:     lr.Date,
					Duration: lr.Duration,
					Failed:   lr.Failed,
					Skipped:  lr.Skipped,
					Error:    errStr,
					Stdout:   stdout,
					Stderr:   stderr,
				}
			}
		}

		// Compute next/prev execution times from the cron schedule.
		// Triggered-only jobs and jobs without a cron entry return empty slices.
		var nextRuns, prevRuns []time.Time
		if !core.IsTriggeredSchedule(job.GetSchedule()) {
			entry := s.scheduler.EntryByName(job.GetName())
			if entry.Valid() && entry.Schedule != nil {
				nextRuns = cron.NextN(entry.Schedule, now, scheduleRunCount)
				prevRuns = cron.PrevN(entry.Schedule, now, scheduleRunCount)
			}
		}
		if nextRuns == nil {
			nextRuns = []time.Time{}
		}
		if prevRuns == nil {
			prevRuns = []time.Time{}
		}

		origin := s.jobOrigin(job.GetName())
		cfgBytes, _ := json.Marshal(job)
		jobs = append(jobs, apiJob{
			Name:     job.GetName(),
			Type:     jobType(job),
			Schedule: job.GetSchedule(),
			Command:  job.GetCommand(),
			Running:  s.scheduler.IsJobRunning(job.GetName()),
			LastRun:  execInfo,
			NextRuns: nextRuns,
			PrevRuns: prevRuns,
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
			NextRuns: []time.Time{},
			PrevRuns: []time.Time{},
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
	if err := s.scheduler.RunJob(r.Context(), req.Name); err != nil {
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
	job, err := s.jobFromRequest(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Try atomic update first; fall back to disable+add for new jobs
	if err := s.scheduler.UpdateJob(req.Name, req.Schedule, job); err != nil {
		// Job doesn't exist yet — disable any remnant and add fresh
		_ = s.scheduler.DisableJob(req.Name)
		if err := s.scheduler.AddJob(job); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
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
		if s.provider == nil {
			return nil, fmt.Errorf("docker provider unavailable for run job")
		}
		j := core.NewRunJob(s.provider)
		j.Name = req.Name
		j.Schedule = req.Schedule
		j.Command = req.Command
		j.Image = req.Image
		j.Container = req.Container
		return j, nil
	case "exec":
		if s.provider == nil {
			return nil, fmt.Errorf("docker provider unavailable for exec job")
		}
		j := core.NewExecJob(s.provider)
		j.Name = req.Name
		j.Schedule = req.Schedule
		j.Command = req.Command
		j.Container = req.Container
		return j, nil
	case "compose":
		// Validate compose job parameters
		validator := config.NewCommandValidator()
		if req.File != "" {
			if err := validator.ValidateFilePath(req.File); err != nil {
				return nil, fmt.Errorf("invalid compose file path: %w", err)
			}
		}
		if err := validator.ValidateServiceName(req.Service); err != nil {
			return nil, fmt.Errorf("invalid service name: %w", err)
		}
		if req.Command != "" {
			cmdArgs := args.GetArgs(req.Command)
			if err := validator.ValidateCommandArgs(cmdArgs); err != nil {
				return nil, fmt.Errorf("invalid command arguments: %w", err)
			}
		}
		j := &core.ComposeJob{}
		j.Name = req.Name
		j.Schedule = req.Schedule
		j.Command = req.Command
		j.File = req.File
		j.Service = req.Service
		j.Exec = req.ExecFlag
		return j, nil
	case "", "local":
		// Validate local job command if provided to prevent injection attacks
		// Note: Empty commands will be caught at runtime by LocalJob.buildCommand()
		if req.Command != "" {
			validator := config.NewCommandValidator()
			cmdArgs := args.GetArgs(req.Command)
			if err := validator.ValidateCommandArgs(cmdArgs); err != nil {
				return nil, fmt.Errorf("invalid command arguments: %w", err)
			}
		}
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

func stripJobs(cfg any) any {
	if cfg == nil {
		return nil
	}
	v := reflect.ValueOf(cfg)
	isPtr := false
	if v.Kind() == reflect.Pointer {
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
		// Get output streams using execution methods
		stdout := e.GetStdout()
		stderr := e.GetStderr()

		out = append(out, apiExecution{
			Date:     e.Date,
			Duration: e.Duration,
			Failed:   e.Failed,
			Skipped:  e.Skipped,
			Error:    errStr,
			Stdout:   stdout,
			Stderr:   stderr,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := extractToken(r)
	if token != "" && s.tokenManager != nil {
		s.tokenManager.RevokeToken(token)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "logged out"})
}

func (s *Server) authStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.authConfig == nil || !s.authConfig.Enabled {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authEnabled":   false,
			"authenticated": true,
		})
		return
	}

	token := extractToken(r)
	if token == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authEnabled":   true,
			"authenticated": false,
		})
		return
	}

	data, valid := s.tokenManager.ValidateToken(token)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"authEnabled":   true,
		"authenticated": valid,
		"username":      data.Username,
	})
}

func (s *Server) csrfTokenHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.tokenManager == nil {
		http.Error(w, "Auth not enabled", http.StatusNotFound)
		return
	}

	csrfToken, err := s.tokenManager.GenerateCSRFToken()
	if err != nil {
		http.Error(w, "Failed to generate CSRF token", http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"csrf_token": csrfToken})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if path == "/api/login" || path == "/api/csrf-token" || path == "/api/auth/status" ||
			path == "/health" || path == "/healthz" || path == "/ready" || path == "/live" {
			next.ServeHTTP(w, r)
			return
		}

		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		token := extractToken(r)
		if token == "" {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		data, valid := s.tokenManager.ValidateToken(token)
		if !valid {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		r.Header.Set("X-Auth-User", data.Username)
		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		return after
	}

	cookie, err := r.Cookie("auth_token")
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	return ""
}
