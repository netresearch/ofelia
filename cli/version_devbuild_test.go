// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"runtime/debug"
	"strings"
	"testing"
)

// devBuildParts is what a dev binary reports when someone asks which build
// they are running — the answer people paste into bug reports. Its inputs come
// from the linker, so the existing VersionString tests can only observe
// whatever the test binary happens to carry. Feeding it BuildInfo directly
// pins the two decisions it makes: shortening the revision, and when a build
// counts as dirty.

func TestDevBuildParts_ShortensRevision(t *testing.T) {
	t.Parallel()

	parts := devBuildParts(&debug.BuildInfo{
		GoVersion: "go1.26.5",
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
		},
	})

	joined := strings.Join(parts, ", ")
	if !strings.Contains(joined, "0123456") {
		t.Errorf("parts = %q, want the 7-character short revision", joined)
	}
	if strings.Contains(joined, "0123456789abcdef") {
		t.Errorf("parts = %q, want the revision shortened, not the full hash", joined)
	}
}

// TestDevBuildParts_ShortRevisionKeptWhole guards the boundary: a revision at
// or below seven characters must survive untouched rather than being sliced.
func TestDevBuildParts_ShortRevisionKeptWhole(t *testing.T) {
	t.Parallel()

	parts := devBuildParts(&debug.BuildInfo{
		GoVersion: "go1.26.5",
		Settings:  []debug.BuildSetting{{Key: "vcs.revision", Value: "abc12"}},
	})

	if joined := strings.Join(parts, ", "); !strings.Contains(joined, "abc12") {
		t.Errorf("parts = %q, want the short revision kept whole", joined)
	}
}

// TestDevBuildParts_DirtyFlag covers the modified check, which treats anything
// other than "false" and the empty string as dirty. Getting this backwards
// would label every clean build dirty, or worse, hide that a binary was built
// from uncommitted changes.
func TestDevBuildParts_DirtyFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		modified  string
		wantDirty bool
	}{
		{name: "explicitly modified", modified: "true", wantDirty: true},
		{name: "explicitly clean", modified: "false", wantDirty: false},
		{name: "absent value", modified: "", wantDirty: false},
		{name: "unexpected value counts as dirty", modified: "maybe", wantDirty: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parts := devBuildParts(&debug.BuildInfo{
				GoVersion: "go1.26.5",
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abc1234"},
					{Key: "vcs.modified", Value: tc.modified},
				},
			})

			joined := strings.Join(parts, ", ")
			if got := strings.Contains(joined, "dirty"); got != tc.wantDirty {
				t.Errorf("parts = %q, dirty=%v, want dirty=%v", joined, got, tc.wantDirty)
			}
		})
	}
}

// TestDevBuildParts_NoVCSInfo covers a build without VCS settings at all,
// which is what `go build` produces from an unpacked source tarball: the Go
// version alone, with no stray empty entries.
func TestDevBuildParts_NoVCSInfo(t *testing.T) {
	t.Parallel()

	parts := devBuildParts(&debug.BuildInfo{GoVersion: "go1.26.5"})

	if len(parts) != 1 || parts[0] != "go1.26.5" {
		t.Errorf("parts = %q, want exactly the Go version", parts)
	}
}

// TestVersionCommand_Execute covers the subcommand itself, so a wiring change
// that stops it printing is caught here rather than by a user running
// `ofelia version`.
func TestVersionCommand_Execute(t *testing.T) {
	t.Parallel()

	if err := (&VersionCommand{}).Execute(nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}
