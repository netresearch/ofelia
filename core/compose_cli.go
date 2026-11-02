// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// MinComposeJobsVersion is the first docker compose release that understands
// the top-level `jobs:` section and runs a job with `docker compose run`
// (Compose 5.6.0). Older releases reject the file before running anything.
const MinComposeJobsVersion = "5.6.0"

// ErrComposeJobsUnsupported reports that the docker compose CLI Ofelia would
// run Compose jobs with is missing or older than MinComposeJobsVersion.
var ErrComposeJobsUnsupported = errors.New("docker compose CLI cannot run compose jobs")

// ComposeCLIVersion returns the version `docker compose version --short`
// reports, run with the same Docker endpoint as a compose job (see
// composeChildEnv), without a leading "v".
func ComposeCLIVersion(ctx context.Context) (string, error) {
	env, err := composeChildEnv(os.Environ())
	if err != nil {
		return "", fmt.Errorf("compose docker endpoint: %w", err)
	}
	// Resolve the binary once, as buildCommand does for compose jobs, so the
	// check runs the same docker the jobs will.
	bin, err := exec.LookPath("docker")
	if err != nil {
		return "", fmt.Errorf("look path %q: %w", "docker", err)
	}
	cmd := exec.CommandContext(ctx, bin, "compose", "version", "--short") // #nosec G204 -- LookPath'd docker, constant args
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker compose version: %w", err)
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "v"), nil
}

// CheckComposeJobsSupport returns an error wrapping ErrComposeJobsUnsupported
// when the docker compose CLI is missing or older than MinComposeJobsVersion,
// naming the version it found.
func CheckComposeJobsSupport(ctx context.Context) error {
	version, err := ComposeCLIVersion(ctx)
	if err != nil {
		return fmt.Errorf("%w (compose %s or later is required): %w", ErrComposeJobsUnsupported, MinComposeJobsVersion, err)
	}
	if !ComposeVersionAtLeast(version, MinComposeJobsVersion) {
		return fmt.Errorf("%w: found docker compose %s, compose %s or later is required",
			ErrComposeJobsUnsupported, version, MinComposeJobsVersion)
	}
	return nil
}

// ComposeVersionAtLeast reports whether version is min or later, comparing
// the numeric major, minor and patch parts. A pre-release or build suffix is
// ignored, and a version that does not parse counts as older.
func ComposeVersionAtLeast(version, minimum string) bool {
	v, ok := parseVersion(version)
	if !ok {
		return false
	}
	m, ok := parseVersion(minimum)
	if !ok {
		return false
	}
	for i := range v {
		if v[i] != m[i] {
			return v[i] > m[i]
		}
	}
	return true
}

func parseVersion(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
