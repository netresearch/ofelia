package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/netresearch/ofelia/cli"
	"github.com/netresearch/ofelia/core"
	"github.com/sirupsen/logrus"
	gcfg "gopkg.in/gcfg.v1"
)

var version string
var build string

const logFormat = "%{time} %{color} %{shortfile} ▶ %{level} %{color:reset} %{message}"

func buildLogger(level string) core.Logger {
	logrus.SetOutput(os.Stdout)
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		ForceColors:     true,
		DisableQuote:    true,
		TimestampFormat: "2006-01-02 15:04:05",
	})
	lvl, err := logrus.ParseLevel(strings.ToLower(level))
	if err != nil {
		lvl = logrus.InfoLevel
	}
	logrus.SetLevel(lvl)
	return &core.LogrusAdapter{Logger: logrus.StandardLogger()}
}

func main() {
	// Pre-parse log-level flag to configure logger early
	var pre struct {
		LogLevel   string `long:"log-level"`
		ConfigFile string `long:"config" default:"/etc/ofelia.conf"`
	}
	preParser := flags.NewParser(&pre, flags.IgnoreUnknown)
	remainingArgs, _ := preParser.ParseArgs(os.Args[1:])

	if pre.LogLevel == "" {
		var levelConfig struct {
			Global struct {
				LogLevel string `gcfg:"log-level"`
			}
		}
		if err := gcfg.ReadFileInto(&levelConfig, pre.ConfigFile); err == nil {
			pre.LogLevel = levelConfig.Global.LogLevel
		}
	}

	logger := buildLogger(pre.LogLevel)

	parser := flags.NewNamedParser("ofelia", flags.Default)
	parser.AddCommand(
		"daemon",
		"daemon process",
		"",
		&cli.DaemonCommand{Logger: logger, LogLevel: pre.LogLevel, ConfigFile: pre.ConfigFile},
	)
	parser.AddCommand(
		"validate",
		"validates the config file",
		"",
		&cli.ValidateCommand{Logger: logger, LogLevel: pre.LogLevel, ConfigFile: pre.ConfigFile},
	)

	if _, err := parser.ParseArgs(remainingArgs); err != nil {
		if flagErr, ok := err.(*flags.Error); ok {
			if flagErr.Type == flags.ErrHelp {
				return
			}

			parser.WriteHelp(os.Stdout)
			fmt.Printf("\nBuild information\n  commit: %s\n  date:%s\n", version, build)
		}

		os.Exit(1)
	}
}
