// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // #nosec G108
	"sync"
	"time"

	"github.com/gobs/args"

	cfgvalidator "github.com/netresearch/ofelia/config"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/web"
)

// Default listen addresses for the web UI and pprof server. These mirror
// the `default:` struct-tag values below; we keep them as named constants
// so the "is the user still on the default?" comparisons elsewhere don't
// hardcode the same literal.
const (
	defaultWebAddr   = ":8081"
	defaultPprofAddr = "127.0.0.1:8080"
)

// DaemonCommand daemon process
type DaemonCommand struct {
	ConfigFile           string         `long:"config" env:"OFELIA_CONFIG" description:"Config file path" default:"/etc/ofelia/config.ini"`
	DockerFilters        []string       `short:"f" long:"docker-filter" env:"OFELIA_DOCKER_FILTER" description:"Docker container filter"`
	DockerPollInterval   *time.Duration `long:"docker-poll-interval" env:"OFELIA_POLL_INTERVAL" description:"Docker label poll interval"`
	DockerUseEvents      *bool          `long:"docker-events" env:"OFELIA_DOCKER_EVENTS" description:"Use Docker events for changes"`
	DockerNoPoll         *bool          `long:"docker-no-poll" env:"OFELIA_DOCKER_NO_POLL" description:"Disable Docker label polling"`
	DockerIncludeStopped *bool          `long:"docker-include-stopped" env:"OFELIA_DOCKER_INCLUDE_STOPPED" description:"Include stopped containers when reading Docker labels"` //nolint:revive
	LogLevel             string         `long:"log-level" env:"OFELIA_LOG_LEVEL" description:"Log level (trace,debug,info,warn,error)"`
	EnablePprof          bool           `long:"enable-pprof" env:"OFELIA_ENABLE_PPROF" description:"Enable pprof server"`
	PprofAddr            string         `long:"pprof-address" env:"OFELIA_PPROF_ADDRESS" description:"Pprof addr" default:"127.0.0.1:8080"`
	EnableWeb            bool           `long:"enable-web" env:"OFELIA_ENABLE_WEB" description:"Enable web UI"`
	WebAddr              string         `long:"web-address" env:"OFELIA_WEB_ADDRESS" description:"Web UI address" default:":8081"`
	WebAuthEnabled       bool           `long:"web-auth-enabled" env:"OFELIA_WEB_AUTH_ENABLED" description:"Enable web UI auth"`
	WebUsername          string         `long:"web-username" env:"OFELIA_WEB_USERNAME" description:"Web UI auth username"`
	WebPasswordHash      string         `long:"web-password-hash" env:"OFELIA_WEB_PASSWORD_HASH" description:"Bcrypt hash" default-mask:"-"`
	WebSecretKey         string         `long:"web-secret-key" env:"OFELIA_WEB_SECRET_KEY" description:"JWT signing key" default-mask:"-"`
	WebTokenExpiry       int            `long:"web-token-expiry" env:"OFELIA_WEB_TOKEN_EXPIRY" description:"Token expiry hours" default:"24"`                             //nolint:revive
	WebMaxLoginAttempts  int            `long:"web-max-login-attempts" env:"OFELIA_WEB_MAX_LOGIN_ATTEMPTS" description:"Lockout" default:"5"`                             //nolint:revive
	WebTrustedProxies    []string       `long:"web-trusted-proxies" env:"OFELIA_WEB_TRUSTED_PROXIES" env-delim:"," description:"Trusted proxy CIDRs for X-Forwarded-For"` //nolint:revive

	// Docker startup-retry knobs (#523). Placed at the end of the field
	// block so the long descriptions don't push the existing columns past
	// the line-length limit when gofmt aligns struct-tag columns.
	DockerStartupRetryCount    *int           `long:"docker-startup-retry-count" env:"OFELIA_DOCKER_STARTUP_RETRY_COUNT" description:"Extra Docker connection attempts on startup beyond the initial ping (default 0)"` //nolint:revive,lll
	DockerStartupRetryInterval *time.Duration `long:"docker-startup-retry-interval" env:"OFELIA_DOCKER_STARTUP_RETRY_INTERVAL" description:"Base interval for Docker startup retries; doubles each attempt"`            //nolint:revive,lll

	// StateFile enables persistence of API-mutated state (jobs and
	// disable flags) across daemon restarts. Empty (the default) means
	// no persistence — every API-created job is lost on restart, the
	// pre-#593 behavior. See [persist] for format and semantics.
	StateFile string `long:"state-file" env:"OFELIA_STATE_FILE" description:"Path to JSON state file for API-mutated jobs and disable flags (#593); empty disables persistence" default-mask:"-"` //nolint:revive,lll

	scheduler       *core.Scheduler
	pprofServer     *http.Server
	webServer       *web.Server
	dockerHandler   *DockerHandler
	config          *Config
	done            chan struct{}
	doneOnce        sync.Once // protects done channel close
	Logger          *slog.Logger
	LevelVar        *slog.LevelVar
	shutdownManager *core.ShutdownManager
	healthChecker   *web.HealthChecker
	persistStore    *persist.Store // #593; nil when --state-file is empty
}

