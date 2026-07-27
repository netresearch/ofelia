// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

// The split SDK types network addresses where the frozen SDK used strings.
// These tests pin the conversion contract at that boundary: the zero
// netip.Prefix stringifies to "invalid Prefix" and the zero netip.Addr to
// "invalid IP", so an unguarded conversion would put those literals into
// domain fields where "" means unset.

func TestParsePrefixOrZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  netip.Prefix
	}{
		{"ipv4 prefix", "10.0.0.0/8", netip.MustParsePrefix("10.0.0.0/8")},
		{"ipv6 prefix", "fd00::/64", netip.MustParsePrefix("fd00::/64")},
		{"empty", "", netip.Prefix{}},
		{"bare address without mask", "10.0.0.1", netip.Prefix{}},
		{"malformed", "not-a-prefix", netip.Prefix{}},
		{"mask out of range", "10.0.0.0/99", netip.Prefix{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parsePrefixOrZero(tt.input); got != tt.want {
				t.Errorf("parsePrefixOrZero(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// An unparseable value must round-trip back to "" rather than to the
// "invalid Prefix" literal netip.Prefix.String() produces for the zero value.
func TestPrefixRoundTripsUnsetToEmptyString(t *testing.T) {
	t.Parallel()

	if got := prefixToString(parsePrefixOrZero("garbage")); got != "" {
		t.Errorf("prefixToString(parsePrefixOrZero(garbage)) = %q, want empty", got)
	}
	if got := addrToString(parseAddrOrZero("garbage")); got != "" {
		t.Errorf("addrToString(parseAddrOrZero(garbage)) = %q, want empty", got)
	}
	if got := prefixToString(parsePrefixOrZero("10.0.0.0/8")); got != "10.0.0.0/8" {
		t.Errorf("prefixToString round-trip = %q, want %q", got, "10.0.0.0/8")
	}
}

func TestParseAddrMapOrEmpty(t *testing.T) {
	t.Parallel()

	if got := parseAddrMapOrEmpty(nil); got != nil {
		t.Errorf("parseAddrMapOrEmpty(nil) = %v, want nil", got)
	}
	if got := parseAddrMapOrEmpty(map[string]string{}); got != nil {
		t.Errorf("parseAddrMapOrEmpty(empty) = %v, want nil", got)
	}

	got := parseAddrMapOrEmpty(map[string]string{
		"gw":  "10.0.0.1",
		"bad": "not-an-ip",
	})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (unparseable entries dropped): %v", len(got), got)
	}
	if got["gw"] != netip.MustParseAddr("10.0.0.1") {
		t.Errorf("got[gw] = %v, want 10.0.0.1", got["gw"])
	}
	if _, ok := got["bad"]; ok {
		t.Error("unparseable entry was kept")
	}
}

func TestParsePlatform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantOK      bool
		wantOS      string
		wantArch    string
		wantVariant string
	}{
		{name: "os and arch", input: "linux/amd64", wantOK: true, wantOS: "linux", wantArch: "amd64"},
		{name: "with variant", input: "linux/arm/v7", wantOK: true, wantOS: "linux", wantArch: "arm", wantVariant: "v7"},
		{name: "empty", input: "", wantOK: false},
		{name: "os only", input: "linux", wantOK: false},
		{name: "missing arch", input: "linux/", wantOK: false},
		{name: "missing os", input: "/amd64", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parsePlatform(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("parsePlatform(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.OS != tt.wantOS || got.Architecture != tt.wantArch || got.Variant != tt.wantVariant {
				t.Errorf("parsePlatform(%q) = %+v, want os=%q arch=%q variant=%q",
					tt.input, got, tt.wantOS, tt.wantArch, tt.wantVariant)
			}
		})
	}
}

// ServerVersionResult dropped GitCommit/GoVersion/KernelVersion/BuildTime as
// typed fields; they are recovered from the Engine component's Details map.
func TestEngineDetail(t *testing.T) {
	t.Parallel()

	components := []system.ComponentVersion{
		{Name: "containerd", Details: map[string]string{"GitCommit": "containerd-sha"}},
		{Name: "Engine", Details: map[string]string{"GitCommit": "engine-sha", "GoVersion": "go1.26.5"}},
	}

	if got := engineDetail(components, "GitCommit"); got != "engine-sha" {
		t.Errorf("engineDetail(GitCommit) = %q, want %q (must not read a non-Engine component)", got, "engine-sha")
	}
	if got := engineDetail(components, "GoVersion"); got != "go1.26.5" {
		t.Errorf("engineDetail(GoVersion) = %q, want %q", got, "go1.26.5")
	}
	if got := engineDetail(components, "BuildTime"); got != "" {
		t.Errorf("engineDetail(missing key) = %q, want empty", got)
	}
	if got := engineDetail(nil, "GitCommit"); got != "" {
		t.Errorf("engineDetail(nil) = %q, want empty", got)
	}
	if got := engineDetail([]system.ComponentVersion{{Name: "Engine"}}, "GitCommit"); got != "" {
		t.Errorf("engineDetail(nil Details) = %q, want empty", got)
	}
}

func TestToSDKFilters(t *testing.T) {
	t.Parallel()

	if got := toSDKFilters(nil); got != nil {
		t.Errorf("toSDKFilters(nil) = %v, want nil", got)
	}
	if got := toSDKFilters(map[string][]string{}); got != nil {
		t.Errorf("toSDKFilters(empty) = %v, want nil", got)
	}

	got := toSDKFilters(map[string][]string{
		"label":  {"a=1", "b=2"},
		"health": {"healthy"},
	})
	want := client.Filters{
		"label":  {"a=1": true, "b=2": true},
		"health": {"healthy": true},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for term, values := range want {
		for v := range values {
			if !got[term][v] {
				t.Errorf("missing filter %s=%s in %v", term, v, got)
			}
		}
	}
}

// Add panics on the zero Filters because it is a nil map, so the helper must
// never hand back a non-nil-intent zero value for a non-empty input.
func TestToSDKFiltersResultIsAddable(t *testing.T) {
	t.Parallel()

	f := toSDKFilters(map[string][]string{"label": {"a=1"}})
	f.Add("event", "start")

	if !f["event"]["start"] {
		t.Error("Add on the returned Filters did not take effect")
	}
}

func TestConsoleSize(t *testing.T) {
	t.Parallel()

	if got := consoleSize(nil); got != (client.ConsoleSize{}) {
		t.Errorf("consoleSize(nil) = %+v, want zero (daemon default)", got)
	}
	if got := consoleSize(&[2]uint{24, 80}); got != (client.ConsoleSize{Height: 24, Width: 80}) {
		t.Errorf("consoleSize([24,80]) = %+v, want height=24 width=80", got)
	}
}

func TestParseHardwareAddrOrZero(t *testing.T) {
	t.Parallel()

	if got := parseHardwareAddrOrZero(""); got != nil {
		t.Errorf("parseHardwareAddrOrZero(empty) = %v, want nil", got)
	}
	if got := parseHardwareAddrOrZero("not-a-mac"); got != nil {
		t.Errorf("parseHardwareAddrOrZero(malformed) = %v, want nil", got)
	}
	if got := parseHardwareAddrOrZero("02:42:ac:11:00:02"); got.String() != "02:42:ac:11:00:02" {
		t.Errorf("parseHardwareAddrOrZero round-trip = %q, want %q", got.String(), "02:42:ac:11:00:02")
	}
}
