// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/creasty/defaults"
	"github.com/gobs/args"
	cron "github.com/netresearch/go-cron"

	"github.com/netresearch/ofelia/config"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
)

// Server is the HTTP front end of a running scheduler: the JSON API under
// /api, the embedded single-page UI, and — once RegisterHealthEndpoints has
// been called — the health and readiness probes. It owns the middleware chain
// (rate limiting, security headers, optional token auth) and the bookkeeping
// that decides which jobs the API is allowed to mutate.
//
// Build one with NewServer or NewServerWithAuth; the zero value is not usable.
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
	originWeb   = "web"   // X-Origin: web (sent by static/ui/app.js)
	originINI   = "ini"   // job-run "name" / job-exec "name" / etc. in the INI file
	originLabel = "label" // ofelia.job-run.<name>.* Docker labels
)

// HTTP route paths, header names, and error messages reused across handlers.
const (
	pathAPILogin      = "/api/login"
	pathAPIAuthStatus = "/api/auth/status"
	pathAPICSRFToken  = "/api/csrf-token" // #nosec G101 -- public API route path, not a credential
	pathAPIJobsPrefix = "/api/jobs/"

	headerContentType = "Content-Type"
	headerETag        = "ETag"
	contentTypeJSON   = "application/json"

	msgMethodNotAllowed   = "method not allowed"
	msgInvalidRequestBody = "invalid request body"
	msgJobNotFound        = "job not found"
)

// The job-type tokens the API speaks, in jobRequest.Type and in the type
// field of a job payload. Named because the literals occur in three
// switches here and again throughout the package's tests, which is
// enough occurrences for goconst to ask. The tests keep spelling the
// tokens out on purpose: one written against these constants would
// follow a wrong rename instead of catching it.
const (
	jobTypeRun     = "run"
	jobTypeExec    = "exec"
	jobTypeLocal   = "local"
	jobTypeService = "service"
	jobTypeCompose = "compose"
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

// NewServer builds an unauthenticated Server on addr for scheduler s. cfg is
// the parsed configuration served by /api/config and reflected over to tell
// config-owned jobs from API-created ones; provider is the Docker client the
// handlers use.
//
// This is NewServerWithAuth with no auth config: every route, including the
// job create/update/delete endpoints, is reachable without credentials, so
// only bind it where that is acceptable. Returns nil if construction failed.
func NewServer(addr string, s *core.Scheduler, cfg any, provider core.DockerProvider) *Server {
	return NewServerWithAuth(addr, s, cfg, provider, nil)
}

// setupAuth initializes the token manager, rate limiter, and trusted
// proxies on server from authCfg. Returns an error only when token
// manager construction fails (caller should return nil on error).
func setupAuth(server *Server, authCfg *SecureAuthConfig) error {
	tokenExpiry := authCfg.TokenExpiry
	if tokenExpiry == 0 {
		tokenExpiry = 24
	}
	tm, err := NewSecureTokenManager(authCfg.SecretKey, tokenExpiry)
	if err != nil {
		return err
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
			server.scheduler.Logger.Error("failed to parse trusted proxies", "error", tpErr)
		} else {
			server.trustedProxies = tp
			server.scheduler.Logger.Info("trusted proxies configured", "count", len(tp))
		}
	}
	return nil
}

// NewServerWithAuth builds a Server on addr and assembles its handler chain:
// the API routes and embedded UI, wrapped in a 100-request-per-minute per-IP
// rate limiter and the security headers, and — only when authCfg is non-nil
// and Enabled — token authentication plus the /api/login, /api/logout,
// /api/auth/status and /api/csrf-token endpoints. Authentication covers the
// /api/ routes only: the static UI, the login/CSRF/auth-status endpoints and
// the health probes stay reachable without a token. A nil or disabled authCfg
// leaves everything open; there is no partial mode.
//
// Returns nil, after logging the reason, when the token manager cannot be
// created or the embedded UI assets cannot be opened — callers must check for
// nil rather than assume a usable server. Nothing is listening until Start.
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
		if err := setupAuth(server, authCfg); err != nil {
			s.Logger.Error("failed to initialize token manager", "error", err)
			return nil
		}
	}

	server.rl = newRateLimiter(100, time.Minute)
	server.rl.trustedProxies = server.trustedProxies

	ui, err := uiHandler()
	if err != nil {
		server.scheduler.Logger.Error(fmt.Sprintf("failed to load UI subdirectory: %v", err))
		return nil
	}

	server.srv = &http.Server{
		Addr:              addr,
		Handler:           server.wrapMiddleware(server.newMux(nil, ui)),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return server
}

