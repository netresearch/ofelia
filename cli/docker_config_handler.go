package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/domain"
)

var ErrNoContainerWithOfeliaEnabled = errors.New("couldn't find containers with label 'ofelia.enabled=true'")

type DockerHandler struct {
	ctx            context.Context //nolint:containedctx // holds lifecycle for background goroutines
	cancel         context.CancelFunc
	filters        []string
	dockerProvider core.DockerProvider // SDK-based provider
	notifier       dockerLabelsUpdate
	logger         core.Logger
	pollInterval   time.Duration
	useEvents      bool
	disablePolling bool
}

type dockerLabelsUpdate interface {
	dockerLabelsUpdate(map[string]map[string]string)
}

// GetDockerProvider returns the DockerProvider interface for SDK-based operations.
// This is the preferred method for new code using the official Docker SDK.
func (c *DockerHandler) GetDockerProvider() core.DockerProvider {
	return c.dockerProvider
}

func NewDockerHandler(
	ctx context.Context, //nolint:contextcheck // external callers provide base context; we derive cancelable child
	notifier dockerLabelsUpdate,
	logger core.Logger,
	cfg *DockerConfig,
	provider core.DockerProvider,
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
	if provider == nil {
		c.dockerProvider, err = c.buildSDKProvider()
		if err != nil {
			cancel()
			return nil, err
		}
	} else {
		c.dockerProvider = provider
	}

	// Do a sanity check on docker
	if err = c.dockerProvider.Ping(ctx); err != nil {
		cancel()
		//nolint:revive // Error message intentionally verbose for UX (actionable troubleshooting hints)
		return nil, fmt.Errorf("failed to connect to Docker daemon: %w\n  → Check Docker daemon is running: systemctl status docker\n  → Verify Docker API is accessible: docker info\n  → Check for Docker daemon errors: journalctl -u docker -n 50", err)
	}

	if !c.disablePolling && c.pollInterval > 0 {
		go c.watch()
	}
	if c.useEvents {
		go c.watchEvents()
	}
	return c, nil
}

// buildSDKProvider creates the new SDK-based Docker provider.
func (c *DockerHandler) buildSDKProvider() (core.DockerProvider, error) {
	provider, err := core.NewSDKDockerProviderDefault()
	if err != nil {
		return nil, fmt.Errorf("failed to create SDK Docker provider: %w", err)
	}

	// Verify connection
	if err := provider.Ping(context.Background()); err != nil {
		_ = provider.Close()
		return nil, fmt.Errorf("SDK provider failed to connect to Docker: %w", err)
	}

	return provider, nil
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

	conts, err := c.dockerProvider.ListContainers(c.ctx, domain.ListOptions{
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

	for _, cont := range conts {
		name := cont.Name
		if name != "" && len(cont.Labels) > 0 {
			// Filter to only ofelia labels
			ofeliaLabels := make(map[string]string)
			for k, v := range cont.Labels {
				if strings.HasPrefix(k, labelPrefix) {
					ofeliaLabels[k] = v
				}
			}
			if len(ofeliaLabels) > 0 {
				labels[name] = ofeliaLabels
			}
		}
	}

	return labels, nil
}

func (c *DockerHandler) watchEvents() {
	eventCh, errCh := c.dockerProvider.SubscribeEvents(c.ctx, domain.EventFilter{
		Filters: map[string][]string{"type": {"container"}},
	})

	for {
		select {
		case <-c.ctx.Done():
			return
		case err := <-errCh:
			if err != nil {
				c.logger.Debugf("Event subscription error: %v", err)
			}
			return
		case <-eventCh:
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

	// Close SDK provider if it was created
	if c.dockerProvider != nil {
		if err := c.dockerProvider.Close(); err != nil {
			c.logger.Warningf("Error closing Docker provider: %v", err)
		}
		c.dockerProvider = nil
	}

	return nil
}
