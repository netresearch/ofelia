package cli

// SPDX-License-Identifier: MIT

import (
	// dummyNotifier implements dockerLabelsUpdate for testing
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/fsouza/go-dockerclient/testing"
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

	h := &DockerHandler{}
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
	handler, err := NewDockerHandler(notifier, &TestLogger{}, &DockerConfig{})
	c.Assert(handler, IsNil)
	c.Assert(err, NotNil)
}

// TestGetDockerLabelsInvalidFilter verifies that GetDockerLabels returns an error on invalid filter strings
func (s *DockerHandlerSuite) TestGetDockerLabelsInvalidFilter(c *C) {
	h := &DockerHandler{filters: []string{"invalidfilter"}, logger: &TestLogger{}}
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

	h := &DockerHandler{filters: []string{}, logger: &TestLogger{}}
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

	h := &DockerHandler{filters: []string{}, logger: &TestLogger{}}
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

// TestPollingDisabled ensures no updates are triggered when polling is disabled and no events occur
func (s *DockerHandlerSuite) TestPollingDisabled(c *C) {
	ch := make(chan struct{}, 1)
	notifier := &chanNotifier{ch: ch}

	server, err := testing.NewServer("127.0.0.1:0", nil, nil)
	c.Assert(err, IsNil)
	defer server.Stop()
	server.CustomHandler("/containers/json", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `[{"Names":["/cont"],"Labels":{"ofelia.enabled":"true"}}]`)
	}))
	tsURL := server.URL()

	os.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(tsURL, "http://"))
	defer os.Unsetenv("DOCKER_HOST")

	cfg := &DockerConfig{Filters: []string{}, PollInterval: time.Millisecond * 50, UseEvents: false, DisablePolling: true}
	_, err = NewDockerHandler(notifier, &TestLogger{}, cfg)
	c.Assert(err, IsNil)

	select {
	case <-ch:
		c.Error("unexpected update")
	case <-time.After(time.Millisecond * 150):
	}
}
