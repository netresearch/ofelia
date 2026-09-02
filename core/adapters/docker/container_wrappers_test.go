// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/moby/moby/client"

	"github.com/netresearch/ofelia/core/domain"
)

// The adapters in container.go are thin translations between the domain types
// and the Docker SDK. Their error branches are well covered by the nil-input
// and empty-ID tests; their success branches were not, because reaching them
// needs a daemon to answer. A stub daemon supplies the answers, so the
// translation each wrapper performs — request shape out, domain type back — is
// asserted without Docker being installed.
//
// This complements rather than replaces the mock provider used by core: these
// tests pin the SDK boundary, which the mock by definition cannot.

// stubSDK starts an httptest server whose handler is assembled from per-route
// responders and returns an SDK client pointed at it. Routes are matched on a
// path substring, which is enough to distinguish the Docker endpoints and
// keeps the tests readable.
//
// Fragments are tried longest-first, deliberately: several Docker endpoints are
// prefixes of others ("/networks" contains "/networks/create"), and ranging a
// map would pick between them in Go's randomized order, so the same test would
// hit different handlers on different runs.
//
// An unmatched request fails the test and answers 404, so a changed request
// path surfaces as a named failure rather than only as a generic SDK error.
func stubSDK(t *testing.T, routes map[string]http.HandlerFunc) *client.Client {
	t.Helper()

	fragments := make([]string, 0, len(routes))
	for fragment := range routes {
		fragments = append(fragments, fragment)
	}
	sort.Slice(fragments, func(i, j int) bool { return len(fragments[i]) > len(fragments[j]) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, fragment := range fragments {
			if strings.Contains(r.URL.Path, fragment) {
				routes[fragment](w, r)
				return
			}
		}
		t.Errorf("stub daemon received an unrouted request: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return newSDKClientForStubServer(t, srv)
}

// requestRecorder carries observations from a stub route back to the test.
//
// The handler runs on the server's goroutine and the assertions run on the
// test's, so the two need synchronization that does not depend on net/http's
// internals happening to order them: the handlers here assign before writing
// the response, which today gives the client's return a happens-before edge,
// but nothing in the memory model guarantees that and a later edit that moves
// an assignment after the write would make it a genuine race.
type requestRecorder struct {
	mu     sync.Mutex
	path   string
	query  url.Values
	visits int
}

// record captures the request under the lock. Call it from a stub route.
func (rr *requestRecorder) record(r *http.Request) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.path = r.URL.Path
	rr.query = r.URL.Query()
	rr.visits++
}

// param returns a captured query parameter.
func (rr *requestRecorder) param(name string) string {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.query.Get(name)
}

// requestPath returns the captured request path.
func (rr *requestRecorder) requestPath() string {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.path
}

// count returns how many requests the route has served.
func (rr *requestRecorder) count() int {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.visits
}

// stubDaemon wires a container adapter to a stub daemon.
func stubDaemon(t *testing.T, routes map[string]http.HandlerFunc) *ContainerServiceAdapter {
	t.Helper()
	return &ContainerServiceAdapter{client: stubSDK(t, routes)}
}

// writeJSON is the success path every non-streaming stub route needs.
func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("stub daemon failed to encode response: %v", err)
	}
}

