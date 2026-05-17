// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core/domain"
)

// TestExecServiceAdapter_Create_NilConfig pins the contract that Create
// returns an error (and does NOT panic) when called with a nil ExecConfig.
//
// Before the fix this panicked on `config.User` at exec.go:27 because
// ExecOptions construction dereferences every config field unconditionally.
//
// Uses a loopback SDK client so the input-validation guard fires before
// the new ErrNilDockerClient guard added in #623 short-circuits the call.
func TestExecServiceAdapter_Create_NilConfig(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "Create with nil config")()

	adapter := &ExecServiceAdapter{client: newLoopbackSDKClient(t)}

	id, err := adapter.Create(context.Background(), "some-container", nil)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
	if id != "" {
		t.Errorf("expected empty exec ID on error, got %q", id)
	}
	if !errors.Is(err, ErrNilExecConfig) {
		t.Errorf("expected errors.Is(err, ErrNilExecConfig), got: %v", err)
	}
}

// TestExecServiceAdapter_Run_NilWritersNonTTY pins the contract that Run
// returns an error (and does NOT panic) when stdout AND stderr are nil
// in non-TTY mode. stdcopy.StdCopy panics on nil writers when there is
// real output to demultiplex, so the adapter must guard the input.
//
// Uses a loopback SDK client so the writer-validation guard fires before
// the new ErrNilDockerClient guard added in #623 short-circuits the call.
func TestExecServiceAdapter_Run_NilWritersNonTTY(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "Run with nil stdout+stderr in non-TTY")()

	adapter := &ExecServiceAdapter{client: newLoopbackSDKClient(t)}

	cfg := &domain.ExecConfig{Cmd: []string{"true"}, Tty: false}
	// Non-TTY mode: stdcopy demuxing path is exercised.
	code, err := adapter.Run(context.Background(), "some-container", cfg, nil, nil)
	if !errors.Is(err, ErrNoExecOutputWriter) {
		t.Errorf("expected errors.Is(err, ErrNoExecOutputWriter), got: %v", err)
	}
	if code != -1 {
		t.Errorf("expected exit code -1 on error, got %d", code)
	}
}

// TestExecServiceAdapter_Create_PropagatesConsoleSize pins the SDK
// boundary for the #235 console-size feature: domain.ExecConfig
// .ConsoleSize must arrive at the Docker daemon's
// /containers/{id}/exec endpoint as the documented [height, width]
// JSON field. Without this test, a refactor that drops
// `ConsoleSize: config.ConsoleSize` from the ExecOptions struct
// literal in exec.go:64 would silently break #235 — and all the
// core-level tests in core/execjob_console_size_test.go would still
// pass because they only assert that the field reaches the mock
// provider, not the SDK.
//
// Uses an httptest server to capture the SDK request body and
// inspect the ConsoleSize field directly.
func TestExecServiceAdapter_Create_PropagatesConsoleSize(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the exec-create POST body for inspection.
		if strings.Contains(r.URL.Path, "/exec") && r.Method == http.MethodPost {
			capturedBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id": "test-exec-id"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	adapter := &ExecServiceAdapter{client: newSDKClientForStubServer(t, srv)}

	cfg := &domain.ExecConfig{
		Cmd:         []string{"echo", "ok"},
		Tty:         true,
		ConsoleSize: &[2]uint{30, 100},
	}
	id, err := adapter.Create(context.Background(), "test-container", cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id != "test-exec-id" {
		t.Errorf("unexpected exec id: %q", id)
	}

	var body struct {
		ConsoleSize *[2]uint
	}
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("unmarshal captured body %q: %v", string(capturedBody), err)
	}
	if body.ConsoleSize == nil {
		t.Fatalf("ConsoleSize missing from exec-create request body; got: %s", string(capturedBody))
	}
	if got := *body.ConsoleSize; got != [2]uint{30, 100} {
		t.Errorf("ConsoleSize = %v, want [30, 100] (#235 [height, width] order)", got)
	}
}

// TestExecServiceAdapter_Create_OmitsConsoleSizeWhenNil pins the
// inverse: a nil ConsoleSize (the default for legacy ExecJobs without
// the new console-height/console-width fields) must NOT send a
// ConsoleSize JSON key — Docker treats omitted as "use default" and
// any populated zero-value {0,0} as an explicit override. The SDK
// uses `omitempty` so this test is really pinning that the adapter
// passes nil through unchanged rather than allocating a zero array.
func TestExecServiceAdapter_Create_OmitsConsoleSizeWhenNil(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/exec") && r.Method == http.MethodPost {
			capturedBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id": "x"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	adapter := &ExecServiceAdapter{client: newSDKClientForStubServer(t, srv)}

	cfg := &domain.ExecConfig{Cmd: []string{"echo", "ok"}, Tty: true /* no ConsoleSize */}
	if _, err := adapter.Create(context.Background(), "test-container", cfg); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// JSON omitempty contract: the field must NOT appear at all,
	// otherwise Docker would interpret {0,0} as an explicit-default
	// override rather than "use whatever the daemon picks".
	if strings.Contains(string(capturedBody), "ConsoleSize") {
		t.Errorf("ConsoleSize unexpectedly present in body: %s", string(capturedBody))
	}
}
