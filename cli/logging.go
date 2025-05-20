package cli

import (
	"strings"

	logging "github.com/op/go-logging"
)

// ApplyLogLevel sets the global logging level if level is valid.
func ApplyLogLevel(level string) {
	if level == "" {
		return
	}
	lvl, err := logging.LogLevel(strings.ToUpper(level))
	if err != nil {
		return
	}
	logging.SetLevel(lvl, "")
}
