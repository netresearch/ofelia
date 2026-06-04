// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

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
// This implementation uses context-based cancellation for safe channel management.
type EventServiceAdapter struct {
	client *client.Client
}

// checkClient returns ErrNilDockerClient if the embedded SDK client is nil.
// See docker.ErrNilDockerClient for rationale.
func (s *EventServiceAdapter) checkClient() error {
	if s.client == nil {
		return ErrNilDockerClient
	}
	return nil
}

// Subscribe subscribes to Docker events.
// The returned channels are closed when the context is canceled or an error occurs.
// The caller should NOT close these channels.
//
// If the embedded SDK client is nil (defense-in-depth), both channels are
// closed synchronously after pushing ErrNilDockerClient on errCh — no
// goroutine is launched.
func (s *EventServiceAdapter) Subscribe(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
	eventCh := make(chan domain.Event, 100)
	errCh := make(chan error, 1)

	if err := s.checkClient(); err != nil {
		errCh <- err
		close(eventCh)
		close(errCh)
		return eventCh, errCh
	}

	go s.streamEvents(ctx, filter, eventCh, errCh)

	return eventCh, errCh
}

// buildEventListOptions converts a domain.EventFilter into the SDK events.ListOptions.
func buildEventListOptions(filter domain.EventFilter) events.ListOptions {
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
	return opts
}

// streamEvents drives the event loop for Subscribe. It is launched as a
// goroutine and owns both eventCh and errCh until it returns (deferred
// closes guarantee exactly-once close).
func (s *EventServiceAdapter) streamEvents(ctx context.Context, filter domain.EventFilter, eventCh chan domain.Event, errCh chan error) {
	defer close(eventCh)
	defer close(errCh)

	opts := buildEventListOptions(filter)

	// Subscribe to events from SDK
	// The SDK handles cleanup automatically when context is canceled
	sdkEventCh, sdkErrCh := s.client.Events(ctx, opts)

	for {
		select {
		case <-ctx.Done():
			// Context canceled - clean exit
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
}

// SubscribeWithCallback subscribes to events with a callback.
func (s *EventServiceAdapter) SubscribeWithCallback(
	ctx context.Context,
	filter domain.EventFilter,
	callback ports.EventCallback,
) error {
	if err := s.checkClient(); err != nil {
		return err
	}
	eventChan, errChan := s.Subscribe(ctx, filter)

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errChan:
			if err != nil {
				return err
			}
			return nil
		case event, ok := <-eventChan:
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
// Returns the zero domain.Event when e is nil — defense-in-depth for
// the Subscribe goroutine, see #632. Note that events.Actor is a value
// type (not a pointer), so only the outer *events.Message can be nil.
func convertFromSDKEvent(e *events.Message) domain.Event {
	if e == nil {
		return domain.Event{}
	}

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
