package cli

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/netresearch/ofelia/core"
)

// DaemonCommand daemon process
type DaemonCommand struct {
	ConfigFile    string   `long:"config" description:"configuration file" default:"/etc/ofelia.conf"`
	DockerFilters []string `short:"f" long:"docker-filter" description:"Filter for docker containers"`
	EnablePprof   bool     `long:"enable-pprof" description:"Enable the pprof HTTP server"`
	PprofAddr     string   `long:"pprof-address" description:"Address for the pprof HTTP server to listen on" default:"127.0.0.1:8080"`

	scheduler  *core.Scheduler
	signals    chan os.Signal
	httpServer *http.Server
	done       chan struct{}
	Logger     core.Logger
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
	c.httpServer = &http.Server{Addr: c.PprofAddr}

	// Always try to read the config file, as there are options such as globals or some tasks that can be specified there and not in docker
	config, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		c.Logger.Debugf("Config file: %v not found", c.ConfigFile)
	}
	config.Docker.Filters = c.DockerFilters

	err = config.InitializeApp()
	if err != nil {
		c.Logger.Criticalf("Can't start the app: %v", err)
	}
	c.scheduler = config.sh

	return err
}

func (c *DaemonCommand) start() error {
	c.setSignals()
	if err := c.scheduler.Start(); err != nil {
		return err
	}

	if c.EnablePprof {
		go func() {
			if err := c.httpServer.ListenAndServe(); err != http.ErrServerClosed {
				c.Logger.Errorf("Error starting HTTP server: %v", err)
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
	if err := c.httpServer.Shutdown(context.Background()); err != nil {
		c.Logger.Warningf("Error stopping HTTP server: %v", err)
	}

	if !c.scheduler.IsRunning() {
		return nil
	}

	c.Logger.Warningf("Waiting running jobs.")
	return c.scheduler.Stop()
}
