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
	// Note: Not parallel - modifies global newDockerHandler

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
	// Note: Not parallel - modifies global newDockerHandler

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
	// Note: Not parallel - modifies global newDockerHandler

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
	// Note: Not parallel - modifies global newDockerHandler

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
	// Note: Not parallel - modifies global newDockerHandler

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

func (s *DaemonBootSuite) TestApplyAuthOptionsCopiesNonDefaults(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:              logger,
		WebAuthEnabled:      true,
		WebUsername:         "testuser",
		WebPasswordHash:     "testhash",
		WebSecretKey:        "testsecret",
		WebTokenExpiry:      48,
		WebMaxLoginAttempts: 10,
	}
	config := NewConfig(logger)

	cmd.applyAuthOptions(config)

	c.Assert(config.Global.WebAuthEnabled, Equals, true)
	c.Assert(config.Global.WebUsername, Equals, "testuser")
	c.Assert(config.Global.WebPasswordHash, Equals, "testhash")
	c.Assert(config.Global.WebSecretKey, Equals, "testsecret")
	c.Assert(config.Global.WebTokenExpiry, Equals, 48)
	c.Assert(config.Global.WebMaxLoginAttempts, Equals, 10)
}

func (s *DaemonBootSuite) TestApplyAuthOptionsSkipsDefaults(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:              logger,
		WebAuthEnabled:      false,
		WebUsername:         "",
		WebPasswordHash:     "",
		WebSecretKey:        "",
		WebTokenExpiry:      24,
		WebMaxLoginAttempts: 5,
	}
	config := NewConfig(logger)
	config.Global.WebUsername = "existing"
	config.Global.WebTokenExpiry = 12

	cmd.applyAuthOptions(config)

	c.Assert(config.Global.WebAuthEnabled, Equals, false)
	c.Assert(config.Global.WebUsername, Equals, "existing")
	c.Assert(config.Global.WebTokenExpiry, Equals, 12)
}

func (s *DaemonBootSuite) TestApplyAuthDefaultsCopiesFromConfig(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:              logger,
		WebAuthEnabled:      false,
		WebUsername:         "",
		WebPasswordHash:     "",
		WebSecretKey:        "",
		WebTokenExpiry:      24,
		WebMaxLoginAttempts: 5,
	}
	config := NewConfig(logger)
	config.Global.WebAuthEnabled = true
	config.Global.WebUsername = "configuser"
	config.Global.WebPasswordHash = "confighash"
	config.Global.WebSecretKey = "configsecret"
	config.Global.WebTokenExpiry = 48
	config.Global.WebMaxLoginAttempts = 10

	cmd.applyAuthDefaults(config)

	c.Assert(cmd.WebAuthEnabled, Equals, true)
	c.Assert(cmd.WebUsername, Equals, "configuser")
	c.Assert(cmd.WebPasswordHash, Equals, "confighash")
	c.Assert(cmd.WebSecretKey, Equals, "configsecret")
	c.Assert(cmd.WebTokenExpiry, Equals, 48)
	c.Assert(cmd.WebMaxLoginAttempts, Equals, 10)
}

func (s *DaemonBootSuite) TestApplyAuthDefaultsPreservesCLIValues(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:              logger,
		WebAuthEnabled:      true,
		WebUsername:         "cliuser",
		WebPasswordHash:     "clihash",
		WebSecretKey:        "clisecret",
		WebTokenExpiry:      72,
		WebMaxLoginAttempts: 3,
	}
	config := NewConfig(logger)
	config.Global.WebAuthEnabled = false
	config.Global.WebUsername = "configuser"
	config.Global.WebPasswordHash = "confighash"
	config.Global.WebSecretKey = "configsecret"
	config.Global.WebTokenExpiry = 48
	config.Global.WebMaxLoginAttempts = 10

	cmd.applyAuthDefaults(config)

	c.Assert(cmd.WebAuthEnabled, Equals, true)
	c.Assert(cmd.WebUsername, Equals, "cliuser")
	c.Assert(cmd.WebPasswordHash, Equals, "clihash")
	c.Assert(cmd.WebSecretKey, Equals, "clisecret")
	c.Assert(cmd.WebTokenExpiry, Equals, 72)
	c.Assert(cmd.WebMaxLoginAttempts, Equals, 3)
}

func (s *DaemonBootSuite) TestApplyAuthDefaultsSkipsEmptyConfigValues(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:              logger,
		WebUsername:         "",
		WebPasswordHash:     "",
		WebSecretKey:        "",
		WebTokenExpiry:      24,
		WebMaxLoginAttempts: 5,
	}
	config := NewConfig(logger)

	cmd.applyAuthDefaults(config)

	c.Assert(cmd.WebUsername, Equals, "")
	c.Assert(cmd.WebPasswordHash, Equals, "")
	c.Assert(cmd.WebSecretKey, Equals, "")
	c.Assert(cmd.WebTokenExpiry, Equals, 24)
	c.Assert(cmd.WebMaxLoginAttempts, Equals, 5)
}

func (s *DaemonBootSuite) TestApplyWebDefaultsCopiesFromConfig(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:    logger,
		EnableWeb: false,
		WebAddr:   ":8081",
	}
	config := NewConfig(logger)
	config.Global.EnableWeb = true
	config.Global.WebAddr = ":9090"

	cmd.applyWebDefaults(config)

	c.Assert(cmd.EnableWeb, Equals, true)
	c.Assert(cmd.WebAddr, Equals, ":9090")
}

func (s *DaemonBootSuite) TestApplyWebDefaultsPreservesCLIValues(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:    logger,
		EnableWeb: true,
		WebAddr:   ":7070",
	}
	config := NewConfig(logger)
	config.Global.EnableWeb = false
	config.Global.WebAddr = ":9090"

	cmd.applyWebDefaults(config)

	c.Assert(cmd.EnableWeb, Equals, true)
	c.Assert(cmd.WebAddr, Equals, ":7070")
}

func (s *DaemonBootSuite) TestApplyServerDefaultsCopiesFromConfig(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:      logger,
		EnablePprof: false,
		PprofAddr:   "127.0.0.1:8080",
	}
	config := NewConfig(logger)
	config.Global.EnablePprof = true
	config.Global.PprofAddr = "0.0.0.0:6060"

	cmd.applyServerDefaults(config)

	c.Assert(cmd.EnablePprof, Equals, true)
	c.Assert(cmd.PprofAddr, Equals, "0.0.0.0:6060")
}

func (s *DaemonBootSuite) TestApplyServerDefaultsPreservesCLIValues(c *C) {
	_, logger := newMemoryLogger(logrus.InfoLevel)
	cmd := &DaemonCommand{
		Logger:      logger,
		EnablePprof: true,
		PprofAddr:   "localhost:9999",
	}
	config := NewConfig(logger)
	config.Global.EnablePprof = false
	config.Global.PprofAddr = "0.0.0.0:6060"

	cmd.applyServerDefaults(config)

	c.Assert(cmd.EnablePprof, Equals, true)
	c.Assert(cmd.PprofAddr, Equals, "localhost:9999")
}
