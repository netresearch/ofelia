// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
)

func TestStartLogsRuntimeCPU(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))
	cmd := &DaemonCommand{
		Logger:          logger,
		LevelVar:        &slog.LevelVar{},
		scheduler:       core.NewScheduler(logger),
		config:          NewConfig(logger),
		done:            make(chan struct{}),
		shutdownManager: core.NewShutdownManager(logger, 5*time.Second),
	}
	if err := cmd.start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	var line string
	for l := range strings.Lines(out.String()) {
		if strings.Contains(l, "Go runtime CPU settings") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("start logged no runtime CPU line:\n%s", out.String())
	}
	for _, want := range []string{
		"gomaxprocs=" + strconv.Itoa(runtime.GOMAXPROCS(0)) + " ",
		"cpus=" + strconv.Itoa(runtime.NumCPU()) + "\n",
		"gomaxprocsSource=",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("runtime CPU line %q lacks %q", line, want)
		}
	}
}

func TestGomaxprocsSource(t *testing.T) {
	t.Parallel()
	for env, want := range map[string]string{
		"":    "default",
		"4":   "GOMAXPROCS environment variable",
		"0":   "default",
		"-1":  "default",
		"abc": "default",
	} {
		if got := gomaxprocsSource(env); got != want {
			t.Errorf("gomaxprocsSource(%q) = %q, want %q", env, got, want)
		}
	}
}
