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

// TestUIHandler_DevDirServesFromDisk pins the OFELIA_UI_DEV_DIR development
// mode: when the env var points at a directory, uiHandler serves files from
// that directory on every request (so an edit is visible on the next request,
// no rebuild), and when it is unset the embedded assets are served.
func TestUIHandler_DevDirServesFromDisk(t *testing.T) {
	dir := t.TempDir()
	marker := "<!doctype html><title>dev-dir marker</title>"
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "layout.html"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OFELIA_UI_DEV_DIR", dir)

	h, err := uiHandler()
	if err != nil {
		t.Fatalf("uiHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dev dir: expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != marker {
		t.Fatalf("dev dir: expected marker file from disk, got %q", rec.Body.String())
	}

	// An edit must be visible on the next request without a new handler.
	// This fixture uses a visible text change, not an HTML comment: the
	// html/template escaper strips HTML comments from its output (documented
	// XSS-hardening behavior), so a comment-only edit would render
	// identically to the pre-edit page and this assertion would FAIL
	// spuriously even if per-request re-parsing is working.
	edited := "<!doctype html><title>dev-dir marker edited</title>"
	if err := os.WriteFile(filepath.Join(dir, "templates", "layout.html"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Body.String() != edited {
		t.Fatalf("dev dir: edit not visible on next request, got %q", rec.Body.String())
	}
}

func TestUIHandler_NoDevDirServesEmbedded(t *testing.T) {
	t.Setenv("OFELIA_UI_DEV_DIR", "")

	h, err := uiHandler()
	if err != nil {
		t.Fatalf("uiHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("embedded: expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "<title>Ofelia</title>") {
		t.Fatalf("embedded: response does not look like the embedded UI (len %d)", len(body))
	}
}
