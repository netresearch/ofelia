// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

func TestWebAddrIsLoopback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr string
		want bool
	}{
		{":8081", false},
		{"0.0.0.0:8081", false},
		{"[::]:8081", false},
		{"192.168.1.10:8081", false},
		{"127.0.0.1:8081", true},
		{"127.0.0.2:8081", true},
		{"[::1]:8081", true},
		{"localhost:8081", true},
		{"not-an-address", false},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, webAddrIsLoopback(tc.addr), tc.addr)
	}
}

// TestWarnUnauthenticatedWeb pins the startup warning for a web UI without
// authentication: the API can run commands on the host, so the operator is
// told, and told more strongly when the address is reachable from elsewhere.
func TestWarnUnauthenticatedWeb(t *testing.T) {
	t.Parallel()

	logger, handler := test.NewTestLoggerWithHandler()
	(&DaemonCommand{Logger: logger, WebAddr: ":8081"}).warnUnauthenticatedWeb()
	assert.True(t, handler.HasWarning("reachable from other hosts"))
	assert.True(t, handler.HasWarning("web-auth-enabled"))

	logger, handler = test.NewTestLoggerWithHandler()
	(&DaemonCommand{Logger: logger, WebAddr: "127.0.0.1:8081"}).warnUnauthenticatedWeb()
	assert.True(t, handler.HasWarning("without authentication"))
	assert.False(t, handler.HasWarning("reachable from other hosts"))
}

// TestBuildWebAuthConfig_SecretKeyMessage pins that the daemon no longer
// promises sessions that survive a restart: the key signs nothing.
func TestBuildWebAuthConfig_SecretKeyMessage(t *testing.T) {
	t.Parallel()

	logger, handler := test.NewTestLoggerWithHandler()
	cmd := &DaemonCommand{
		Logger:          logger,
		WebUsername:     "admin",
		WebPasswordHash: "$2a$12$hash",
	}
	_, err := cmd.buildWebAuthConfig()
	require.NoError(t, err)
	assert.Empty(t, handler.GetMessages(), "an unset key changes nothing and is not worth a line")

	logger, handler = test.NewTestLoggerWithHandler()
	cmd.Logger = logger
	cmd.WebSecretKey = "a-secret-key-that-is-at-least-32-characters"
	_, err = cmd.buildWebAuthConfig()
	require.NoError(t, err)
	assert.True(t, handler.HasMessage("has no effect"))
	assert.False(t, handler.HasMessage("persistent sessions"))
}
