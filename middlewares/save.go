package middlewares

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/netresearch/ofelia/core"
)

// SaveConfig configuration for the Save middleware
type SaveConfig struct {
	SaveFolder           string        `gcfg:"save-folder" mapstructure:"save-folder"`
	SaveOnlyOnError      bool          `gcfg:"save-only-on-error" mapstructure:"save-only-on-error"`
	RestoreHistory       *bool         `gcfg:"restore-history" mapstructure:"restore-history"`
	RestoreHistoryMaxAge time.Duration `gcfg:"restore-history-max-age" mapstructure:"restore-history-max-age"`
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

	if ctx.Execution.Failed || !m.SaveOnlyOnError {
		err := m.saveToDisk(ctx)
		if err != nil {
			ctx.Logger.Errorf("Save error: %q", err)
		}
	}

	return err
}

func (m *Save) saveToDisk(ctx *core.Context) error {
	// Validate save folder before use
	if err := DefaultSanitizer.ValidateSaveFolder(m.SaveFolder); err != nil {
		return fmt.Errorf("invalid save folder: %w", err)
	}

	if err := os.MkdirAll(m.SaveFolder, 0o750); err != nil {
		return fmt.Errorf("mkdir %q: %w", m.SaveFolder, err)
	}

	// Use enhanced sanitization for job name
	safeName := SanitizeJobName(ctx.Job.GetName())

	root := filepath.Join(m.SaveFolder, fmt.Sprintf(
		"%s_%s",
		ctx.Execution.Date.Format("20060102_150405"), safeName,
	))

	e := ctx.Execution
	err := m.writeFile(e.ErrorStream.Bytes(), fmt.Sprintf("%s.stderr.log", root))
	if err != nil {
		return fmt.Errorf("write stderr log: %w", err)
	}

	err = m.writeFile(e.OutputStream.Bytes(), fmt.Sprintf("%s.stdout.log", root))
	if err != nil {
		return fmt.Errorf("write stdout log: %w", err)
	}

	err = m.saveContextToDisk(ctx, fmt.Sprintf("%s.json", root))
	if err != nil {
		return fmt.Errorf("write context json: %w", err)
	}

	return nil
}

func (m *Save) saveContextToDisk(ctx *core.Context, filename string) error {
	js, _ := json.MarshalIndent(map[string]interface{}{
		"Job":       ctx.Job,
		"Execution": ctx.Execution,
	}, "", "  ")

	if err := m.writeFile(js, filename); err != nil {
		return fmt.Errorf("write json file: %w", err)
	}
	return nil
}

func (m *Save) writeFile(data []byte, filename string) error {
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		return fmt.Errorf("write file %q: %w", filename, err)
	}
	return nil
}
