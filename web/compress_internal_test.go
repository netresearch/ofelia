// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCompressMiddlewareBodilessResponse pins the bodiless-status guard: a
// 304 (or 204) must not advertise Content-Encoding: gzip and must not
// grow an empty gzip stream as a body — httptest.ResponseRecorder,
// unlike net/http, records every stray write, so a leaked ~23-byte
// footer would show up here.
func TestCompressMiddlewareBodilessResponse(t *testing.T) {
	t.Parallel()

	for _, code := range []int{http.StatusNoContent, http.StatusNotModified} {
		h := compressMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != code {
			t.Fatalf("status = %d, want %d", rec.Code, code)
		}
		if enc := rec.Header().Get("Content-Encoding"); enc != "" {
			t.Fatalf("%d response carries Content-Encoding %q", code, enc)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("%d response has %d body bytes (leaked gzip framing?)", code, rec.Body.Len())
		}
	}
}
