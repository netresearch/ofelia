package cli

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/web"
)

// DaemonCommand daemon process
type DaemonCommand struct {
	ConfigFile         string         `long:"config" env:"OFELIA_CONFIG" description:"configuration file" default:"/etc/ofelia/config.ini"`
	DockerFilters      []string       `short:"f" long:"docker-filter" env:"OFELIA_DOCKER_FILTER" description:"Filter for docker containers"`
	DockerPollInterval *time.Duration `long:"docker-poll-interval" env:"OFELIA_POLL_INTERVAL" description:"Interval for docker polling and INI reload (0 disables)"`
	DockerUseEvents    *bool          `long:"docker-events" env:"OFELIA_DOCKER_EVENTS" description:"Use docker events instead of polling"`
	DockerNoPoll       *bool          `long:"docker-no-poll" env:"OFELIA_DOCKER_NO_POLL" description:"Disable polling docker for labels"`
	LogLevel           string         `long:"log-level" env:"OFELIA_LOG_LEVEL" description:"Set log level (overrides config)"`
	EnablePprof        bool           `long:"enable-pprof" env:"OFELIA_ENABLE_PPROF" description:"Enable the pprof HTTP server"`
	PprofAddr          string         `long:"pprof-address" env:"OFELIA_PPROF_ADDRESS" description:"Address for the pprof HTTP server to listen on" default:"127.0.0.1:8080"`
	EnableWeb          bool           `long:"enable-web" env:"OFELIA_ENABLE_WEB" description:"Enable the web UI"`
	WebAddr            string         `long:"web-address" env:"OFELIA_WEB_ADDRESS" description:"Address for the web UI HTTP server to listen on" default:":8081"`

	scheduler     *core.Scheduler
	signals       chan os.Signal
	pprofServer   *http.Server
	webServer     *web.Server
	dockerHandler *DockerHandler
	done          chan struct{}
	Logger        core.Logger
}

// Execute runs the daemon
func (c *DaemonCommand) Execute(args []string) error {
	if err := c.boot(); err != nil {
		return err
	}

	if err := c.start(); err != nil {
		return err
	}

	if err := c.shutdown(); err != nil {
		return err
	}

	return nil
}

func (c *DaemonCommand) boot() (err error) {
	// Apply CLI log level before reading config
	ApplyLogLevel(c.LogLevel)

	// Always try to read the config file, as there are options such as globals or some tasks that can be specified there and not in docker
	config, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		c.Logger.Warningf("Could not load config file %q: %v", c.ConfigFile, err)
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

	c.pprofServer = &http.Server{Addr: c.PprofAddr}

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
	if c.EnableWeb {
		c.webServer = web.NewServer(c.WebAddr, c.scheduler)
	}

	return err
}

func (c *DaemonCommand) start() error {
	c.setSignals()
	if err := c.scheduler.Start(); err != nil {
		return err
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

func (c *DaemonCommand) setSignals() {
	c.signals = make(chan os.Signal, 1)
	c.done = make(chan struct{})

	signal.Notify(c.signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-c.signals
		c.Logger.Warningf(
			"Signal received: %s, shutting down the process\n", sig,
		)

		close(c.done)
	}()
}

func (c *DaemonCommand) shutdown() error {
	<-c.done

	c.Logger.Warningf("Stopping HTTP server")
	if err := c.pprofServer.Shutdown(context.Background()); err != nil {
		c.Logger.Warningf("Error stopping HTTP server: %v", err)
	}
	if c.EnableWeb && c.webServer != nil {
		c.Logger.Warningf("Stopping web server")
		if err := c.webServer.Shutdown(context.Background()); err != nil {
			c.Logger.Warningf("Error stopping web server: %v", err)
		}
	}

	if c.dockerHandler != nil {
		c.Logger.Warningf("Stopping docker handler")
		_ = c.dockerHandler.Shutdown(context.Background())
	}

	if !c.scheduler.IsRunning() {
		return nil
	}

	c.Logger.Warningf("Waiting running jobs.")
	return c.scheduler.Stop()
}

func (c *DaemonCommand) applyOptions(config *Config) {
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
