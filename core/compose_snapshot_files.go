// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/netresearch/ofelia/core/domain"
)

// retainSnapshotInput keeps immutable input files after the CLI exits because
// dependency service containers can keep them bind-mounted and can restart
// later. Content addressing also keeps their paths stable between executions
// and process restarts. Operators prune them only after removing containers
// that reference them; guessing their lifetime from one CLI exit is unsafe.
func retainSnapshotInput(dir string, input domain.ComposeSnapshotFile) (string, error) {
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("create captured input directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("inspect captured input directory: %w", err)
	}
	if !info.IsDir() || !snapshotPathPrivate(info) {
		return "", fmt.Errorf("%w: captured input directory must be private and owned by this process", errInvalidComposeSnapshot)
	}
	header := fmt.Sprintf("%o:%d:%d:", input.Mode, input.UID, input.GID)
	sum := sha256.Sum256(append([]byte(header), input.Content...))
	path := filepath.Join(dir, hex.EncodeToString(sum[:]))
	if _, err := os.Lstat(path); err == nil {
		return path, verifySnapshotInput(path, input)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect retained input: %w", err)
	}
	// Link a completed private temporary file into place without overwriting
	// an existing path. Concurrent executions can only observe complete data.
	file, err := os.CreateTemp(dir, ".input-")
	if err != nil {
		return "", fmt.Errorf("create captured input: %w", err)
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	if _, err := file.Write(input.Content); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write captured input: %w", err)
	}
	if err := setSnapshotInputIdentity(file, input); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("preserve source file access: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("flush captured input: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close captured input: %w", err)
	}
	if err := os.Link(temp, path); err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("publish captured input: %w", err)
	}
	return path, verifySnapshotInput(path, input)
}

func verifySnapshotInput(path string, input domain.ComposeSnapshotFile) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect captured input: %w", err)
	}
	if !info.Mode().IsRegular() || !snapshotInputIdentityMatches(info, input) {
		return fmt.Errorf("%w: captured input does not preserve source file access", errInvalidComposeSnapshot)
	}
	actual, err := os.ReadFile(path) // #nosec G304 -- content-addressed child of a verified private directory
	if err != nil {
		return fmt.Errorf("read captured input: %w", err)
	}
	if !bytes.Equal(actual, input.Content) {
		return fmt.Errorf("%w: captured input contents changed", errInvalidComposeSnapshot)
	}
	return nil
}
