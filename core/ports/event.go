package ports

import (
	"context"

	"github.com/netresearch/ofelia/core/domain"
)

// EventService provides operations for subscribing to Docker events.
// This interface is designed to fix the go-dockerclient issue #911 by using
// context-based cancellation instead of manual channel management.
type EventService interface {
	// Subscribe returns channels that receive Docker events.
	// The events channel receives events matching the filter.
	// The errors channel receives any errors during subscription.
	//
	// Both channels are closed when the context is cancelled or an error occurs.
	// The caller should NOT close these channels; they are managed by the implementation.
	//
	// Example usage:
	//   ctx, cancel := context.WithCancel(context.Background())
	//   defer cancel() // This cleanly stops event streaming
	//
	//   events, errs := client.Events().Subscribe(ctx, filter)
	//   for {
	//       select {
	//       case event := <-events:
	//           // Handle event
	//       case err := <-errs:
	//           // Handle error
	//           return
	//       }
	//   }
	Subscribe(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error)

	// SubscribeWithCallback provides callback-based event subscription.
	// The callback is invoked for each event received.
	// This method blocks until the context is cancelled or an error occurs.
	// Returns nil if cancelled cleanly, or an error if subscription fails.
	SubscribeWithCallback(ctx context.Context, filter domain.EventFilter, callback EventCallback) error
}

// EventCallback is called for each Docker event received.
// Return an error to stop the subscription.
type EventCallback func(event domain.Event) error
