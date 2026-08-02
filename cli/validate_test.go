// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/test"
)

func TestValidateExecuteValidFile(t *testing.T) {
	// Not parallel: modifies global os.Stdout which races with other tests.

	configFile := filepath.Join(t.TempDir(), "config.ini")
	// container is part of a runnable job-exec: without it every run fails
	// with `run_exec container "": invalid container name or ID`. The fixture
	// omitted it and this test asserted the config was valid, which is what
	// job validation now catches.
	content := `
[job-exec "foo"]
schedule = @every 10s
container = some-container
command = echo "foo"
`
	err := os.WriteFile(configFile, []byte(content), 0o644)
	require.NoError(t, err)

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	cmd := ValidateCommand{ConfigFile: configFile, Logger: test.NewTestLogger()}
	err = cmd.Execute(nil)
	require.NoError(t, err)

	w.Close()
	out, _ := io.ReadAll(r)

	var conf Config
	err = json.Unmarshal(out, &conf)
	require.NoError(t, err)
	job, ok := conf.ExecJobs["foo"]
	assert.True(t, ok)
	assert.Equal(t, 10, job.HistoryLimit)
}

func TestValidateExecuteInvalidFile(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.ini")
	err := os.WriteFile(configFile, []byte("[job-exec \"foo\"\nschedule = @every 10s\n"), 0o644)
	require.NoError(t, err)

	cmd := ValidateCommand{ConfigFile: configFile, Logger: test.NewTestLogger()}
	err = cmd.Execute(nil)
	assert.Error(t, err)
}

func TestValidateExecuteMissingFile(t *testing.T) {
	t.Parallel()

	cmd := ValidateCommand{ConfigFile: "/nonexistent/ofelia/config.ini", Logger: test.NewTestLogger()}
	err := cmd.Execute(nil)
	assert.Error(t, err)
}

// TestValidateExecuteRunsValidatorWithoutStrictFlag pins that running validate
// is itself the request to have the config checked. The checks used to sit
// behind enable-strict-validation, which defaults to false, so the one command
// whose purpose is validation reported success on a config it had not
// inspected.
//
// The config below parses cleanly as INI and is only wrong semantically, so it
// exercises the validator rather than the loader.
func TestValidateExecuteRunsValidatorWithoutStrictFlag(t *testing.T) {
	// Not parallel: modifies global os.Stdout which races with other tests.

	configFile := filepath.Join(t.TempDir(), "config.ini")
	content := `
[global]
web-address = definitely-not-an-address

[job-exec "foo"]
schedule = @every 10s
command = echo "foo"
`
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0o644))

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	cmd := ValidateCommand{ConfigFile: configFile, Logger: test.NewTestLogger()}
	err := cmd.Execute(nil)

	w.Close()
	_, _ = io.ReadAll(r)

	require.Error(t, err, "an invalid web-address was accepted without the strict flag")
	assert.Contains(t, err.Error(), "web-address")
}
