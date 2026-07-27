// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestConfigGlobalKeysAreDocumented walks every mapstructure tag on the
// embedded middleware Config structs in Config.Global and asserts that at
// least one of the operator-facing docs files mentions the key. This is a
// coarse drift detector: it catches struct fields that ship without any
// documentation (the same failure mode as the docs-vs-code drift surfaced
// by issues #604 and #621, but in the opposite direction).
//
// The check is intentionally lenient: a single substring match in any of the
// scanned docs counts. It is meant to catch gross drift, not enforce
// per-field prose. Webhook keys, for example, are primarily documented in
// docs/webhooks.md (CONFIGURATION.md links to it) and that's fine.
//
// See https://github.com/netresearch/ofelia/issues/621
func TestConfigGlobalKeysAreDocumented(t *testing.T) {
	t.Parallel()

	docs := loadDocs(t,
		[]string{"docs", "CONFIGURATION.md"},
		[]string{"docs", "webhooks.md"},
		[]string{"docs", "QUICK_REFERENCE.md"},
		[]string{"docs", "TROUBLESHOOTING.md"},
		[]string{"README.md"},
	)

	// Walk Config.Global; for every embedded struct (squash), pull each field's
	// mapstructure tag (stripped of options) and verify the docs mention it.
	//
	// Robust against future Config restructuring: look up the Global field by
	// name rather than by index 0, so a refactor that adds a sibling field
	// before Global doesn't silently change what this test inspects.
	globalField, ok := reflect.TypeFor[Config]().FieldByName("Global")
	if !ok {
		t.Fatal("Config.Global field not found - did the struct get renamed?")
	}
	globalT := globalField.Type
	// Walk both embedded middleware configs (anonymous fields) AND the direct
	// fields declared inline on Global (LogLevel, EnableWeb, WebTrustedProxies,
	// notification-cooldown, etc.). The direct-field walk closed the gap from
	// #635 / #656 — every operator-tunable key on Global is now drift-checked
	// against the docs, regardless of whether it lives on an embedded struct
	// or directly on Global.
	for f := range globalT.Fields() {
		if f.Anonymous {
			for sub := range f.Type.Fields() {
				assertDocumented(t, docs, sub, f.Type.Name())
			}
			continue
		}
		assertDocumented(t, docs, f, "Config.Global")
	}
}

// assertDocumented checks that the mapstructure tag on a struct field is
// mentioned in the concatenated docs blob. The container name is used for the
// error message so failures point at the originating struct (embedded
// middleware config name, or "Global" for direct fields — note: anonymous
// struct types report an empty Name(), in which case the message just
// references the field name).
func assertDocumented(t *testing.T, docs string, f reflect.StructField, container string) {
	t.Helper()
	tag := f.Tag.Get("mapstructure")
	if tag == "" {
		return
	}
	name := strings.SplitN(tag, ",", 2)[0]
	if name == "" || name == "-" {
		return // squash on the embed itself, or explicitly ignored
	}
	if !strings.Contains(docs, name) {
		t.Errorf("operator docs do not mention global key %q (from %s.%s) - drift detected",
			name, container, f.Name)
	}
}

// loadDocs concatenates the contents of the given repo-relative files into a
// single string for substring scanning by the drift test.
func loadDocs(t *testing.T, files ...[]string) string {
	t.Helper()
	var sb strings.Builder
	for _, parts := range files {
		path := findRepoFile(t, parts...)
		b, err := os.ReadFile(path) // #nosec G304 -- test reads repo file by computed path
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// findRepoFile locates a repo-relative file from a test running in the cli/
// package. Walks up from the test file's directory until the file is found.
func findRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for range 6 {
		candidate := filepath.Join(append([]string{dir}, parts...)...)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("could not locate repo file %v from %s", parts, thisFile)
	return ""
}
