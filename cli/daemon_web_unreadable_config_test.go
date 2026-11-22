// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/test"
)

// bootWithWebFlag boots a daemon whose web UI is enabled by flag, with the
// config file at path, and returns it with its log handler. Docker is
// replaced by a mock.
func bootWithWebFlag(t *testing.T, path string, auth bool) (*DaemonCommand, *test.Handler) {
	t.Helper()
	logger, handler := test.NewTestLoggerWithHandler()

	mockProv := &mockDockerProviderForHandler{}
	orig := newDockerHandler
	newDockerHandler = func(ctx context.Context, _ dockerContainersUpdate, l *slog.Logger, _ *DockerConfig, _ core.DockerProvider) (*DockerHandler, error) {
		return &DockerHandler{ctx: ctx, logger: l, dockerProvider: mockProv}, nil
	}
	t.Cleanup(func() { newDockerHandler = orig })

	cmd := &DaemonCommand{
		Logger:              logger,
		LevelVar:            &slog.LevelVar{},
		ConfigFile:          path,
		EnableWeb:           true,
		WebAddr:             "127.0.0.1:0",
		PprofAddr:           "127.0.0.1:0",
		WebTokenExpiry:      24,
		WebMaxLoginAttempts: 5,
	}
	if auth {
		cmd.WebAuthEnabled = true
		cmd.WebUsername = "admin"
		cmd.WebPasswordHash = "$2a$10$abcdefghijklmnopqrstuuJ5pXyBh2f8yQ0XtqX7mJ8u5Yb8l1hJG"
	}
	require.NoError(t, cmd.boot())
	return cmd, handler
}

// brokenConfig writes a config file that does not parse. Its [global] section
// is where web auth would be turned on.
func brokenConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.ini")
	require.NoError(t, os.WriteFile(path, []byte("[global\nweb-auth-enabled = true\n"), 0o600))
	return path
}

func TestBoot_UnreadableConfigKeepsUnauthenticatedWebOff(t *testing.T) {
	// Not parallel - modifies global newDockerHandler
	cmd, handler := bootWithWebFlag(t, brokenConfig(t), false)

	assert.Nil(t, cmd.webServer, "a config that did not load may hold the web auth settings")
	assert.False(t, cmd.EnableWeb)
	assert.True(t, handler.HasError("Web UI and API not started"))
}

func TestBoot_GlobWithADanglingMatchKeepsUnauthenticatedWebOff(t *testing.T) {
	// Not parallel - modifies global newDockerHandler
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a-auth.ini"), []byte(
		"[global]\nweb-auth-enabled = true\nweb-username = admin\nweb-password-hash = x\n"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone.ini"), filepath.Join(dir, "z-extra.ini")))

	cmd, _ := bootWithWebFlag(t, filepath.Join(dir, "*.ini"), false)

	assert.Nil(t, cmd.webServer, "a matched file that could not be read may hold the web auth settings")
}

func TestBoot_ConfigInAnUnreadableDirectoryKeepsUnauthenticatedWebOff(t *testing.T) {
	// Not parallel - modifies global newDockerHandler
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	for name, tc := range map[string]struct {
		mode    os.FileMode
		pattern string
	}{
		"literal path, directory not enterable": {0o000, "config.ini"},
		"glob, directory not listable":          {0o311, "*.ini"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "etc")
			require.NoError(t, os.Mkdir(dir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "config.ini"), []byte("[global]\nweb-auth-enabled = true\n"), 0o600))
			require.NoError(t, os.Chmod(dir, tc.mode))
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

			cmd, _ := bootWithWebFlag(t, filepath.Join(dir, tc.pattern), false)

			assert.Nil(t, cmd.webServer, "a config the daemon cannot see may hold the web auth settings")
		})
	}
}

func TestBoot_MissingConfigStillServesWeb(t *testing.T) {
	// Not parallel - modifies global newDockerHandler
	for name, path := range map[string]string{
		"missing file":     filepath.Join(t.TempDir(), "none.ini"),
		"no glob match":    filepath.Join(t.TempDir(), "*.ini"),
		"empty config arg": "",
	} {
		t.Run(name, func(t *testing.T) {
			cmd, _ := bootWithWebFlag(t, path, false)
			assert.NotNil(t, cmd.webServer, "without a config file, flags and environment hold every setting")
		})
	}
}

func TestBoot_UnreadableConfigServesWebWithAuthFlags(t *testing.T) {
	// Not parallel - modifies global newDockerHandler
	cmd, _ := bootWithWebFlag(t, brokenConfig(t), true)

	assert.NotNil(t, cmd.webServer, "web auth set by flag does not depend on the config file")
}
