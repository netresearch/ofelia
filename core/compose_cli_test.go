// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeVersionAtLeast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		want    bool
	}{
		{"5.6.0", true},
		{"5.6.1", true},
		{"v5.6.0", true},
		{"5.10.0", true},
		{"6.0.0", true},
		{"5.6.0-rc.1", true},
		{"5.5.9", false},
		{"2.38.2", false},
		{"4.99.99", false},
		{"", false},
		{"dev", false},
		{"5.6", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ComposeVersionAtLeast(tt.version, MinComposeJobsVersion), tt.version)
	}
}

func TestCheckComposeJobsSupport_TooOld(t *testing.T) {
	installFakeDocker(t, `echo "2.38.2"`)
	t.Setenv("DOCKER_HOST", "")

	err := CheckComposeJobsSupport(context.Background())

	require.ErrorIs(t, err, ErrComposeJobsUnsupported)
	assert.Contains(t, err.Error(), "found docker compose 2.38.2")
	assert.Contains(t, err.Error(), MinComposeJobsVersion)
}

func TestCheckComposeJobsSupport_CurrentVersion(t *testing.T) {
	installFakeDocker(t, `echo "v5.6.1"`)
	t.Setenv("DOCKER_HOST", "")

	require.NoError(t, CheckComposeJobsSupport(context.Background()))
}

func TestCheckComposeJobsSupport_CLIFails(t *testing.T) {
	installFakeDocker(t, `echo "docker: 'compose' is not a docker command." >&2; exit 1`)
	t.Setenv("DOCKER_HOST", "")

	err := CheckComposeJobsSupport(context.Background())

	require.ErrorIs(t, err, ErrComposeJobsUnsupported)
}
