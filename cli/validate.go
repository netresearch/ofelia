package cli

import (
	"github.com/netresearch/ofelia/core"
)

// ValidateCommand validates the config file
type ValidateCommand struct {
	ConfigFile string `long:"config" description:"configuration file" default:"/etc/ofelia.conf"`
	LogLevel   string `long:"log-level" description:"Set log level (overrides config)"`
	Logger     core.Logger
}

// Execute runs the validation command
func (c *ValidateCommand) Execute(args []string) error {
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
	c.Logger.Debugf("OK")
	return nil
}
