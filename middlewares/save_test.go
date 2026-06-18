// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

const testNameFoo = "foo"

func setupSaveTestContext(t *testing.T) (*core.Context, *TestJob) {
	t.Helper()
	job := &TestJobConfig{
		TestJob: TestJob{
			BareJob: core.BareJob{
				Name: "test-job-save",
			},
		},
		MailConfig: MailConfig{
			SMTPHost:     "test-host",
			SMTPPassword: "secret-password",
			SMTPUser:     "secret-user",
		},
		SlackConfig: SlackConfig{
			SlackWebhook: "secret-url",
		},
	}

	sh := core.NewScheduler(newDiscardLogger())
	e, err := core.NewExecution()
	require.NoError(t, err)

	ctx := core.NewContext(sh, job, e)
	return ctx, &job.TestJob
}

func TestNewSaveEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, NewSave(&SaveConfig{}))
}

func TestSaveRunSuccess(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := t.TempDir()

	ctx.Start()
	ctx.Stop(nil)

	job.Name = testNameFoo
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	require.NoError(t, m.Run(ctx))

	_, err := os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".json"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".stdout.log"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".stderr.log"))
	require.NoError(t, err)
}

func TestSaveRunSuccessOnError(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := t.TempDir()

	ctx.Start()
	ctx.Stop(nil)

	job.Name = testNameFoo
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir, SaveOnlyOnError: new(true)})
	require.NoError(t, m.Run(ctx))

	_, err := os.Stat(filepath.Join(dir, "00010101_000000_"+testNameFoo+".json"))
	assert.Error(t, err)
}

func TestSaveSensitiveData(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := t.TempDir()

	ctx.Start()
	ctx.Stop(nil)

	job.Name = "job-with-sensitive-data"
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	require.NoError(t, m.Run(ctx))

	expectedFileName := "00010101_000000_job-with-sensitive-data"
	_, err := os.Stat(filepath.Join(dir, expectedFileName+".json"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, expectedFileName+".stdout.log"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, expectedFileName+".stderr.log"))
	require.NoError(t, err)

	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, files, 3)

	for _, file := range files {
		b, err := os.ReadFile(filepath.Join(dir, file.Name()))
		require.NoError(t, err)

		if strings.Contains(string(b), "secret") {
			t.Logf("Content: %s", string(b))
			t.Errorf("found secret string in %q", file.Name())
		}
	}
}

func TestSaveCreatesSaveFolder(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := filepath.Join(t.TempDir(), "save-subdir")

	ctx.Start()
	ctx.Stop(nil)

	job.Name = testNameFoo
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	require.NoError(t, m.Run(ctx))

	fi, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
}

func TestSave_ContinueOnStop(t *testing.T) {
	t.Parallel()
	m := &Save{}
	assert.True(t, m.ContinueOnStop(), "Save.ContinueOnStop() should return true")
}

func TestSaveSafeFilename(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := t.TempDir()

	ctx.Start()
	ctx.Stop(nil)

	job.Name = "foo/bar\\baz"
	ctx.Execution.Date = time.Time{}

	m := NewSave(&SaveConfig{SaveFolder: dir})
	require.NoError(t, m.Run(ctx))

	safe := strings.NewReplacer("/", "_", "\\", "_").Replace(job.Name)
	_, err := os.Stat(filepath.Join(dir, "00010101_000000_"+safe+".stdout.log"))
	require.NoError(t, err)
}

// Phase 8: Additional coverage tests for save.go

func TestSaveConfig_RestoreHistoryEnabled_NilPointer(t *testing.T) {
	t.Parallel()

	cfg := SaveConfig{RestoreHistory: nil, SaveFolder: ""}
	assert.False(t, cfg.RestoreHistoryEnabled(), "nil RestoreHistory with empty SaveFolder should be false")
}

func TestSaveConfig_GetRestoreHistoryMaxAge_ZeroReturnsDefault(t *testing.T) {
	t.Parallel()

	cfg := SaveConfig{RestoreHistoryMaxAge: 0}
	assert.Equal(t, 24*time.Hour, cfg.GetRestoreHistoryMaxAge())
}

