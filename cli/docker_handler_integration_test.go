//go:build integration
// +build integration

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// NOTE: mockDockerProviderForHandler is defined in docker_handler_test.go

// chanNotifier implements dockerLabelsUpdate and notifies via channel when updates occur.
type chanNotifier struct{ ch chan struct{} }

func (n *chanNotifier) dockerLabelsUpdate(_ map[DockerContainerInfo]map[string]string) {
	select {
	case n.ch <- struct{}{}:
	default:
	}
}

func TestPollingDisabled(t *testing.T) {
	t.Parallel()

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
	require.NoError(t, err)

	select {
	case <-ch:
		assert.Fail(t, "unexpected update")
	case <-time.After(time.Millisecond * 150):
	}
}
