package core

import "testing"

func TestEntrypointSlice(t *testing.T) {
	if out := entrypointSlice(nil); out != nil {
		t.Fatalf("expected nil for nil entrypoint, got %#v", out)
	}
	ep := "echo hello"
	out := entrypointSlice(&ep)
	if len(out) != 2 || out[0] != "echo" || out[1] != "hello" {
		t.Fatalf("unexpected entrypoint slice: %#v", out)
	}
}
