// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// newHangingExecServer stubs a daemon whose exec never produces output or
// ends: create answers an exec ID, start upgrades the connection and then
// holds it open, and inspect answers with inspectStatus and inspectBody.
func newHangingExecServer(t *testing.T, inspectStatus int, inspectBody string) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/c1/exec"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id": "exec-123"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/exec/exec-123/start"):
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("stub server cannot hijack")
				return
			}
			conn, buf, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_, _ = buf.WriteString("HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\n" +
				"Connection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			_ = buf.Flush()
			<-release // hold the stream open: the process "keeps running"
			_ = conn.Close()
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/exec/exec-123/json"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(inspectStatus)
			_, _ = w.Write([]byte(inspectBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	return srv
}

// runCanceledExec runs an exec against srv, cancels it after 200ms and fails
// the test if Run does not return within 5s.
func runCanceledExec(t *testing.T, srv *httptest.Server) (int, error) {
	t.Helper()
	adapter := &ExecServiceAdapter{client: newSDKClientForStubServer(t, srv)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(200*time.Millisecond, cancel)

	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		var out, errOut bytes.Buffer
		code, err := adapter.Run(ctx, "c1", &domain.ExecConfig{Cmd: []string{"sleep", "30"}, AttachStdout: true}, &out, &errOut)
		done <- result{code, err}
	}()
	select {
	case r := <-done:
		return r.code, r.err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was canceled")
		return 0, nil
	}
}

func TestExecServiceAdapter_Run_CanceledWhileRunning(t *testing.T) {
	t.Parallel()
	srv := newHangingExecServer(t, http.StatusOK, `{"ID":"exec-123","Running":true,"ExitCode":0,"Pid":4242}`)

	code, err := runCanceledExec(t, srv)

	if !errors.Is(err, domain.ErrExecStillRunning) {
		t.Fatalf("expected ErrExecStillRunning, got: %v", err)
	}
	if !strings.Contains(err.Error(), "exec-123") {
		t.Errorf("error must name the exec ID, got: %v", err)
	}
	if code != -1 {
		t.Errorf("expected exit code -1, got %d", code)
	}
}

func TestExecServiceAdapter_Run_CanceledAfterExit(t *testing.T) {
	t.Parallel()
	srv := newHangingExecServer(t, http.StatusOK, `{"ID":"exec-123","Running":false,"ExitCode":7}`)

	code, err := runCanceledExec(t, srv)
	if err != nil {
		t.Fatalf("an exec that exited must report its exit code, got error: %v", err)
	}
	if code != 7 {
		t.Errorf("expected exit code 7, got %d", code)
	}
}

func TestExecServiceAdapter_Run_CanceledInspectFails(t *testing.T) {
	t.Parallel()
	srv := newHangingExecServer(t, http.StatusInternalServerError, `{"message":"daemon broken"}`)

	_, err := runCanceledExec(t, srv)

	if !errors.Is(err, domain.ErrExecOutcomeUnknown) {
		t.Fatalf("expected ErrExecOutcomeUnknown, got: %v", err)
	}
	if !strings.Contains(err.Error(), "exec-123") {
		t.Errorf("error must name the exec ID, got: %v", err)
	}
}
