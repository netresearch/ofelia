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

// otherUnkeyedFakeMiddleware is a SECOND legacy-style middleware type used
// to discriminate "empty-key fallback ran" from "fallback always returns
// the same string here". With two distinct Go types, the type-string
// fallback returns two distinct keys and they coexist — which is the
// observation that kills the `if key != ""` ↔ `if key == ""` mutation.
type otherUnkeyedFakeMiddleware struct{}

func (o *otherUnkeyedFakeMiddleware) Run(_ *Context) error { return nil }
func (o *otherUnkeyedFakeMiddleware) ContinueOnStop() bool { return false }

// emptyKeyedFake implements Middleware + Key() returning "". Used to lock
// the opt-out path: a middleware that explicitly declines a custom key
// gets the legacy type-string dedup, NOT a distinct empty-string-keyed
// slot. Mirrors what any keyed-by-default middleware can do if it wants
// to defer per-instance dedup to a wrapper / composite type.
type emptyKeyedFake struct{}

func (e *emptyKeyedFake) Run(_ *Context) error { return nil }
func (e *emptyKeyedFake) ContinueOnStop() bool { return false }
func (e *emptyKeyedFake) Key() string          { return "" }

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

// TestMiddlewareContainer_Use_EmptyKeyMixWithUnkeyed strengthens the
// empty-key fallback assertion: an `emptyKeyedFake` (Key() == "") and an
// `unkeyedFakeMiddleware` of a different type must coexist. They cannot
// collapse because their reflect.TypeOf strings are distinct. This is
// the discriminator that kills the `if key != ""` ↔ `if key == ""`
// mutation cleanly — the prior _EmptyKeyFallsBackToType test used two
// instances of the SAME type and would have passed under either branch.
func TestMiddlewareContainer_Use_EmptyKeyMixWithUnkeyed(t *testing.T) {
	t.Parallel()

	a := &emptyKeyedFake{}
	b := &otherUnkeyedFakeMiddleware{}

	var c middlewareContainer
	c.Use(a, b)

	got := c.Middlewares()
	if len(got) != 2 {
		t.Fatalf("Middlewares() len = %d, want 2 (empty-key fallback + unkeyed of different type must coexist via distinct type strings)", len(got))
	}
}

// TestMiddlewareContainer_Use_InsertionOrderWithDuplicate pins that the
// order slice tracks unique keys only — adding [a-key, b-key, a-dup-key]
// must produce [a, b] in insertion order, with a-dup-key skipped from
// BOTH c.m and c.order. A regression that appended the duplicate to
// c.order while skipping the map update would yield 3 entries from
// Middlewares() (the third lookup hitting the wrong map entry).
func TestMiddlewareContainer_Use_InsertionOrderWithDuplicate(t *testing.T) {
	t.Parallel()

	var c middlewareContainer
	a := &keyedFakeMiddleware{key: "alpha"}
	b := &keyedFakeMiddleware{key: "beta"}
	aDup := &keyedFakeMiddleware{key: "alpha"}
	c.Use(a, b, aDup)

	got := c.Middlewares()
	if len(got) != 2 {
		t.Fatalf("Middlewares() len = %d, want 2 (duplicate key must not append to order slice)", len(got))
	}
	if got[0] != a {
		t.Errorf("Middlewares()[0] = %p, want %p (alpha first-wins)", got[0], a)
	}
	if got[1] != b {
		t.Errorf("Middlewares()[1] = %p, want %p (beta in insertion order, not displaced by aDup)", got[1], b)
	}
}

// TestMiddlewareContainer_Use_SkipsNil pins that nil middlewares are
// silently dropped — a defensive guard that has lived in Use since the
// pre-#672 implementation. Without this, a caller building a slice via
// `var ms []Middleware; ms = append(ms, NewWebhook(nil, l))` (where
// NewWebhook returns (nil, nil) for a nil config) would panic at the
// type assertion inside middlewareKey. Closes the coverage hole flagged
// by the test-effectiveness review on #697.
func TestMiddlewareContainer_Use_SkipsNil(t *testing.T) {
	t.Parallel()

	var c middlewareContainer
	a := &keyedFakeMiddleware{key: "alpha"}
	c.Use(nil, a, nil)

	got := c.Middlewares()
	if len(got) != 1 {
		t.Fatalf("Middlewares() len = %d, want 1 (nil entries must be skipped)", len(got))
	}
	if got[0] != a {
		t.Errorf("Middlewares()[0] = %p, want %p (the only non-nil entry)", got[0], a)
	}
}

// TestMiddlewareContainer_ResetMiddlewares_ClearsThenRebuilds pins the
// behavior of ResetMiddlewares, which is called on every config hot-
// reload at cli/config.go and cli/config_webhook.go. A regression that
// clears c.m but not c.order (or vice versa) would leave the container
// in an inconsistent state where Middlewares() returns dangling nil
// values. Closes a critical coverage hole flagged by the test-
// effectiveness review on #697 (ResetMiddlewares had 0% coverage).
func TestMiddlewareContainer_ResetMiddlewares_ClearsThenRebuilds(t *testing.T) {
	t.Parallel()

	var c middlewareContainer
	old := &keyedFakeMiddleware{key: "old"}
	c.Use(old)
	if len(c.Middlewares()) != 1 {
		t.Fatalf("pre-Reset Middlewares() len = %d, want 1", len(c.Middlewares()))
	}

	// Reset with a fresh middleware set.
	newA := &keyedFakeMiddleware{key: "new-a"}
	newB := &keyedFakeMiddleware{key: "new-b"}
	c.ResetMiddlewares(newA, newB)

	got := c.Middlewares()
	if len(got) != 2 {
		t.Fatalf("post-Reset Middlewares() len = %d, want 2 (old entry cleared, two new entries added)", len(got))
	}
	if got[0] != newA || got[1] != newB {
		t.Errorf("post-Reset order = [%p, %p], want [%p, %p]", got[0], got[1], newA, newB)
	}
	// Verify old is fully gone — its key must not resolve in the dedup map.
	c.Use(&keyedFakeMiddleware{key: "old"})
	got = c.Middlewares()
	if len(got) != 3 {
		t.Fatalf("Use(old-key) after Reset Middlewares() len = %d, want 3 (Reset must have cleared the dedup map so the same key inserts fresh)", len(got))
	}
}
