package cli

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
)

// =============================================================================
// resolveConfig mutation tests (line 60: useEvents && dockerPoll > 0)
// =============================================================================

// TestResolveConfig_EventsAndPollingWarning targets CONDITIONALS_NEGATION at line 60.
// When both useEvents=true and dockerPoll>0, a warning should be logged.
func TestResolveConfig_EventsAndPollingWarning(t *testing.T) {
	t.Parallel()

	t.Run("both_events_and_polling_warns", func(t *testing.T) {
		t.Parallel()
		logger := test.NewTestLogger()
		cfg := &DockerConfig{
			UseEvents:          true,
			DockerPollInterval: 10 * time.Second, // > 0
		}

		resolveConfig(cfg, logger)

		assert.True(t, logger.HasWarning("Both Docker events and container polling"),
			"Should warn when both events and polling are enabled")
	})

	t.Run("events_only_no_warning", func(t *testing.T) {
		t.Parallel()
		logger := test.NewTestLogger()
		cfg := &DockerConfig{
			UseEvents:          true,
			DockerPollInterval: 0, // disabled
		}

		resolveConfig(cfg, logger)

		assert.False(t, logger.HasWarning("Both Docker events and container polling"),
			"Should NOT warn when only events are enabled")
	})

	t.Run("polling_only_no_warning", func(t *testing.T) {
		t.Parallel()
		logger := test.NewTestLogger()
		cfg := &DockerConfig{
			UseEvents:          false,
			DockerPollInterval: 10 * time.Second,
		}

		resolveConfig(cfg, logger)

		assert.False(t, logger.HasWarning("Both Docker events and container polling"),
			"Should NOT warn when only polling is enabled (no events)")
	})

	t.Run("neither_no_warning", func(t *testing.T) {
		t.Parallel()
		logger := test.NewTestLogger()
		cfg := &DockerConfig{
			UseEvents:          false,
			DockerPollInterval: 0,
		}

		resolveConfig(cfg, logger)

		assert.False(t, logger.HasWarning("Both Docker events and container polling"),
			"Should NOT warn when neither events nor polling enabled")
	})
}

// TestResolveConfig_ReturnValues verifies resolved values are returned correctly.
func TestResolveConfig_ReturnValues(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	cfg := &DockerConfig{
		ConfigPollInterval: 15 * time.Second,
		DockerPollInterval: 20 * time.Second,
		PollingFallback:    25 * time.Second,
		UseEvents:          true,
	}

	configPoll, dockerPoll, fallback, useEvents := resolveConfig(cfg, logger)

	assert.Equal(t, 15*time.Second, configPoll)
	assert.Equal(t, 20*time.Second, dockerPoll)
	assert.Equal(t, 25*time.Second, fallback)
	assert.True(t, useEvents)
}

// =============================================================================
// GetDockerLabels container filtering mutation tests (lines 276, 284)
// =============================================================================

// TestGetDockerLabels_ContainerNameEmpty targets CONDITIONALS_NEGATION at line 276.
// Container with empty name should be skipped.
func TestGetDockerLabels_ContainerNameEmpty(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{
			{
				Name: "", // empty name
				Labels: map[string]string{
					"ofelia.enabled":              "true",
					"ofelia.job-run.foo.schedule": "@daily",
				},
			},
			{
				Name: "valid-container",
				Labels: map[string]string{
					"ofelia.enabled":              "true",
					"ofelia.job-run.bar.schedule": "@hourly",
				},
			},
		},
	}

	h := &DockerHandler{
		filters:        []string{},
		logger:         test.NewTestLogger(),
		ctx:            context.Background(),
		dockerProvider: mockProvider,
	}

	containers, err := h.GetDockerContainers()
	require.NoError(t, err)

	// Only the container with a valid name should be included
	assert.Len(t, containers, 1, "Only named containers should be included")
	containerNames := make([]string, len(containers))
	for i, container := range containers {
		containerNames[i] = container.Name
	}
	assert.Contains(t, containerNames, "valid-container")
	assert.NotContains(t, containerNames, "")
}

