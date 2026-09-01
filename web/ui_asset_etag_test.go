// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEmbeddedAssetsRevalidate pins that the embedded UI assets carry a
// validator and answer 304 to a conditional request.
//
// Files served out of the embedded FS carry no ModTime, so
// http.ServeContent had nothing to build a Last-Modified from and set no
// ETag either. Every asset therefore came back 200 with a full body,
// recompressed per request, however often the browser asked — and the
// rate limiter counts each of those requests.
func TestEmbeddedAssetsRevalidate(t *testing.T) {
	t.Setenv("OFELIA_UI_DEV_DIR", "")

	h, err := uiHandler()
	if err != nil {
		t.Fatalf("uiHandler: %v", err)
	}

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("asset: expected 200, got %d", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("embedded assets must carry an ETag, or they can never 304")
	}
	if first.Body.Len() == 0 {
		t.Fatal("asset body is empty")
	}

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	h.ServeHTTP(second, req)
	if second.Code != http.StatusNotModified {
		t.Fatalf("a matching If-None-Match must answer 304, got %d", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("a 304 must carry no body, got %d bytes", second.Body.Len())
	}

	// A stale validator must still deliver the asset.
	req = httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("If-None-Match", `"stale"`)
	third := httptest.NewRecorder()
	h.ServeHTTP(third, req)
	if third.Code != http.StatusOK {
		t.Fatalf("a stale If-None-Match must answer 200, got %d", third.Code)
	}
	if third.Body.Len() != first.Body.Len() {
		t.Fatalf("body length differs between unconditional responses: %d vs %d",
			third.Body.Len(), first.Body.Len())
	}
}
