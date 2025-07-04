package cli

import (
	"fmt"
	"testing"

	. "gopkg.in/check.v1"
)

func TestDockerLabels(t *testing.T) { TestingT(t) }

type DockerLabelSuite struct{}

var _ = Suite(&DockerLabelSuite{})

type RecordingLogger struct {
	warnings []string
}

func (l *RecordingLogger) Criticalf(format string, args ...interface{}) {}
func (l *RecordingLogger) Debugf(format string, args ...interface{})    {}
func (l *RecordingLogger) Errorf(format string, args ...interface{})    {}
func (l *RecordingLogger) Noticef(format string, args ...interface{})   {}
func (l *RecordingLogger) Warningf(format string, args ...interface{}) {
	l.warnings = append(l.warnings, fmt.Sprintf(format, args...))
}

func (s *DockerLabelSuite) TestUnknownLabelWarning(c *C) {
	logger := &RecordingLogger{}
	cfg := Config{logger: logger}

	labels := map[string]map[string]string{
		"some": {
			requiredLabel:                         "true",
			serviceLabel:                          "true",
			labelPrefix + ".invalid.job.schedule": "@daily",
			labelPrefix + ".unknown":              "foo",
		},
	}

	err := cfg.buildFromDockerLabels(labels)
	c.Assert(err, IsNil)
	c.Assert(len(logger.warnings), Equals, 2)
	c.Assert(logger.warnings[0], Matches, ".*invalid.job.schedule.*")
	c.Assert(logger.warnings[1], Matches, ".*unknown.*")
}
