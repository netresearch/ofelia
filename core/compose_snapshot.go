// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/netresearch/ofelia/core/domain"
)

var errInvalidComposeSnapshot = errors.New("invalid compose execution snapshot")

// ComposeExecutionSnapshot is an accepted execution model and its captured
// file inputs. It can contain resolved secrets and must not be serialized to
// an API response, logged, or written to the schedule state file.
type ComposeExecutionSnapshot struct {
	Model       string                                `json:"-"`
	Files       map[string]domain.ComposeSnapshotFile `json:"-"`
	Environment map[string]string                     `json:"-"`
}

// prepareSnapshot materializes the accepted model and captured
// config/secret inputs in a private directory for this execution. Compose
// reads an empty env file rather than discovering a newly edited .env.
func (j *ComposeJob) prepareSnapshot(cmd *exec.Cmd) (func(), error) {
	snapshot := j.Project.Snapshot
	if snapshot == nil {
		return func() {}, nil
	}
	dir, err := os.MkdirTemp("", "ofelia-compose-")
	if err != nil {
		return nil, fmt.Errorf("create private directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	inputDir := filepath.Join(os.TempDir(), "ofelia-compose-inputs")
	model, err := materializeSnapshot(snapshot, inputDir)
	if err != nil {
		cleanup()
		return nil, err
	}
	envFile := filepath.Join(dir, "empty.env")
	if err := os.WriteFile(envFile, []byte(snapshotEnvironment(snapshot.Environment)), 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("write empty interpolation environment: %w", err)
	}
	// The model was already interpolated. Escape every dollar so Compose's
	// second parse preserves literal shell variables and secret/config data.
	modelFile := filepath.Join(dir, "compose.json")
	escaped, err := escapeSnapshotInterpolation(model)
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := os.WriteFile(modelFile, escaped, 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("write accepted model: %w", err)
	}
	// projectArgs reserves the first file argument for the snapshot. Compose
	// loads a job twice while selecting it, so stdin cannot supply this model.
	cmd.Args[3] = modelFile
	cmd.Args = append([]string{"docker", "compose", "--env-file", envFile}, cmd.Args[2:]...)
	return cleanup, nil
}

func escapeSnapshotInterpolation(model string) ([]byte, error) {
	decoder := json.NewDecoder(strings.NewReader(model))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode snapshot interpolation: %w", err)
	}
	data, err := json.Marshal(escapeSnapshotValue(value))
	if err != nil {
		return nil, fmt.Errorf("encode snapshot interpolation: %w", err)
	}
	return data, nil
}

func escapeSnapshotValue(value any) any {
	switch item := value.(type) {
	case string:
		return strings.ReplaceAll(item, "$", "$$")
	case []any:
		for i, entry := range item {
			item[i] = escapeSnapshotValue(entry)
		}
	case map[string]any:
		for key, entry := range item {
			item[key] = escapeSnapshotValue(entry)
		}
	}
	return value
}

// Quote captured values for compose-go's dotenv parser. Dollars are escaped
// separately so the parser never interpolates accepted config/secret data.
func snapshotEnvironment(environment map[string]string) string {
	names := make([]string, 0, len(environment))
	for name := range environment {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	escape := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`,
		"\n", `\n`, "\r", `\r`, "\t", `\t`, "\a", `\a`, "\b", `\b`, "\v", `\v`, "\f", `\f`, "\x00", `\000`)
	for _, name := range names {
		fmt.Fprintf(&out, "%s=\"%s\"\n", name, escape.Replace(environment[name]))
	}
	return out.String()
}

func withoutSnapshotEnvironment(environment []string, captured map[string]string) []string {
	out := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := captured[name]; !replaced {
			out = append(out, entry)
		}
	}
	return out
}

func materializeSnapshot(snapshot *ComposeExecutionSnapshot, dir string) (string, error) {
	var model map[string]json.RawMessage
	if err := json.Unmarshal([]byte(snapshot.Model), &model); err != nil {
		return "", fmt.Errorf("decode accepted model: %w", err)
	}
	keys := make([]string, 0, len(snapshot.Files))
	for key := range snapshot.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path, err := retainSnapshotInput(dir, snapshot.Files[key])
		if err != nil {
			return "", fmt.Errorf("retain captured %s: %w", key, err)
		}
		if err := replaceSnapshotFile(model, key, path); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(model)
	if err != nil {
		return "", fmt.Errorf("encode accepted model: %w", err)
	}
	return string(data), nil
}

func replaceSnapshotFile(model map[string]json.RawMessage, key, path string) error {
	kind, name, ok := strings.Cut(key, "/")
	if !ok || kind != "configs" && kind != "secrets" {
		return fmt.Errorf("%w: invalid captured resource %q", errInvalidComposeSnapshot, key)
	}
	var resources map[string]map[string]json.RawMessage
	if err := json.Unmarshal(model[kind], &resources); err != nil {
		return fmt.Errorf("decode captured %s: %w", kind, err)
	}
	if resources[name] == nil {
		return fmt.Errorf("%w: captured resource %q is missing from the accepted model", errInvalidComposeSnapshot, key)
	}
	file, err := json.Marshal(path)
	if err != nil {
		return fmt.Errorf("encode snapshot path: %w", err)
	}
	resources[name]["file"] = file
	data, err := json.Marshal(resources)
	if err != nil {
		return fmt.Errorf("encode captured %s: %w", kind, err)
	}
	model[kind] = data
	return nil
}
