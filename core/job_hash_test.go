// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"reflect"
	"testing"
	"time"
)

// hashable is the method set a reload compares jobs by.
type hashable interface{ Hash() (string, error) }

// TestJobHashCoversEveryTaggedField pins issue #876: every `hash:"true"` field
// of every job type, the embedded BareJob ones included, must change the key a
// live reload compares. The fields are found by reflection, so a field added
// later with the tag is covered without touching this test.
func TestJobHashCoversEveryTaggedField(t *testing.T) {
	t.Parallel()

	types := map[string]func() hashable{
		"ExecJob":       func() hashable { return &ExecJob{} },
		"LocalJob":      func() hashable { return &LocalJob{} },
		"ComposeJob":    func() hashable { return &ComposeJob{} },
		"RunServiceJob": func() hashable { return &RunServiceJob{} },
	}

	for typeName, newJob := range types {
		base, err := newJob().Hash()
		if err != nil {
			t.Fatalf("%s: hash of the zero value failed: %v", typeName, err)
		}

		fields := taggedFieldPaths(reflect.TypeOf(newJob()).Elem(), nil)
		if len(fields) == 0 {
			t.Fatalf("%s: no hash-tagged fields found; the reflection walk is broken", typeName)
		}
		for _, path := range fields {
			job := newJob()
			field := reflect.ValueOf(job).Elem().FieldByIndex(path.index)
			setNonZero(t, field)

			got, err := job.Hash()
			if err != nil {
				t.Fatalf("%s.%s: hash failed: %v", typeName, path.name, err)
			}
			if got == base {
				t.Errorf("%s.%s: changing the field does not change Hash(); "+
					"a live reload would keep running the old definition", typeName, path.name)
			}
		}
	}
}

// TestGetHashSupportsUnsignedIntegers pins the kind ExecJob's console size
// fields need: a uint must hash without error, and different values must hash
// differently.
func TestGetHashSupportsUnsignedIntegers(t *testing.T) {
	t.Parallel()

	type uintJob struct {
		U uint `hash:"true"`
	}
	hashOfUint := func(u uint) string {
		var h string
		if err := GetHash(reflect.TypeFor[uintJob](), reflect.ValueOf(uintJob{U: u}), &h); err != nil {
			t.Fatalf("GetHash(uint %d) failed: %v", u, err)
		}
		return h
	}
	if hashOfUint(24) == hashOfUint(80) {
		t.Error("two different uint values hashed alike")
	}
}

type fieldPath struct {
	name  string
	index []int
}

// taggedFieldPaths returns every `hash:"true"` field of t, descending into
// nested structs the same way GetHash does.
func taggedFieldPaths(t reflect.Type, prefix []int) []fieldPath {
	var out []fieldPath
	for field := range t.Fields() {
		index := append(append([]int{}, prefix...), field.Index...)
		if field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeFor[time.Duration]() {
			out = append(out, taggedFieldPaths(field.Type, index)...)
			continue
		}
		if field.Tag.Get(HashmeTagName) == hashmeTagEnabled {
			out = append(out, fieldPath{name: field.Name, index: index})
		}
	}
	return out
}

func setNonZero(t *testing.T, v reflect.Value) {
	t.Helper()
	//nolint:exhaustive // only the kinds hash-tagged fields use
	switch v.Kind() {
	case reflect.String:
		v.SetString("changed")
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int64:
		v.SetInt(v.Int() + 7)
	case reflect.Uint, reflect.Uint64:
		v.SetUint(v.Uint() + 7)
	case reflect.Slice:
		v.Set(reflect.ValueOf([]string{"changed"}))
	case reflect.Pointer:
		s := "changed"
		v.Set(reflect.ValueOf(&s))
	default:
		t.Fatalf("no test value for kind %s", v.Kind())
	}
}
