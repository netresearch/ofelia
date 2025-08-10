package cli

import (
	"encoding/json"
	"fmt"

	defaults "github.com/creasty/defaults"
	"github.com/netresearch/ofelia/core"
)

// ValidateCommand validates the config file
type ValidateCommand struct {
	ConfigFile string `long:"config" env:"OFELIA_CONFIG" description:"configuration file" default:"/etc/ofelia/config.ini"`
	LogLevel   string `long:"log-level" env:"OFELIA_LOG_LEVEL" description:"Set log level (overrides config)"`
	Logger     core.Logger
}

// Execute runs the validation command
func (c *ValidateCommand) Execute(_ []string) error {
	ApplyLogLevel(c.LogLevel)
	c.Logger.Debugf("Validating %q ... ", c.ConfigFile)
	conf, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		c.Logger.Errorf("ERROR")
		return err
	}
	if c.LogLevel == "" {
		ApplyLogLevel(conf.Global.LogLevel)
	}

	applyConfigDefaults(conf)
	out, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	c.Logger.Debugf("OK")
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
