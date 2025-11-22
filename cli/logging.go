package cli

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

// ApplyLogLevel sets the global logging level if level is valid.
// Returns an error if the level is invalid, with a list of valid options.
func ApplyLogLevel(level string) error {
	if level == "" {
		return nil
	}

	lvl, err := logrus.ParseLevel(strings.ToLower(level))
	if err != nil {
		// Dynamically generate list of valid levels
		var validLevels []string
		for _, l := range logrus.AllLevels {
			validLevels = append(validLevels, l.String())
		}

		// Log warning for immediate visibility
		logrus.Warnf("Invalid log level %q. Valid levels: %s",
			level, strings.Join(validLevels, ", "))

		// Return error for programmatic handling
		return fmt.Errorf("invalid log level %q: %w", level, err)
	}

	logrus.SetLevel(lvl)
	return nil
}
