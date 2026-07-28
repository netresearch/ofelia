// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import "log/slog"

// CronUtils adapts an slog.Logger to the logger interface expected by
// github.com/netresearch/go-cron.
type CronUtils struct {
	Logger *slog.Logger
}

// NewCronUtils returns a CronUtils that forwards the cron engine's output to l.
func NewCronUtils(l *slog.Logger) *CronUtils {
	return &CronUtils{Logger: l}
}

// Info records an informational cron engine message. It logs at debug level,
// keeping the engine's per-tick scheduling chatter out of ofelia's info output.
func (c *CronUtils) Info(msg string, keysAndValues ...any) {
	c.Logger.Debug(msg, keysAndValues...)
}

func (c *CronUtils) Error(err error, msg string, keysAndValues ...any) {
	args := append([]any{"error", err}, keysAndValues...)
	c.Logger.Error(msg, args...)
}