func TestContainerServiceAdapter_Create_ReturnsID(t *testing.T) {
	t.Parallel()

	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/containers/create": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{"Id": "created-abc123", "Warnings": []string{}})
		},
	})

	id, err := adapter.Create(context.Background(), &domain.ContainerConfig{
		Image: "alpine:3.20",
		Name:  "ofelia-test",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id != "created-abc123" {
		t.Errorf("Create returned id %q, want created-abc123", id)
	}
}

// TestContainerServiceAdapter_Create_SendsName pins that the container name
// travels as the documented `name` query parameter rather than inside the
// body, which is where the SDK expects it and where a refactor could drop it
// without any local error.
func TestContainerServiceAdapter_Create_SendsName(t *testing.T) {
	t.Parallel()

	var rec requestRecorder
	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/containers/create": func(w http.ResponseWriter, r *http.Request) {
			rec.record(r)
			writeJSON(t, w, map[string]any{"Id": "id", "Warnings": []string{}})
		},
	})

	if _, err := adapter.Create(context.Background(), &domain.ContainerConfig{
		Image: "alpine:3.20",
		Name:  "named-container",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := rec.param("name"); got != "named-container" {
		t.Errorf("daemon `name` query param = %q, want named-container", got)
	}
}

func TestContainerServiceAdapter_Start_Succeeds(t *testing.T) {
	t.Parallel()

	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/start": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	})

	if err := adapter.Start(context.Background(), "abc123"); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// TestContainerServiceAdapter_List_ConvertsContainers covers the conversion
// loop, which is the part of List that carries logic rather than delegation:
// every daemon entry must arrive as a domain.Container in the same order.
func TestContainerServiceAdapter_List_ConvertsContainers(t *testing.T) {
	t.Parallel()

	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/containers/json": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []map[string]any{
				{"Id": "one", "Names": []string{"/first"}, "Image": "alpine"},
				{"Id": "two", "Names": []string{"/second"}, "Image": "busybox"},
			})
		},
	})

	got, err := adapter.List(context.Background(), domain.ListOptions{All: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d containers, want 2", len(got))
	}
	if got[0].ID != "one" || got[1].ID != "two" {
		t.Errorf("List returned ids %q, %q; want one, two (order must be preserved)", got[0].ID, got[1].ID)
	}
}

// TestContainerServiceAdapter_List_EmptyIsNotNil pins that an empty daemon
// response yields an empty slice rather than nil, so callers can range over
// the result unconditionally.
func TestContainerServiceAdapter_List_EmptyIsNotNil(t *testing.T) {
	t.Parallel()

	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/containers/json": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []map[string]any{})
		},
	})

	got, err := adapter.List(context.Background(), domain.ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil {
		t.Fatal("List returned nil for an empty result; want an empty slice")
	}
	if len(got) != 0 {
		t.Errorf("List returned %d containers for an empty daemon response", len(got))
	}
}

func TestContainerServiceAdapter_Wait_ReturnsExitStatus(t *testing.T) {
	t.Parallel()

	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/wait": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{"StatusCode": 137})
		},
	})

	respCh, errCh := adapter.Wait(context.Background(), "abc123")
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Wait reported error: %v", err)
		}
	case resp := <-respCh:
		if resp.StatusCode != 137 {
			t.Errorf("Wait returned status %d, want 137", resp.StatusCode)
		}
	}
}

// TestContainerServiceAdapter_Wait_PropagatesDaemonError covers the branch
// that maps a daemon-side wait error onto the error channel, which is what
// callers use to distinguish "container failed" from "wait failed".
func TestContainerServiceAdapter_Wait_PropagatesDaemonError(t *testing.T) {
	t.Parallel()

	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/wait": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(t, w, map[string]any{"message": "no such container"})
		},
	})

	respCh, errCh := adapter.Wait(context.Background(), "missing")
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Wait on a missing container returned nil error")
		}
	case resp := <-respCh:
		t.Fatalf("Wait on a missing container returned a status (%d) instead of an error", resp.StatusCode)
	}
}

func TestContainerServiceAdapter_Logs_ReturnsStream(t *testing.T) {
	t.Parallel()

	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/logs": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("hello from the container"))
		},
	})

	rc, err := adapter.Logs(context.Background(), "abc123", domain.LogOptions{ShowStdout: true})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer rc.Close()

	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading log stream: %v", err)
	}
	if string(body) != "hello from the container" {
		t.Errorf("log stream = %q, want %q", body, "hello from the container")
	}
}

// TestContainerServiceAdapter_CopyLogs_DemuxesNonTTY covers the stdcopy branch:
// a non-TTY container multiplexes stdout and stderr into one stream with an
// 8-byte header per frame, and CopyLogs must split them back apart. Writing
// both frames to the same writer would pass a naive test, so stdout and stderr
// are asserted separately.
func TestContainerServiceAdapter_CopyLogs_DemuxesNonTTY(t *testing.T) {
	t.Parallel()

	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		// Inspect reports a non-TTY container (Config.Tty absent/false)
		// so CopyLogs takes the demultiplexing path rather than the raw
		// TTY copy.
		"/json": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"Id":    "abc123",
				"Name":  "/demux",
				"State": map[string]any{"Running": false},
				"Config": map[string]any{
					"Image": "alpine",
				},
			})
		},
		"/logs": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(stdcopyFrame(1, "to stdout"))
			_, _ = w.Write(stdcopyFrame(2, "to stderr"))
		},
	})

	var out, errOut strings.Builder
	if err := adapter.CopyLogs(context.Background(), "abc123", &out, &errOut, domain.LogOptions{
		ShowStdout: true,
		ShowStderr: true,
	}); err != nil {
		t.Fatalf("CopyLogs: %v", err)
	}

	if out.String() != "to stdout" {
		t.Errorf("stdout = %q, want %q", out.String(), "to stdout")
	}
	if errOut.String() != "to stderr" {
		t.Errorf("stderr = %q, want %q", errOut.String(), "to stderr")
	}
}

