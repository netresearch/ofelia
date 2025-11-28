package cli

import (
	"context"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/test"
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

			// Verify context was cancelled
			if handler.cancel != nil && handler.ctx.Err() == nil {
				t.Error("Expected context to be cancelled after shutdown")
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
	tests := []struct {
		name          string
		setupProvider func() *mockEventProvider
		checkNotifier func(*trackingNotifier) bool
		waitDuration  time.Duration
	}{
		{
			name: "receives container event",
			setupProvider: func() *mockEventProvider {
				return &mockEventProvider{
					events: []domain.Event{
						{Type: "container", Action: "start"},
					},
				}
			},
			checkNotifier: func(n *trackingNotifier) bool {
				return n.updateCount > 0
			},
			waitDuration: 200 * time.Millisecond,
		},
		{
			name: "handles error in event stream",
			setupProvider: func() *mockEventProvider {
				return &mockEventProvider{
					err: context.Canceled,
				}
			},
			checkNotifier: func(n *trackingNotifier) bool {
				return true // Just check it doesn't panic
			},
			waitDuration: 200 * time.Millisecond,
		},
		{
			name: "stops on context cancellation",
			setupProvider: func() *mockEventProvider {
				return &mockEventProvider{
					blockForever: true,
				}
			},
			checkNotifier: func(n *trackingNotifier) bool {
				return true // Just check clean shutdown
			},
			waitDuration: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockProvider := tt.setupProvider()
			notifier := &trackingNotifier{}

			ctx, cancel := context.WithCancel(context.Background())

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

			// Wait for events to be processed
			time.Sleep(tt.waitDuration)

			// Cancel context to stop watching
			cancel()

			// Give time for goroutine to exit
			time.Sleep(50 * time.Millisecond)

			if tt.checkNotifier != nil && !tt.checkNotifier(notifier) {
				t.Error("Notifier check failed")
			}
		})
	}
}

// trackingNotifier tracks dockerLabelsUpdate calls
type trackingNotifier struct {
	updateCount int
	lastLabels  map[string]map[string]string
}

func (n *trackingNotifier) dockerLabelsUpdate(labels map[string]map[string]string) {
	n.updateCount++
	n.lastLabels = labels
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
		// Return channels that block forever until context is cancelled
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
