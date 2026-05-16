// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"testing"
)

// keyedFakeMiddleware exercises the Key()-based dedup path: two distinct
// instances of the same Go type can coexist in a middlewareContainer when
// they declare distinct keys. Mirrors the *middlewares.Webhook contract
// from #670 / #672 at the core layer, without depending on the middlewares
// package (which would create an import cycle).
type keyedFakeMiddleware struct {
	key string
}

func (k *keyedFakeMiddleware) Run(_ *Context) error { return nil }
func (k *keyedFakeMiddleware) ContinueOnStop() bool { return false }
func (k *keyedFakeMiddleware) Key() string          { return k.key }

// unkeyedFakeMiddleware exercises the legacy type-string fallback path.
// It does NOT implement Key(); middlewareContainer.Use must then dedup by
// reflect.TypeOf(m).String() so the existing 1-per-type middlewares
// (Slack, Mail, Save, Overlap) keep their current semantics.
type unkeyedFakeMiddleware struct {
	name string
}

func (u *unkeyedFakeMiddleware) Run(_ *Context) error { return nil }
func (u *unkeyedFakeMiddleware) ContinueOnStop() bool { return false }

// TestMiddlewareContainer_Use_DistinctKeysKeepsBoth pins the fix for
// https://github.com/netresearch/ofelia/issues/672 at the core layer.
// Pre-fix, middlewareContainer.Use deduped by reflect.TypeOf(m).String(),
// silently dropping any second instance of the same Go type. PR #671
// worked around this at the webhook layer with a composite type; the
// structural fix is here.
//
// Two keyedFakeMiddleware instances with distinct keys ("alpha", "beta")
// must both end up in Middlewares() in insertion order.
func TestMiddlewareContainer_Use_DistinctKeysKeepsBoth(t *testing.T) {
	t.Parallel()

	var c middlewareContainer
	a := &keyedFakeMiddleware{key: "alpha"}
	b := &keyedFakeMiddleware{key: "beta"}
	c.Use(a, b)

	got := c.Middlewares()
	if len(got) != 2 {
		t.Fatalf("Middlewares() len = %d, want 2 (distinct keys must coexist)", len(got))
	}
	if got[0] != a {
		t.Errorf("Middlewares()[0] = %p, want %p (alpha; insertion order)", got[0], a)
	}
	if got[1] != b {
		t.Errorf("Middlewares()[1] = %p, want %p (beta; insertion order)", got[1], b)
	}
}

// TestMiddlewareContainer_Use_SameKeyDedups pins that adding two distinct
// instances with the SAME key still dedups to the first (the existing
// "first wins" semantics from the type-string-keyed dedup). Without this
// guarantee, scheduler-level webhook propagation via
// j.Use(s.Middlewares()...) would double-fire when the job already has a
// webhook with the same Config.Name.
func TestMiddlewareContainer_Use_SameKeyDedups(t *testing.T) {
	t.Parallel()

	var c middlewareContainer
	a := &keyedFakeMiddleware{key: "shared"}
	b := &keyedFakeMiddleware{key: "shared"}
	c.Use(a, b)

	got := c.Middlewares()
	if len(got) != 1 {
		t.Fatalf("Middlewares() len = %d, want 1 (same key must dedup)", len(got))
	}
	if got[0] != a {
		t.Errorf("Middlewares()[0] = %p, want %p (first wins)", got[0], a)
	}
}

// TestMiddlewareContainer_Use_UnkeyedFallsBackToType pins the legacy
// behavior for middlewares that do not implement Key(): two distinct
// instances of the same Go type collapse into the first. This preserves
// the Slack/Mail/Save/Overlap "1-per-type" semantics so j.Use(s.Middlewares()...)
// propagation does not silently double-send notifications.
func TestMiddlewareContainer_Use_UnkeyedFallsBackToType(t *testing.T) {
	t.Parallel()

	var c middlewareContainer
	a := &unkeyedFakeMiddleware{name: "first"}
	b := &unkeyedFakeMiddleware{name: "second"}
	c.Use(a, b)

	got := c.Middlewares()
	if len(got) != 1 {
		t.Fatalf("Middlewares() len = %d, want 1 (unkeyed same-type must dedup via reflect.TypeOf)", len(got))
	}
	if got[0] != a {
		t.Errorf("Middlewares()[0] = %p, want %p (first wins on type-fallback dedup)", got[0], a)
	}
}

// TestMiddlewareContainer_Use_KeyedAndUnkeyedCoexist pins that a keyed
// middleware and an unkeyed one share no namespace: the keyed middleware
// is indexed under its Key() string, the unkeyed one under its
// reflect.TypeOf string. They cannot collide by construction.
func TestMiddlewareContainer_Use_KeyedAndUnkeyedCoexist(t *testing.T) {
	t.Parallel()

	var c middlewareContainer
	k := &keyedFakeMiddleware{key: "alpha"}
	u := &unkeyedFakeMiddleware{name: "first"}
	c.Use(k, u)

	got := c.Middlewares()
	if len(got) != 2 {
		t.Fatalf("Middlewares() len = %d, want 2 (different key namespaces must coexist)", len(got))
	}
}

// TestMiddlewareContainer_Use_EmptyKeyFallsBackToType pins that a
// middleware that implements Key() but returns the empty string is
// treated the same as an unkeyed middleware (type-string fallback). The
// WebhookMiddleware composite added in PR #671 uses this opt-out: it
// implements Key() returning "" so it stays 1-per-job (the composite
// wraps multiple webhooks internally).
func TestMiddlewareContainer_Use_EmptyKeyFallsBackToType(t *testing.T) {
	t.Parallel()

	a := &emptyKeyedFake{}
	b := &emptyKeyedFake{}

	var c middlewareContainer
	c.Use(a, b)

	got := c.Middlewares()
	if len(got) != 1 {
		t.Fatalf("Middlewares() len = %d, want 1 (empty Key() must fall back to type dedup)", len(got))
	}
	if got[0] != a {
		t.Errorf("Middlewares()[0] = %p, want %p (first wins on empty-key type fallback)", got[0], a)
	}
}

// emptyKeyedFake implements Middleware + Key() returning "". Used by
// TestMiddlewareContainer_Use_EmptyKeyFallsBackToType to lock in the
// opt-out path: a middleware that explicitly declines a custom key gets
// the legacy type-string dedup, NOT a distinct empty-string-keyed slot.
type emptyKeyedFake struct{}

func (e *emptyKeyedFake) Run(_ *Context) error { return nil }
func (e *emptyKeyedFake) ContinueOnStop() bool { return false }
func (e *emptyKeyedFake) Key() string          { return "" }
