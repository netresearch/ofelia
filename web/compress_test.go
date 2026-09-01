// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/netresearch/ofelia/core"
	webpkg "github.com/netresearch/ofelia/web"
)

// TestResponseCompression pins the response compression: clients
// advertising a supported codec get compressed pages and API payloads
// (with Content-Length dropped and Vary set), clients advertising none
// get identity responses untouched. This function covers the gzip
// branch — Safari and other clients without zstd; TestZstdCompression
// covers what the mainstream browsers actually receive.
func TestResponseCompression(t *testing.T) {
	t.Parallel()

	sched := &core.Scheduler{Jobs: compressibleJobs(), Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)
	handler := srv.HTTPServer().Handler

	get := func(url string, acceptGzip bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		if acceptGzip {
			req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: unexpected status %d", url, w.Code)
		}
		return w
	}

	// The rendered page, compressed.
	w := get("/", true)
	if enc := w.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("expected gzip encoding, got %q", enc)
	}
	if vary := w.Header().Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Fatalf("Vary must include Accept-Encoding, got %q", vary)
	}
	if cl := w.Header().Get("Content-Length"); cl != "" {
		t.Fatalf("stale Content-Length %q on a compressed response", cl)
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if !strings.Contains(string(body), "<title>Ofelia</title>") {
		t.Fatalf("gunzipped page does not look like the UI")
	}

	// An API response, compressed and still valid JSON.
	w = get("/api/dashboard", true)
	if enc := w.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("expected gzip on /api/dashboard, got %q", enc)
	}
	zr, err = gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	var dash map[string]json.RawMessage
	if err := json.NewDecoder(zr).Decode(&dash); err != nil {
		t.Fatalf("decode gunzipped dashboard: %v", err)
	}
	if _, ok := dash["jobs"]; !ok {
		t.Fatalf("gunzipped dashboard missing jobs section")
	}

	// No Accept-Encoding: identity, readable directly.
	w = get("/", false)
	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("client without gzip support got Content-Encoding %q", enc)
	}
	if !strings.Contains(w.Body.String(), "<title>Ofelia</title>") {
		t.Fatalf("identity response does not look like the UI")
	}
}

// TestZstdCompression pins what real browsers receive. Chrome, Edge and
// Firefox send "gzip, deflate, br, zstd"; the wrapper enables zstd and
// prefers it at equal q-values, so those clients get zstd, not gzip.
// Dropping zstd from the header must fall back to gzip — that is the
// Safari path.
func TestZstdCompression(t *testing.T) {
	t.Parallel()

	sched := &core.Scheduler{Jobs: compressibleJobs(), Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)
	handler := srv.HTTPServer().Handler

	get := func(url, acceptEncoding string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Accept-Encoding", acceptEncoding)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: unexpected status %d", url, w.Code)
		}
		return w
	}

	// The header every mainstream browser sends.
	w := get("/", "gzip, deflate, br, zstd")
	if enc := w.Header().Get("Content-Encoding"); enc != "zstd" {
		t.Fatalf("expected zstd for a browser Accept-Encoding, got %q", enc)
	}
	if vary := w.Header().Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Fatalf("Vary must include Accept-Encoding, got %q", vary)
	}
	if cl := w.Header().Get("Content-Length"); cl != "" {
		t.Fatalf("stale Content-Length %q on a compressed response", cl)
	}
	zr, err := zstd.NewReader(w.Body)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer zr.Close()
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("zstd decode: %v", err)
	}
	if !strings.Contains(string(body), "<title>Ofelia</title>") {
		t.Fatalf("decoded page does not look like the UI")
	}

	// An API payload over zstd is still valid JSON.
	w = get("/api/dashboard", "gzip, deflate, br, zstd")
	if enc := w.Header().Get("Content-Encoding"); enc != "zstd" {
		t.Fatalf("expected zstd on /api/dashboard, got %q", enc)
	}
	dr, err := zstd.NewReader(w.Body)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer dr.Close()
	var dash map[string]json.RawMessage
	if err := json.NewDecoder(dr).Decode(&dash); err != nil {
		t.Fatalf("decode zstd dashboard: %v", err)
	}
	if _, ok := dash["jobs"]; !ok {
		t.Fatalf("decoded dashboard missing jobs section")
	}

	// Without zstd in the header the client gets gzip.
	w = get("/", "gzip, deflate, br")
	if enc := w.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("client without zstd got %q, want gzip", enc)
	}
}

