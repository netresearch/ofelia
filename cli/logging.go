package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// ErrInvalidLogLevel indicates an invalid log level string was provided.
var ErrInvalidLogLevel = errors.New("invalid log level")

// ApplyLogLevel sets the logging level if level is valid.
// Returns an error if the level is invalid, with a list of valid options.
func ApplyLogLevel(level string, lv *slog.LevelVar) error {
	if level == "" {
		return nil
	}

	// Map legacy logrus level names to slog levels
	var l slog.Level
	switch strings.ToLower(level) {
	case "trace", "debug":
		l = slog.LevelDebug
	case "info", "notice":
		l = slog.LevelInfo
	case "warning", "warn":
		l = slog.LevelWarn
	case "error", "fatal", "panic", "critical":
		l = slog.LevelError
	default:
		return fmt.Errorf("%w: %q (valid levels are debug, info, warn, error)", ErrInvalidLogLevel, level)
	}

	if lv != nil {
		lv.Set(l)
	}
	return nil
}
