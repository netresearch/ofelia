package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	. "gopkg.in/check.v1"
)

func TestDaemonBoot(t *testing.T) { TestingT(t) }

type DaemonBootSuite struct{}

var _ = Suite(&DaemonBootSuite{})

func newMemoryLogger(level logrus.Level) (*logtest.Hook, core.Logger) {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logger.SetLevel(level)
	hook := logtest.NewLocal(logger)
	return hook, &core.LogrusAdapter{Logger: logger}
}

func (s *DaemonBootSuite) TestBootLogsConfigError(c *C) {
	tmpFile, err := os.CreateTemp("", "ofelia_bad_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("[global\nno-overlap = true\n")
	c.Assert(err, IsNil)
	tmpFile.Close()

	backend, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{ConfigFile: tmpFile.Name(), Logger: logger, LogLevel: "DEBUG"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	var warnMsg bool
	for _, e := range backend.AllEntries() {
		if e.Level == logrus.WarnLevel && strings.Contains(e.Message, "Could not load config file") {
			warnMsg = true
		}
	}
	c.Assert(warnMsg, Equals, true)
}

func (s *DaemonBootSuite) TestBootLogsConfigErrorSuppressed(c *C) {
	tmpFile, err := os.CreateTemp("", "ofelia_bad_*.ini")
	c.Assert(err, IsNil)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("[global\nno-overlap = true\n")
	c.Assert(err, IsNil)
	tmpFile.Close()

	backend, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{ConfigFile: tmpFile.Name(), Logger: logger, LogLevel: "INFO"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	var debugMsg bool
	for _, e := range backend.AllEntries() {
		if e.Level == logrus.DebugLevel {
			debugMsg = true
		}
	}
	c.Assert(debugMsg, Equals, false)
}

func (s *DaemonBootSuite) TestBootLogsMissingConfig(c *C) {
	tmpFile, err := os.CreateTemp("", "ofelia_missing_*.ini")
	c.Assert(err, IsNil)
	path := tmpFile.Name()
	tmpFile.Close()
	os.Remove(path)

	backend, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{ConfigFile: path, Logger: logger, LogLevel: "DEBUG"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	var warnMsg bool
	for _, e := range backend.AllEntries() {
		if e.Level == logrus.WarnLevel && strings.Contains(e.Message, "Could not load config file") {
			warnMsg = true
		}
	}
	c.Assert(warnMsg, Equals, true)
}

func (s *DaemonBootSuite) TestBootLogsMissingConfigIncludesFilename(c *C) {
	tmpFile, err := os.CreateTemp("", "ofelia_missing_*.ini")
	c.Assert(err, IsNil)
	path := tmpFile.Name()
	tmpFile.Close()
	os.Remove(path)

	backend, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{ConfigFile: path, Logger: logger, LogLevel: "DEBUG"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	var warnMsg bool
	for _, e := range backend.AllEntries() {
		if e.Level == logrus.WarnLevel &&
			strings.Contains(e.Message, "Could not load config file") &&
			strings.Contains(e.Message, path) {
			warnMsg = true
		}
	}
	c.Assert(warnMsg, Equals, true)
}

func (s *DaemonBootSuite) TestBootWebWithoutDocker(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{Logger: logger, EnableWeb: true}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, cli dockerClient) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()
	c.Assert(cmd.webServer, NotNil)
}