// closeDone safely closes the done channel at most once, preventing
// double-close panics when multiple goroutines detect errors concurrently.
func (c *DaemonCommand) closeDone() {
	c.doneOnce.Do(func() { close(c.done) })
}

// Execute runs the daemon
func (c *DaemonCommand) Execute(_ []string) error {
	if err := c.boot(); err != nil {
		return err
	}

	if err := c.start(); err != nil {
		return err
	}
	return c.shutdown()
}

func (c *DaemonCommand) boot() (err error) {
	// Initialize done channel for clean shutdown
	c.done = make(chan struct{})

	// Apply CLI log level before reading config
	if err := ApplyLogLevel(c.LogLevel, c.LevelVar); err != nil {
		c.Logger.Error(fmt.Sprintf("Failed to apply log level: %v", err))
		return fmt.Errorf("invalid log level configuration: %w", err)
	}

	// Initialize shutdown manager
	c.shutdownManager = core.NewShutdownManager(c.Logger, 30*time.Second)

	// Always try to read the config file, as there are options such as globals or some tasks that can be specified there and not in docker
	config, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		c.Logger.Warn(fmt.Sprintf("Could not load config file %q: %v", c.ConfigFile, err))
		// Create an empty config if loading failed
		config = NewConfig(c.Logger)
	}
	config.levelVar = c.LevelVar
	c.applyOptions(config)
	c.applyConfigDefaults(config)

	c.pprofServer = &http.Server{
		Addr:              c.PprofAddr,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if c.LogLevel == "" {
		if err := ApplyLogLevel(config.Global.LogLevel, c.LevelVar); err != nil {
			c.Logger.Warn(fmt.Sprintf("Failed to apply config log level (using default): %v", err))
		}
	}

	err = config.InitializeApp()
	if err != nil {
		c.Logger.Error(fmt.Sprintf("Can't start the app: %v", err))
	}
	// Re-apply CLI/environment options so they override Docker labels
	c.applyOptions(config)
	c.scheduler = config.sh

	// Restore job history from saved files if configured
	c.restoreJobHistory(config)
	c.dockerHandler = config.dockerHandler
	c.config = config

	// Initialize health checker with Docker provider
	var dockerProvider core.DockerProvider
	if c.dockerHandler != nil {
		dockerProvider = c.dockerHandler.GetDockerProvider()
	}
	c.healthChecker = web.NewHealthChecker(dockerProvider, "1.0.0")

	// Create graceful scheduler with shutdown support
	gracefulScheduler := core.NewGracefulScheduler(c.scheduler, c.shutdownManager)
	c.scheduler = gracefulScheduler.Scheduler

	// #593: load persisted API-mutated state and apply to the live
	// scheduler before the web server starts accepting requests. The
	// scheduler now sees: INI jobs + Docker label jobs + persisted API
	// jobs (last). Persisted disable flags are applied on top so a
	// paused INI/label job stays paused across restart.
	if err := c.initPersistStore(); err != nil {
		return fmt.Errorf("load state file: %w", err)
	}

	if c.EnableWeb {
		if err := c.setupWebServer(); err != nil {
			return err
		}
	}

	return err
}

