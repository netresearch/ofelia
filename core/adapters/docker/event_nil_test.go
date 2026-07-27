// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"testing"

	"github.com/moby/moby/api/types/events"

	"github.com/netresearch/ofelia/core/domain"
)

// Regression test for #632. convertFromSDKEvent dereferenced its
// *events.Message pointer unconditionally — `e.Type`, `e.Action`,
// `e.Actor.ID`, etc. — so any caller that handed it a nil message
// crashed the event consumer goroutine in EventServiceAdapter.Subscribe.
//
// Note: events.Actor is a value type (struct, not pointer), so the
// "nil e.Actor" branch in the issue is technically inaccurate — only
// `e == nil` can panic. A single guard at the top is sufficient.

// TestConvertFromSDKEvent_NilInput pins the contract that
// convertFromSDKEvent returns the zero domain.Event (no panic) when
// called with a nil *events.Message.
func TestConvertFromSDKEvent_NilInput(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "convertFromSDKEvent(nil)")()

	got := convertFromSDKEvent(nil)
	want := domain.Event{}
	if got.Type != want.Type ||
		got.Action != want.Action ||
		got.Scope != want.Scope ||
		got.Actor.ID != want.Actor.ID ||
		got.Actor.Attributes != nil ||
		got.TimeNano != want.TimeNano ||
		!got.Time.IsZero() {
		t.Errorf("convertFromSDKEvent(nil) = %+v, want zero domain.Event", got)
	}
}

// TestConvertFromSDKEvent_ValidInput sanity-checks that the guard does
// not regress the happy path. Mirrors the live EventServiceAdapter
// translation that Subscribe performs on each SDK event.
func TestConvertFromSDKEvent_ValidInput(t *testing.T) {
	t.Parallel()

	msg := &events.Message{
		Type:   events.ContainerEventType,
		Action: events.ActionStart,
		Actor: events.Actor{
			ID:         "container-abc",
			Attributes: map[string]string{"image": "nginx:latest"},
		},
		Scope:    "local",
		Time:     1_700_000_000,
		TimeNano: 1_700_000_000_000_000_000,
	}

	got := convertFromSDKEvent(msg)
	if got.Type != string(events.ContainerEventType) {
		t.Errorf("Type = %q, want %q", got.Type, events.ContainerEventType)
	}
	if got.Action != string(events.ActionStart) {
		t.Errorf("Action = %q, want %q", got.Action, events.ActionStart)
	}
	if got.Actor.ID != "container-abc" {
		t.Errorf("Actor.ID = %q, want %q", got.Actor.ID, "container-abc")
	}
	if got.Scope != "local" {
		t.Errorf("Scope = %q, want %q", got.Scope, "local")
	}
	if got.TimeNano != 1_700_000_000_000_000_000 {
		t.Errorf("TimeNano = %d, want %d", got.TimeNano, int64(1_700_000_000_000_000_000))
	}
}
