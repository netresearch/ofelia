// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"reflect"
	"testing"
)

// The job hash decides whether a job counts as changed, so a hash that fails
// to distinguish two configurations is a silent failure: the job keeps running
// with its old definition and nothing reports it. The existing tests assert
// that GetHash produces a non-empty string, which an implementation returning
// a constant would also satisfy. These pin the property that matters — two
// different values must not hash alike — for every kind appendFieldHash
// accepts, plus the error it owes for the kinds it does not.

// hashOf is the common call these tests make.
func hashOf[T any](t *testing.T, val T) string {
	t.Helper()
	var h string
	if err := GetHash(reflect.TypeFor[T](), reflect.ValueOf(val), &h); err != nil {
		t.Fatalf("GetHash: %v", err)
	}
	return h
}

type hashKindsJob struct {
	Str   string   `hash:"true"`
	Num   int      `hash:"true"`
	Flg   bool     `hash:"true"`
	Items []string `hash:"true"`
	Ptr   *string  `hash:"true"`
}

// TestAppendFieldHash_DistinguishesEachKind walks one field at a time: change
// only that field and the hash has to move. A kind silently dropped from the
// switch would leave the hash identical and this is what catches it.
func TestAppendFieldHash_DistinguishesEachKind(t *testing.T) {
	t.Parallel()

	base := hashKindsJob{Str: "a", Num: 1, Flg: false, Items: []string{"x"}}
	baseHash := hashOf(t, base)

	one, two := "one", "two"
	cases := []struct {
		field string
		val   hashKindsJob
	}{
		{field: "Str", val: hashKindsJob{Str: "b", Num: 1, Flg: false, Items: []string{"x"}}},
		{field: "Num", val: hashKindsJob{Str: "a", Num: 2, Flg: false, Items: []string{"x"}}},
		{field: "Flg", val: hashKindsJob{Str: "a", Num: 1, Flg: true, Items: []string{"x"}}},
		{field: "Items", val: hashKindsJob{Str: "a", Num: 1, Flg: false, Items: []string{"y"}}},
		{field: "Items length", val: hashKindsJob{Str: "a", Num: 1, Flg: false, Items: []string{"x", "x"}}},
		{field: "Ptr set", val: hashKindsJob{Str: "a", Num: 1, Flg: false, Items: []string{"x"}, Ptr: &one}},
	}

	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			if got := hashOf(t, tc.val); got == baseHash {
				t.Errorf("changing %s left the hash unchanged (%q); the job would not be seen as changed",
					tc.field, got)
			}
		})
	}

	// Two different pointer targets must also differ from each other, not just
	// from the nil case.
	if hashOf(t, hashKindsJob{Ptr: &one}) == hashOf(t, hashKindsJob{Ptr: &two}) {
		t.Error("two different *string values hashed alike")
	}
}

// TestAppendFieldHash_SliceEncodingIsUnambiguous pins the length prefix in the
// slice encoding. Concatenating the elements with a separator alone would make
// []string{"a,b"} and []string{"a", "b"} produce the same text, so a job whose
// command list was regrouped would look unchanged. The prefix is what keeps
// those apart, and it is invisible in any test that only checks non-emptiness.
func TestAppendFieldHash_SliceEncodingIsUnambiguous(t *testing.T) {
	t.Parallel()

	type sliceJob struct {
		Items []string `hash:"true"`
	}

	joined := hashOf(t, sliceJob{Items: []string{"a,b"}})
	split := hashOf(t, sliceJob{Items: []string{"a", "b"}})

	if joined == split {
		t.Errorf("[]string{\"a,b\"} and []string{\"a\", \"b\"} hashed alike (%q); "+
			"the element length prefix is not doing its job", joined)
	}
}

// TestAppendFieldHash_NilPointerIsDistinct pins that an unset pointer is not
// interchangeable with a pointer to the empty string. Both carry "no value" to
// a reader, but only one of them means the field was configured.
func TestAppendFieldHash_NilPointerIsDistinct(t *testing.T) {
	t.Parallel()

	type ptrJob struct {
		Ptr *string `hash:"true"`
	}

	empty := ""
	if hashOf(t, ptrJob{Ptr: nil}) == hashOf(t, ptrJob{Ptr: &empty}) {
		t.Error("a nil pointer and a pointer to \"\" hashed alike")
	}
}

// TestAppendFieldHash_RejectsUnsupportedKinds pins the error rather than a
// silent skip: a tagged field whose type the hash cannot represent must
// surface, because ignoring it quietly would drop that field out of change
// detection for good.
func TestAppendFieldHash_RejectsUnsupportedKinds(t *testing.T) {
	t.Parallel()

	type floatJob struct {
		F float64 `hash:"true"`
	}
	type intSliceJob struct {
		S []int `hash:"true"`
	}
	type intPtrJob struct {
		P *int `hash:"true"`
	}

	cases := map[string]any{
		"float":            floatJob{F: 1.5},
		"non-string slice": intSliceJob{S: []int{1}},
		"pointer to int":   intPtrJob{P: new(int)},
	}

	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var h string
			err := GetHash(reflect.TypeOf(val), reflect.ValueOf(val), &h)
			if !errors.Is(err, ErrUnsupportedFieldType) {
				t.Errorf("GetHash error = %v, want ErrUnsupportedFieldType", err)
			}
		})
	}
}

// TestGetHash_SkipsUntaggedFields pins the other half of the contract: a field
// without the hash tag takes no part in the hash at all. Without this, adding
// an unrelated field to a job struct could start triggering "changed" on every
// job that carries it.
func TestGetHash_SkipsUntaggedFields(t *testing.T) {
	t.Parallel()

	type mixedJob struct {
		Counted string `hash:"true"`
		Ignored string
	}

	withoutExtra := hashOf(t, mixedJob{Counted: "a"})
	withExtra := hashOf(t, mixedJob{Counted: "a", Ignored: "anything at all"})

	if withoutExtra != withExtra {
		t.Errorf("an untagged field changed the hash (%q vs %q)", withoutExtra, withExtra)
	}
}