// route is one entry of the HTTP route table: the ServeMux pattern, the
// handler, and whether the endpoint stays reachable without a token while
// authentication is enabled.
//
// The table is the single source of truth for both registration sites
// (NewServerWithAuth and RegisterHealthEndpoints) and for
// TestRouteAuthExpectations, which replays every entry against the real
// middleware chain. Adding a route means declaring its auth expectation here.
type route struct {
	pattern string
	handler http.Handler
	public  bool
}

// routes returns the route table for the current auth configuration. hc is nil
// until RegisterHealthEndpoints runs, in which case the probe routes are
// omitted; ui is nil when the embedded assets could not be opened.
//
// public must match what authMiddleware actually exempts: the path is either in
// its allowlist or outside the /api/ prefix.
func (s *Server) routes(hc *HealthChecker, ui http.Handler) []route {
	rs := make([]route, 0, 17)

	if s.authConfig != nil && s.authConfig.Enabled {
		loginHandler := NewSecureLoginHandler(s.authConfig, s.tokenManager, s.loginLimiter, s.trustedProxies...)
		rs = append(rs,
			route{pathAPILogin, loginHandler, true},
			route{"/api/logout", http.HandlerFunc(s.logoutHandler), false},
			route{pathAPIAuthStatus, http.HandlerFunc(s.authStatusHandler), true},
			route{pathAPICSRFToken, http.HandlerFunc(s.csrfTokenHandler), true},
		)
	}

	rs = append(rs,
		route{"/api/jobs/removed", http.HandlerFunc(s.removedJobsHandler), false},
		route{"/api/jobs/disabled", http.HandlerFunc(s.disabledJobsHandler), false},
		route{"/api/jobs/run", http.HandlerFunc(s.runJobHandler), false},
		route{"/api/jobs/disable", http.HandlerFunc(s.disableJobHandler), false},
		route{"/api/jobs/enable", http.HandlerFunc(s.enableJobHandler), false},
		route{"/api/jobs/create", http.HandlerFunc(s.createJobHandler), false},
		route{"/api/jobs/update", http.HandlerFunc(s.updateJobHandler), false},
		route{"/api/jobs/delete", http.HandlerFunc(s.deleteJobHandler), false},
		route{pathAPIJobsPrefix, http.HandlerFunc(s.historyHandler), false},
		route{"/api/jobs", http.HandlerFunc(s.jobsHandler), false},
		route{"/api/config", http.HandlerFunc(s.configHandler), false},
		route{"/api/dashboard", http.HandlerFunc(s.dashboardHandler), false},
	)

	if hc != nil {
		rs = append(rs,
			route{"/health", hc.HealthHandler(), true},
			route{"/healthz", hc.HealthHandler(), true},
			route{"/ready", hc.ReadinessHandler(), true},
			route{"/live", hc.LivenessHandler(), true},
		)
	}

	if ui != nil {
		rs = append(rs, route{"/", ui, true})
	}

	return rs
}

// newMux registers the route table on a fresh ServeMux.
func (s *Server) newMux(hc *HealthChecker, ui http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	for _, rt := range s.routes(hc, ui) {
		mux.Handle(rt.pattern, rt.handler)
	}
	return mux
}

// Start serves in a background goroutine and returns immediately, always with
// a nil error. ListenAndServe's error is discarded, so a failure to bind addr
// shows up as refused connections rather than as a return value here — check
// reachability if you need to know the listener came up. Stop with Shutdown.
func (s *Server) Start() error { go func() { _ = s.srv.ListenAndServe() }(); return nil }

// Shutdown stops the background goroutines of the rate limiter and the token
// manager, then gracefully shuts the HTTP server down, letting in-flight
// requests finish until ctx expires. A Server that has been shut down cannot
// be started again.
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

