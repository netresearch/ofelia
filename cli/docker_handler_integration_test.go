//go:build integration
// +build integration

package cli

import (
	"context"
	"time"

	. "gopkg.in/check.v1"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// NOTE: mockDockerProviderForHandler is defined in docker_handler_test.go

// chanNotifier implements dockerLabelsUpdate and notifies via channel when updates occur.
type chanNotifier struct{ ch chan struct{} }

func (n *chanNotifier) dockerLabelsUpdate(_ map[string]map[string]string) {
	select {
	case n.ch <- struct{}{}:
	default:
	}
}

func (s *DockerHandlerSuite) TestPollingDisabled(c *C) {
	ch := make(chan struct{}, 1)
	notifier := &chanNotifier{ch: ch}

	// Use mock provider instead of real Docker connection
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{
			{
				Name:   "cont",
				Labels: map[string]string{"ofelia.enabled": "true"},
			},
		},
	}

	cfg := &DockerConfig{Filters: []string{}, PollInterval: time.Millisecond * 50, UseEvents: false, DisablePolling: true}
	_, err := NewDockerHandler(context.Background(), notifier, &test.Logger{}, cfg, mockProvider)
	c.Assert(err, IsNil)

	select {
	case <-ch:
		c.Error("unexpected update")
	case <-time.After(time.Millisecond * 150):
	}
}
