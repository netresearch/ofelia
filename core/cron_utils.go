package core

import "strings"

// Implement the cron logger interface
type CronUtils struct {
	Logger Logger
}

func NewCronUtils(l Logger) *CronUtils {
	return &CronUtils{Logger: l}
}

func (c *CronUtils) Info(msg string, keysAndValues ...interface{}) {
	format := cronFormatString(len(keysAndValues))
	args := append([]interface{}{msg}, keysAndValues...)
	c.Logger.Debugf(format, args...)
}

func (c *CronUtils) Error(err error, msg string, keysAndValues ...interface{}) {
	format := cronFormatString(len(keysAndValues) + 2)
	args := append([]interface{}{msg, "error", err}, keysAndValues...)
	c.Logger.Errorf(format, args...)
}

// cronFormatString returns a logfmt-like format string for the number of
// key/value pairs. This mirrors the format used by robfig/cron.
func cronFormatString(numKeysAndValues int) string {
	var sb strings.Builder
	sb.WriteString("%s")
	if numKeysAndValues > 0 {
		sb.WriteString(", ")
	}
	for i := 0; i < numKeysAndValues/2; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("%v=%v")
	}
	return sb.String()
}
