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

func TestApplyAuthOptionsCopiesNonDefaults(t *testing.T) {
	t.Parallel()
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

	assert.True(t, config.Global.WebAuthEnabled)
	assert.Equal(t, "testuser", config.Global.WebUsername)
	assert.Equal(t, "testhash", config.Global.WebPasswordHash)
	assert.Equal(t, "testsecret", config.Global.WebSecretKey)
	assert.Equal(t, 48, config.Global.WebTokenExpiry)
	assert.Equal(t, 10, config.Global.WebMaxLoginAttempts)
}

func TestApplyAuthOptionsSkipsDefaults(t *testing.T) {
	t.Parallel()
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

	assert.False(t, config.Global.WebAuthEnabled)
	assert.Equal(t, "existing", config.Global.WebUsername)
	assert.Equal(t, 12, config.Global.WebTokenExpiry)
}

func TestApplyAuthDefaultsCopiesFromConfig(t *testing.T) {
	t.Parallel()
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

	assert.True(t, cmd.WebAuthEnabled)
	assert.Equal(t, "configuser", cmd.WebUsername)
	assert.Equal(t, "confighash", cmd.WebPasswordHash)
	assert.Equal(t, "configsecret", cmd.WebSecretKey)
	assert.Equal(t, 48, cmd.WebTokenExpiry)
	assert.Equal(t, 10, cmd.WebMaxLoginAttempts)
}

func TestApplyAuthDefaultsPreservesCLIValues(t *testing.T) {
	t.Parallel()
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

	assert.True(t, cmd.WebAuthEnabled)
	assert.Equal(t, "cliuser", cmd.WebUsername)
	assert.Equal(t, "clihash", cmd.WebPasswordHash)
	assert.Equal(t, "clisecret", cmd.WebSecretKey)
	assert.Equal(t, 72, cmd.WebTokenExpiry)
	assert.Equal(t, 3, cmd.WebMaxLoginAttempts)
}

func TestApplyAuthDefaultsSkipsEmptyConfigValues(t *testing.T) {
	t.Parallel()
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

	assert.Empty(t, cmd.WebUsername)
	assert.Empty(t, cmd.WebPasswordHash)
	assert.Empty(t, cmd.WebSecretKey)
	assert.Equal(t, 24, cmd.WebTokenExpiry)
	assert.Equal(t, 5, cmd.WebMaxLoginAttempts)
}

func TestApplyWebDefaultsCopiesFromConfig(t *testing.T) {
	t.Parallel()
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

	assert.True(t, cmd.EnableWeb)
	assert.Equal(t, ":9090", cmd.WebAddr)
}

func TestApplyWebDefaultsPreservesCLIValues(t *testing.T) {
	t.Parallel()
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

	assert.True(t, cmd.EnableWeb)
	assert.Equal(t, ":7070", cmd.WebAddr)
}

func TestApplyServerDefaultsCopiesFromConfig(t *testing.T) {
	t.Parallel()
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

	assert.True(t, cmd.EnablePprof)
	assert.Equal(t, "0.0.0.0:6060", cmd.PprofAddr)
}

func TestApplyServerDefaultsPreservesCLIValues(t *testing.T) {
	t.Parallel()
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

	assert.True(t, cmd.EnablePprof)
	assert.Equal(t, "localhost:9999", cmd.PprofAddr)
}
