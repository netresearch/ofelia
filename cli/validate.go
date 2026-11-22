// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	defaults "github.com/creasty/defaults"

	"github.com/netresearch/ofelia/config"
)

// ValidateCommand validates the config file
type ValidateCommand struct {
	ConfigFile string `long:"config" env:"OFELIA_CONFIG" description:"configuration file"`
	// StateFile mirrors the daemon's option. Validation only checks whether one
	// is set: Compose jobs with missed_fires: one or concurrency: queue need it.
	StateFile string `long:"state-file" env:"OFELIA_STATE_FILE" description:"the daemon's state file; only checks whether one is set"`
	LogLevel  string `long:"log-level" env:"OFELIA_LOG_LEVEL" description:"Set log level (overrides config)"`
	Logger    *slog.Logger
	LevelVar  *slog.LevelVar
}

// ErrJobDefinitionInvalid is returned by validate for jobs the daemon refuses
// at load.
var ErrJobDefinitionInvalid = errors.New("jobs that cannot run")

// validateJobDefinitions runs the validation that job assembly applies to
// every job of the configuration and reports each job that fails it.
func (c *Config) validateJobDefinitions() error {
	var failed []string
	check := func(section, name string, j any) {
		if v, ok := j.(validatable); ok {
			if err := v.Validate(); err != nil {
				failed = append(failed, fmt.Sprintf("[%s %q]: %v", section, name, err))
			}
		}
	}
	for name, j := range c.ExecJobs {
		check(jobExec, name, j)
	}
	for name, j := range c.RunJobs {
		check(jobRun, name, j)
	}
	for name, j := range c.LocalJobs {
		check(jobLocal, name, j)
	}
	for name, j := range c.ServiceJobs {
		check(jobServiceRun, name, j)
	}
	for name, j := range c.ComposeJobs {
		check(jobCompose, name, j)
	}
	if len(failed) == 0 {
		return nil
	}
	sort.Strings(failed)
	return fmt.Errorf("%w: %s", ErrJobDefinitionInvalid, strings.Join(failed, "; "))
}

// Execute runs the validation command
func (c *ValidateCommand) Execute(_ []string) error {
	if err := ApplyLogLevel(c.LogLevel, c.LevelVar); err != nil {
		c.Logger.Error(fmt.Sprintf("Failed to apply log level: %v", err))
		return fmt.Errorf("invalid log level configuration: %w", err)
	}

	c.Logger.Debug(fmt.Sprintf("Validating %q ... ", c.ConfigFile))
	conf, err := buildFromFile(c.ConfigFile, c.Logger, scheduleStateFor(c.StateFile))
	if err != nil {
		c.Logger.Error("ERROR")
		return err
	}
	if c.LogLevel == "" {
		if err := ApplyLogLevel(conf.Global.LogLevel, c.LevelVar); err != nil {
			c.Logger.Warn(fmt.Sprintf("Failed to apply config log level (using default): %v", err))
		}
	}

	// The daemon runs without a rejected Compose project and logs why; this
	// command exists to catch exactly that before deployment.
	if rejected := conf.rejectedComposeProjects(); len(rejected) > 0 {
		c.Logger.Error("ERROR")
		return fmt.Errorf("%w: %s", ErrComposeProjectRejected, strings.Join(rejected, ", "))
	}

	// Validate regardless of enable-strict-validation. That flag decides
	// whether the *daemon* refuses to start on a questionable config; running
	// this command is itself the request to have the config checked, so
	// answering "looks fine" because a runtime toggle is off would make the
	// one command whose purpose is validation the one that does not validate.
	//
	// BuildFromFile has already run the same validator when the flag is on, so
	// this only adds work in the default case.
	if !conf.Global.EnableStrictValidation {
		if err := config.NewConfigValidator(conf).Validate(); err != nil {
			c.Logger.Error("ERROR")
			return fmt.Errorf("configuration validation failed: %w", err)
		}
	}

	applyConfigDefaults(conf)
	// The daemon refuses a job that fails its own validation at load, after
	// the defaults are applied, so this command does too.
	if err := conf.validateJobDefinitions(); err != nil {
		c.Logger.Error("ERROR")
		return err
	}
	out, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	_, _ = fmt.Fprintln(os.Stdout, string(out))

	c.Logger.Debug("OK")
	return nil
}

func applyConfigDefaults(conf *Config) {
	for _, j := range conf.ExecJobs {
		_ = defaults.Set(j)
	}
	for _, j := range conf.RunJobs {
		_ = defaults.Set(j)
	}
	for _, j := range conf.LocalJobs {
		_ = defaults.Set(j)
	}
	for _, j := range conf.ServiceJobs {
		_ = defaults.Set(j)
	}
	for _, j := range conf.ComposeJobs {
		_ = defaults.Set(j)
	}
}