// RegisterHealthEndpoints rebuilds the whole route table so that hc's /health,
// /healthz, /ready and /live endpoints sit next to the API and UI routes, and
// installs the result on the running http.Server. The probes stay exempt from
// authentication but still pass through the security headers and the rate
// limiter, which is replaced by a fresh one here, discarding the request
// counters accumulated so far.
//
// Call it before Start: it assigns http.Server.Handler, which is racy once the
// server is serving. It does nothing if the Server was not constructed
// successfully.
func (s *Server) RegisterHealthEndpoints(hc *HealthChecker) {
	if s.srv == nil || s.srv.Handler == nil {
		return
	}

	// A broken embedded UI must not take the probes down with it: register
	// everything else and drop only the "/" route.
	ui, err := uiHandler()
	if err != nil {
		ui = nil
	}
	mux := s.newMux(hc, ui)

	if s.rl != nil {
		s.rl.close()
	}
	s.rl = newRateLimiter(100, time.Minute)
	s.rl.trustedProxies = s.trustedProxies
	s.srv.Handler = s.wrapMiddleware(mux)
}

// wrapMiddleware layers the shared middleware chain around mux —
// compression innermost, then security headers, then auth when enabled,
// with the rate limiter outermost so a request rejected with 401 has
// still been counted (#804). Single source for both construction sites
// (NewServerWithAuth and RegisterHealthEndpoints) so the chain cannot
// drift between them.
func (s *Server) wrapMiddleware(mux http.Handler) http.Handler {
	handler := compressMiddleware(mux)
	handler = securityHeaders(handler)
	if s.authConfig != nil && s.authConfig.Enabled {
		handler = s.authMiddleware(handler)
	}
	// The limiter goes outside auth, so a request rejected with 401 has
	// still been counted. With the order reversed, /api/* token guessing
	// was the one traffic the limiter never saw: authMiddleware answers
	// it and returns, so a caller could try tokens without limit while
	// the same IP was being metered on every static asset it fetched.
	//
	// Nothing else changes. authMiddleware only guards /api/*, so the
	// rendered page and the static assets already passed through it and
	// were already counted; /live and /ready were exempt before the move
	// and stay exempt, because isOrchestratorProbePath returns early in
	// the limiter itself and does so from either position. See #804.
	handler = s.rl.middleware(handler)
	return handler
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
	// RecentRuns is a light outcome summary of the job's newest
	// executions (oldest first, at most recentRunCount entries) so list
	// views can show a result sparkline without fetching each job's full
	// history. Additive field; omitted when the job keeps no history.
	RecentRuns []apiRecentRun `json:"recentRuns,omitempty"`
}

// apiRecentRun is one entry of apiJob.RecentRuns.
type apiRecentRun struct {
	Date     time.Time     `json:"date"`
	Duration time.Duration `json:"duration"`
	Failed   bool          `json:"failed"`
	Skipped  bool          `json:"skipped"`
}

// recentRunCount caps apiJob.RecentRuns.
const recentRunCount = 10

