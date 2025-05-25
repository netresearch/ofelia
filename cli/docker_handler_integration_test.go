//go:build integration
// +build integration

package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient/testing"
	. "gopkg.in/check.v1"
)

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
	_, err = NewDockerHandler(context.Background(), notifier, &TestLogger{}, cfg, nil)
	c.Assert(err, IsNil)

	select {
	case <-ch:
		c.Error("unexpected update")
	case <-time.After(time.Millisecond * 150):
	}
}
