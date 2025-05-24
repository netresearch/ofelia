package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/netresearch/ofelia/cli"
	"github.com/netresearch/ofelia/core"
	"github.com/sirupsen/logrus"
	"golang.org/x/term"
	ini "gopkg.in/ini.v1"
)

var version string
var build string

const logFormat = "%{time} %{color} %{shortfile} ▶ %{level} %{color:reset} %{message}"

func buildLogger(level string) core.Logger {
	logrus.SetOutput(os.Stdout)
	logrus.SetReportCaller(true)
	forceColors := false
	if term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("TERM") != "dumb" && os.Getenv("NO_COLOR") == "" {
		forceColors = true
	}
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		ForceColors:     forceColors,
		DisableQuote:    true,
		TimestampFormat: "2006-01-02 15:04:05",
		CallerPrettyfier: func(frame *runtime.Frame) (string, string) {
			return "", fmt.Sprintf("%s:%d", filepath.Base(frame.File), frame.Line)
		},
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
		ConfigFile string `long:"config" default:"/etc/ofelia/config.ini"`
	}
	args := os.Args[1:]
	preParser := flags.NewParser(&pre, flags.IgnoreUnknown)
	_, _ = preParser.ParseArgs(args)

	if pre.LogLevel == "" {
		cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true}, pre.ConfigFile)
		if err == nil {
			if sec, err := cfg.GetSection("global"); err == nil {
				pre.LogLevel = sec.Key("log-level").String()
			}
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

	if _, err := parser.ParseArgs(args); err != nil {
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
