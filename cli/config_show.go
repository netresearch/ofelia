package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

// ConfigShowCommand displays the effective runtime configuration
type ConfigShowCommand struct {
	ConfigFile string `long:"config" env:"OFELIA_CONFIG" description:"configuration file" default:"/etc/ofelia/config.ini"`
	LogLevel   string `long:"log-level" env:"OFELIA_LOG_LEVEL" description:"Set log level (overrides config)"`
	Logger     *slog.Logger
	LevelVar   *slog.LevelVar
}

// Execute runs the config show command
func (c *ConfigShowCommand) Execute(_ []string) error {
	_ = ApplyLogLevel(c.LogLevel, c.LevelVar) // Ignore error, will use default level

	c.Logger.Debug(fmt.Sprintf("Loading configuration from %q ... ", c.ConfigFile))
	conf, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		c.Logger.Error("Failed to load configuration")
		return fmt.Errorf("load config: %w", err)
	}

	// Apply CLI log level override if provided
	if c.LogLevel == "" {
		_ = ApplyLogLevel(conf.Global.LogLevel, c.LevelVar) // Ignore error, will use default level
	}

	// Apply defaults to all job configurations
	applyConfigDefaults(conf)

	// Marshal the effective configuration to JSON
	out, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Output to stdout
	_, _ = fmt.Fprintln(os.Stdout, string(out))

	c.Logger.Debug("Configuration displayed successfully")
	return nil
}
