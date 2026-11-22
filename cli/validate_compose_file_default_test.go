// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/config"
	"github.com/netresearch/ofelia/test"
)

// A compose job without a file key runs with compose.yml, so validation does
// not refuse it; a job without a service still fails.
func TestValidate_ComposeJobWithoutFileUsesTheDefault(t *testing.T) {
	t.Parallel()
	cfg, err := BuildFromString("[job-compose \"backup\"]\nschedule = @daily\nservice = db\n", test.NewTestLogger())
	require.NoError(t, err)
	assert.NoError(t, config.NewConfigValidator(cfg).Validate())

	cfg, err = BuildFromString("[job-compose \"backup\"]\nschedule = @daily\nfile = compose.yml\n", test.NewTestLogger())
	require.NoError(t, err)
	assert.Error(t, config.NewConfigValidator(cfg).Validate(), "a compose job without a service passes")
}
