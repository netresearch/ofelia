// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/netresearch/ofelia/core"
)

// ErrFileModeRange is returned when a configured save-mode/save-folder-mode
// carries bits outside the permission range (0000-0777).
var ErrFileModeRange = errors.New("file mode out of range: only permission bits 0000-0777 are allowed")

// Default permissions for save-folder output. These match the security-hardened
// defaults (gosec G301/G306): logs are readable only by the daemon's uid because
// job output may contain secrets/PII. Operators can widen them via save-mode /
// save-folder-mode when their environment needs shared or non-root read access.
const (
	defaultSaveFileMode   os.FileMode = 0o600
	defaultSaveFolderMode os.FileMode = 0o750
)

// SaveConfig configuration for the Save middleware
type SaveConfig struct {
	// SaveFolder is the directory path where job execution logs and metadata are saved.
	// When configured, execution output (stdout, stderr) and context (JSON) are saved
	// after each job run. Leave empty to disable saving.
	SaveFolder string `gcfg:"save-folder" mapstructure:"save-folder"`
	// SaveOnlyOnError when true, only saves execution logs when a job fails.
	// Defaults to false (saves all executions).
	SaveOnlyOnError *bool `gcfg:"save-only-on-error" mapstructure:"save-only-on-error"`
	// RestoreHistory controls whether previously saved execution history is restored on startup.
	// When nil (default), history restoration is enabled if SaveFolder is configured.
	// Set explicitly to false to disable restoration even when SaveFolder is set.
	RestoreHistory *bool `gcfg:"restore-history" mapstructure:"restore-history"`
	// RestoreHistoryMaxAge defines the maximum age of execution history to restore on startup.
	// Only executions newer than this duration are restored. Defaults to 24 hours.
	RestoreHistoryMaxAge time.Duration `gcfg:"restore-history-max-age" mapstructure:"restore-history-max-age"`
	// SaveMode is the octal file mode applied to the per-execution log/JSON files.
	// Parsed as octal (e.g. "0644", "0o644" or "644"). Empty uses the secure
	// default 0600 (readable only by the daemon uid). Set a wider mode to allow
	// non-root operators or a shared group to read logs on the host.
	SaveMode string `gcfg:"save-mode" mapstructure:"save-mode"`
	// SaveFolderMode is the octal directory mode applied when creating SaveFolder.
	// Parsed as octal (e.g. "0755"). Empty uses the secure default 0750.
	SaveFolderMode string `gcfg:"save-folder-mode" mapstructure:"save-folder-mode"`
}

// parseFileMode parses an octal permission string (e.g. "0644", "0o644" or
// "644") into an os.FileMode. Only the permission bits (<= 0777) are accepted;
// special bits (setuid/setgid/sticky) are intentionally rejected as the save
// middleware writes plain log files. An empty string is rejected — callers
// resolve the default before calling this.
func parseFileMode(s string) (os.FileMode, error) {
	trimmed := strings.TrimSpace(s)
	// Accept an optional 0o/0O prefix; the remainder is always interpreted as octal.
	trimmed = strings.TrimPrefix(trimmed, "0o")
	trimmed = strings.TrimPrefix(trimmed, "0O")
	v, err := strconv.ParseUint(trimmed, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid octal file mode %q: %w", s, err)
	}
	if v > 0o777 {
		return 0, fmt.Errorf("%q: %w", s, ErrFileModeRange)
	}
	return os.FileMode(v), nil
}

// GetSaveFileMode resolves the mode for output files, defaulting to 0600.
func (c *SaveConfig) GetSaveFileMode() (os.FileMode, error) {
	if strings.TrimSpace(c.SaveMode) == "" {
		return defaultSaveFileMode, nil
	}
	return parseFileMode(c.SaveMode)
}

// GetSaveFolderMode resolves the mode for the save folder, defaulting to 0750.
func (c *SaveConfig) GetSaveFolderMode() (os.FileMode, error) {
	if strings.TrimSpace(c.SaveFolderMode) == "" {
		return defaultSaveFolderMode, nil
	}
	return parseFileMode(c.SaveFolderMode)
}

