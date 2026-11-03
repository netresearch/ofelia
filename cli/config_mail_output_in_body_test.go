// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

// TestMailOutputInBody_ParsedFromINI pins the INI decoding of the *int
// mail-output-in-body key at global and job level, including an explicit 0
// on the job that must stay distinguishable from "not set".
func TestMailOutputInBody_ParsedFromINI(t *testing.T) {
	t.Parallel()

	cfg, err := BuildFromString(`
[global]
smtp-host = mail.example.com
mail-output-in-body = 20

[job-exec "inherits"]
schedule = @daily
container = app
command = echo ok

[job-exec "attachments"]
schedule = @daily
container = app
command = echo ok
mail-output-in-body = 0

[job-exec "all"]
schedule = @daily
container = app
command = echo ok
mail-output-in-body = -1
`, test.NewTestLogger())
	require.NoError(t, err)

	require.NotNil(t, cfg.Global.MailOutputInBody)
	assert.Equal(t, 20, *cfg.Global.MailOutputInBody)

	assert.Nil(t, cfg.ExecJobs["inherits"].MailOutputInBody, "absent key stays nil")
	require.NotNil(t, cfg.ExecJobs["attachments"].MailOutputInBody, "explicit 0 must not read as unset")
	assert.Equal(t, 0, *cfg.ExecJobs["attachments"].MailOutputInBody)
	require.NotNil(t, cfg.ExecJobs["all"].MailOutputInBody)
	assert.Equal(t, -1, *cfg.ExecJobs["all"].MailOutputInBody)
}

func TestMergeMailDefaults_MailOutputInBody(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		global *int
		job    *int
		want   *int
	}{
		{"both unset", nil, nil, nil},
		{"job inherits global", new(20), nil, new(20)},
		{"job 0 overrides global", new(20), new(0), new(0)},
		{"job value overrides global", new(20), new(-1), new(-1)},
		{"job value without global", nil, new(5), new(5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := NewConfig(test.NewTestLogger())
			cfg.Global.MailConfig.MailOutputInBody = tc.global
			job := middlewares.MailConfig{SMTPHost: "mail.example.com", MailOutputInBody: tc.job}

			cfg.mergeMailDefaults(&job)

			assert.Equal(t, tc.want, job.MailOutputInBody)
			if tc.global != nil && job.MailOutputInBody != nil {
				assert.NotSame(t, tc.global, job.MailOutputInBody,
					"job must not share the global's pointer")
			}
		})
	}
}

func TestMergeMailFlagGlobals_MailOutputInBody(t *testing.T) {
	t.Parallel()

	// Label fills an unset INI value.
	dst := &middlewares.MailConfig{}
	src := &middlewares.MailConfig{MailOutputInBody: new(10)}
	assert.True(t, mergeMailFlagGlobals(dst, src))
	require.NotNil(t, dst.MailOutputInBody)
	assert.Equal(t, 10, *dst.MailOutputInBody)
	assert.NotSame(t, src.MailOutputInBody, dst.MailOutputInBody)

	// An INI value, including 0, wins over the label.
	dst = &middlewares.MailConfig{MailOutputInBody: new(0)}
	assert.False(t, mergeMailFlagGlobals(dst, src))
	assert.Equal(t, 0, *dst.MailOutputInBody)

	// Neither set: nothing changes.
	dst = &middlewares.MailConfig{}
	assert.False(t, mergeMailFlagGlobals(dst, &middlewares.MailConfig{}))
	assert.Nil(t, dst.MailOutputInBody)
}

// TestMailOutputInBody_FromLabels covers both label scopes: the global key on
// the service container (allow-listed) and the per-job key.
func TestMailOutputInBody_FromLabels(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(test.NewTestLogger())
	err := cfg.buildFromDockerContainers([]DockerContainerInfo{
		{
			Name:  "ofelia-service",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":             "true",
				"ofelia.service":             "true",
				"ofelia.mail-output-in-body": "15",
			},
		},
		{
			Name:  "worker",
			State: domain.ContainerState{Running: true},
			Labels: map[string]string{
				"ofelia.enabled":                             "true",
				"ofelia.job-exec.report.schedule":            "@daily",
				"ofelia.job-exec.report.command":             "report.sh",
				"ofelia.job-exec.report.mail-output-in-body": "0",
			},
		},
	})
	require.NoError(t, err)

	require.NotNil(t, cfg.Global.MailOutputInBody, "allow-listed global label must decode")
	assert.Equal(t, 15, *cfg.Global.MailOutputInBody)

	job, ok := cfg.ExecJobs["worker.report"]
	require.True(t, ok, "job keys: %v", keysOf(cfg.ExecJobs))
	require.NotNil(t, job.MailOutputInBody)
	assert.Equal(t, 0, *job.MailOutputInBody)
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
