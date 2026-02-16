package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // #nosec G108
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/web"
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
	WebTokenExpiry       int            `long:"web-token-expiry" env:"OFELIA_WEB_TOKEN_EXPIRY" description:"Token expiry hours" default:"24"` //nolint:revive
	WebMaxLoginAttempts  int            `long:"web-max-login-attempts" env:"OFELIA_WEB_MAX_LOGIN_ATTEMPTS" description:"Lockout" default:"5"` //nolint:revive

	scheduler       *core.Scheduler
	pprofServer     *http.Server
	webServer       *web.Server
	dockerHandler   *DockerHandler
	config          *Config
	done            chan struct{}
	Logger          *slog.Logger
	LevelVar        *slog.LevelVar
	shutdownManager *core.ShutdownManager
	healthChecker   *web.HealthChecker
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

	if c.EnableWeb {
		var provider core.DockerProvider
		if c.dockerHandler != nil {
			provider = c.dockerHandler.GetDockerProvider()
		}

		var authCfg *web.SecureAuthConfig
		if c.WebAuthEnabled {
			if c.WebUsername == "" {
				return ErrWebAuthUsername
			}
			if c.WebPasswordHash == "" {
				return ErrWebAuthPassword
			}

			if c.WebSecretKey == "" {
				c.Logger.Warn("No web-secret-key provided. " +
					"Auth tokens will not survive daemon restarts. " +
					"Set OFELIA_WEB_SECRET_KEY for persistent sessions.")
			}

			authCfg = &web.SecureAuthConfig{
				Enabled:      true,
				Username:     c.WebUsername,
				PasswordHash: c.WebPasswordHash,
				SecretKey:    c.WebSecretKey,
				TokenExpiry:  c.WebTokenExpiry,
				MaxAttempts:  c.WebMaxLoginAttempts,
			}
		}
		c.webServer = web.NewServerWithAuth(c.WebAddr, c.scheduler, c.config, provider, authCfg)
		if c.webServer == nil {
			return fmt.Errorf("failed to initialize web server (check logs for details)")
		}

		c.webServer.RegisterHealthEndpoints(c.healthChecker)

		gracefulServer := core.NewGracefulServer(c.webServer.GetHTTPServer(), c.shutdownManager, c.Logger)
		_ = gracefulServer
	}

	return err
}

func (c *DaemonCommand) start() error {
	// Start listening for shutdown signals
	c.shutdownManager.ListenForShutdown()

	// Set up a goroutine to close done channel when shutdown completes
	go func() {
		<-c.shutdownManager.ShutdownChan()
		// Give some time for graceful shutdown to complete
		// The shutdown manager handles the actual shutdown process
		close(c.done)
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
				close(c.done)
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
				close(c.done)
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

	c.applyWebOptions(config)
	c.applyAuthOptions(config)
	c.applyServerOptions(config)
}

func (c *DaemonCommand) applyWebOptions(config *Config) {
	if c.EnableWeb {
		config.Global.EnableWeb = true
	}
	if c.WebAddr != ":8081" {
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
}

func (c *DaemonCommand) applyServerOptions(config *Config) {
	if c.EnablePprof {
		config.Global.EnablePprof = true
	}
	if c.PprofAddr != "127.0.0.1:8080" {
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
	if c.WebAddr == ":8081" && config.Global.WebAddr != "" {
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
}

func (c *DaemonCommand) applyServerDefaults(config *Config) {
	if !c.EnablePprof {
		c.EnablePprof = config.Global.EnablePprof
	}
	if c.PprofAddr == "127.0.0.1:8080" && config.Global.PprofAddr != "" {
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
