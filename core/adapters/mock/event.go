// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package mock

import (
	"context"
	"sync"
	"time"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// EventService is a mock implementation of ports.EventService.
type EventService struct {
	mu sync.RWMutex

	// Callbacks for customizing behavior
	OnSubscribe func(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error)

	// Call tracking
	SubscribeCalls []domain.EventFilter

	// Simulated events to send
	events []domain.Event

	// Error to return during subscription
	subscribeErr error
}

// NewEventService creates a new mock EventService.
func NewEventService() *EventService {
	return &EventService{}
}

// Subscribe subscribes to Docker events.
func (s *EventService) Subscribe(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
	s.mu.Lock()
	s.SubscribeCalls = append(s.SubscribeCalls, filter)
	events := s.events
	subscribeErr := s.subscribeErr
	s.mu.Unlock()

	if s.OnSubscribe != nil {
		return s.OnSubscribe(ctx, filter)
	}

	eventCh := make(chan domain.Event, len(events)+1)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		if subscribeErr != nil {
			errCh <- subscribeErr
			return
		}

		// Send simulated events
		for _, event := range events {
			select {
			case <-ctx.Done():
				return
			case eventCh <- event:
			}
		}

		// Wait for context cancellation
		<-ctx.Done()
	}()

	return eventCh, errCh
}

// SubscribeWithCallback subscribes to events with a callback.
//
// A pending error is reported even when the event channel has already been
// closed. Subscribe's goroutine buffers a configured subscribe error and
// then closes both channels, so once it has run to completion the error and
// the closed event channel are ready in the same instant — and a select
// picks between ready cases at random, which made the error reported or
// silently swallowed on a coin flip. Measured at 89 losses in 200 runs once
// the goroutine got there first, which is what a loaded CI runner arranges
// (#820).
func (s *EventService) SubscribeWithCallback(ctx context.Context, filter domain.EventFilter, callback ports.EventCallback) error {
	events, errs := s.Subscribe(ctx, filter)

	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-errs:
			if !ok {
				// Closed and carrying nothing. Left in the select it would
				// stay permanently ready and keep competing with buffered
				// events for the random pick, ending the stream before they
				// were delivered. A nil channel blocks forever, which takes
				// this arm out for good.
				errs = nil
				continue
			}
			if err != nil {
				return err
			}
			return nil
		case event, ok := <-events:
			if !ok {
				// The stream ended -- but Subscribe closes both channels in
				// one breath after buffering the error, so this arm and the
				// error arm are ready together and the pick between them is
				// random. Reporting success without looking is how the
				// configured error went missing on roughly half the
				// schedules that got here (#820). Checking here rather than
				// before the select leaves no window: this is the only path
				// that turns "no more events" into a nil return.
				select {
				case err := <-errs:
					if err != nil {
						return err
					}
				default:
				}
				return nil
			}
			if err := callback(event); err != nil {
				return err
			}
		}
	}
}

// SetEvents sets the events to send on Subscribe().
func (s *EventService) SetEvents(events []domain.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = events
}

// AddEvent adds an event to send.
func (s *EventService) AddEvent(event domain.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

// AddContainerStopEvent adds a container stop event.
func (s *EventService) AddContainerStopEvent(containerID string) {
	s.AddEvent(domain.Event{
		Type:   domain.EventTypeContainer,
		Action: domain.EventActionDie,
		Actor: domain.EventActor{
			ID: containerID,
		},
		Time: time.Now(),
	})
}

// SetSubscribeError sets an error to return during subscription.
func (s *EventService) SetSubscribeError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribeErr = err
}

// ClearEvents clears all simulated events.
func (s *EventService) ClearEvents() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = nil
}
