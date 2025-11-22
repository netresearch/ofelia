package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	docker "github.com/fsouza/go-dockerclient"

	"github.com/netresearch/ofelia/core"
)

var ErrNoContainerWithOfeliaEnabled = errors.New("couldn't find containers with label 'ofelia.enabled=true'")

// dockerClient defines the Docker client methods used by DockerHandler.
type dockerClient interface {
	Info() (*docker.DockerInfo, error)
	ListContainers(opts docker.ListContainersOptions) ([]docker.APIContainers, error)
	AddEventListenerWithOptions(opts docker.EventsOptions, listener chan<- *docker.APIEvents) error
}

type DockerHandler struct {
	ctx            context.Context //nolint:containedctx // holds lifecycle for background goroutines
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
	// First try optimized client
	if optimized, ok := c.dockerClient.(*core.OptimizedDockerClient); ok {
		return optimized.GetClient()
	}
	// Fall back to plain client (for tests or backwards compatibility)
	if client, ok := c.dockerClient.(*docker.Client); ok {
		return client
	}
	return nil
}

func (c *DockerHandler) buildDockerClient() (dockerClient, error) {
	// Create optimized Docker client with connection pooling and circuit breaker
	optimizedClient, err := core.NewOptimizedDockerClient(
		core.DefaultDockerClientConfig(),
		c.logger,
		core.GlobalPerformanceMetrics,
	)
	if err != nil {
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return nil, fmt.Errorf("failed to create Docker client: %w\n  → Check Docker daemon is running: docker ps\n  → Verify Docker socket is accessible: ls -l /var/run/docker.sock\n  → Check DOCKER_HOST environment variable if using remote Docker\n  → Ensure current user has Docker permissions: groups | grep docker", err)
	}

	// Sanity check Docker connection
	if _, err := optimizedClient.Info(); err != nil {
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return nil, fmt.Errorf("failed to connect to Docker daemon: %w\n  → Check Docker daemon is running: systemctl status docker\n  → Verify network connectivity if using remote Docker\n  → Check Docker socket permissions: ls -l /var/run/docker.sock\n  → Try: docker info (should work if Docker is accessible)", err)
	}

	return optimizedClient, nil
}

func NewDockerHandler(
	ctx context.Context, //nolint:contextcheck // external callers provide base context; we derive cancelable child
	notifier dockerLabelsUpdate,
	logger core.Logger,
	cfg *DockerConfig,
	client dockerClient,
) (*DockerHandler, error) {
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
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return nil, fmt.Errorf("failed to query Docker daemon info: %w\n  → Check Docker daemon is running: systemctl status docker\n  → Verify Docker API is accessible: docker info\n  → Check for Docker daemon errors: journalctl -u docker -n 50", err)
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
			//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
			return nil, fmt.Errorf("invalid docker filter %q\n  → Filters must use key=value format (e.g., 'label=app=web')\n  → Valid filter keys: label, name, id, status, network\n  → Example: --docker-filter='label=environment=production'\n  → Check your OFELIA_DOCKER_FILTER environment variable or config file", f)
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
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return nil, fmt.Errorf("failed to list Docker containers: %w\n  → Check Docker daemon is running: docker ps\n  → Verify user has Docker permissions: groups | grep docker\n  → Check Docker filters are valid: %v\n  → Try listing containers manually: docker ps -a", err, filters)
	}

	if len(conts) == 0 {
		return nil, ErrNoContainerWithOfeliaEnabled
	}

	labels := make(map[string]map[string]string)

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