func TestSaveConfig_GetRestoreHistoryMaxAge_NegativeReturnsDefault(t *testing.T) {
	t.Parallel()

	cfg := SaveConfig{RestoreHistoryMaxAge: -1 * time.Hour}
	assert.Equal(t, 24*time.Hour, cfg.GetRestoreHistoryMaxAge())
}

func TestSaveConfig_GetSaveFileMode_DefaultAndCustom(t *testing.T) {
	t.Parallel()

	def, err := (&SaveConfig{}).GetSaveFileMode()
	require.NoError(t, err)
	assert.Equal(t, defaultSaveFileMode, def, "empty save-mode resolves to 0600")

	custom, err := (&SaveConfig{SaveMode: "0644"}).GetSaveFileMode()
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), custom)
}

func TestSaveConfig_GetSaveFolderMode_DefaultAndCustom(t *testing.T) {
	t.Parallel()

	def, err := (&SaveConfig{}).GetSaveFolderMode()
	require.NoError(t, err)
	assert.Equal(t, defaultSaveFolderMode, def, "empty save-folder-mode resolves to 0750")

	custom, err := (&SaveConfig{SaveFolderMode: "0755"}).GetSaveFolderMode()
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), custom)
}

func TestParseFileMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    os.FileMode
		wantErr bool
	}{
		{in: "0644", want: 0o644},
		{in: "0o644", want: 0o644},
		{in: "0O644", want: 0o644},
		{in: "644", want: 0o644},
		{in: " 0755 ", want: 0o755},
		{in: "0", want: 0},
		{in: "0777", want: 0o777},
		{in: "1777", wantErr: true}, // special bits rejected
		{in: "0888", wantErr: true}, // not octal
		{in: "abc", wantErr: true},
		{in: "", wantErr: true},
	}
	require.GreaterOrEqual(t, len(cases), 11, "table accidentally emptied — parseFileMode would be untested")
	for _, tc := range cases {
		got, err := parseFileMode(tc.in)
		if tc.wantErr {
			require.Errorf(t, err, "parseFileMode(%q) should error", tc.in)
			continue
		}
		require.NoErrorf(t, err, "parseFileMode(%q)", tc.in)
		assert.Equalf(t, tc.want, got, "parseFileMode(%q)", tc.in)
	}
}

func TestSaveInvalidModeReturnsError(t *testing.T) {
	t.Parallel()

	// Both sibling mode resolvers must surface a clear, attributable error.
	cases := []struct {
		name    string
		cfg     SaveConfig
		wantMsg string
	}{
		{"invalid save-mode", SaveConfig{SaveMode: "not-octal"}, "save-mode"},
		{"invalid save-folder-mode", SaveConfig{SaveFolderMode: "not-octal"}, "save-folder-mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, job := setupSaveTestContext(t)

			dir := filepath.Join(t.TempDir(), "logs")
			ctx.Start()
			ctx.Stop(nil)
			job.Name = testNameFoo
			ctx.Execution.Date = time.Time{}

			cfg := tc.cfg
			cfg.SaveFolder = dir
			m := &Save{cfg}
			err := m.saveToDisk(ctx)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

func TestSaveRunOnlyOnError_Saves(t *testing.T) {
	t.Parallel()
	ctx, job := setupSaveTestContext(t)

	dir := t.TempDir()

	ctx.Start()
	ctx.Stop(fmt.Errorf("simulated failure"))
	ctx.Execution.Failed = true

	job.Name = "error-job"
	ctx.Execution.Date = time.Time{}

	trueVal := true
	m := NewSave(&SaveConfig{SaveFolder: dir, SaveOnlyOnError: &trueVal})
	require.NoError(t, m.Run(ctx))

	_, err := os.Stat(filepath.Join(dir, "00010101_000000_error-job.json"))
	require.NoError(t, err, "JSON file should exist when save-only-on-error is set and job failed")
}
