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

func main() {
	cli.Version = version
	cli.Build = build

	// Handle --version flag before parser setup
	if slices.Contains(os.Args[1:], "--version") {
		_, _ = fmt.Fprintln(os.Stdout, cli.VersionString())
		return
	}

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
				pre.LogLevel = cli.ExpandEnvVars(sec.Key("log-level").String())
			}
		}
	}

	logger, levelVar := buildLogger(pre.LogLevel)

	parser := flags.NewNamedParser("ofelia", flags.Default|flags.AllowBoolValues)
	_, _ = parser.AddCommand(
		"daemon",
		"daemon process",
		"",
		&cli.DaemonCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel, ConfigFile: pre.ConfigFile},
	)
	_, _ = parser.AddCommand(
		"validate",
		"validates the config file",
		"",
		&cli.ValidateCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel, ConfigFile: pre.ConfigFile},
	)
	_, _ = parser.AddCommand(
		"config",
		"shows the effective runtime configuration",
		"",
		&cli.ConfigShowCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel, ConfigFile: pre.ConfigFile},
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
		&cli.DoctorCommand{Logger: logger, LevelVar: levelVar, LogLevel: pre.LogLevel},
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
		if flags.WroteHelp(err) {
			return
		}

		var flagErr *flags.Error
		if errors.As(err, &flagErr) {
			parser.WriteHelp(os.Stdout)
			_, _ = fmt.Fprintf(os.Stdout, "\n%s\n", cli.VersionString())
		}

		logger.Error("Command failed to execute")
		return // Exit gracefully instead of os.Exit(1)
	}
}
