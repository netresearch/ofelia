package core

import "log/slog"

// Implement the cron logger interface
type CronUtils struct {
	Logger *slog.Logger
}

func NewCronUtils(l *slog.Logger) *CronUtils {
	return &CronUtils{Logger: l}
}

func (c *CronUtils) Info(msg string, keysAndValues ...any) {
	c.Logger.Debug(msg, keysAndValues...)
}

func (c *CronUtils) Error(err error, msg string, keysAndValues ...any) {
	args := append([]any{"error", err}, keysAndValues...)
	c.Logger.Error(msg, args...)
}
