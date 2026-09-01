// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/netresearch/ofelia/static"
)

// parseUITemplates parses the page templates from fsys, whose root must
// contain a templates/ directory (the embedded ui/ tree or a dev dir).
func parseUITemplates(fsys fs.FS) (*template.Template, error) {
	tpl, err := template.ParseFS(fsys, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse UI templates: %w", err)
	}
	return tpl, nil
}

// uiHandler serves the web UI: GET / renders the page from the
// html/template partials in templates/, every other path is served as a
// static asset. Returns an error when the embedded assets cannot be
// opened or the embedded templates do not parse (fail fast at startup).
//
// Development mode: when OFELIA_UI_DEV_DIR names a directory, assets are
// read and templates re-parsed from it on every request, so an edit is
// visible on the next reload without rebuilding the binary. A template
// parse error is returned as a 500 with the error text so the developer
// sees what broke. Unset in production; embedded assets are the default.
func uiHandler() (http.Handler, error) {
	if dir := os.Getenv("OFELIA_UI_DEV_DIR"); dir != "" {
		files := http.FileServer(http.Dir(dir))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isTemplateSourcePath(r.URL.Path) {
				http.NotFound(w, r)
				return
			}
			if !isUIPagePath(r.URL.Path) {
				files.ServeHTTP(w, r)
				return
			}
			tpl, err := parseUITemplates(os.DirFS(dir))
			if err != nil {
				http.Error(w, "ui template parse error: "+err.Error(), http.StatusInternalServerError)
				return
			}
			renderUIPage(w, tpl)
		}), nil
	}

	uiFS, err := fs.Sub(static.UI, "ui")
	if err != nil {
		return nil, fmt.Errorf("load UI subdirectory: %w", err)
	}
	tpl, err := parseUITemplates(uiFS)
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(uiFS))
	etag, err := embeddedAssetsETag(uiFS)
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isTemplateSourcePath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		if !isUIPagePath(r.URL.Path) {
			// Embedded files carry no ModTime, so http.ServeContent had
			// nothing to build a validator from and every asset came back
			// 200 with a full body — recompressed per request — however
			// often the browser asked. One ETag for the whole embedded
			// tree is enough: it is per-URL on the client side, and the
			// assets can only change together, with the binary.
			//
			// It is deliberately not varied per content-coding. gzhttp
			// can suffix the ETag of a compressed response, but the
			// suffix is added on the way out and never removed on the way
			// in, so the browser would echo a tag ServeContent cannot
			// match and no request would ever 304 — the whole point of
			// setting one. Vary: Accept-Encoding (which gzhttp sets) is
			// what keeps a shared cache from serving the wrong coding.
			w.Header().Set(headerETag, etag)
			files.ServeHTTP(w, r)
			return
		}
		renderUIPage(w, tpl)
	}), nil
}

// embeddedAssetsETag derives a strong validator for the embedded UI tree
// from the names and contents of its files, so it changes exactly when a
// rebuild changes an asset. http.ServeContent answers 304 against it.
func embeddedAssetsETag(fsys fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		fmt.Fprintf(h, "%s:%d:", path, len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("fingerprint UI assets: %w", err)
	}
	return `"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`, nil
}

// isTemplateSourcePath reports whether the request points into the
// templates/ directory. The templates are inputs to the rendered page,
// not assets: without this guard the file server would list the
// directory and serve the unexecuted {{template}} source — and once the
// render seam carries server-injected values, the raw route would
// silently bypass them.
func isTemplateSourcePath(path string) bool {
	return path == "/templates" || strings.HasPrefix(path, "/templates/")
}

// isUIPagePath reports whether the request is for the rendered page
// rather than a static asset. /index.html is included so old bookmarks
// keep working instead of 404ing against the deleted file.
func isUIPagePath(path string) bool {
	return path == "/" || path == "/index.html"
}

// renderUIPage executes the layout template, which pulls in the tab
// partials. Template data is nil for now; the parameter of
// ExecuteTemplate is the seam for future server-injected values.
//
// The page is rendered into a buffer first. Executing straight into the
// ResponseWriter commits a 200 with the bytes written so far the moment
// the template fails mid-execution — reachable under OFELIA_UI_DEV_DIR,
// where the templates are re-parsed per request — and http.Error then
// appended its message to the half-rendered page behind a superfluous
// WriteHeader. With the buffer, a failed render is a clean 500.
func renderUIPage(w http.ResponseWriter, tpl *template.Template) {
	var page bytes.Buffer
	if err := tpl.ExecuteTemplate(&page, "layout.html", nil); err != nil {
		http.Error(w, "ui render error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set(headerContentType, "text/html; charset=utf-8")
	_, _ = w.Write(page.Bytes())
}