// setupWebServer initializes the web server, authentication, persistence
// wiring and health endpoints. Only called when web support is enabled.
func (c *DaemonCommand) setupWebServer() error {
	var provider core.DockerProvider
	if c.dockerHandler != nil {
		provider = c.dockerHandler.GetDockerProvider()
	}

	var authCfg *web.SecureAuthConfig
	if c.WebAuthEnabled {
		var err error
		authCfg, err = c.buildWebAuthConfig()
		if err != nil {
			return err
		}
	}

	c.webServer = web.NewServerWithAuth(c.WebAddr, c.scheduler, c.config, provider, authCfg)
	if c.webServer == nil {
		return fmt.Errorf("failed to initialize web server (check logs for details)")
	}

	// #593: wire the persist store into the web server so API
	// create/update/delete/disable/enable handlers record their
	// mutations to disk. The store was already loaded earlier in
	// boot (see initPersistStore) so the live scheduler already
	// reflects whatever the file contained; from this point on,
	// the store mirrors live mutations forward to disk.
	if c.persistStore != nil {
		c.webServer.SetPersistStore(c.persistStore)
		// Re-establish origin="api" for every persisted job so the
		// delete-gate (web/server.go) recognizes them as
		// API-deletable after restart — otherwise jobOrigin() would
		// return "" and any future tightening of the gate would
		// silently break delete for persisted jobs.
		for name := range c.persistStore.Snapshot().Jobs {
			c.webServer.MarkOriginAPI(name)
		}
	}

	c.webServer.RegisterHealthEndpoints(c.healthChecker)

	gracefulServer := core.NewGracefulServer(c.webServer.GetHTTPServer(), c.shutdownManager, c.Logger)
	_ = gracefulServer
	return nil
}

// buildWebAuthConfig builds the web authentication configuration from the
// daemon's web-auth flags. Only called when web auth is enabled; it returns
// an error when a required credential is missing.
func (c *DaemonCommand) buildWebAuthConfig() (*web.SecureAuthConfig, error) {
	if c.WebUsername == "" {
		return nil, ErrWebAuthUsername
	}
	if c.WebPasswordHash == "" {
		return nil, ErrWebAuthPassword
	}
	if c.WebSecretKey == "" {
		c.Logger.Warn("No web-secret-key provided. " +
			"Auth tokens will not survive daemon restarts. " +
			"Set OFELIA_WEB_SECRET_KEY for persistent sessions.")
	}
	return &web.SecureAuthConfig{
		Enabled:        true,
		Username:       c.WebUsername,
		PasswordHash:   c.WebPasswordHash,
		SecretKey:      c.WebSecretKey,
		TokenExpiry:    c.WebTokenExpiry,
		MaxAttempts:    c.WebMaxLoginAttempts,
		TrustedProxies: c.WebTrustedProxies,
	}, nil
}

