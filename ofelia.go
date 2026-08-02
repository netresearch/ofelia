// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/jessevdk/go-flags"
	ini "gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/cli"
)

var (
	version string
	build   string
)

func buildLogger(level string) (*slog.Logger, *slog.LevelVar) {
	levelVar := &slog.LevelVar{}
	switch strings.ToLower(level) {
	case "trace", "debug":
		levelVar.Set(slog.LevelDebug)
	case "", "info", "notice":
		levelVar.Set(slog.LevelInfo)
	case "warning", "warn":
		levelVar.Set(slog.LevelWarn)
	case "error", "fatal", "panic", "critical":
		levelVar.Set(slog.LevelError)
	default:
		levelVar.Set(slog.LevelInfo)
	}
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     levelVar,
	})
	return slog.New(handler), levelVar
}

// Exit codes. A command that failed has to say so in the only channel a shell
// reads, otherwise `ofelia validate … || exit 1` in a pipeline never fires and
// a broken config sails through the gate that exists to stop it.
const (
	exitOK      = 0
	exitFailure = 1
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// globalOptions are the flags that apply to ofelia as a whole rather than to
// one subcommand: they are read out of argv before the logger exists, so the
// logger can be built at the requested level and the config can be located.
//
// They are declared on the top-level parser as well, so that `ofelia
// --config=x daemon` works and not only `ofelia daemon --config=x`. Being
// pre-parsed made them look global while the parser rejected them in the
// position a user would naturally write them.
type globalOptions struct {
	LogLevel   string `long:"log-level" description:"Set log level (overrides config)"`
	ConfigFile string `long:"config" description:"Configuration file path (default: /etc/ofelia/config.ini)"`
}

// defaultConfigFile is where ofelia looks when --config is absent.
//
// It is resolved here rather than declared as a struct-tag default so that an
// unset flag stays distinguishable from an explicit one: `doctor` searches a
// list of well-known locations when given no path, and a pre-filled default
// would silently take that away.
const defaultConfigFile = "/etc/ofelia/config.ini"

// run holds what main used to do and returns the process exit code instead of
// ending the process, so the exit status is a value tests can assert on.
func run(args []string) int {
	cli.Version = version
	cli.Build = build

	// Handle --version flag before parser setup
	if slices.Contains(args, "--version") {
		_, _ = fmt.Fprintln(os.Stdout, cli.VersionString())
		return exitOK
	}

	// Pre-parse log-level flag to configure logger early
	var pre globalOptions
	preParser := flags.NewParser(&pre, flags.IgnoreUnknown)
	_, _ = preParser.ParseArgs(args)

	// Commands that read a config need a concrete path; doctor is handed the
	// raw value so an absent flag still means "go and find it".
	configFile := pre.ConfigFile
	if configFile == "" {
		configFile = defaultConfigFile
	}

	if pre.LogLevel == "" {
		cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true}, configFile)
		if err == nil {
			if sec, err := cfg.GetSection("global"); err == nil {
				pre.LogLevel = cli.ExpandEnvVars(sec.Key("log-level").String())
			}
		}
	}

	logger, levelVar := buildLogger(pre.LogLevel)

	parser := flags.NewNamedParser("ofelia", flags.Default|flags.AllowBoolValues)

	// Accept the global flags before the subcommand too. The values are
	// already in `pre`; re-declaring them here is what stops the parser
	// rejecting `ofelia --config=x daemon` as an unknown flag.
	if _, err := parser.AddGroup("Global Options", "Flags that apply to every subcommand", &pre); err != nil {
		logger.Error("registering global options failed", "error", err)
		return exitFailure
	}
	_, _ = parser.AddCommand(
		"daemon",
		"daemon process",
		"",
		&cli.DaemonCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel, ConfigFile: configFile},
	)
	_, _ = parser.AddCommand(
		"validate",
		"validates the config file",
		"",
		&cli.ValidateCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel, ConfigFile: configFile},
	)
	_, _ = parser.AddCommand(
		"config",
		"shows the effective runtime configuration",
		"",
		&cli.ConfigShowCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel, ConfigFile: configFile},
	)
	_, _ = parser.AddCommand(
		"init",
		"creates configuration through interactive wizard",
		"",
		&cli.InitCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel},
	)
	_, _ = parser.AddCommand(
		"doctor",
		"diagnose Ofelia configuration and environment health",
		"",
		&cli.DoctorCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel, ConfigFile: pre.ConfigFile},
	)
	_, _ = parser.AddCommand(
		"hash-password",
		"generate a bcrypt hash for web authentication",
		"",
		&cli.HashPasswordCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel},
	)
	_, _ = parser.AddCommand(
		"version",
		"print version information",
		"",
		&cli.VersionCommand{},
	)

	if _, err := parser.ParseArgs(args); err != nil {
		// Help was asked for and printed. That is the command doing its job,
		// not a failure.
		if flags.WroteHelp(err) {
			return exitOK
		}

		var flagErr *flags.Error
		if errors.As(err, &flagErr) {
			parser.WriteHelp(os.Stdout)
			_, _ = fmt.Fprintf(os.Stdout, "\n%s\n", cli.VersionString())
		}

		// Every other error — an unusable config, a subcommand that returned
		// an error, an unknown command — is a failure and has to leave a
		// non-zero status behind. This used to return 0 with a logged message,
		// which meant `ofelia validate … || exit 1` could not fire and a
		// broken config passed the gate meant to catch it.
		logger.Error("Command failed to execute", "error", err)
		return exitFailure
	}

	return exitOK
}
