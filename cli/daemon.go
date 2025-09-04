package cli

import (
	"fmt"
	"net/http"
	_ "net/http/pprof" // #nosec G108
	"time"

	dockerclient "github.com/fsouza/go-dockerclient"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/web"
)

// DaemonCommand daemon process
type DaemonCommand struct {
	ConfigFile         string         `long:"config" env:"OFELIA_CONFIG" default:"/etc/ofelia/config.ini"`
	DockerFilters      []string       `short:"f" long:"docker-filter" env:"OFELIA_DOCKER_FILTER"`
	DockerPollInterval *time.Duration `long:"docker-poll-interval" env:"OFELIA_POLL_INTERVAL"`
	DockerUseEvents    *bool          `long:"docker-events" env:"OFELIA_DOCKER_EVENTS"`
	DockerNoPoll       *bool          `long:"docker-no-poll" env:"OFELIA_DOCKER_NO_POLL"`
	LogLevel           string         `long:"log-level" env:"OFELIA_LOG_LEVEL"`
	EnablePprof        bool           `long:"enable-pprof" env:"OFELIA_ENABLE_PPROF"`
	PprofAddr          string         `long:"pprof-address" env:"OFELIA_PPROF_ADDRESS" default:"127.0.0.1:8080"`
	EnableWeb          bool           `long:"enable-web" env:"OFELIA_ENABLE_WEB"`
	WebAddr            string         `long:"web-address" env:"OFELIA_WEB_ADDRESS" default:":8081"`

	scheduler       *core.Scheduler
	pprofServer     *http.Server
	webServer       *web.Server
	dockerHandler   *DockerHandler
	config          *Config
	done            chan struct{}
	Logger          core.Logger
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
	// Apply CLI log level before reading config
	ApplyLogLevel(c.LogLevel)

	// Initialize shutdown manager
	c.shutdownManager = core.NewShutdownManager(c.Logger, 30*time.Second)

	// Always try to read the config file, as there are options such as globals or some tasks that can be specified there and not in docker
	config, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		c.Logger.Warningf("Could not load config file %q: %v", c.ConfigFile, err)
		// Create an empty config if loading failed
		config = NewConfig(c.Logger)
	}
	c.applyOptions(config)

	// Apply global settings from config if flags were not provided
	if !c.EnableWeb {
		c.EnableWeb = config.Global.EnableWeb
	}
	if c.WebAddr == ":8081" && config.Global.WebAddr != "" {
		c.WebAddr = config.Global.WebAddr
	}
	if !c.EnablePprof {
		c.EnablePprof = config.Global.EnablePprof
	}
	if c.PprofAddr == "127.0.0.1:8080" && config.Global.PprofAddr != "" {
		c.PprofAddr = config.Global.PprofAddr
	}

	c.pprofServer = &http.Server{
		Addr:              c.PprofAddr,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if c.LogLevel == "" {
		ApplyLogLevel(config.Global.LogLevel)
	}

	err = config.InitializeApp()
	if err != nil {
		c.Logger.Criticalf("Can't start the app: %v", err)
	}
	// Re-apply CLI/environment options so they override Docker labels
	c.applyOptions(config)
	c.scheduler = config.sh
	c.dockerHandler = config.dockerHandler
	c.config = config

	// Initialize health checker
	var dockerClient *dockerclient.Client
	if c.dockerHandler != nil {
		dockerClient = c.dockerHandler.GetInternalDockerClient()
	}
	c.healthChecker = web.NewHealthChecker(dockerClient, "1.0.0")

	// Create graceful scheduler with shutdown support
	gracefulScheduler := core.NewGracefulScheduler(c.scheduler, c.shutdownManager)
	c.scheduler = gracefulScheduler.Scheduler

	if c.EnableWeb {
		c.webServer = web.NewServer(c.WebAddr, c.scheduler, c.config, dockerClient)

		// Register health endpoints
		c.webServer.RegisterHealthEndpoints(c.healthChecker)

		// Create graceful server with shutdown support
		gracefulServer := core.NewGracefulServer(c.webServer.GetHTTPServer(), c.shutdownManager, c.Logger)
		_ = gracefulServer // The hooks are registered internally
	}

	return err
}

func (c *DaemonCommand) start() error {
	// Start listening for shutdown signals
	c.shutdownManager.ListenForShutdown()

	if err := c.scheduler.Start(); err != nil {
		return fmt.Errorf("start scheduler: %w", err)
	}

	if c.EnablePprof {
		c.Logger.Noticef("Starting pprof server on %s", c.PprofAddr)
		go func() {
			if err := c.pprofServer.ListenAndServe(); err != http.ErrServerClosed {
				c.Logger.Errorf("Error starting HTTP server: %v", err)
				close(c.done)
			}
		}()
	} else {
		c.Logger.Noticef("pprof server disabled")
	}

	if c.EnableWeb {
		c.Logger.Noticef("Starting web server on %s", c.WebAddr)
		go func() {
			if err := c.webServer.Start(); err != nil {
				c.Logger.Errorf("Error starting web server: %v", err)
				close(c.done)
			}
		}()
	} else {
		c.Logger.Noticef("web server disabled")
	}

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

	if c.EnableWeb {
		config.Global.EnableWeb = true
	}
	if c.WebAddr != ":8081" {
		config.Global.WebAddr = c.WebAddr
	}
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