// TestGetDockerLabels_ContainerNoLabels targets CONDITIONALS_BOUNDARY at line 276.
// Container with name but empty labels should be skipped.
func TestGetDockerLabels_ContainerNoLabels(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{
			{
				Name:   "no-labels",
				Labels: map[string]string{}, // empty labels
			},
			{
				Name: "has-labels",
				Labels: map[string]string{
					"ofelia.enabled":              "true",
					"ofelia.job-run.baz.schedule": "@daily",
				},
			},
		},
	}

	h := &DockerHandler{
		filters:        []string{},
		logger:         test.NewTestLogger(),
		ctx:            context.Background(),
		dockerProvider: mockProvider,
	}

	containers, err := h.GetDockerContainers()
	require.NoError(t, err)

	assert.Len(t, containers, 1)
	containerNames := make([]string, len(containers))
	for i, container := range containers {
		containerNames[i] = container.Name
	}
	assert.Contains(t, containerNames, "has-labels")
	assert.NotContains(t, containerNames, "no-labels")
}

// TestGetDockerLabels_OnlyNonOfeliaLabels targets CONDITIONALS_BOUNDARY at line 284.
// Container with labels but none starting with "ofelia." should be skipped.
func TestGetDockerLabels_OnlyNonOfeliaLabels(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{
			{
				Name: "non-ofelia",
				Labels: map[string]string{
					"app":     "myapp",
					"version": "1.0",
				},
			},
		},
	}

	h := &DockerHandler{
		filters:        []string{},
		logger:         test.NewTestLogger(),
		ctx:            context.Background(),
		dockerProvider: mockProvider,
	}

	containers, err := h.GetDockerContainers()
	require.NoError(t, err)

	assert.Empty(t, containers,
		"Container with only non-ofelia labels should produce empty result")
}

// TestGetDockerLabels_MixedOfeliaAndNonOfeliaLabels verifies only ofelia-prefixed
// labels are included. Targets the HasPrefix check at line 280.
func TestGetDockerLabels_MixedLabels(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{
			{
				Name: "mixed",
				Labels: map[string]string{
					"ofelia.enabled":             "true",
					"ofelia.job-run.x.schedule":  "@daily",
					"not-ofelia":                 "ignored",
					"com.docker.compose.project": "test",
				},
			},
		},
	}

	h := &DockerHandler{
		filters:        []string{},
		logger:         test.NewTestLogger(),
		ctx:            context.Background(),
		dockerProvider: mockProvider,
	}

	containers, err := h.GetDockerContainers()
	require.NoError(t, err)

	require.Len(t, containers, 1)
	container := containers[0]
	require.Equal(t, "mixed", container.Name)
	assert.Len(t, container.Labels, 2, "Only 2 ofelia-prefixed labels should be included")
	assert.Contains(t, container.Labels, "ofelia.enabled")
	assert.Contains(t, container.Labels, "ofelia.job-run.x.schedule")
	assert.NotContains(t, container.Labels, "not-ofelia")
}

// TestGetDockerLabels_FilterMerging verifies that filters merge correctly.
// Targets the filter append logic at lines 252-257.
func TestGetDockerLabels_FilterMerging(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{
			{
				Name: "filtered",
				Labels: map[string]string{
					"ofelia.enabled": "true",
				},
			},
		},
	}

	h := &DockerHandler{
		filters:        []string{"label=env=prod", "label=team=backend"},
		logger:         test.NewTestLogger(),
		ctx:            context.Background(),
		dockerProvider: mockProvider,
	}

	containers, err := h.GetDockerContainers()
	require.Len(t, containers, 1)
	container := containers[0]
	require.NoError(t, err)
	assert.Equal(t, "filtered", container.Name)
}

// =============================================================================
// handleEventStreamError mutation tests (lines 296, 303, 310)
// =============================================================================

// TestHandleEventStreamError_AlreadyFailed targets CONDITIONALS_NEGATION at line 296.
// When eventsFailed is already true, should return early without starting another fallback.
func TestHandleEventStreamError_AlreadyFailed(t *testing.T) {
	t.Parallel()

	h := &DockerHandler{
		logger:       test.NewTestLogger(),
		eventsFailed: true, // already marked as failed
	}

	// Should return early (no-op)
	h.handleEventStreamError()

	// eventsFailed should still be true
	h.mu.Lock()
	assert.True(t, h.eventsFailed)
	h.mu.Unlock()
}

