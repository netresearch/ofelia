package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core"
	. "gopkg.in/check.v1"
)

func TestDaemonBoot(t *testing.T) { TestingT(t) }

type DaemonBootSuite struct{}

var _ = Suite(&DaemonBootSuite{})

// BufferLogger collects formatted logs in memory for inspection.
type BufferLogger struct{ buf bytes.Buffer }

func (l *BufferLogger) Criticalf(format string, args ...interface{}) {
	fmt.Fprintf(&l.buf, format, args...)
	l.buf.WriteByte('\n')
}
func (l *BufferLogger) Debugf(format string, args ...interface{}) {
	fmt.Fprintf(&l.buf, format, args...)
	l.buf.WriteByte('\n')
}
func (l *BufferLogger) Errorf(format string, args ...interface{}) {
	fmt.Fprintf(&l.buf, format, args...)
	l.buf.WriteByte('\n')
}
func (l *BufferLogger) Noticef(format string, args ...interface{}) {
	fmt.Fprintf(&l.buf, format, args...)
	l.buf.WriteByte('\n')
}
func (l *BufferLogger) Warningf(format string, args ...interface{}) {
	fmt.Fprintf(&l.buf, format, args...)
	l.buf.WriteByte('\n')
}

func (l *BufferLogger) String() string { return l.buf.String() }

func (s *DaemonBootSuite) TestBootLogsConfigError(c *C) {
	tmpFile, err := os.CreateTemp("", "ofelia_bad_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("[global]\nno-overlap = true\n")
	c.Assert(err, IsNil)
	tmpFile.Close()

	logger := &BufferLogger{}
	cmd := &DaemonCommand{ConfigFile: tmpFile.Name(), Logger: logger}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(notifier dockerLabelsUpdate, logger core.Logger, filters []string) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	logOutput := logger.String()
	c.Assert(strings.Contains(logOutput, "no-overlap"), Equals, true)
	c.Assert(strings.Contains(logOutput, "not found"), Equals, false)
}
