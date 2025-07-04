package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/netresearch/ofelia/core"
)

var ErrNoContainerWithOfeliaEnabled = errors.New("Couldn't find containers with label 'ofelia.enabled=true'")

// dockerClient defines the Docker client methods used by DockerHandler.
type dockerClient interface {
	Info() (*docker.DockerInfo, error)
	ListContainers(opts docker.ListContainersOptions) ([]docker.APIContainers, error)
	AddEventListenerWithOptions(opts docker.EventsOptions, listener chan<- *docker.APIEvents) error
	RemoveEventListener(listener chan *docker.APIEvents) error
}

type DockerHandler struct {
	ctx            context.Context
	cancel         context.CancelFunc
	filters        []string
	dockerClient   dockerClient
	notifier       dockerLabelsUpdate
	logger         core.Logger
	pollInterval   time.Duration
	useEvents      bool
	disablePolling bool
}

type dockerLabelsUpdate interface {
	dockerLabelsUpdate(map[string]map[string]string)
}

// TODO: Implement an interface so the code does not have to use third parties directly
func (c *DockerHandler) GetInternalDockerClient() *docker.Client {
	if client, ok := c.dockerClient.(*docker.Client); ok {
		return client
	}
	return nil
}

func (c *DockerHandler) buildDockerClient() (dockerClient, error) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		return nil, err
	}

	// Sanity check Docker connection
	if _, err := client.Info(); err != nil {
		return nil, err
	}

	return client, nil
}

func NewDockerHandler(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, client dockerClient) (*DockerHandler, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	c := &DockerHandler{
		ctx:            ctx,
		cancel:         cancel,
		filters:        cfg.Filters,
		notifier:       notifier,
		logger:         logger,
		pollInterval:   cfg.PollInterval,
		useEvents:      cfg.UseEvents,
		disablePolling: cfg.DisablePolling,
	}

	var err error
	if client == nil {
		c.dockerClient, err = c.buildDockerClient()
		if err != nil {
			return nil, err
		}
	} else {
		c.dockerClient = client
	}

	// Do a sanity check on docker
	if _, err = c.dockerClient.Info(); err != nil {
		return nil, err
	}

	if !c.disablePolling && c.pollInterval > 0 {
		go c.watch()
	}
	if c.useEvents {
		go c.watchEvents()
	}
	return c, nil
}

func (c *DockerHandler) watch() {
	if c.pollInterval <= 0 {
		// Skip polling when interval is not positive
		return
	}

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			labels, err := c.GetDockerLabels()
			if err != nil && !errors.Is(err, ErrNoContainerWithOfeliaEnabled) {
				c.logger.Debugf("%v", err)
			}
			c.notifier.dockerLabelsUpdate(labels)
			if cfg, ok := c.notifier.(*Config); ok {
				cfg.logger.Debugf("checking config file %s", cfg.configPath)
				if err := cfg.iniConfigUpdate(); err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						c.logger.Warningf("%v", err)
					}
				}
			}
		}
	}
}

func (c *DockerHandler) GetDockerLabels() (map[string]map[string]string, error) {
	filters := map[string][]string{
		"label": {requiredLabelFilter},
	}
	for _, f := range c.filters {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) != 2 {
			return nil, errors.New("invalid docker filter: " + f)
		}
		key, value := parts[0], parts[1]
		values, ok := filters[key]
		if ok {
			filters[key] = append(values, value)
		} else {
			filters[key] = []string{value}
		}
	}

	conts, err := c.dockerClient.ListContainers(docker.ListContainersOptions{
		Filters: filters,
	})
	if err != nil {
		return nil, err
	}

	if len(conts) == 0 {
		return nil, ErrNoContainerWithOfeliaEnabled
	}

	var labels = make(map[string]map[string]string)

	for _, c := range conts {
		if len(c.Names) > 0 && len(c.Labels) > 0 {
			name := strings.TrimPrefix(c.Names[0], "/")
			for k := range c.Labels {
				// remove all not relevant labels
				if !strings.HasPrefix(k, labelPrefix) {
					delete(c.Labels, k)
					continue
				}
			}

			labels[name] = c.Labels
		}
	}

	return labels, nil
}

func (c *DockerHandler) watchEvents() {
	ch := make(chan *docker.APIEvents)
	if err := c.dockerClient.AddEventListenerWithOptions(docker.EventsOptions{
		Filters: map[string][]string{"type": {"container"}},
	}, ch); err != nil {
		c.logger.Debugf("%v", err)
		return
	}
	for {
		select {
		case <-c.ctx.Done():
			if err := c.dockerClient.RemoveEventListener(ch); err != nil {
				c.logger.Debugf("error removing event listener: %v", err)
			}
			return
		case <-ch:
			labels, err := c.GetDockerLabels()
			if err != nil && !errors.Is(err, ErrNoContainerWithOfeliaEnabled) {
				c.logger.Debugf("%v", err)
			}
			c.notifier.dockerLabelsUpdate(labels)
		}
	}
}

func (c *DockerHandler) Shutdown(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}
