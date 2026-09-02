// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/cli"
)

// TestConfigJobCollectionsMatchTheClientRule pins the assumption that
// makes the server-side strip and the client-side backstop agree.
//
// stripJobs (web/server.go) removes every field shaped like a job
// collection — a map keyed by string whose values are structs — because
// a hand-written name list had already drifted and shipped a whole
// collection to /api/config. renderConfigTable (static/ui/app.js) hides
// map-valued keys whose name ends in "Jobs", because a list is inert
// against exactly the case a backstop exists for: a collection from a
// newer server.
//
// The two rules produce identical output only while every collection-
// shaped field in cli.Config is also named *Jobs. A future field that is
// map[string]*SomeStruct but not a job collection would be stripped by
// the server and shown by the client, which is the old leak inverted
// into a silent hide.
//
// This test is where that assumption is checked rather than assumed. It
// deliberately does not move the decision into a struct tag: a tag the
// server trusts exclusively would make an untagged new collection LEAK,
// trading a cosmetic divergence for the failure the shape rule exists to
// prevent. If this test fails, either name the new field *Jobs or give
// stripJobs an explicit exception — do not relax the shape rule.
func TestConfigJobCollectionsMatchTheClientRule(t *testing.T) {
	t.Parallel()

	// The collections known today. A new one appearing here is expected;
	// the assertions below are what must keep holding for it.
	known := map[string]bool{
		"ExecJobs":    true,
		"RunJobs":     true,
		"ServiceJobs": true,
		"LocalJobs":   true,
		"ComposeJobs": true,
	}

	cfgType := reflect.TypeFor[cli.Config]()
	seen := map[string]bool{}
	for i := range cfgType.NumField() {
		f := cfgType.Field(i)
		if !isCollectionShaped(f.Type) {
			if strings.HasSuffix(f.Name, "Jobs") {
				t.Errorf("%s is named like a job collection but is %s: "+
					"the client hides it, the server does not", f.Name, f.Type)
			}
			continue
		}
		seen[f.Name] = true
		if !strings.HasSuffix(f.Name, "Jobs") {
			t.Errorf("%s is shaped like a job collection (%s) but is not named *Jobs: "+
				"the server strips it, the client shows it", f.Name, f.Type)
		}
		if !known[f.Name] {
			t.Logf("new job collection %s — confirm it must not reach /api/config", f.Name)
		}
	}

	for name := range known {
		if !seen[name] {
			t.Errorf("%s is gone or no longer collection-shaped; "+
				"stripJobs may have stopped stripping it", name)
		}
	}
}

// isCollectionShaped mirrors isJobCollection in web/server.go.
func isCollectionShaped(t reflect.Type) bool {
	if t.Kind() != reflect.Map || t.Key().Kind() != reflect.String {
		return false
	}
	elem := t.Elem()
	if elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}
	return elem.Kind() == reflect.Struct
}