// RestoreHistoryEnabled returns whether history restoration is enabled.
// Defaults to true when SaveFolder is configured.
func (c *SaveConfig) RestoreHistoryEnabled() bool {
	if c.RestoreHistory != nil {
		return *c.RestoreHistory
	}
	// Default: enabled if save folder is configured
	return c.SaveFolder != ""
}

// GetRestoreHistoryMaxAge returns the max age for history restoration.
// Defaults to 24 hours.
func (c *SaveConfig) GetRestoreHistoryMaxAge() time.Duration {
	if c.RestoreHistoryMaxAge > 0 {
		return c.RestoreHistoryMaxAge
	}
	return 24 * time.Hour
}

// NewSave returns a Save middleware if the given configuration is not empty
func NewSave(c *SaveConfig) core.Middleware {
	var m core.Middleware
	if !IsEmpty(c) {
		m = &Save{*c}
	}

	return m
}

// Save the save middleware saves to disk a dump of the stdout and stderr after
// every execution of the process
type Save struct {
	SaveConfig
}

// ContinueOnStop always returns true; we always want to report the final status
func (m *Save) ContinueOnStop() bool {
	return true
}

// Run save the result of the execution to disk
func (m *Save) Run(ctx *core.Context) error {
	err := ctx.Next()
	ctx.Stop(err)

	if ctx.Execution.Failed || !boolVal(m.SaveOnlyOnError) {
		err := m.saveToDisk(ctx)
		if err != nil {
			ctx.Logger.Error(fmt.Sprintf("Save error: %q", err))
		}
	}

	return err
}

func (m *Save) saveToDisk(ctx *core.Context) error {
	// Validate save folder before use
	if err := DefaultSanitizer.ValidateSaveFolder(m.SaveFolder); err != nil {
		return fmt.Errorf("invalid save folder: %w", err)
	}

	folderMode, err := m.GetSaveFolderMode()
	if err != nil {
		return fmt.Errorf("invalid save-folder-mode: %w", err)
	}
	fileMode, err := m.GetSaveFileMode()
	if err != nil {
		return fmt.Errorf("invalid save-mode: %w", err)
	}

	if err := os.MkdirAll(m.SaveFolder, folderMode); err != nil {
		return fmt.Errorf("mkdir %q: %w", m.SaveFolder, err)
	}

	// Use enhanced sanitization for job name
	safeName := SanitizeJobName(ctx.Job.GetName())

	root := filepath.Join(m.SaveFolder, fmt.Sprintf(
		"%s_%s",
		ctx.Execution.Date.Format("20060102_150405"), safeName,
	))

	e := ctx.Execution
	if err := m.writeFile(e.ErrorStream.Bytes(), fmt.Sprintf("%s.stderr.log", root), fileMode); err != nil {
		return fmt.Errorf("write stderr log: %w", err)
	}

	if err := m.writeFile(e.OutputStream.Bytes(), fmt.Sprintf("%s.stdout.log", root), fileMode); err != nil {
		return fmt.Errorf("write stdout log: %w", err)
	}

	if err := m.saveContextToDisk(ctx, fmt.Sprintf("%s.json", root), fileMode); err != nil {
		return fmt.Errorf("write context json: %w", err)
	}

	return nil
}

func (m *Save) saveContextToDisk(ctx *core.Context, filename string, mode os.FileMode) error {
	js, _ := json.MarshalIndent(map[string]any{
		notificationVarJob:       ctx.Job,
		notificationVarExecution: ctx.Execution,
	}, "", "  ")

	if err := m.writeFile(js, filename, mode); err != nil {
		return fmt.Errorf("write json file: %w", err)
	}
	return nil
}

func (m *Save) writeFile(data []byte, filename string, mode os.FileMode) error {
	// mode is operator-configured (save-mode) and defaults to 0600; parseFileMode
	// caps it at 0777, so it can never grant special bits.
	if err := os.WriteFile(filename, data, mode); err != nil { // #nosec G306 -- mode is operator-configured, defaults to 0600
		return fmt.Errorf("write file %q: %w", filename, err)
	}
	return nil
}
