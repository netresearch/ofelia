// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUIHandler_MidRenderErrorIs500 pins that a template failing partway
// through execution yields a clean 500, not a 200 carrying half a page.
//
// renderUIPage used to execute straight into the ResponseWriter, which
// commits 200 with the bytes written so far as soon as the first byte
// goes out. A later execution error then reached http.Error, which
// appended its message to the half-rendered page behind a superfluous
// WriteHeader — a broken page served as success. The path is reachable
// under OFELIA_UI_DEV_DIR, where templates are re-parsed per request.
func TestUIHandler_MidRenderErrorIs500(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o750); err != nil {
		t.Fatal(err)
	}
	// Parses cleanly; fails at execution time, after the prefix has
	// already been written, because len has nothing to measure.
	broken := "<!doctype html><title>partial</title><p>before{{ len .Missing }}after"
	if err := os.WriteFile(filepath.Join(dir, "templates", "layout.html"), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OFELIA_UI_DEV_DIR", dir)

	h, err := uiHandler()
	if err != nil {
		t.Fatalf("uiHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failed render must answer 500, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<title>partial</title>") {
		t.Fatalf("the error response carries the half-rendered page: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ui render error") {
		t.Fatalf("the error response does not name the failure: %q", rec.Body.String())
	}
}
