// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package composeproject

import (
	"fmt"
	"io"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/docker/cli/pkg/kvfile"
)

func init() {
	// Match docker/compose's raw env_file parser: it uses the Docker CLI
	// key/value parser, preserving quotes and dollar signs literally.
	dotenv.RegisterFormat("raw", parseRawEnvironment)
}

func parseRawEnvironment(reader io.Reader, _ string, variables map[string]string, lookup func(string) (string, bool)) error {
	lines, err := kvfile.ParseFromReader(reader, lookup)
	if err != nil {
		return fmt.Errorf("parse raw environment: %w", err)
	}
	for _, line := range lines {
		key, value, _ := strings.Cut(line, "=")
		variables[key] = value
	}
	return nil
}