func (c *DaemonCommand) start() error {
	// Start listening for shutdown signals
	c.shutdownManager.ListenForShutdown()

	// Set up a goroutine to close done channel when shutdown completes
	go func() {
		<-c.shutdownManager.ShutdownChan()
		// Give some time for graceful shutdown to complete
		// The shutdown manager handles the actual shutdown process
		c.closeDone()
	}()

	// Start scheduler with progress feedback
	c.Logger.Info("Starting scheduler...")

	if err := c.scheduler.Start(); err != nil {
		c.Logger.Error("Failed to start scheduler")
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return fmt.Errorf("failed to start scheduler: %w\n  → Check all job schedules are valid cron expressions\n  → Verify no duplicate job names exist\n  → Use 'ofelia validate --config=%q' to check configuration\n  → Check Docker daemon is running if using Docker jobs\n  → Review logs above for specific job errors", err, c.ConfigFile)
	}

	jobCount := 0
	if c.config != nil {
		jobCount = len(c.config.RunJobs) + len(c.config.LocalJobs) +
			len(c.config.ExecJobs) + len(c.config.ServiceJobs) + len(c.config.ComposeJobs)
	}
	c.Logger.Info("Scheduler started", "jobCount", jobCount)

	if c.EnablePprof {
		c.Logger.Info(fmt.Sprintf("Starting pprof server on %s...", c.PprofAddr))
		pprofErrChan := make(chan error, 1)
		go func() {
			if err := c.pprofServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				c.Logger.Error(fmt.Sprintf("Error starting HTTP server: %v", err))
				pprofErrChan <- err
				c.closeDone()
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := waitForServerWithErrChan(ctx, c.PprofAddr, pprofErrChan); err != nil {
			c.Logger.Error(fmt.Sprintf("pprof server failed to start: %v", err))
			return fmt.Errorf("pprof server startup failed: %w", err)
		}
		c.Logger.Info(fmt.Sprintf("pprof server ready on %s", c.PprofAddr))
	} else {
		c.Logger.Info("pprof server disabled")
	}

	if c.EnableWeb {
		c.Logger.Info(fmt.Sprintf("Starting web server on %s...", c.WebAddr))
		webErrChan := make(chan error, 1)
		go func() {
			if err := c.webServer.Start(); err != nil {
				c.Logger.Error(fmt.Sprintf("Error starting web server: %v", err))
				webErrChan <- err
				c.closeDone()
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := waitForServerWithErrChan(ctx, c.WebAddr, webErrChan); err != nil {
			c.Logger.Error(fmt.Sprintf("web server failed to start: %v", err))
			return fmt.Errorf("web server startup failed: %w", err)
		}
		c.Logger.Info(fmt.Sprintf("Web UI ready at http://%s", c.WebAddr))
	} else {
		c.Logger.Info("web server disabled")
	}

	c.Logger.Info("Ofelia is now running. Press Ctrl+C to stop.")

	return nil
}

func (c *DaemonCommand) shutdown() error {
	<-c.done
	// Shutdown manager handles everything through registered hooks
	return nil
}

func (c *DaemonCommand) applyOptions(config *Config) {
	if config == nil {
		return
	}
	if len(c.DockerFilters) > 0 {
		config.Docker.Filters = c.DockerFilters
	}
	if c.DockerPollInterval != nil {
		config.Docker.PollInterval = *c.DockerPollInterval
	}
	if c.DockerUseEvents != nil {
		config.Docker.UseEvents = *c.DockerUseEvents
	}
	if c.DockerNoPoll != nil {
		config.Docker.DisablePolling = *c.DockerNoPoll
	}
	if c.DockerIncludeStopped != nil {
		config.Docker.IncludeStopped = *c.DockerIncludeStopped
	}
	if c.DockerStartupRetryCount != nil {
		config.Docker.StartupRetryCount = *c.DockerStartupRetryCount
	}
	if c.DockerStartupRetryInterval != nil {
		config.Docker.StartupRetryInterval = *c.DockerStartupRetryInterval
	}

	c.applyWebOptions(config)
	c.applyAuthOptions(config)
	c.applyServerOptions(config)
}

func (c *DaemonCommand) applyWebOptions(config *Config) {
	if c.EnableWeb {
		config.Global.EnableWeb = true
	}
	if c.WebAddr != defaultWebAddr {
		config.Global.WebAddr = c.WebAddr
	}
}

func (c *DaemonCommand) applyAuthOptions(config *Config) {
	if c.WebAuthEnabled {
		config.Global.WebAuthEnabled = true
	}
	if c.WebUsername != "" {
		config.Global.WebUsername = c.WebUsername
	}
	if c.WebPasswordHash != "" {
		config.Global.WebPasswordHash = c.WebPasswordHash
	}
	if c.WebSecretKey != "" {
		config.Global.WebSecretKey = c.WebSecretKey
	}
	if c.WebTokenExpiry != 24 {
		config.Global.WebTokenExpiry = c.WebTokenExpiry
	}
	if c.WebMaxLoginAttempts != 5 {
		config.Global.WebMaxLoginAttempts = c.WebMaxLoginAttempts
	}
	if len(c.WebTrustedProxies) > 0 {
		config.Global.WebTrustedProxies = c.WebTrustedProxies
	}
}

func (c *DaemonCommand) applyServerOptions(config *Config) {
	if c.EnablePprof {
		config.Global.EnablePprof = true
	}
	if c.PprofAddr != defaultPprofAddr {
		config.Global.PprofAddr = c.PprofAddr
	}
	if c.LogLevel != "" {
		config.Global.LogLevel = c.LogLevel
	}
}

// Config returns the active configuration used by the daemon.
func (c *DaemonCommand) Config() *Config {
	return c.config
}

func (c *DaemonCommand) applyConfigDefaults(config *Config) {
	c.applyWebDefaults(config)
	c.applyAuthDefaults(config)
	c.applyServerDefaults(config)
}

func (c *DaemonCommand) applyWebDefaults(config *Config) {
	if !c.EnableWeb {
		c.EnableWeb = config.Global.EnableWeb
	}
	if c.WebAddr == defaultWebAddr && config.Global.WebAddr != "" {
		c.WebAddr = config.Global.WebAddr
	}
}

func (c *DaemonCommand) applyAuthDefaults(config *Config) {
	if !c.WebAuthEnabled {
		c.WebAuthEnabled = config.Global.WebAuthEnabled
	}
	if c.WebUsername == "" && config.Global.WebUsername != "" {
		c.WebUsername = config.Global.WebUsername
	}
	if c.WebPasswordHash == "" && config.Global.WebPasswordHash != "" {
		c.WebPasswordHash = config.Global.WebPasswordHash
	}
	if c.WebSecretKey == "" && config.Global.WebSecretKey != "" {
		c.WebSecretKey = config.Global.WebSecretKey
	}
	if c.WebTokenExpiry == 24 && config.Global.WebTokenExpiry != 0 {
		c.WebTokenExpiry = config.Global.WebTokenExpiry
	}
	if c.WebMaxLoginAttempts == 5 && config.Global.WebMaxLoginAttempts != 0 {
		c.WebMaxLoginAttempts = config.Global.WebMaxLoginAttempts
	}
	if len(c.WebTrustedProxies) == 0 && len(config.Global.WebTrustedProxies) > 0 {
		c.WebTrustedProxies = config.Global.WebTrustedProxies
	}
}

func (c *DaemonCommand) applyServerDefaults(config *Config) {
	if !c.EnablePprof {
		c.EnablePprof = config.Global.EnablePprof
	}
	if c.PprofAddr == defaultPprofAddr && config.Global.PprofAddr != "" {
		c.PprofAddr = config.Global.PprofAddr
	}
}

// restoreJobHistory restores job history from saved files if configured.
func (c *DaemonCommand) restoreJobHistory(config *Config) {
	if !config.Global.SaveConfig.RestoreHistoryEnabled() {
		return
	}
	saveFolder := config.Global.SaveConfig.SaveFolder
	maxAge := config.Global.SaveConfig.GetRestoreHistoryMaxAge()
	if err := middlewares.RestoreHistory(saveFolder, maxAge, c.scheduler.Jobs, c.Logger); err != nil {
		c.Logger.Warn(fmt.Sprintf("Failed to restore job history: %v", err))
	}
}

func waitForServerWithErrChan(ctx context.Context, addr string, errChan <-chan error) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server: %w", ctx.Err())
		case err := <-errChan:
			if err != nil {
				return fmt.Errorf("server failed to start: %w", err)
			}
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
	}
}

// initPersistStore constructs the persist.Store from the configured
// --state-file path, loads its contents, and applies persisted jobs
// and disable flags to the live scheduler. Called once during boot,
// after INI/labels have populated the scheduler — so persisted API
// jobs land last and shadow any same-named INI/label job (this is the
// documented precedence). Persisted disable flags are applied
// regardless of origin so an INI/label job an operator paused via the
// UI stays paused after restart.
//
// Empty StateFile is a no-op (persistence disabled), keeping the
// pre-#593 behavior for operators who haven't opted in.
func (c *DaemonCommand) initPersistStore() error {
	c.persistStore = persist.NewStore(c.StateFile)
	if !c.persistStore.Enabled() {
		return nil
	}
	if err := c.persistStore.Load(); err != nil {
		return fmt.Errorf("persist load %q: %w", c.persistStore.Path(), err)
	}
	snap := c.persistStore.Snapshot()
	for name, j := range snap.Jobs {
		job, err := c.persistedJobToScheduler(name, j)
		if err != nil {
			c.Logger.Warn("skip persisted job (could not materialize)",
				"job", name, "error", err)
			continue
		}
		// Replace any same-named job from INI/labels — persisted API
		// state is authoritative for its own jobs. Use UpdateJob when
		// the slot exists, else AddJob. The RemoveJob error path is
		// logged but not fatal: a failed remove typically means the
		// job already vanished between GetAnyJob and RemoveJob (race
		// with label sync); the subsequent AddJob will either succeed
		// or hit a duplicate-name path, both of which are surfaced.
		if existing := c.scheduler.GetAnyJob(name); existing != nil {
			if rmErr := c.scheduler.RemoveJob(existing); rmErr != nil {
				c.Logger.Warn("could not remove existing job to apply persisted state",
					"job", name, "error", rmErr)
			}
		}
		if err := c.scheduler.AddJob(job); err != nil {
			c.Logger.Warn("could not apply persisted job", "job", name, "error", err)
		}
	}
	for _, name := range snap.Disabled {
		if err := c.scheduler.DisableJob(name); err != nil {
			// Not an error worth failing boot on — the job may have
			// been removed from INI/labels between snapshot and now.
			c.Logger.Info("persisted disable target not present",
				"job", name, "error", err)
		}
	}
	c.Logger.Info("loaded persisted state",
		"path", c.persistStore.Path(),
		"jobs", len(snap.Jobs),
		"disabled", len(snap.Disabled))
	return nil
}

// persistedJobToScheduler reconstructs a scheduler-ready core.Job
// from the on-disk persist.Job. Mirrors web.Server.jobFromRequest so
// the round-trip from API → state-file → load lands the same job
// shape. Returns an error on unknown type, when a job kind needs a
// Docker provider that isn't available on this daemon, or when a
// validation parity check fails — the state file is operator-trusted
// but the same input validators that gate the API path also run on
// load so a malformed hand-edit can't bypass the safety net.
//
// CONTRACT: any field web.jobFromRequest sets on a job MUST also be
// captured here (and in persist.Job) — otherwise the round-trip
// from API → state-file → load silently drops the field. When
// jobRequest gains a field, mirror it in persist.Job and add the
// matching switch arm here in the same commit.
func (c *DaemonCommand) persistedJobToScheduler(name string, j *persist.Job) (core.Job, error) {
	if err := validatePersistedJobName(name); err != nil {
		return nil, err
	}
	provider := c.dockerProviderOrNil()
	validator := cfgvalidator.NewCommandValidator()
	switch j.Type {
	case persist.JobTypeRun:
		if provider == nil {
			return nil, fmt.Errorf("docker provider unavailable for run job")
		}
		rj := core.NewRunJob(provider)
		rj.Name = name
		rj.Schedule = j.Schedule
		rj.Command = j.Command
		rj.Image = j.Image
		rj.Container = j.Container
		return rj, nil
	case persist.JobTypeExec:
		if provider == nil {
			return nil, fmt.Errorf("docker provider unavailable for exec job")
		}
		ej := core.NewExecJob(provider)
		ej.Name = name
		ej.Schedule = j.Schedule
		ej.Command = j.Command
		ej.Container = j.Container
		return ej, nil
	case persist.JobTypeCompose:
		if j.File != "" {
			if err := validator.ValidateFilePath(j.File); err != nil {
				return nil, fmt.Errorf("invalid compose file path: %w", err)
			}
		}
		if err := validator.ValidateServiceName(j.Service); err != nil {
			return nil, fmt.Errorf("invalid service name: %w", err)
		}
		if j.Command != "" {
			if err := validator.ValidateCommandArgs(args.GetArgs(j.Command)); err != nil {
				return nil, fmt.Errorf("invalid command arguments: %w", err)
			}
		}
		cj := &core.ComposeJob{}
		cj.Name = name
		cj.Schedule = j.Schedule
		cj.Command = j.Command
		cj.File = j.File
		cj.Service = j.Service
		cj.Exec = j.Exec
		return cj, nil
	case persist.JobTypeLocal, "":
		if j.Command != "" {
			if err := validator.ValidateCommandArgs(args.GetArgs(j.Command)); err != nil {
				return nil, fmt.Errorf("invalid command arguments: %w", err)
			}
		}
		lj := &core.LocalJob{}
		lj.Name = name
		lj.Schedule = j.Schedule
		lj.Command = j.Command
		return lj, nil
	default:
		return nil, fmt.Errorf("unknown job type %q", j.Type)
	}
}

// validatePersistedJobName mirrors web.validateJobName but lives in
// cli to avoid a cli → web → cli import cycle. Kept aligned by a
// docstring contract; both implementations must apply the same rules
// (non-empty, ≤256 chars, no control chars). Job names land in INI
// section headers and command-line args downstream, so the loose
// checks are conservative defense-in-depth, not strict validation.
func validatePersistedJobName(name string) error {
	if name == "" {
		return fmt.Errorf("persisted job name must not be empty")
	}
	if len(name) > 256 {
		return fmt.Errorf("persisted job name exceeds 256 chars: %q", name[:32]+"…")
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return fmt.Errorf("persisted job name %q contains control character", name)
		}
	}
	return nil
}

func (c *DaemonCommand) dockerProviderOrNil() core.DockerProvider {
	if c.dockerHandler == nil {
		return nil
	}
	return c.dockerHandler.GetDockerProvider()
}
