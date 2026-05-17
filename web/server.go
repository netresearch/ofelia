// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gobs/args"
	cron "github.com/netresearch/go-cron"

	"github.com/netresearch/ofelia/config"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/static"
)

type Server struct {
	addr           string
	scheduler      *core.Scheduler
	config         any
	srv            *http.Server
	origins        map[string]string
	originsMu      sync.RWMutex
	provider       core.DockerProvider
	authConfig     *SecureAuthConfig
	tokenManager   *SecureTokenManager
	loginLimiter   *RateLimiter
	rl             *rateLimiter
	trustedProxies []*net.IPNet
	// persistStore optionally tracks API-mutated state across daemon
	// restarts (#593). Nil-safe: methods on persist.Store are no-ops
	// when the store wasn't constructed with a path, so handlers don't
	// have to branch on configuration.
	persistStore *persist.Store
}

// SetPersistStore wires the daemon-owned persistence store into the
// server so create/update/delete/disable/enable handlers can record
// their mutations across restarts. Pass nil to disable persistence
// (the default). Safe to call only before Start.
func (s *Server) SetPersistStore(store *persist.Store) {
	s.persistStore = store
}

// Recognized origin tokens. Stored in s.origins (in-memory) or on
// the JobConfig.JobSource field (config-reflected via jobOrigin()).
// Used by the delete-gate and the persist hook to decide which jobs
// are config-owned (and thus immutable via API) vs API-mutated (and
// thus persistable + deletable).
//
// Pre-extraction these literals were sprinkled across six handler
// sites; consolidating them lets the type checker catch typos and
// makes the "what does this string mean" question one-grep-away.
const (
	originAPI   = "api"   // POST /api/jobs/{create,update} without an X-Origin header
	originWeb   = "web"   // X-Origin: web (sent by static/ui/index.html)
	originINI   = "ini"   // job-run "name" / job-exec "name" / etc. in the INI file
	originLabel = "label" // ofelia.job-run.<name>.* Docker labels
)

// isConfigOwned reports whether `origin` denotes a job whose
// authoritative source is the INI file or Docker labels. Such jobs
// are NOT deletable via the API and NOT persisted in the state file
// (their source is already durable elsewhere). Everything else —
// api, web, empty, future origins — is treated as API-mutated.
func isConfigOwned(origin string) bool {
	return origin == originINI || origin == originLabel
}

