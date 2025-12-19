package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

func newMemoryLogger(level logrus.Level) (*logtest.Hook, core.Logger) {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logger.SetLevel(level)
	hook := logtest.NewLocal(logger)
	return hook, &core.LogrusAdapter{Logger: logger}
}

func TestBootLogsConfigError(t *testing.T) {
	t.Parallel()

	tmpFile, err := os.CreateTemp("", "ofelia_bad_*.ini")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("[global\nno-overlap = true\n")
	require.NoError(t, err)
	tmpFile.Close()

	backend, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{ConfigFile: tmpFile.Name(), Logger: logger, LogLevel: "DEBUG"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	var warnMsg bool
	for _, e := range backend.AllEntries() {
		if e.Level == logrus.WarnLevel && strings.Contains(e.Message, "Could not load config file") {
			warnMsg = true
		}
	}
	assert.True(t, warnMsg)
}

func TestBootLogsConfigErrorSuppressed(t *testing.T) {
	t.Parallel()

	tmpFile, err := os.CreateTemp("", "ofelia_bad_*.ini")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("[global\nno-overlap = true\n")
	require.NoError(t, err)
	tmpFile.Close()

	backend, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{ConfigFile: tmpFile.Name(), Logger: logger, LogLevel: "INFO"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	var debugMsg bool
	for _, e := range backend.AllEntries() {
		if e.Level == logrus.DebugLevel {
			debugMsg = true
		}
	}
	assert.False(t, debugMsg)
}

func TestBootLogsMissingConfig(t *testing.T) {
	t.Parallel()

	tmpFile, err := os.CreateTemp("", "ofelia_missing_*.ini")
	require.NoError(t, err)
	path := tmpFile.Name()
	tmpFile.Close()
	os.Remove(path)

	backend, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{ConfigFile: path, Logger: logger, LogLevel: "DEBUG"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()

	var warnMsg bool
	for _, e := range backend.AllEntries() {
		if e.Level == logrus.WarnLevel && strings.Contains(e.Message, "Could not load config file") {
			warnMsg = true
		}
	}
	assert.True(t, warnMsg)
}

func TestBootLogsMissingConfigIncludesFilename(t *testing.T) {
	t.Parallel()

	tmpFile, err := os.CreateTemp("", "ofelia_missing_*.ini")
	require.NoError(t, err)
	path := tmpFile.Name()
	tmpFile.Close()
	os.Remove(path)

	backend, logger := newMemoryLogger(logrus.DebugLevel)
	cmd := &DaemonCommand{ConfigFile: path, Logger: logger, LogLevel: "DEBUG"}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
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
	assert.True(t, warnMsg)
}

func TestBootWebWithoutDocker(t *testing.T) {
	t.Parallel()

	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{Logger: logger, EnableWeb: true}

	orig := newDockerHandler
	defer func() { newDockerHandler = orig }()
	newDockerHandler = func(ctx context.Context, notifier dockerLabelsUpdate, logger core.Logger, cfg *DockerConfig, provider core.DockerProvider) (*DockerHandler, error) {
		return nil, errors.New("docker unavailable")
	}

	_ = cmd.boot()
	assert.NotNil(t, cmd.webServer)
}
