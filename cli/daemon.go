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
	ConfigFile         string        `long:"config" description:"configuration file" default:"/etc/ofelia.conf"`
	DockerFilters      []string      `short:"f" long:"docker-filter" description:"Filter for docker containers"`
	DockerPollInterval time.Duration `long:"docker-poll-interval" description:"Interval for docker polling and INI reload (0 disables)" default:"10s"`
	DockerUseEvents    bool          `long:"docker-events" description:"Use docker events instead of polling"`
	DockerNoPoll       bool          `long:"docker-no-poll" description:"Disable polling docker for labels"`
	LogLevel           string        `long:"log-level" description:"Set log level"`
	EnablePprof        bool          `long:"enable-pprof" description:"Enable the pprof HTTP server"`
	PprofAddr          string        `long:"pprof-address" description:"Address for the pprof HTTP server to listen on" default:"127.0.0.1:8080"`
	EnableWeb          bool          `long:"enable-web" description:"Enable the web UI"`
	WebAddr            string        `long:"web-address" description:"Address for the web UI HTTP server to listen on" default:":8081"`

	scheduler   *core.Scheduler
	signals     chan os.Signal
	pprofServer *http.Server
	webServer   *web.Server
	done        chan struct{}
	Logger      core.Logger
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
	c.pprofServer = &http.Server{Addr: c.PprofAddr}

	// Apply CLI log level before reading config
	ApplyLogLevel(c.LogLevel)

	// Always try to read the config file, as there are options such as globals or some tasks that can be specified there and not in docker
	config, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		c.Logger.Warningf("Could not load config file %q: %v", c.ConfigFile, err)
	}
	config.Docker.Filters = c.DockerFilters
	config.Docker.PollInterval = c.DockerPollInterval
	config.Docker.UseEvents = c.DockerUseEvents
	config.Docker.DisablePolling = c.DockerNoPoll

	if c.LogLevel == "" {
		ApplyLogLevel(config.Global.LogLevel)
	}

	err = config.InitializeApp()
	if err != nil {
		c.Logger.Criticalf("Can't start the app: %v", err)
	}
	c.scheduler = config.sh
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
		go func() {
			if err := c.pprofServer.ListenAndServe(); err != http.ErrServerClosed {
				c.Logger.Errorf("Error starting HTTP server: %v", err)
				close(c.done)
			}
		}()
	}

	if c.EnableWeb {
		go func() {
			if err := c.webServer.Start(); err != nil {
				c.Logger.Errorf("Error starting web server: %v", err)
				close(c.done)
			}
		}()
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

	if !c.scheduler.IsRunning() {
		return nil
	}

	c.Logger.Warningf("Waiting running jobs.")
	return c.scheduler.Stop()
}