// mapJobSource looks up name in the map field m and returns the
// JobSource string for that entry, if any. Returns ("", false) when
// m is not a valid map or the entry/field is absent.
func mapJobSource(m reflect.Value, name string) (string, bool) {
	if !m.IsValid() || m.Kind() != reflect.Map {
		return "", false
	}
	jv := m.MapIndex(reflect.ValueOf(name))
	if !jv.IsValid() {
		return "", false
	}
	if jv.Kind() == reflect.Pointer {
		if jv.IsNil() {
			return "", false
		}
		jv = jv.Elem()
	}
	src := jv.FieldByName("JobSource")
	if !src.IsValid() {
		return "", false
	}
	return src.String(), true
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
		if src, ok := mapJobSource(v.FieldByName(f), name); ok {
			return src
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

// guardConfigOwned answers 403 and reports true when the named job is
// owned by the INI file or by Docker labels. #593: such jobs are changed
// at their source, so create, update and delete all refuse them.
//
// The gate lives in one function because it was copy-pasted per handler
// and create simply did not get a copy — with AddJob no backstop, since a
// config job with an empty or malformed schedule holds no cron entry and
// so never triggers ErrDuplicateName. A create under its name replaced it
// with a caller-chosen job and recorded origin=api, after which update
// and delete stopped recognizing it as config-owned as well.
func (s *Server) guardConfigOwned(w http.ResponseWriter, name, verb string) bool {
	origin := s.jobOrigin(name)
	if !isConfigOwned(origin) {
		return false
	}
	http.Error(w,
		"job came from "+origin+" config; edit the source to "+verb+" it (or use /api/jobs/disable to suppress it)",
		http.StatusForbidden)
	return true
}

// requestOrigin returns the origin to record for an API mutation.
//
// Client-supplied X-Origin is untrusted metadata: "api"/"web" are taken
// verbatim but any "ini"/"label" claim is forced to "api", so a caller
// cannot mark its own job as config-owned (which would then block the
// legitimate API delete). Every request reaching a mutation handler is an
// API mutation regardless of what the header says.
func requestOrigin(r *http.Request) string {
	origin := r.Header.Get("X-Origin")
	if origin == "" || isConfigOwned(origin) {
		return originAPI
	}
	return origin
}

func jobType(j core.Job) string {
	switch j.(type) {
	case *core.RunJob:
		return jobTypeRun
	case *core.ExecJob:
		return jobTypeExec
	case *core.LocalJob:
		return jobTypeLocal
	case *core.RunServiceJob:
		return jobTypeService
	case *core.ComposeJob:
		return jobTypeCompose
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

// newAPIExecution converts a core.Execution into its API representation.
// Returns nil when lr is nil.
func newAPIExecution(lr *core.Execution) *apiExecution {
	if lr == nil {
		return nil
	}
	errStr := ""
	if lr.Error != nil {
		errStr = lr.Error.Error()
	}
	return &apiExecution{
		Date:     lr.Date,
		Duration: lr.Duration,
		Failed:   lr.Failed,
		Skipped:  lr.Skipped,
		Error:    errStr,
		Stdout:   lr.GetStdout(),
		Stderr:   lr.GetStderr(),
	}
}

// computeRunTimes returns the next/prev run-time slices for job.
// Triggered-only jobs, disabled (paused) jobs, and jobs without a cron
// entry return empty (non-nil) slices.
func (s *Server) computeRunTimes(job core.Job, now time.Time) (next, prev []time.Time) {
	if s.scheduler.GetDisabledJob(job.GetName()) == nil {
		entry := s.scheduler.EntryByName(job.GetName())
		if entry.Valid() && entry.Schedule != nil && !cron.IsTriggered(entry.Schedule) {
			// Anchor on the entry's own Next/Prev when the cron has them:
			// for interval (@every) schedules, Next(t) is t+interval with
			// no anchor, so projecting from `now` made the reported next
			// run drift forward on every poll.
			if !entry.Next.IsZero() {
				next = append([]time.Time{entry.Next}, cron.NextN(entry.Schedule, entry.Next, scheduleRunCount-1)...)
			} else {
				next = cron.NextN(entry.Schedule, now, scheduleRunCount)
			}
			if !entry.Prev.IsZero() {
				prev = append([]time.Time{entry.Prev}, cron.PrevN(entry.Schedule, entry.Prev, scheduleRunCount-1)...)
			} else {
				prev = cron.PrevN(entry.Schedule, now, scheduleRunCount)
			}
		}
	}
	if next == nil {
		next = []time.Time{}
	}
	if prev == nil {
		prev = []time.Time{}
	}
	return next, prev
}

// buildAPIJobs converts a slice of core.Job into apiJob payloads,
// including the recent-run summary the jobs table's sparkline and
// duration cell read.
func (s *Server) buildAPIJobs(list []core.Job) []apiJob {
	return s.buildAPIJobList(list, true)
}

// buildAPIRemovedJobs is buildAPIJobs without the recent-run summary.
// The removed tab shows a name, a type, a schedule and the last run;
// nothing there reads recentRuns, so computing and marshaling the
// newest runs of every removed job was work the 5s poll paid for on
// every tick and threw away.
func (s *Server) buildAPIRemovedJobs(list []core.Job) []apiJob {
	return s.buildAPIJobList(list, false)
}

func (s *Server) buildAPIJobList(list []core.Job, withRecentRuns bool) []apiJob {
	now := time.Now()
	jobs := make([]apiJob, 0, len(list))
	for _, job := range list {
		var execInfo *apiExecution
		if lrGetter, ok := job.(interface{ GetLastRun() *core.Execution }); ok {
			execInfo = newAPIExecution(lrGetter.GetLastRun())
		}

		var recent []apiRecentRun
		if withRecentRuns {
			hist := job.GetHistory()
			start := 0
			if len(hist) > recentRunCount {
				start = len(hist) - recentRunCount
			}
			for _, e := range hist[start:] {
				recent = append(recent, apiRecentRun{Date: e.Date, Duration: e.Duration, Failed: e.Failed, Skipped: e.Skipped})
			}
		}

		nextRuns, prevRuns := s.computeRunTimes(job, now)
		origin := s.jobOrigin(job.GetName())
		cfgBytes, _ := json.Marshal(job)
		jobs = append(jobs, apiJob{
			Name:       job.GetName(),
			Type:       jobType(job),
			Schedule:   job.GetSchedule(),
			Command:    job.GetCommand(),
			Running:    s.scheduler.IsJobRunning(job.GetName()),
			LastRun:    execInfo,
			NextRuns:   nextRuns,
			PrevRuns:   prevRuns,
			Origin:     origin,
			Config:     cfgBytes,
			RecentRuns: recent,
		})
	}
	return jobs
}

func (s *Server) jobsHandler(w http.ResponseWriter, _ *http.Request) {
	jobs := s.buildAPIJobs(s.scheduler.GetActiveJobs())
	w.Header().Set(headerContentType, contentTypeJSON)
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) removedJobsHandler(w http.ResponseWriter, _ *http.Request) {
	jobs := s.buildAPIRemovedJobs(s.scheduler.GetRemovedJobs())
	w.Header().Set(headerContentType, contentTypeJSON)
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) disabledJobsHandler(w http.ResponseWriter, _ *http.Request) {
	jobs := s.buildAPIJobs(s.scheduler.GetDisabledJobs())
	w.Header().Set(headerContentType, contentTypeJSON)
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
	// MaxRuntime is a Go duration string (e.g. "30m"), run-jobs only —
	// see core.ParseMaxRuntime. Empty means "no per-job override": the
	// job then inherits `[global] max-runtime`, and only falls through
	// to the scheduler's 24h default (defaultJobMaxRuntime) when there
	// is no global either. "0s" is equivalent to omitting it: it is not
	// a way to ask for no bound.
	//
	// That is the same ladder a config.ini [job-run] section climbs
	// (cli/config.go, registerAllJobs). It used to stop one rung short
	// here — an API-created job never saw the global, so an operator who
	// set one got it for INI run jobs and 24h for API ones — which was
	// issue #806.
	MaxRuntime string `json:"maxRuntime,omitempty"`
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
		http.Error(w, msgMethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, msgInvalidRequestBody, http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.scheduler.RunJob(r.Context(), req.Name); err != nil {
		if errors.Is(err, core.ErrJobNotFound) {
			http.Error(w, msgJobNotFound, http.StatusNotFound)
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
		http.Error(w, msgMethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, msgInvalidRequestBody, http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := toggle(req.Name); err != nil {
		if errors.Is(err, core.ErrJobNotFound) {
			http.Error(w, msgJobNotFound, http.StatusNotFound)
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
	case jobTypeRun:
		j.Type = persist.JobTypeRun
		j.Image = req.Image
		j.Container = req.Container
		j.MaxRuntime = req.MaxRuntime
	case jobTypeExec:
		j.Type = persist.JobTypeExec
		j.Container = req.Container
	case jobTypeCompose:
		j.Type = persist.JobTypeCompose
		j.File = req.File
		j.Service = req.Service
		j.Exec = req.ExecFlag
	case "", jobTypeLocal:
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
		http.Error(w, msgMethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, msgInvalidRequestBody, http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Same policy as update and delete (#593): a name the config owns is
	// not available to the API, whether or not the scheduler managed to
	// register it.
	if s.guardConfigOwned(w, req.Name, "create") {
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
	s.originsMu.Lock()
	s.origins[req.Name] = requestOrigin(r)
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
		http.Error(w, msgMethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, msgInvalidRequestBody, http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Same policy as delete (#593): config-owned jobs are changed at
	// their source. Without this gate an update silently overrode the
	// job in memory until the next config sync — and rewrote the job's
	// origin to api/web, after which the delete gate no longer saw the
	// job as config-owned: editing a label job unlocked deleting it.
	if s.guardConfigOwned(w, req.Name, "change") {
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

	s.originsMu.Lock()
	s.origins[req.Name] = requestOrigin(r)
	s.originsMu.Unlock()
	// Persist (#593) any successful API update — same rationale as create.
	if err := s.persistJob(req.Name, &req); err != nil {
		s.scheduler.Logger.Warn("persist updated job failed", "job", req.Name, "error", err)
	}
	w.WriteHeader(status)
}

// GlobalMaxRuntimeProvider is satisfied by the configuration the daemon
// hands NewServer. The server holds that configuration as `any` to keep
// this package from importing cli, so this is how an API-created run job
// reaches `[global] max-runtime`.
//
// Implemented by *cli.Config; cli asserts the link at compile time so a
// rename cannot quietly reduce this to the nil case.
type GlobalMaxRuntimeProvider interface {
	GlobalMaxRuntime() time.Duration
}

// globalMaxRuntime returns the operator's `[global] max-runtime`, or zero
// when the server was built without a configuration that exposes one --
// which is every test that passes nil, and any embedder that does not
// implement the interface.
//
// The typed-nil check is not ceremony: config arrives as `any` through an
// exported constructor, and a nil *cli.Config stored in one satisfies the
// assertion while panicking on the call. Falling back to zero is what a
// caller who has no configuration meant.
func (s *Server) globalMaxRuntime() time.Duration {
	p, ok := s.config.(GlobalMaxRuntimeProvider)
	if !ok || isNilPointer(p) {
		return 0
	}
	return p.GlobalMaxRuntime()
}

// isNilPointer reports whether v holds a nil pointer, which a plain
// `v == nil` misses once the value carries a type.
func isNilPointer(v any) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func (s *Server) newRunJobFromRequest(req *jobRequest) (core.Job, error) {
	if s.provider == nil {
		return nil, fmt.Errorf("docker provider unavailable for run job")
	}
	j := core.NewRunJob(s.provider)
	j.Name = req.Name
	j.Schedule = req.Schedule
	j.Command = req.Command
	j.Image = req.Image
	j.Container = req.Container
	maxRuntime, err := core.ParseMaxRuntime(req.MaxRuntime)
	if err != nil {
		return nil, fmt.Errorf("invalid maxRuntime: %w", err)
	}
	// Same rule registerAllJobs applies to an INI run job: a job that
	// named no bound of its own inherits the operator's global, and only
	// falls through to the scheduler's 24h constant when there is no
	// global either. "0s" parses to zero and therefore inherits too,
	// which is what makes it equivalent to omitting the field (#789)
	// rather than a way to ask for no bound. See #806.
	if maxRuntime == 0 {
		maxRuntime = s.globalMaxRuntime()
	}
	j.MaxRuntime = maxRuntime
	return j, nil
}

func (s *Server) newExecJobFromRequest(req *jobRequest) (core.Job, error) {
	if s.provider == nil {
		return nil, fmt.Errorf("docker provider unavailable for exec job")
	}
	j := core.NewExecJob(s.provider)
	j.Name = req.Name
	j.Schedule = req.Schedule
	j.Command = req.Command
	j.Container = req.Container
	return j, nil
}

// newComposeJobFromRequest validates compose job parameters and builds the job.
func newComposeJobFromRequest(req *jobRequest) (core.Job, error) {
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
}

// newLocalJobFromRequest validates local job command and builds the job.
// Note: Empty commands will be caught at runtime by LocalJob.buildCommand()
func newLocalJobFromRequest(req *jobRequest) (core.Job, error) {
	if req.Command != "" {
		// Validate local job command if provided to prevent injection attacks
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
}

func (s *Server) jobFromRequest(req *jobRequest) (core.Job, error) {
	var (
		job core.Job
		err error
	)
	switch req.Type {
	case jobTypeRun:
		job, err = s.newRunJobFromRequest(req)
	case jobTypeExec:
		job, err = s.newExecJobFromRequest(req)
	case jobTypeCompose:
		job, err = newComposeJobFromRequest(req)
	case "", jobTypeLocal:
		job, err = newLocalJobFromRequest(req)
	default:
		return nil, fmt.Errorf("unknown job type %q", req.Type)
	}
	if err != nil {
		return nil, err
	}
	// Struct-tag defaults are applied by the config decoder, not by the
	// constructors, so an API-built job used to keep every zero value —
	// most damagingly HistoryLimit 0, which makes BareJob.GetHistory
	// retain every Execution (up to 2×10 MB of output buffers each) for
	// the lifetime of the daemon. Only the run-job constructor applied
	// them; doing it here covers every type and every future one.
	// creasty/defaults fills initial values only, so the fields the
	// request set above are left alone.
	_ = defaults.Set(job)
	return job, nil
}

func (s *Server) deleteJobHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, msgMethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, msgInvalidRequestBody, http.StatusBadRequest)
		return
	}
	if err := validateJobName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	j := s.scheduler.GetAnyJob(req.Name)
	if j == nil {
		http.Error(w, msgJobNotFound, http.StatusNotFound)
		return
	}
	// #593: only API-mutated jobs can be deleted. INI/Docker-label
	// jobs are owned by their source config — operators must edit the
	// source to remove them. Pre-#593 this handler would silently
	// remove from memory until next reload; the new behavior is to
	// reject explicitly so the operator sees the right error path.
	// Operators wanting to suppress an INI/label job temporarily
	// should use POST /api/jobs/disable instead, which now persists.
	if s.guardConfigOwned(w, req.Name, "delete") {
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
	w.Header().Set(headerContentType, contentTypeJSON)
	cfg := stripJobs(s.config)
	_ = json.NewEncoder(w).Encode(cfg)
}

// isJobCollection reports whether t has the shape of a job collection: a map
// keyed by job name whose values are job-config structs. Every collection in
// cli.Config is map[string]*XxxJobConfig and no other field shares that shape.
//
// Matching on shape rather than on a list of field names is what keeps a newly
// added collection stripped: the name list this replaced had already drifted
// (it carried a field that no longer existed and missed ComposeJobs), and a
// drifted list ships every job's full definition to any /api/config reader.
func isJobCollection(t reflect.Type) bool {
	if t.Kind() != reflect.Map || t.Key().Kind() != reflect.String {
		return false
	}
	elem := t.Elem()
	if elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}
	return elem.Kind() == reflect.Struct
}

// stripJobs returns a copy of cfg with every job collection zeroed. Job
// definitions carry commands and credential-bearing fields, and the jobs API
// already exposes what the UI needs, so they must not ride along in the config
// payload of /api/config and /api/dashboard.
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
	for i := range out.NumField() {
		if fv := out.Field(i); fv.CanSet() && isJobCollection(fv.Type()) {
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

// buildAPIHistory converts the named job's execution history into API
// payloads. ok is false when the job does not exist. Shared by the
// per-job history endpoint and the dashboard aggregate.
func (s *Server) buildAPIHistory(name string) (out []apiExecution, ok bool) {
	target := s.scheduler.GetAnyJob(name)
	if target == nil {
		return nil, false
	}
	hist := target.GetHistory()
	out = make([]apiExecution, 0, len(hist))
	for _, e := range hist {
		if a := newAPIExecution(e); a != nil {
			out = append(out, *a)
		}
	}
	return out, true
}

func (s *Server) historyHandler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/history") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, pathAPIJobsPrefix), "/history")
	out, ok := s.buildAPIHistory(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set(headerContentType, contentTypeJSON)
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

	// Mirrors the attributes used when the cookie was issued so the browser
	// actually replaces it; see the issuing site in auth_secure.go.
	// #nosec G124 -- Secure is set from the request scheme, MaxAge -1 clears the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	// Explicit Content-Type before WriteHeader: the compression wrapper
	// can only sniff a missing type on the first body write, which the
	// explicit WriteHeader here skips — without this the sniff runs on
	// the compressed bytes and answers with the codec's own type
	// (application/x-gzip, application/zstd) instead of JSON. See
	// health.go's LivenessHandler for the same trap.
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "logged out"})
}

func (s *Server) authStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(headerContentType, contentTypeJSON)

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
	w.Header().Set(headerContentType, contentTypeJSON)

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

		if path == pathAPILogin || path == pathAPICSRFToken || path == pathAPIAuthStatus ||
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