// TestHandleEventStreamError_StartsFallback targets CONDITIONALS at line 303.
// When pollingFallback > 0 and not already active, should start fallback polling.
func TestHandleEventStreamError_StartsFallback(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{},
	}

	h := &DockerHandler{
		pollingFallback: 100 * time.Millisecond,
		notifier:        &dummyNotifier{},
		logger:          test.NewTestLogger(),
		ctx:             ctx,
		dockerProvider:  mockProvider,
	}

	h.handleEventStreamError()

	// Wait briefly for goroutine to start
	time.Sleep(50 * time.Millisecond)

	h.mu.Lock()
	assert.True(t, h.eventsFailed)
	assert.True(t, h.fallbackPollingActive)
	h.mu.Unlock()

	// Clean up
	cancel()
}

// TestHandleEventStreamError_NoFallback targets CONDITIONALS at line 310.
// When pollingFallback is 0, should log error but not start fallback.
func TestHandleEventStreamError_NoFallback(t *testing.T) {
	t.Parallel()
	logger := test.NewTestLogger()

	h := &DockerHandler{
		pollingFallback: 0,
		logger:          logger,
	}

	h.handleEventStreamError()

	h.mu.Lock()
	assert.True(t, h.eventsFailed)
	assert.False(t, h.fallbackPollingActive)
	h.mu.Unlock()

	assert.True(t, logger.HasError("Docker event stream failed"),
		"Should log error when no fallback configured")
}

// =============================================================================
// clearEventStreamError mutation tests (lines 318-327)
// =============================================================================

// TestClearEventStreamError_NoFallbackCancel verifies behavior when
// fallbackCancel is nil. Targets the nil check at line 322.
func TestClearEventStreamError_NoFallbackCancel(t *testing.T) {
	t.Parallel()

	h := &DockerHandler{
		logger:         test.NewTestLogger(),
		eventsFailed:   true,
		fallbackCancel: nil, // no fallback to cancel
	}

	// Should not panic
	h.clearEventStreamError()

	h.mu.Lock()
	assert.False(t, h.eventsFailed)
	h.mu.Unlock()
}

// =============================================================================
// Shutdown mutation tests (lines 407, 412-413)
// =============================================================================

// TestShutdown_NilCancelAndProvider targets nil checks at lines 407 and 412.
func TestShutdown_NilCancelAndProvider(t *testing.T) {
	t.Parallel()

	h := &DockerHandler{
		cancel:         nil,
		dockerProvider: nil,
		logger:         test.NewTestLogger(),
	}

	// Should not panic with nil cancel and nil provider
	err := h.Shutdown(context.Background())
	assert.NoError(t, err)
}

// TestShutdown_WithCancelAndProvider verifies proper cleanup.
func TestShutdown_WithCancelAndProvider(t *testing.T) {
	t.Parallel()

	cancelCalled := false
	mockProvider := &mockDockerProviderForHandler{}

	h := &DockerHandler{
		cancel:         func() { cancelCalled = true },
		dockerProvider: mockProvider,
		logger:         test.NewTestLogger(),
	}

	err := h.Shutdown(context.Background())
	require.NoError(t, err)
	assert.True(t, cancelCalled, "cancel should have been called")
	assert.Nil(t, h.dockerProvider, "dockerProvider should be nil after shutdown")
}

// =============================================================================
// NewDockerHandler ctx==nil mutation test (line 76)
// =============================================================================

// TestNewDockerHandler_NilContext targets CONDITIONALS_NEGATION at line 76.
// When ctx is nil, it should use context.Background().
func TestNewDockerHandler_NilContext(t *testing.T) {
	t.Parallel()
	mockProvider := &mockDockerProviderForHandler{}
	notifier := &dummyNotifier{}
	logger := test.NewTestLogger()
	cfg := &DockerConfig{} // zero values = no polling, no events

	handler, err := NewDockerHandler(nil, notifier, logger, cfg, mockProvider)
	require.NoError(t, err)
	require.NotNil(t, handler)
	defer handler.Shutdown(context.Background())

	// Handler should have a valid context (not nil)
	assert.NotNil(t, handler.ctx)
}

