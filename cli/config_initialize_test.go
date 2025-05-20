package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/netresearch/ofelia/core"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func TestConfigInit(t *testing.T) { TestingT(t) }

type ConfigInitSuite struct{}

var _ = Suite(&ConfigInitSuite{})

// TestInitializeAppSuccess verifies that InitializeApp succeeds when Docker handler connects and no containers are found.
func (s *ConfigInitSuite) TestInitializeAppSuccess(c *C) {
	// HTTP test server returning empty container list
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	// Override newDockerHandler to use the test server
	origFactory := newDockerHandler
	defer func() { newDockerHandler = origFactory }()
	newDockerHandler = func(notifier dockerLabelsUpdate, logger core.Logger, filters []string, interval time.Duration) (*DockerHandler, error) {
		client, err := docker.NewClient(ts.URL)
		if err != nil {
			return nil, err
		}
		return &DockerHandler{
			filters:      filters,
			notifier:     notifier,
			logger:       logger,
			dockerClient: client,
			pollInterval: interval,
		}, nil
	}

	cfg := NewConfig(&TestLogger{})
	cfg.Docker.Filters = []string{}
	err := cfg.InitializeApp()
	c.Assert(err, IsNil)
	c.Assert(cfg.sh, NotNil)
	c.Assert(cfg.dockerHandler, NotNil)
	c.Assert(cfg.dockerHandler.pollInterval, Equals, cfg.Docker.PollInterval)
}