// stdcopyFrame builds one frame of Docker's multiplexed stream format:
// a stream byte (1 = stdout, 2 = stderr), three reserved zero bytes, a
// big-endian uint32 payload length, then the payload.
func stdcopyFrame(stream byte, payload string) []byte {
	header := make([]byte, 8, 8+len(payload))
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

// TestContainerServiceAdapter_SignalWrappers covers Kill, Pause, Unpause and
// Rename together: each is the same shape (delegate, convert the error), so a
// table keeps the duplication that SonarCloud measures on new code down while
// still asserting each one reaches its own endpoint.
func TestContainerServiceAdapter_SignalWrappers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		route    string
		invoke   func(*ContainerServiceAdapter) error
		wantPath string
	}{
		{
			name:     "Kill",
			route:    "/kill",
			invoke:   func(a *ContainerServiceAdapter) error { return a.Kill(context.Background(), "abc", "SIGTERM") },
			wantPath: "/kill",
		},
		{
			name:     "Pause",
			route:    "/pause",
			invoke:   func(a *ContainerServiceAdapter) error { return a.Pause(context.Background(), "abc") },
			wantPath: "/pause",
		},
		{
			name:     "Unpause",
			route:    "/unpause",
			invoke:   func(a *ContainerServiceAdapter) error { return a.Unpause(context.Background(), "abc") },
			wantPath: "/unpause",
		},
		{
			name:     "Rename",
			route:    "/rename",
			invoke:   func(a *ContainerServiceAdapter) error { return a.Rename(context.Background(), "abc", "new") },
			wantPath: "/rename",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var rec requestRecorder
			adapter := stubDaemon(t, map[string]http.HandlerFunc{
				tc.route: func(w http.ResponseWriter, r *http.Request) {
					rec.record(r)
					w.WriteHeader(http.StatusNoContent)
				},
			})

			if err := tc.invoke(adapter); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := rec.requestPath(); !strings.Contains(got, tc.wantPath) {
				t.Errorf("%s hit %q, want a path containing %q", tc.name, got, tc.wantPath)
			}
		})
	}
}

// TestContainerServiceAdapter_Kill_SendsSignal pins the signal as a query
// parameter — the one piece of Kill that is not pure delegation, and the piece
// a caller relies on to stop a job with anything other than the default.
func TestContainerServiceAdapter_Kill_SendsSignal(t *testing.T) {
	t.Parallel()

	var rec requestRecorder
	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/kill": func(w http.ResponseWriter, r *http.Request) {
			rec.record(r)
			w.WriteHeader(http.StatusNoContent)
		},
	})

	if err := adapter.Kill(context.Background(), "abc", "SIGUSR1"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if got := rec.param("signal"); got != "SIGUSR1" {
		t.Errorf("daemon `signal` query param = %q, want SIGUSR1", got)
	}
}

// TestContainerServiceAdapter_Rename_SendsName mirrors the Kill assertion for
// the rename target.
func TestContainerServiceAdapter_Rename_SendsName(t *testing.T) {
	t.Parallel()

	var rec requestRecorder
	adapter := stubDaemon(t, map[string]http.HandlerFunc{
		"/rename": func(w http.ResponseWriter, r *http.Request) {
			rec.record(r)
			w.WriteHeader(http.StatusNoContent)
		},
	})

	if err := adapter.Rename(context.Background(), "abc", "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got := rec.param("name"); got != "renamed" {
		t.Errorf("daemon `name` query param = %q, want renamed", got)
	}
}
