package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		client, err := docker.NewClient(ts.URL)
		if err != nil {
			return nil, err
		}
		return &DockerHandler{
			ctx:            ctx,
			filters:        cfg.Filters,
			notifier:       notifier,
			logger:         logger,
			dockerClient:   client,
			pollInterval:   cfg.PollInterval,
			useEvents:      cfg.UseEvents,
			disablePolling: cfg.DisablePolling,
		}, nil
	}

	cfg := NewConfig(&TestLogger{})
	cfg.Docker.Filters = []string{}
	err := cfg.InitializeApp()
	c.Assert(err, IsNil)
	c.Assert(cfg.sh, NotNil)
	c.Assert(cfg.dockerHandler, NotNil)
}

// TestInitializeAppLabelConflict ensures label-defined jobs do not override INI jobs at startup.
func (s *ConfigInitSuite) TestInitializeAppLabelConflict(c *C) {
	iniStr := "[job-run \"foo\"]\nschedule = @every 5s\nimage = busybox\ncommand = echo ini\n"
	cfg, err := BuildFromString(iniStr, &TestLogger{})
	c.Assert(err, IsNil)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `[{"Names":["/cont1"],"Labels":{`+
				`"ofelia.enabled":"true",`+
				`"ofelia.job-run.foo.schedule":"@every 10s",`+
				`"ofelia.job-run.foo.image":"busybox",`+
				`"ofelia.job-run.foo.command":"echo label"}}]`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	origFactory := newDockerHandler
	defer func() { newDockerHandler = origFactory }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		client, err := docker.NewClient(ts.URL)
		if err != nil {
			return nil, err
		}
		return &DockerHandler{
			ctx:          ctx,
			filters:      cfg.Filters,
			notifier:     notifier,
			logger:       logger,
			dockerClient: client,
			pollInterval: 0,
		}, nil
	}

	cfg.logger = &TestLogger{}
	err = cfg.InitializeApp()
	c.Assert(err, IsNil)
	c.Assert(len(cfg.RunJobs), Equals, 1)
	j, ok := cfg.RunJobs["foo"]
	c.Assert(ok, Equals, true)
	c.Assert(j.GetSchedule(), Equals, "@every 5s")
	c.Assert(j.JobSource, Equals, JobSourceINI)
}
