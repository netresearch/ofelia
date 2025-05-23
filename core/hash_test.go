package core

import (
	"reflect"
	"testing"
)

// TestGetHashSimple tests getHash with simple struct fields.
func TestGetHashSimple(t *testing.T) {
	type S struct {
		A string `hash:"true"`
		B int    `hash:"true"`
		C bool   `hash:"true"`
	}
	val := S{A: "foo", B: 42, C: true}
	var h string
	if err := getHash(reflect.TypeOf(val), reflect.ValueOf(val), &h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "foo42true"
	if h != want {
		t.Errorf("expected hash %q, got %q", want, h)
	}
}

// TestGetHashNested tests getHash with nested structs.
func TestGetHashNested(t *testing.T) {
	type Inner struct {
		X string `hash:"true"`
	}
	type Outer struct {
		Inner
	}
	val := Outer{Inner: Inner{X: "bar"}}
	var h string
	if err := getHash(reflect.TypeOf(val), reflect.ValueOf(val), &h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "bar"
	if h != want {
		t.Errorf("expected nested hash %q, got %q", want, h)
	}
}

// TestGetHashPanicUnsupported tests that getHash panics on unsupported field types.
func TestGetHashUnsupported(t *testing.T) {
	type Bad struct {
		F float64 `hash:"true"`
	}
	val := Bad{F: 3.14}
	var h string
	if err := getHash(reflect.TypeOf(val), reflect.ValueOf(val), &h); err == nil {
		t.Errorf("expected error on unsupported type")
	}
}
