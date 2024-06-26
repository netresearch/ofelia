package cli

import (
	"errors"
	"strings"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/netresearch/ofelia/core"
)

var ErrNoContainerWithOfeliaEnabled = errors.New("Couldn't find containers with label 'ofelia.enabled=true'")

type DockerHandler struct {
	filters      []string
	dockerClient *docker.Client
	notifier     dockerLabelsUpdate
	logger       core.Logger
}

type dockerLabelsUpdate interface {
	dockerLabelsUpdate(map[string]map[string]string)
}

// TODO: Implement an interface so the code does not have to use third parties directly
func (c *DockerHandler) GetInternalDockerClient() *docker.Client {
	return c.dockerClient
}

func (c *DockerHandler) buildDockerClient() (*docker.Client, error) {
	d, err := docker.NewClientFromEnv()
	if err != nil {
		return nil, err
	}

	return d, nil
}

func NewDockerHandler(notifier dockerLabelsUpdate, logger core.Logger, filters []string) (*DockerHandler, error) {
	c := &DockerHandler{
		filters:  filters,
		notifier: notifier,
		logger:   logger,
	}

	var err error
	c.dockerClient, err = c.buildDockerClient()
	if err != nil {
		return nil, err
	}

	// Do a sanity check on docker
	if _, err = c.dockerClient.Info(); err != nil {
		return nil, err
	}

	go c.watch()
	return c, nil
}

func (c *DockerHandler) watch() {
	// Poll for changes
	tick := time.Tick(10000 * time.Millisecond)
	for {
		select {
		case <-tick:
			labels, err := c.GetDockerLabels()
			// Do not print or care if there is no container up right now
			if err != nil && !errors.Is(err, ErrNoContainerWithOfeliaEnabled) {
				c.logger.Debugf("%v", err)
			}
			c.notifier.dockerLabelsUpdate(labels)
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
