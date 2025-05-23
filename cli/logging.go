package cli

import (
	"strings"

	"github.com/sirupsen/logrus"
)

// ApplyLogLevel sets the global logging level if level is valid.
func ApplyLogLevel(level string) {
	if level == "" {
		return
	}
	lvl, err := logrus.ParseLevel(strings.ToLower(level))
	if err != nil {
		return
	}
	logrus.SetLevel(lvl)
}