// =============================================================================
// configPollInterval and dockerPollInterval boundary tests (lines 115, 125)
// =============================================================================

// TestNewDockerHandler_PollIntervalBoundary targets CONDITIONALS_BOUNDARY at lines 115, 125.
// Ensures that polling goroutines are started only when interval > 0.
func TestNewDockerHandler_PollIntervalBoundary(t *testing.T) {
	t.Parallel()

	t.Run("zero_intervals_no_goroutines", func(t *testing.T) {
		t.Parallel()
		mockProvider := &mockDockerProviderForHandler{}
		notifier := &dummyNotifier{}
		logger := test.NewTestLogger()
		cfg := &DockerConfig{
			ConfigPollInterval: 0,
			DockerPollInterval: 0,
			UseEvents:          false,
		}

		handler, err := NewDockerHandler(context.Background(), notifier, logger, cfg, mockProvider)
		require.NoError(t, err)
		require.NotNil(t, handler)
		defer handler.Shutdown(context.Background())

		// With zero intervals, no polling goroutines should start
		assert.Equal(t, time.Duration(0), handler.configPollInterval)
		assert.Equal(t, time.Duration(0), handler.dockerPollInterval)
	})

	t.Run("positive_intervals_start_goroutines", func(t *testing.T) {
		t.Parallel()
		mockProvider := &mockDockerProviderForHandler{
			containers: []domain.Container{},
		}
		notifier := &dummyNotifier{}
		logger := test.NewTestLogger()
		cfg := &DockerConfig{
			ConfigPollInterval: 1 * time.Second,
			DockerPollInterval: 1 * time.Second,
			UseEvents:          false,
		}

		handler, err := NewDockerHandler(context.Background(), notifier, logger, cfg, mockProvider)
		require.NoError(t, err)
		require.NotNil(t, handler)
		defer handler.Shutdown(context.Background())

		assert.Equal(t, 1*time.Second, handler.configPollInterval)
		assert.Equal(t, 1*time.Second, handler.dockerPollInterval)
	})
}

// =============================================================================
// refreshContainerLabels mutation test (line 235)
// =============================================================================

// TestRefreshContainerLabels_NonOfeliaError targets CONDITIONALS_NEGATION at line 235.
// Non-ErrNoContainerWithOfeliaEnabled errors should be logged as debug.
func TestRefreshContainerLabels_NonOfeliaError(t *testing.T) {
	t.Parallel()

	mockProvider := &mockDockerProviderListFail{}
	logger := test.NewTestLogger()
	notifier := &dummyNotifier{}

	h := &DockerHandler{
		filters:        []string{},
		logger:         logger,
		ctx:            context.Background(),
		dockerProvider: mockProvider,
		notifier:       notifier,
	}

	h.refreshContainerLabels()

	// The non-ErrNoContainerWithOfeliaEnabled error should be logged as debug
	assert.True(t, logger.HasMessage("connection refused"),
		"Non-ErrNoContainer error should be logged as debug")
}

// TestRefreshContainerLabels_OfeliaError verifies ErrNoContainerWithOfeliaEnabled
// is NOT logged (silenced).
func TestRefreshContainerLabels_OfeliaError(t *testing.T) {
	t.Parallel()

	mockProvider := &mockDockerProviderForHandler{
		containers: []domain.Container{}, // empty = returns ErrNoContainerWithOfeliaEnabled
	}
	logger := test.NewTestLogger()
	notifier := &dummyNotifier{}

	h := &DockerHandler{
		filters:        []string{},
		logger:         logger,
		ctx:            context.Background(),
		dockerProvider: mockProvider,
		notifier:       notifier,
	}

	h.refreshContainerLabels()

	// ErrNoContainerWithOfeliaEnabled should NOT be logged
	assert.Equal(t, 0, logger.MessageCount(),
		"ErrNoContainerWithOfeliaEnabled should be silenced")
}

// mockDockerProviderListFail returns a generic error from ListContainers
type mockDockerProviderListFail struct {
	mockDockerProviderForHandler
}

func (m *mockDockerProviderListFail) ListContainers(_ context.Context, _ domain.ListOptions) ([]domain.Container, error) {
	return nil, fmt.Errorf("failed to list Docker containers: connection refused")
}
