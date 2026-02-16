package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	defaults "github.com/creasty/defaults"
)

// ValidateCommand validates the config file
type ValidateCommand struct {
	ConfigFile string `long:"config" env:"OFELIA_CONFIG" description:"configuration file" default:"/etc/ofelia/config.ini"`
	LogLevel   string `long:"log-level" env:"OFELIA_LOG_LEVEL" description:"Set log level (overrides config)"`
	Logger     *slog.Logger
	LevelVar   *slog.LevelVar
}

// Execute runs the validation command
func (c *ValidateCommand) Execute(_ []string) error {
	if err := ApplyLogLevel(c.LogLevel, c.LevelVar); err != nil {
		c.Logger.Error(fmt.Sprintf("Failed to apply log level: %v", err))
		return fmt.Errorf("invalid log level configuration: %w", err)
	}

	c.Logger.Debug(fmt.Sprintf("Validating %q ... ", c.ConfigFile))
	conf, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		c.Logger.Error("ERROR")
		return err
	}
	if c.LogLevel == "" {
		if err := ApplyLogLevel(conf.Global.LogLevel, c.LevelVar); err != nil {
			c.Logger.Warn(fmt.Sprintf("Failed to apply config log level (using default): %v", err))
		}
	}

	applyConfigDefaults(conf)
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
