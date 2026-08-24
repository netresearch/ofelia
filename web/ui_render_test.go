// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUIPage_RendersFullStructure pins the server-rendered page: GET /
// must produce the complete single-page UI assembled from the embedded
// templates. Markers cover every partial so a template accidentally
// dropped from the layout fails here.
func TestUIPage_RendersFullStructure(t *testing.T) {
	t.Setenv("OFELIA_UI_DEV_DIR", "")

	h, err := uiHandler()
	if err != nil {
		t.Fatalf("uiHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected text/html content type, got %q", ct)
	}

	body := rec.Body.String()
	markers := []string{
		`<title>Ofelia</title>`,         // layout head
		`href="styles.css"`,             // layout: css link
		`src="app.js"`,                  // layout: js include
		`id="tab-jobs"`,                 // tabs-jobs partial
		`id="jobSearch"`,                // tabs-jobs partial (search input)
		`id="historyModal"`,             // tabs-jobs partial (history dialog)
		`id="jobForm"`,                  // tabs-form partial
		`id="fields-compose"`,           // tabs-form partial
		`id="tab-removed"`,              // tabs-removed partial
		`id="configTable"`,              // tabs-config partial
		`id="footer-version"`,           // layout footer
		`localStorage.getItem('theme')`, // inline pre-paint script survives
	}
	for _, m := range markers {
		if !strings.Contains(body, m) {
			t.Errorf("rendered page missing %q", m)
		}
	}
}

// TestUIPage_AssetPathsStillServed pins that non-root paths fall through
// to the file server after the render split.
func TestUIPage_AssetPathsStillServed(t *testing.T) {
	t.Setenv("OFELIA_UI_DEV_DIR", "")

	h, err := uiHandler()
	if err != nil {
		t.Fatalf("uiHandler: %v", err)
	}
	for _, path := range []string{"/styles.css", "/app.js", "/pico.min.css"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, rec.Code)
		}
	}
}

// TestUIPage_TemplateSourcesNotServed pins that the template sources are
// render inputs, not assets: the file server must neither list the
// templates/ directory nor serve the raw {{template}} source.
func TestUIPage_TemplateSourcesNotServed(t *testing.T) {
	t.Setenv("OFELIA_UI_DEV_DIR", "")

	h, err := uiHandler()
	if err != nil {
		t.Fatalf("uiHandler: %v", err)
	}
	for _, path := range []string{"/templates", "/templates/", "/templates/layout.html"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, rec.Code)
		}
	}
}
