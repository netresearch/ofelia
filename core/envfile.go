// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/netresearch/ofelia/core/domain"
)

// ErrNilContainerInspector is returned when ResolveEnvFrom is called with a nil provider.
var ErrNilContainerInspector = errors.New("container inspector is nil")

// ContainerInspector is a narrow interface for inspecting containers.
// Satisfied by DockerProvider.
type ContainerInspector interface {
	InspectContainer(ctx context.Context, containerID string) (*domain.Container, error)
}

// ParseEnvFile reads an env file and returns KEY=VALUE pairs.
// Skips blank lines, lines starting with #, and lines without =.
// Strips optional "export " prefix and surrounding quotes from values.
func ParseEnvFile(path string) ([]string, error) {
	f, err := os.Open(path) // #nosec G304 -- path is user-configured env-file, intentional
	if err != nil {
		return nil, fmt.Errorf("open env file: %w", err)
	}
	defer f.Close()

	var result []string
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024) // Support long values (certs, JSON blobs)
	for scanner.Scan() {
		if entry, ok := parseEnvLine(scanner.Text()); ok {
			result = append(result, entry)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read env file: %w", err)
	}

	return result, nil
}

// parseEnvLine parses a single env-file line into a KEY=VALUE entry.
// Returns the entry and true on success; returns "", false for blank lines,
// comments, and lines without a valid key=value assignment.
func parseEnvLine(raw string) (string, bool) {
	line := strings.TrimSpace(raw)

	// Skip blank lines and comments
	if line == "" || strings.HasPrefix(line, "#") {
		return "", false
	}

	// Strip "export " prefix
	line = strings.TrimPrefix(line, "export ")

	// Must contain = to be a valid env assignment
	idx := strings.IndexByte(line, '=')
	if idx <= 0 {
		return "", false // skip lines without = or with empty key (e.g., "=value")
	}

	key := line[:idx]
	value := line[idx+1:]

	// Strip surrounding quotes from value
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}

	return key + "=" + value, true
}

// ResolveEnvFrom inspects a running Docker container and returns its env vars.
func ResolveEnvFrom(ctx context.Context, provider ContainerInspector, containerName string) ([]string, error) {
	if provider == nil {
		return nil, fmt.Errorf("resolve env from container %q: %w", containerName, ErrNilContainerInspector)
	}

	container, err := provider.InspectContainer(ctx, containerName)
	if err != nil {
		return nil, fmt.Errorf("inspect container %q: %w", containerName, err)
	}

	if container.Config == nil {
		return nil, nil
	}

	return container.Config.Env, nil
}

// MergeEnvironments merges environment variable sources with last-wins semantics.
// Order: envFiles -> envFrom -> explicit environment.
func MergeEnvironments(envFiles, envFrom, explicit []string) []string {
	if len(envFiles) == 0 && len(envFrom) == 0 && len(explicit) == 0 {
		return nil
	}

	seen := make(map[string]int) // key -> index in result
	var result []string

	add := func(entries []string) {
		for _, entry := range entries {
			idx := strings.IndexByte(entry, '=')
			if idx < 0 {
				continue
			}
			key := entry[:idx]
			if i, ok := seen[key]; ok {
				result[i] = entry // overwrite in place
			} else {
				seen[key] = len(result)
				result = append(result, entry)
			}
		}
	}

	add(envFiles)
	add(envFrom)
	add(explicit)

	return result
}

// ResolveJobEnvironment resolves env-file and env-from sources, merging with explicit environment.
// If provider is nil, env-from entries are skipped with a warning via warnFn.
// Returns the merged []string in KEY=VALUE format. Does NOT mutate input slices.
func ResolveJobEnvironment(
	ctx context.Context,
	envFiles, envFrom, explicit []string,
	provider ContainerInspector,
	warnFn func(string),
) ([]string, error) {
	var envFileVars []string
	for _, path := range envFiles {
		vars, err := ParseEnvFile(path)
		if err != nil {
			return nil, fmt.Errorf("env-file %q: %w", path, err)
		}
		envFileVars = append(envFileVars, vars...)
	}

	var envFromVars []string
	for _, container := range envFrom {
		if provider == nil {
			if warnFn != nil {
				warnFn(fmt.Sprintf("env-from %q ignored: no Docker provider available (not supported for local jobs)", container))
			}
			continue
		}
		vars, err := ResolveEnvFrom(ctx, provider, container)
		if err != nil {
			return nil, fmt.Errorf("env-from %q: %w", container, err)
		}
		envFromVars = append(envFromVars, vars...)
	}

	return MergeEnvironments(envFileVars, envFromVars, explicit), nil
}
