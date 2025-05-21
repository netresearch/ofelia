package cli

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core"
	logging "github.com/op/go-logging"
	. "gopkg.in/check.v1"
)

func TestDaemonBoot(t *testing.T) { TestingT(t) }

type DaemonBootSuite struct{}

var _ = Suite(&DaemonBootSuite{})

func newMemoryLogger(level logging.Level) (*logging.MemoryBackend, core.Logger) {
	backend := logging.InitForTesting(level)
	logging.SetFormatter(logging.MustStringFormatter(logFormat))
	logger := logging.MustGetLogger("ofelia")
	return backend, logger
}

func (s *DaemonBootSuite) TestBootLogsConfigError(c *C) {
	tmpFile, err := os.CreateTemp("", "ofelia_bad_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("[global]\nno-overlap = true\n")
	c.Assert(err, IsNil)
	tmpFile.Close()

	backend, logger := newMemoryLogger(logging.DEBUG)
	defer logging.Reset()
	cmd := &DaemonCommand{ConfigFile: tmpFile.Name(), Logger: logger, LogLevel: "DEBUG"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	var debugMsg bool
	for n := backend.Head(); n != nil; n = n.Next() {
		if n.Record.Level == logging.DEBUG && strings.Contains(n.Record.Message(), "no-overlap") {
			debugMsg = true
		}
	}
	c.Assert(debugMsg, Equals, true)
}

func (s *DaemonBootSuite) TestBootLogsConfigErrorSuppressed(c *C) {
	tmpFile, err := os.CreateTemp("", "ofelia_bad_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("[global]\nno-overlap = true\n")
	c.Assert(err, IsNil)
	tmpFile.Close()

	backend, logger := newMemoryLogger(logging.INFO)
	defer logging.Reset()
	cmd := &DaemonCommand{ConfigFile: tmpFile.Name(), Logger: logger, LogLevel: "INFO"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	var debugMsg bool
	for n := backend.Head(); n != nil; n = n.Next() {
		if n.Record.Level == logging.DEBUG {
			debugMsg = true
		}
	}
	c.Assert(debugMsg, Equals, false)
}
