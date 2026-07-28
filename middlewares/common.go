// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import "reflect"

// IsEmpty reports whether i — which must be a non-nil pointer — points at the
// zero value of its type. The middleware constructors (NewSlack, NewMail,
// NewSave, NewOverlap) use it to tell "the operator configured this" from "the
// config struct was never filled in" and return a nil middleware for the
// latter.
//
// The comparison is reflect.DeepEqual against a freshly allocated zero value,
// so unexported fields are taken into account and a field explicitly set to
// its zero value still counts as empty. Panics if i is nil or not a pointer.
func IsEmpty(i any) bool {
	t := reflect.TypeOf(i).Elem()
	e := reflect.New(t).Interface()

	return reflect.DeepEqual(i, e)
}

// boolVal safely dereferences a *bool, returning false when nil.
func boolVal(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
