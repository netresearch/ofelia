package docker

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// EventServiceAdapter implements ports.EventService using Docker SDK.
// This implementation uses context-based cancellation to fix the
// go-dockerclient issue #911 (panic on event channel close).
type EventServiceAdapter struct {
	client *client.Client
}

// Subscribe subscribes to Docker events.
// The returned channels are closed when the context is cancelled or an error occurs.
// The caller should NOT close these channels.
func (s *EventServiceAdapter) Subscribe(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
	eventCh := make(chan domain.Event, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		// Build filters
		opts := events.ListOptions{}
		if !filter.Since.IsZero() {
			opts.Since = filter.Since.Format(time.RFC3339Nano)
		}
		if !filter.Until.IsZero() {
			opts.Until = filter.Until.Format(time.RFC3339Nano)
		}
		if len(filter.Filters) > 0 {
			opts.Filters = filters.NewArgs()
			for key, values := range filter.Filters {
				for _, v := range values {
					opts.Filters.Add(key, v)
				}
			}
		}

		// Subscribe to events from SDK
		// The SDK handles cleanup automatically when context is cancelled
		sdkEventCh, sdkErrCh := s.client.Events(ctx, opts)

		for {
			select {
			case <-ctx.Done():
				// Context cancelled - clean exit
				return

			case err := <-sdkErrCh:
				if err != nil {
					errCh <- convertError(err)
				}
				return

			case sdkEvent, ok := <-sdkEventCh:
				if !ok {
					// Channel closed
					return
				}

				// Convert and send event
				event := convertFromSDKEvent(&sdkEvent)
				select {
				case eventCh <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return eventCh, errCh
}

// SubscribeWithCallback subscribes to events with a callback.
func (s *EventServiceAdapter) SubscribeWithCallback(ctx context.Context, filter domain.EventFilter, callback ports.EventCallback) error {
	events, errs := s.Subscribe(ctx, filter)

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			if err != nil {
				return err
			}
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if err := callback(event); err != nil {
				return err
			}
		}
	}
}

// convertFromSDKEvent converts SDK events.Message to domain.Event.
func convertFromSDKEvent(e *events.Message) domain.Event {
	return domain.Event{
		Type:   string(e.Type),
		Action: string(e.Action),
		Actor: domain.EventActor{
			ID:         e.Actor.ID,
			Attributes: e.Actor.Attributes,
		},
		Scope:    e.Scope,
		Time:     time.Unix(e.Time, e.TimeNano),
		TimeNano: e.TimeNano,
	}
}