// TestCompressNegotiationEdgeCases pins the paths where compression must
// NOT happen and the sniffing of a Content-Type from uncompressed bytes.
func TestCompressNegotiationEdgeCases(t *testing.T) {
	t.Parallel()

	sched := &core.Scheduler{Jobs: []core.Job{}, Logger: stubDiscardLogger()}
	srv := webpkg.NewServer("", sched, nil, nil)
	// The probe routes (/live) exist only after health registration.
	srv.RegisterHealthEndpoints(webpkg.NewHealthChecker(nil, nil, "test"))
	handler := srv.HTTPServer().Handler

	do := func(mutate func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		mutate(req)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	// "gzip;q=0" is an explicit refusal, not consent.
	w := do(func(r *http.Request) { r.Header.Set("Accept-Encoding", "gzip;q=0") })
	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("client refusing gzip via q=0 got Content-Encoding %q", enc)
	}
	if !strings.Contains(w.Body.String(), "<title>Ofelia</title>") {
		t.Fatalf("q=0 response is not readable identity")
	}

	// A bare wildcard permits gzip but does not name it; gzhttp answers
	// identity, which is always RFC-compliant (compression is optional —
	// the hard requirement is only the refusal direction below). This
	// pin documents the library's behavior, not a contract of ours.
	w = do(func(r *http.Request) { r.Header.Set("Accept-Encoding", "*") })
	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("bare wildcard got Content-Encoding %q, want identity", enc)
	}

	// An explicitly named refusal must never receive gzip, wildcard or
	// not (RFC 9110 §12.5.3).
	w = do(func(r *http.Request) { r.Header.Set("Accept-Encoding", "*, gzip;q=0") })
	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("wildcard followed by gzip;q=0 got Content-Encoding %q", enc)
	}

	// Range requests pass through identity: http.FileServer slices
	// identity bytes and gzipping the slice would corrupt the download.
	w = do(func(r *http.Request) {
		r.URL.Path = "/app.js"
		r.Header.Set("Accept-Encoding", "gzip")
		r.Header.Set("Range", "bytes=0-9")
	})
	if w.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 for ranged asset, got %d", w.Code)
	}
	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("ranged response must stay identity, got Content-Encoding %q", enc)
	}
	if w.Body.Len() != 10 {
		t.Fatalf("expected 10 identity bytes, got %d", w.Body.Len())
	}

	// A handler that sets no Content-Type must be sniffed on the
	// uncompressed bytes — not answer application/x-gzip.
	w = do(func(r *http.Request) {
		r.URL.Path = "/live"
		r.Header.Set("Accept-Encoding", "gzip")
	})
	if w.Code != http.StatusOK {
		t.Fatalf("/live status %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("/live under gzip answered Content-Type %q", ct)
	}
	// And it is not compressed at all: below gzhttp's 1 KiB threshold the
	// framing and the CPU cost buy no fewer bytes on the wire. /live's
	// two-byte "OK" is the clearest case.
	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("a two-byte body was compressed: Content-Encoding %q", enc)
	}
}

// compressibleJobs returns enough jobs for /api/dashboard to exceed
// gzhttp's 1 KiB threshold, which is what a real deployment looks like.
// An empty scheduler produces a payload too small to be worth
// compressing, and the wrapper correctly leaves it alone.
func compressibleJobs() []core.Job {
	jobs := make([]core.Job, 0, 12)
	for i := range 12 {
		j := &testJob{}
		j.Name = fmt.Sprintf("compressible-job-%02d", i)
		j.Schedule = schedDaily
		j.Command = "echo " + strings.Repeat("payload ", 8)
		jobs = append(jobs, j)
	}
	return jobs
}
