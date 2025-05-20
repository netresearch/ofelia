package main

import (
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"
	"github.com/netresearch/ofelia/cli"
	"github.com/netresearch/ofelia/core"
	"github.com/op/go-logging"
)

var version string
var build string

const logFormat = "%{time} %{color} %{shortfile} ▶ %{level} %{color:reset} %{message}"

func buildLogger(level string) core.Logger {
	stdout := logging.NewLogBackend(os.Stdout, "", 0)
	leveled := logging.AddModuleLevel(stdout)
	lvl := logging.INFO
	if l, err := logging.LogLevel(level); err == nil {
		lvl = l
	}
	leveled.SetLevel(lvl, "")
	logging.SetBackend(leveled)
	logging.SetFormatter(logging.MustStringFormatter(logFormat))
	return logging.MustGetLogger("ofelia")
}

func main() {
	// Pre-parse log-level flag to configure logger early
	var pre struct {
		LogLevel string `long:"log-level"`
	}
	preParser := flags.NewParser(&pre, flags.IgnoreUnknown)
	remainingArgs, _ := preParser.ParseArgs(os.Args[1:])

	logger := buildLogger(pre.LogLevel)

	parser := flags.NewNamedParser("ofelia", flags.Default)
	parser.AddCommand("daemon", "daemon process", "", &cli.DaemonCommand{Logger: logger, LogLevel: pre.LogLevel})
	parser.AddCommand("validate", "validates the config file", "", &cli.ValidateCommand{Logger: logger, LogLevel: pre.LogLevel})

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
