package cli

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
	"github.com/netresearch/ofelia/test/testutil"
)

// TestDockerHandler_Shutdown tests the Shutdown method
func TestDockerHandler_Shutdown(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() *DockerHandler
		wantErr   bool
	}{
		{
			name: "successful shutdown",
			setupFunc: func() *DockerHandler {
				mockProvider := &mockDockerProviderForHandler{}
				handler, _ := NewDockerHandler(
					context.Background(),
					&dummyNotifier{},
					test.NewTestLogger(),
					&DockerConfig{
						PollInterval:   1 * time.Second,
						DisablePolling: true,
					},
					mockProvider,
				)
				return handler
			},
			wantErr: false,
		},
		{
			name: "shutdown with nil cancel",
			setupFunc: func() *DockerHandler {
				handler := &DockerHandler{
					ctx:            context.Background(),
					cancel:         nil,
					logger:         test.NewTestLogger(),
					dockerProvider: &mockDockerProviderForHandler{},
				}
				return handler
			},
			wantErr: false,
		},
		{
			name: "shutdown with nil provider",
			setupFunc: func() *DockerHandler {
				ctx, cancel := context.WithCancel(context.Background())
				handler := &DockerHandler{
					ctx:            ctx,
					cancel:         cancel,
					logger:         test.NewTestLogger(),
					dockerProvider: nil,
				}
				return handler
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.setupFunc()

			err := handler.Shutdown(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("Shutdown() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify context was canceled
			if handler.cancel != nil && handler.ctx.Err() == nil {
				t.Error("Expected context to be canceled after shutdown")
			}

			// Verify provider is nil after shutdown
			if handler.dockerProvider != nil {
				t.Error("Expected dockerProvider to be nil after shutdown")
			}
		})
	}
}

// TestDockerHandler_watchEvents tests the watchEvents method
func TestDockerHandler_watchEvents(t *testing.T) {
	t.Run("receives container event", func(t *testing.T) {
		mockProvider := &mockEventProvider{
			events: []domain.Event{
				{Type: "container", Action: "start"},
			},
		}
		notifier := &trackingNotifier{
			updated: make(chan struct{}, 1),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		handler := &DockerHandler{
			ctx:            ctx,
			cancel:         cancel,
			dockerProvider: mockProvider,
			notifier:       notifier,
			logger:         test.NewTestLogger(),
			useEvents:      true,
		}

		// Start watchEvents in background
		go handler.watchEvents()

		// Wait for update with timeout
		select {
		case <-notifier.updated:
			// Success - event was received
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for event to be processed")
		}

		if notifier.getUpdateCount() == 0 {
			t.Error("Expected updateCount > 0")
		}
	})

	t.Run("handles error in event stream", func(t *testing.T) {
		mockProvider := &mockEventProvider{
			err: context.Canceled,
		}
		notifier := &trackingNotifier{}

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		handler := &DockerHandler{
			ctx:            ctx,
			cancel:         cancel,
			dockerProvider: mockProvider,
			notifier:       notifier,
			logger:         test.NewTestLogger(),
			useEvents:      true,
		}

		go func() {
			handler.watchEvents()
			close(done)
		}()

		time.Sleep(10 * time.Millisecond)
		cancel()

		testutil.Eventually(t, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}, testutil.WithTimeout(100*time.Millisecond), testutil.WithInterval(5*time.Millisecond))
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		mockProvider := &mockEventProvider{
			blockForever: true,
		}
		notifier := &trackingNotifier{}

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		handler := &DockerHandler{
			ctx:            ctx,
			cancel:         cancel,
			dockerProvider: mockProvider,
			notifier:       notifier,
			logger:         test.NewTestLogger(),
			useEvents:      true,
		}

		go func() {
			handler.watchEvents()
			close(done)
		}()

		cancel()

		testutil.Eventually(t, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}, testutil.WithTimeout(500*time.Millisecond), testutil.WithInterval(5*time.Millisecond))
	})
}

// trackingNotifier tracks dockerLabelsUpdate calls
type trackingNotifier struct {
	mu          sync.Mutex
	updateCount int
	lastLabels  map[DockerContainerInfo]map[string]string
	updated     chan struct{}
}

func (n *trackingNotifier) dockerLabelsUpdate(labels map[DockerContainerInfo]map[string]string) {
	n.mu.Lock()
	n.updateCount++
	n.lastLabels = labels
	n.mu.Unlock()

	if n.updated != nil {
		select {
		case n.updated <- struct{}{}:
		default:
		}
	}
}

func (n *trackingNotifier) getUpdateCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.updateCount
}

// mockEventProvider provides mock event streaming
type mockEventProvider struct {
	mockDockerProviderForHandler
	events       []domain.Event
	err          error
	blockForever bool
}

func (m *mockEventProvider) SubscribeEvents(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
	eventCh := make(chan domain.Event, len(m.events))
	errCh := make(chan error, 1)

	if m.blockForever {
		// Return channels that block forever until context is canceled
		go func() {
			<-ctx.Done()
			close(eventCh)
			close(errCh)
		}()
		return eventCh, errCh
	}

	go func() {
		defer close(eventCh)
		defer close(errCh)

		if m.err != nil {
			errCh <- m.err
			return
		}

		for _, event := range m.events {
			select {
			case <-ctx.Done():
				return
			case eventCh <- event:
			}
		}
	}()

	return eventCh, errCh
}

func (m *mockEventProvider) ListContainers(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error) {
	// Return empty list for event tests
	return []domain.Container{}, nil
}
