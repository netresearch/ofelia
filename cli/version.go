// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

// Version info set via ldflags at build time.
var (
	Version string // e.g. "v0.21.4" or "main"
	Build   string // e.g. "abc1234" or "03-17-2026_12_00_00"
)

// VersionString returns a human-readable version string.
// For release builds (ldflags set), it returns "ofelia v0.21.4 (abc1234)".
// For dev builds (no ldflags), it uses runtime/debug.BuildInfo for Go version and VCS info.
func VersionString() string {
	if Version != "" {
		if Build != "" {
			return fmt.Sprintf("ofelia %s (%s)", Version, Build)
		}
		return fmt.Sprintf("ofelia %s", Version)
	}

	// Dev build — extract info from Go's embedded build info
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "ofelia dev"
	}

	return fmt.Sprintf("ofelia dev (%s)", strings.Join(devBuildParts(info), ", "))
}

// devBuildParts assembles the parenthesised components for a dev-build version
// string: Go toolchain version, short VCS revision, and "dirty" when there are
// uncommitted changes.
func devBuildParts(info *debug.BuildInfo) []string {
	parts := []string{info.GoVersion}
	var vcsRev, vcsModified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			vcsRev = s.Value
			if len(vcsRev) > 7 {
				vcsRev = vcsRev[:7]
			}
		case "vcs.modified":
			if s.Value != "false" && s.Value != "" {
				vcsModified = "dirty"
			}
		}
	}
	if vcsRev != "" {
		parts = append(parts, vcsRev)
	}
	if vcsModified != "" {
		parts = append(parts, vcsModified)
	}
	return parts
}

// VersionCommand prints version information.
type VersionCommand struct{}

// Execute prints the version string.
func (c *VersionCommand) Execute(_ []string) error {
	_, _ = fmt.Fprintln(os.Stdout, VersionString())
	return nil
}
