// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package mock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

var errSubscribeSentinel = errors.New("subscribe failed")

// Subscribe buffers a configured error and then closes both channels, so
// once its goroutine has run to completion the pending error and the closed
// event channel are ready in the same instant. A plain select picks between
// ready cases at random, and SubscribeWithCallback returned nil whenever the
// closed-channel arm won — 89 times in 200 runs with the goroutine given a
// head start, and never locally without one, which is why this surfaced as
// an occasional CI failure on unrelated pull requests (#820).
//
// The OnSubscribe hook lets the test hand over channels already in that
// state, so the race is not reproduced by timing but constructed. Against
// the unfixed implementation this fails within a few iterations.
func TestSubscribeWithCallback_ReportsErrorWhenEventChannelAlreadyClosed(t *testing.T) {
	t.Parallel()

	es := mock.NewEventService()
	es.OnSubscribe = func(_ context.Context, _ domain.EventFilter) (<-chan domain.Event, <-chan error) {
		events := make(chan domain.Event)
		close(events)

		errs := make(chan error, 1)
		errs <- errSubscribeSentinel
		close(errs)

		return events, errs
	}

	// Both arms are ready on every iteration, so a random pick would show up
	// as a nil return well before this count is reached.
	for i := range 200 {
		err := es.SubscribeWithCallback(t.Context(), domain.EventFilter{},
			func(domain.Event) error { return nil })
		if !errors.Is(err, errSubscribeSentinel) {
			t.Fatalf("iteration %d: got %v, want the configured subscribe error — "+
				"a closed event channel must not swallow a pending error", i, err)
		}
	}
}

// The end-to-end path through Subscribe, for the case the failing CI test
// exercises: a configured error must come back whichever side of the race
// the goroutine lands on.
func TestSubscribeWithCallback_ReportsConfiguredSubscribeError(t *testing.T) {
	t.Parallel()

	for i := range 50 {
		es := mock.NewEventService()
		es.SetSubscribeError(errSubscribeSentinel)

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		err := es.SubscribeWithCallback(ctx, domain.EventFilter{},
			func(domain.Event) error { return nil })
		cancel()

		if !errors.Is(err, errSubscribeSentinel) {
			t.Fatalf("iteration %d: got %v, want the configured subscribe error", i, err)
		}
	}
}

// The mirror of the same defect: an errs channel that closed without ever
// carrying an error still reads as ready and yields nil, so leaving it in
// the select lets it compete with buffered events for the random pick and
// end the stream early. Taking it out of the select is what stops that.
//
// Iterated for the same reason as the test above — the defect it guards
// against is a coin flip, and a single call passes half the time. One
// iteration was enough to let a mutation of the fix through unnoticed.
func TestSubscribeWithCallback_DeliversBufferedEventsAfterErrChannelClosed(t *testing.T) {
	t.Parallel()

	es := mock.NewEventService()
	es.OnSubscribe = func(_ context.Context, _ domain.EventFilter) (<-chan domain.Event, <-chan error) {
		events := make(chan domain.Event, 2)
		events <- domain.Event{Action: "start"}
		events <- domain.Event{Action: "stop"}
		close(events)

		errs := make(chan error) // closed, never carried an error
		close(errs)

		return events, errs
	}

	for i := range 200 {
		var seen []string
		err := es.SubscribeWithCallback(t.Context(), domain.EventFilter{},
			func(e domain.Event) error {
				seen = append(seen, e.Action)
				return nil
			})
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if len(seen) != 2 || seen[0] != "start" || seen[1] != "stop" {
			t.Fatalf("iteration %d: delivered %v, want both buffered events "+
				"before the stream ends", i, seen)
		}
	}
}
