// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
	"github.com/netresearch/ofelia/test"
)

// assemblyConfig returns a Config whose globals every rule of assembleJob
// reads: a default user, a save folder, a notification cooldown and a
// Docker provider.
func assemblyConfig(t *testing.T) *Config {
	t.Helper()
	cfg := NewConfig(test.NewTestLogger())
	cfg.dockerHandler = &DockerHandler{dockerProvider: &mockDockerProviderForHandler{}}
	cfg.Global.DefaultUser = "nobody"
	cfg.Global.SaveFolder = "/var/log/ofelia"
	cfg.Global.NotificationCooldown = time.Minute
	// Not initNotificationDedup: it also replaces the package-wide
	// middlewares.DefaultNotificationDedup, which parallel tests share.
	cfg.notificationDedup = middlewares.NewNotificationDedup(time.Minute)
	return cfg
}

// userOf returns the user of a job kind that runs in a container, and false
// for a kind without one.
func userOf(j jobConfig) (string, bool) {
	switch j := j.(type) {
	case *ExecJobConfig:
		return j.User, true
	case *RunJobConfig:
		return j.User, true
	case *RunServiceConfig:
		return j.User, true
	}
	return "", false
}

func setUser(j jobConfig, user string) {
	switch j := j.(type) {
	case *ExecJobConfig:
		j.User = user
	case *RunJobConfig:
		j.User = user
	case *RunServiceConfig:
		j.User = user
	}
}

func providerOf(j jobConfig) (core.DockerProvider, bool) {
	switch j := j.(type) {
	case *ExecJobConfig:
		return j.Provider, true
	case *RunJobConfig:
		return j.Provider, true
	case *RunServiceConfig:
		return j.Provider, true
	case *ComposeJobConfig:
		return j.Provider, true
	}
	return nil, false
}

func historyLimitOf(t *testing.T, j jobConfig) int {
	t.Helper()
	switch j := j.(type) {
	case *ExecJobConfig:
		return j.HistoryLimit
	case *RunJobConfig:
		return j.HistoryLimit
	case *RunServiceConfig:
		return j.HistoryLimit
	case *LocalJobConfig:
		return j.HistoryLimit
	case *ComposeJobConfig:
		return j.HistoryLimit
	}
	t.Fatalf("unknown job config %T", j)
	return 0
}

// Every job kind, as each job source hands it over: a definition with
// nothing filled in that the source did not state.
var assemblyKinds = []struct {
	kind string
	new  func() jobConfig
}{
	{"exec", func() jobConfig { return &ExecJobConfig{} }},
	{"run", func() jobConfig { return &RunJobConfig{} }},
	{"service", func() jobConfig { return &RunServiceConfig{} }},
	{"local", func() jobConfig { return &LocalJobConfig{} }},
	{"compose", func() jobConfig { return &ComposeJobConfig{} }},
}

func TestAssembleJob(t *testing.T) {
	t.Parallel()

	for _, k := range assemblyKinds {
		t.Run(k.kind+"/name and struct-tag defaults", func(t *testing.T) {
			t.Parallel()
			j := k.new()
			assemblyConfig(t).assembleJob("job1", j)
			assert.Equal(t, "job1", j.GetName())
			assert.Equal(t, 10, historyLimitOf(t, j), "HistoryLimit default")
		})

		t.Run(k.kind+"/default user", func(t *testing.T) {
			t.Parallel()
			j := k.new()
			assemblyConfig(t).assembleJob("job1", j)
			user, ok := userOf(j)
			if !ok {
				t.Skipf("%s jobs have no user", k.kind)
			}
			assert.Equal(t, "nobody", user, "a job without a user runs as [global] default-user")

			own := k.new()
			setUser(own, "app")
			assemblyConfig(t).assembleJob("job2", own)
			user, _ = userOf(own)
			assert.Equal(t, "app", user, "a job's own user wins")

			def := k.new()
			setUser(def, UserContainerDefault)
			assemblyConfig(t).assembleJob("job3", def)
			user, _ = userOf(def)
			assert.Empty(t, user, "user = default selects the container's user")
		})

		t.Run(k.kind+"/docker provider", func(t *testing.T) {
			t.Parallel()
			cfg := assemblyConfig(t)
			j := k.new()
			cfg.assembleJob("job1", j)
			p, ok := providerOf(j)
			if !ok {
				t.Skipf("%s jobs have no Docker provider", k.kind)
			}
			assert.Same(t, cfg.dockerHandler.GetDockerProvider(), p)
		})

		t.Run(k.kind+"/notification settings inherit from [global]", func(t *testing.T) {
			t.Parallel()
			cfg := assemblyConfig(t)
			j := k.new()
			cfg.assembleJob("job1", j)
			slack, mail, save := j.notificationConfigs()
			assert.Equal(t, "/var/log/ofelia", save.SaveFolder)
			assert.Same(t, cfg.notificationDedup, slack.Dedup)
			assert.Same(t, cfg.notificationDedup, mail.Dedup)

			own := k.new()
			_, _, ownSave := own.notificationConfigs()
			ownSave.SaveFolder = "/srv/own"
			cfg.assembleJob("job2", own)
			assert.Equal(t, "/srv/own", ownSave.SaveFolder, "a job's own setting wins")
		})
	}
}

// RunJob is the kind whose struct-tag default is load-bearing: Delete
// "true" is what removes the container after the run.
func TestAssembleJob_RunJobDeletesItsContainer(t *testing.T) {
	t.Parallel()
	j := &RunJobConfig{}
	assemblyConfig(t).assembleJob("job1", j)
	assert.Equal(t, "true", j.Delete)
}

// A config file may name a max-runtime for a compose job in exec mode,
// which loaded without effect before compose jobs honored it. Assembly
// drops it so the file keeps loading; a run-mode job keeps its value.
func TestAssembleJob_ComposeExecMaxRuntime(t *testing.T) {
	t.Parallel()

	execJob := &ComposeJobConfig{}
	execJob.Exec = true
	execJob.MaxRuntime = time.Hour
	assemblyConfig(t).assembleJob("job1", execJob)
	assert.Zero(t, execJob.MaxRuntime)

	runJob := &ComposeJobConfig{}
	runJob.MaxRuntime = time.Hour
	assemblyConfig(t).assembleJob("job2", runJob)
	assert.Equal(t, time.Hour, runJob.MaxRuntime)
}

// Without a cooldown there is no deduplicator, and assembly leaves the
// job's Dedup fields empty.
func TestAssembleJob_NoDedupWithoutCooldown(t *testing.T) {
	t.Parallel()
	cfg := NewConfig(test.NewTestLogger())
	cfg.dockerHandler = &DockerHandler{}
	j := &ExecJobConfig{}
	cfg.assembleJob("job1", j)
	assert.Nil(t, j.SlackConfig.Dedup)
	assert.Nil(t, j.MailConfig.Dedup)
}