// MarkOriginAPI records that the named job came from the API/UI so
// the delete handler's origin gate (#593) recognizes it after a
// daemon restart. The persist loader in cli/daemon.go calls this for
// every job materialized from the state file — otherwise jobOrigin()
// would fall through to config reflection and either find the
// stale INI entry (refusing delete) or return "" (passing the gate
// only by the `origin != ""` clause, which any future tightening
// would silently break). Safe to call only before Start.
func (s *Server) MarkOriginAPI(name string) {
	s.originsMu.Lock()
	if s.origins == nil {
		s.origins = make(map[string]string)
	}
	s.origins[name] = originAPI
	s.originsMu.Unlock()
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

		// Parse trusted proxy CIDRs for X-Forwarded-For handling
		if len(authCfg.TrustedProxies) > 0 {
			tp, tpErr := ParseTrustedProxies(authCfg.TrustedProxies)
			if tpErr != nil {
				s.Logger.Error("failed to parse trusted proxies", "error", tpErr)
			} else {
				server.trustedProxies = tp
				s.Logger.Info("trusted proxies configured", "count", len(tp))
			}
		}
	}

	mux := http.NewServeMux()

	server.rl = newRateLimiter(100, time.Minute)
	server.rl.trustedProxies = server.trustedProxies

	if server.authConfig != nil && server.authConfig.Enabled {
		loginHandler := NewSecureLoginHandler(server.authConfig, server.tokenManager, server.loginLimiter, server.trustedProxies...)
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
	handler = server.rl.middleware(handler)

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
	if s.rl != nil {
		s.rl.close()
	}
	if s.tokenManager != nil {
		s.tokenManager.Close()
	}
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
		loginHandler := NewSecureLoginHandler(s.authConfig, s.tokenManager, s.loginLimiter, s.trustedProxies...)
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

	if s.rl != nil {
		s.rl.close()
	}
	s.rl = newRateLimiter(100, time.Minute)
	s.rl.trustedProxies = s.trustedProxies
	var handler http.Handler = mux
	handler = securityHeaders(handler)
	handler = s.rl.middleware(handler)

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
	s.originsMu.RLock()
	o, ok := s.origins[name]
	s.originsMu.RUnlock()
	if ok {
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
		// Triggered-only jobs (detected via cron.IsTriggered on the entry's schedule),
		// disabled (paused) jobs, and jobs without a cron entry return empty slices.
		var nextRuns, prevRuns []time.Time
		if s.scheduler.GetDisabledJob(job.GetName()) == nil {
			entry := s.scheduler.EntryByName(job.GetName())
			if entry.Valid() && entry.Schedule != nil && !cron.IsTriggered(entry.Schedule) {
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
	jobs := s.buildAPIJobs(s.scheduler.GetActiveJobs())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) removedJobsHandler(w http.ResponseWriter, _ *http.Request) {
	jobs := s.buildAPIJobs(s.scheduler.GetRemovedJobs())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) disabledJobsHandler(w http.ResponseWriter, _ *http.Request) {
	jobs := s.buildAPIJobs(s.scheduler.GetDisabledJobs())
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

// validateJobName checks that a job name is non-empty, not too long, and does
// not contain control characters.
func validateJobName(name string) error {
	if name == "" {
		return fmt.Errorf("job name must not be empty")
	}
	if len(name) > 256 {
		return fmt.Errorf("job name exceeds maximum length of 256 characters")
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return fmt.Errorf("job name contains invalid control character")
		}
	}
	return nil
}

func (s *Server) runJobHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.scheduler.RunJob(r.Context(), req.Name); err != nil {
		if errors.Is(err, core.ErrJobNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
		} else {
			s.scheduler.Logger.Error("run job failed", "job", req.Name, "error", err)
			http.Error(w, "failed to run job", http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) disableJobHandler(w http.ResponseWriter, r *http.Request) {
	s.toggleJobHandler(w, r, s.scheduler.DisableJob, "disable", s.persistDisable)
}

func (s *Server) enableJobHandler(w http.ResponseWriter, r *http.Request) {
	s.toggleJobHandler(w, r, s.scheduler.EnableJob, "enable", s.persistEnable)
}

// toggleJobHandler is the shared body for enable/disable. The
// `afterPersist` callback is invoked on success and records the new
// state to the persist.Store (#593); it is nil-safe through the
// persistDisable/persistEnable wrappers.
func (s *Server) toggleJobHandler(
	w http.ResponseWriter, r *http.Request,
	toggle func(string) error, action string,
	afterPersist func(string) error,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := toggle(req.Name); err != nil {
		if errors.Is(err, core.ErrJobNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
		} else {
			s.scheduler.Logger.Error(action+" job failed", "job", req.Name, "error", err)
			http.Error(w, "failed to "+action+" job", http.StatusInternalServerError)
		}
		return
	}
	// #593: persist the disable/enable state regardless of the job's
	// origin — operators can toggle INI/label jobs from the UI and
	// expect that pause to survive restart, even though they cannot
	// edit/delete the job through the API.
	if afterPersist != nil {
		if err := afterPersist(req.Name); err != nil {
			s.scheduler.Logger.Warn("persist "+action+" failed", "job", req.Name, "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// persistDisable / persistEnable are nil-safe wrappers around the
// persist.Store so handlers don't have to branch on s.persistStore at
// every call site.
func (s *Server) persistDisable(name string) error {
	if s.persistStore == nil {
		return nil
	}
	if err := s.persistStore.SetDisabled(name); err != nil {
		return fmt.Errorf("persist disable %q: %w", name, err)
	}
	return nil
}

func (s *Server) persistEnable(name string) error {
	if s.persistStore == nil {
		return nil
	}
	if err := s.persistStore.ClearDisabled(name); err != nil {
		return fmt.Errorf("persist enable %q: %w", name, err)
	}
	return nil
}

// persistJob writes the create/update job into the state-file via
// the persist.Store. Translates the API request struct into a
// persist.Job (omitting fields irrelevant to the job type so the
// on-disk shape stays clean). nil-safe.
func (s *Server) persistJob(name string, req *jobRequest) error {
	if s.persistStore == nil {
		return nil
	}
	j := persist.Job{Schedule: req.Schedule, Command: req.Command}
	switch req.Type {
	case "run":
		j.Type = persist.JobTypeRun
		j.Image = req.Image
		j.Container = req.Container
	case "exec":
		j.Type = persist.JobTypeExec
		j.Container = req.Container
	case "compose":
		j.Type = persist.JobTypeCompose
		j.File = req.File
		j.Service = req.Service
		j.Exec = req.ExecFlag
	case "", "local":
		j.Type = persist.JobTypeLocal
	default:
		return fmt.Errorf("unknown job type %q", req.Type)
	}
	if err := s.persistStore.PutJob(name, j); err != nil {
		return fmt.Errorf("persist job %q: %w", name, err)
	}
	return nil
}

func (s *Server) createJobHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	job, err := s.jobFromRequest(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.scheduler.AddJob(job); err != nil {
		s.scheduler.Logger.Error("create job failed", "job", req.Name, "error", err)
		http.Error(w, "failed to create job", http.StatusBadRequest)
		return
	}
	origin := r.Header.Get("X-Origin")
	if origin == "" {
		origin = originAPI
	}
	// Client-supplied X-Origin is treated as untrusted metadata: we
	// accept "api"/"web" verbatim but force any "ini"/"label" claim
	// to "api" so a malicious client can't mark its own job as
	// config-owned (which would later block legitimate API delete).
	// All requests reaching this handler are API mutations regardless
	// of what the header says.
	if isConfigOwned(origin) {
		origin = originAPI
	}
	s.originsMu.Lock()
	s.origins[req.Name] = origin
	s.originsMu.Unlock()
	// Persist (#593) any successful API mutation — origin only gates
	// what the delete handler can later remove, not whether the
	// create was API-initiated. Log + 201 on persist failure rather
	// than roll back the scheduler insert: the job is already live in
	// memory and rolling back would surprise callers who got a
	// successful response shape.
	if err := s.persistJob(req.Name, &req); err != nil {
		s.scheduler.Logger.Warn("persist created job failed", "job", req.Name, "error", err)
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) updateJobHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	job, err := s.jobFromRequest(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Try atomic update first; fall back to remove+add for new jobs
	status := http.StatusOK
	if err := s.scheduler.UpdateJob(req.Name, req.Schedule, job); err != nil {
		if !errors.Is(err, core.ErrJobNotFound) {
			s.scheduler.Logger.Error("update job failed", "job", req.Name, "error", err)
			http.Error(w, "failed to update job", http.StatusInternalServerError)
			return
		}
		// Job doesn't exist yet — remove any remnant and add fresh
		if old := s.scheduler.GetAnyJob(req.Name); old != nil {
			_ = s.scheduler.RemoveJob(old)
		}
		if err := s.scheduler.AddJob(job); err != nil {
			s.scheduler.Logger.Error("add job failed during update", "job", req.Name, "error", err)
			http.Error(w, "failed to create job", http.StatusBadRequest)
			return
		}
		status = http.StatusCreated
	}

	origin := r.Header.Get("X-Origin")
	if origin == "" {
		origin = originAPI
	}
	if isConfigOwned(origin) {
		origin = originAPI // see createJobHandler for the trust rationale
	}
	s.originsMu.Lock()
	s.origins[req.Name] = origin
	s.originsMu.Unlock()
	// Persist (#593) any successful API update — same rationale as create.
	if err := s.persistJob(req.Name, &req); err != nil {
		s.scheduler.Logger.Warn("persist updated job failed", "job", req.Name, "error", err)
	}
	w.WriteHeader(status)
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
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	j := s.scheduler.GetAnyJob(req.Name)
	if j == nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	// #593: only API-mutated jobs can be deleted. INI/Docker-label
	// jobs are owned by their source config — operators must edit the
	// source to remove them. Pre-#593 this handler would silently
	// remove from memory until next reload; the new behavior is to
	// reject explicitly so the operator sees the right error path.
	// Operators wanting to suppress an INI/label job temporarily
	// should use POST /api/jobs/disable instead, which now persists.
	if origin := s.jobOrigin(req.Name); isConfigOwned(origin) {
		http.Error(w,
			"job came from "+origin+" config; edit the source to delete it (or use /api/jobs/disable to suppress it)",
			http.StatusForbidden)
		return
	}
	_ = s.scheduler.RemoveJob(j)
	s.originsMu.Lock()
	delete(s.origins, req.Name)
	s.originsMu.Unlock()
	if s.persistStore != nil {
		if err := s.persistStore.RemoveJob(req.Name); err != nil {
			s.scheduler.Logger.Warn("persist remove job failed", "job", req.Name, "error", err)
		}
	}
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
	target := s.scheduler.GetAnyJob(name)
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
	username := ""
	if data != nil {
		username = data.Username
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"authEnabled":   true,
		"authenticated": valid,
		"username":      username,
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
