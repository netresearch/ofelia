package cli

// SPDX-License-Identifier: MIT

import (
	// dummyNotifier implements dockerLabelsUpdate for testing
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	defaults "github.com/creasty/defaults"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/netresearch/ofelia/core"
	. "gopkg.in/check.v1"
)

// dummyNotifier implements dockerLabelsUpdate
type dummyNotifier struct{}

func (d *dummyNotifier) dockerLabelsUpdate(labels map[string]map[string]string) {}

type chanNotifier struct{ ch chan struct{} }

func (d *chanNotifier) dockerLabelsUpdate(labels map[string]map[string]string) {
	select {
	case d.ch <- struct{}{}:
	default:
	}
}

// DockerHandlerSuite contains tests for DockerHandler methods
type DockerHandlerSuite struct{}

var _ = Suite(&DockerHandlerSuite{})

// TestBuildDockerClientError verifies that buildDockerClient returns an error when DOCKER_HOST is invalid
func (s *DockerHandlerSuite) TestBuildDockerClientError(c *C) {
	orig := os.Getenv("DOCKER_HOST")
	defer os.Setenv("DOCKER_HOST", orig)
	os.Setenv("DOCKER_HOST", "=")

	h := &DockerHandler{ctx: context.Background()}
	_, err := h.buildDockerClient()
	c.Assert(err, NotNil)
}

// TestNewDockerHandlerErrorInfo verifies that NewDockerHandler returns an error when Info() fails
func (s *DockerHandlerSuite) TestNewDockerHandlerErrorInfo(c *C) {
	orig := os.Getenv("DOCKER_HOST")
	defer os.Setenv("DOCKER_HOST", orig)
	// Use a host that will refuse connections
	os.Setenv("DOCKER_HOST", "tcp://127.0.0.1:0")

	notifier := &dummyNotifier{}
	handler, err := NewDockerHandler(context.Background(), notifier, &TestLogger{}, &DockerConfig{}, nil)
	c.Assert(handler, IsNil)
	c.Assert(err, NotNil)
}

// TestGetDockerLabelsInvalidFilter verifies that GetDockerLabels returns an error on invalid filter strings
func (s *DockerHandlerSuite) TestGetDockerLabelsInvalidFilter(c *C) {
	h := &DockerHandler{filters: []string{"invalidfilter"}, logger: &TestLogger{}, ctx: context.Background()}
	_, err := h.GetDockerLabels()
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "invalid docker filter: invalidfilter")
}

// TestGetDockerLabelsNoContainers verifies that GetDockerLabels returns ErrNoContainerWithOfeliaEnabled when no containers match
func (s *DockerHandlerSuite) TestGetDockerLabelsNoContainers(c *C) {
	// HTTP server returning empty container list
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/containers/json") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client, err := docker.NewClient(ts.URL)
	c.Assert(err, IsNil)

	h := &DockerHandler{filters: []string{}, logger: &TestLogger{}, ctx: context.Background()}
	h.dockerClient = client
	_, err = h.GetDockerLabels()
	c.Assert(err, Equals, ErrNoContainerWithOfeliaEnabled)
}

// TestGetDockerLabelsValid verifies that GetDockerLabels filters and returns only ofelia-prefixed labels
func (s *DockerHandlerSuite) TestGetDockerLabelsValid(c *C) {
	// HTTP server returning one container with mixed labels
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/containers/json") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `[
                {"Names":["/cont1"],"Labels":{
                    "ofelia.enabled":"true",
                    "ofelia.job-exec.foo.schedule":"@every 1s",
                    "ofelia.job-run.bar.schedule":"@every 2s",
                    "other.label":"ignore"
                }}
            ]`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client, err := docker.NewClient(ts.URL)
	c.Assert(err, IsNil)

	h := &DockerHandler{filters: []string{}, logger: &TestLogger{}, ctx: context.Background()}
	h.dockerClient = client
	labels, err := h.GetDockerLabels()
	c.Assert(err, IsNil)

	expected := map[string]map[string]string{
		"cont1": {
			"ofelia.enabled":               "true",
			"ofelia.job-exec.foo.schedule": "@every 1s",
			"ofelia.job-run.bar.schedule":  "@every 2s",
		},
	}
	c.Assert(labels, DeepEquals, expected)
}

// TestWatchInvalidInterval verifies that watch exits immediately when
// PollInterval is zero or negative.
func (s *DockerHandlerSuite) TestWatchInvalidInterval(c *C) {
	h := &DockerHandler{pollInterval: 0, notifier: &dummyNotifier{}, logger: &TestLogger{}, ctx: context.Background(), cancel: func() {}}
	done := make(chan struct{})
	go func() {
		h.watch()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(time.Millisecond * 50):
		c.Error("watch did not return for zero interval")
	}

	h = &DockerHandler{pollInterval: -time.Second, notifier: &dummyNotifier{}, logger: &TestLogger{}, ctx: context.Background(), cancel: func() {}}
	done = make(chan struct{})
	go func() {
		h.watch()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(time.Millisecond * 50):
		c.Error("watch did not return for negative interval")
	}
}

// TestDockerLabelsUpdateKeepsIniRunJobs verifies that RunJobs defined via INI
// remain when dockerLabelsUpdate receives no labeled containers.
func (s *DockerHandlerSuite) TestDockerLabelsUpdateKeepsIniRunJobs(c *C) {
	cfg := NewConfig(&TestLogger{})
	cfg.logger = &TestLogger{}
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(&TestLogger{})
	cfg.buildSchedulerMiddlewares(cfg.sh)

	cfg.RunJobs["ini-job"] = &RunJobConfig{RunJob: core.RunJob{BareJob: core.BareJob{Schedule: "@hourly", Command: "echo"}}, JobSource: JobSourceINI}

	for name, j := range cfg.RunJobs {
		defaults.Set(j)
		j.Name = name
		cfg.sh.AddJob(j)
	}

	c.Assert(len(cfg.sh.Entries()), Equals, 1)

	cfg.dockerLabelsUpdate(map[string]map[string]string{})

	c.Assert(len(cfg.RunJobs), Equals, 1)
	c.Assert(len(cfg.sh.Entries()), Equals, 1)
}

// TestDockerLabelsUpdateKeepsIniExecJobs verifies that ExecJobs defined via INI
// remain when dockerLabelsUpdate receives no labeled containers.
func (s *DockerHandlerSuite) TestDockerLabelsUpdateKeepsIniExecJobs(c *C) {
	cfg := NewConfig(&TestLogger{})
	cfg.logger = &TestLogger{}
	cfg.dockerHandler = &DockerHandler{}
	cfg.sh = core.NewScheduler(&TestLogger{})
	cfg.buildSchedulerMiddlewares(cfg.sh)

	cfg.ExecJobs["ini-exec"] = &ExecJobConfig{ExecJob: core.ExecJob{BareJob: core.BareJob{Schedule: "@hourly", Command: "echo"}}, JobSource: JobSourceINI}

	for name, j := range cfg.ExecJobs {
		defaults.Set(j)
		j.Name = name
		cfg.sh.AddJob(j)
	}

	c.Assert(len(cfg.sh.Entries()), Equals, 1)

	cfg.dockerLabelsUpdate(map[string]map[string]string{})

	c.Assert(len(cfg.ExecJobs), Equals, 1)
	c.Assert(len(cfg.sh.Entries()), Equals, 1)
}
