// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"

	defaults "github.com/creasty/defaults"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
)

// assembleJob turns a job definition into a job: it applies Ofelia's
// struct-tag defaults and the global settings every job of that kind takes.
// The kind-specific rules live in the switch, so a rule added here reaches
// boot, INI reload, label reload and BuildAPIJob (web API and state file) at
// once. Last it checks the job with its own Validate, so a job that can never
// run is rejected the same way from every source. Middlewares and
// registration are left to the caller.
func (c *Config) assembleJob(name string, j jobConfig) error {
	_ = defaults.Set(j)
	switch j := j.(type) {
	case *ExecJobConfig:
		c.applyDefaultUser(&j.User)
		j.Provider = c.dockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
	case *RunJobConfig:
		c.applyDefaultUser(&j.User)
		j.Provider = c.dockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
	case *RunServiceConfig:
		c.applyDefaultUser(&j.User)
		j.Provider = c.dockerProvider()
		j.InitializeRuntimeFields()
		j.Name = name
	case *LocalJobConfig:
		j.Name = name
	case *ComposeJobConfig:
		c.dropComposeExecMaxRuntime(name, &j.ComposeJob)
		j.Provider = c.dockerProvider()
		j.Name = name
	default:
		panic(fmt.Sprintf("assembleJob: unknown job config type %T", j))
	}
	slack, mail, save := j.notificationConfigs()
	c.mergeNotificationDefaults(slack, mail, save)
	c.injectDedup(slack, mail)
	if v, ok := j.(validatable); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("invalid job configuration: %w", err)
		}
	}
	return nil
}

// dockerProvider returns the Docker provider jobs are assembled with, or nil
// when the daemon has no Docker handler.
func (c *Config) dockerProvider() core.DockerProvider {
	if c.dockerHandler == nil {
		return nil
	}
	return c.dockerHandler.GetDockerProvider()
}

// assembleAs adapts assembleJob to the prep callback that syncJobMap,
// addNewJob and replaceIfChanged take for one job kind.
func assembleAs[J jobConfig](c *Config) func(string, J) error {
	return func(name string, j J) error { return c.assembleJob(name, j) }
}

// notificationConfigs returns the job's own Slack, mail and save settings,
// the fields that inherit from [global] when the job leaves them empty.
func (c *ExecJobConfig) notificationConfigs() (*middlewares.SlackConfig, *middlewares.MailConfig, *middlewares.SaveConfig) {
	return &c.SlackConfig, &c.MailConfig, &c.SaveConfig
}

// notificationConfigs: see ExecJobConfig.notificationConfigs.
func (c *RunJobConfig) notificationConfigs() (*middlewares.SlackConfig, *middlewares.MailConfig, *middlewares.SaveConfig) {
	return &c.SlackConfig, &c.MailConfig, &c.SaveConfig
}

// notificationConfigs: see ExecJobConfig.notificationConfigs.
func (c *RunServiceConfig) notificationConfigs() (*middlewares.SlackConfig, *middlewares.MailConfig, *middlewares.SaveConfig) {
	return &c.SlackConfig, &c.MailConfig, &c.SaveConfig
}

// notificationConfigs: see ExecJobConfig.notificationConfigs.
func (c *LocalJobConfig) notificationConfigs() (*middlewares.SlackConfig, *middlewares.MailConfig, *middlewares.SaveConfig) {
	return &c.SlackConfig, &c.MailConfig, &c.SaveConfig
}

// notificationConfigs: see ExecJobConfig.notificationConfigs.
func (c *ComposeJobConfig) notificationConfigs() (*middlewares.SlackConfig, *middlewares.MailConfig, *middlewares.SaveConfig) {
	return &c.SlackConfig, &c.MailConfig, &c.SaveConfig
}
