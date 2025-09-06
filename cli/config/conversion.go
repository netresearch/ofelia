package config

import (
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
)

// Legacy job config types - importing from parent package to avoid circular dependencies
// These will be used for backward compatibility conversion

// ExecJobConfigLegacy represents the legacy ExecJobConfig structure
type ExecJobConfigLegacy struct {
	core.ExecJob              `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

// RunJobConfigLegacy represents the legacy RunJobConfig structure
type RunJobConfigLegacy struct {
	core.RunJob               `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

// RunServiceConfigLegacy represents the legacy RunServiceConfig structure
type RunServiceConfigLegacy struct {
	core.RunServiceJob        `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

// LocalJobConfigLegacy represents the legacy LocalJobConfig structure
type LocalJobConfigLegacy struct {
	core.LocalJob             `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

// ComposeJobConfigLegacy represents the legacy ComposeJobConfig structure
type ComposeJobConfigLegacy struct {
	core.ComposeJob           `mapstructure:",squash"`
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
	JobSource                 JobSource `json:"-" mapstructure:"-"`
}

// ConvertFromExecJobConfig converts legacy ExecJobConfig to UnifiedJobConfig
func ConvertFromExecJobConfig(legacy *ExecJobConfigLegacy) *UnifiedJobConfig {
	unified := &UnifiedJobConfig{
		Type:      JobTypeExec,
		JobSource: legacy.JobSource,
		MiddlewareConfig: MiddlewareConfig{
			OverlapConfig: legacy.OverlapConfig,
			SlackConfig:   legacy.SlackConfig,
			SaveConfig:    legacy.SaveConfig,
			MailConfig:    legacy.MailConfig,
		},
		ExecJob: &legacy.ExecJob,
	}
	return unified
}

// ConvertFromRunJobConfig converts legacy RunJobConfig to UnifiedJobConfig
func ConvertFromRunJobConfig(legacy *RunJobConfigLegacy) *UnifiedJobConfig {
	unified := &UnifiedJobConfig{
		Type:      JobTypeRun,
		JobSource: legacy.JobSource,
		MiddlewareConfig: MiddlewareConfig{
			OverlapConfig: legacy.OverlapConfig,
			SlackConfig:   legacy.SlackConfig,
			SaveConfig:    legacy.SaveConfig,
			MailConfig:    legacy.MailConfig,
		},
		RunJob: &legacy.RunJob,
	}
	return unified
}

// ConvertFromRunServiceConfig converts legacy RunServiceConfig to UnifiedJobConfig
func ConvertFromRunServiceConfig(legacy *RunServiceConfigLegacy) *UnifiedJobConfig {
	unified := &UnifiedJobConfig{
		Type:      JobTypeService,
		JobSource: legacy.JobSource,
		MiddlewareConfig: MiddlewareConfig{
			OverlapConfig: legacy.OverlapConfig,
			SlackConfig:   legacy.SlackConfig,
			SaveConfig:    legacy.SaveConfig,
			MailConfig:    legacy.MailConfig,
		},
		RunServiceJob: &legacy.RunServiceJob,
	}
	return unified
}

// ConvertFromLocalJobConfig converts legacy LocalJobConfig to UnifiedJobConfig
func ConvertFromLocalJobConfig(legacy *LocalJobConfigLegacy) *UnifiedJobConfig {
	unified := &UnifiedJobConfig{
		Type:      JobTypeLocal,
		JobSource: legacy.JobSource,
		MiddlewareConfig: MiddlewareConfig{
			OverlapConfig: legacy.OverlapConfig,
			SlackConfig:   legacy.SlackConfig,
			SaveConfig:    legacy.SaveConfig,
			MailConfig:    legacy.MailConfig,
		},
		LocalJob: &legacy.LocalJob,
	}
	return unified
}

// ConvertFromComposeJobConfig converts legacy ComposeJobConfig to UnifiedJobConfig
func ConvertFromComposeJobConfig(legacy *ComposeJobConfigLegacy) *UnifiedJobConfig {
	unified := &UnifiedJobConfig{
		Type:      JobTypeCompose,
		JobSource: legacy.JobSource,
		MiddlewareConfig: MiddlewareConfig{
			OverlapConfig: legacy.OverlapConfig,
			SlackConfig:   legacy.SlackConfig,
			SaveConfig:    legacy.SaveConfig,
			MailConfig:    legacy.MailConfig,
		},
		ComposeJob: &legacy.ComposeJob,
	}
	return unified
}

// ConvertToExecJobConfig converts UnifiedJobConfig back to legacy ExecJobConfig
// Used for backward compatibility where legacy types are still expected
func ConvertToExecJobConfig(unified *UnifiedJobConfig) *ExecJobConfigLegacy {
	if unified.Type != JobTypeExec || unified.ExecJob == nil {
		return nil
	}

	legacy := &ExecJobConfigLegacy{
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
	// Copy job fields individually to avoid copying mutex
	if unified.ExecJob != nil {
		legacy.Schedule = unified.ExecJob.Schedule
		legacy.Name = unified.ExecJob.Name
		legacy.Command = unified.ExecJob.Command
		legacy.Container = unified.ExecJob.Container
		legacy.User = unified.ExecJob.User
		legacy.TTY = unified.ExecJob.TTY
		legacy.Environment = unified.ExecJob.Environment
		legacy.HistoryLimit = unified.ExecJob.HistoryLimit
		legacy.MaxRetries = unified.ExecJob.MaxRetries
		legacy.RetryDelayMs = unified.ExecJob.RetryDelayMs
		legacy.RetryExponential = unified.ExecJob.RetryExponential
		legacy.RetryMaxDelayMs = unified.ExecJob.RetryMaxDelayMs
		legacy.Dependencies = unified.ExecJob.Dependencies
		legacy.OnSuccess = unified.ExecJob.OnSuccess
		legacy.OnFailure = unified.ExecJob.OnFailure
		legacy.AllowParallel = unified.ExecJob.AllowParallel
	}
	return legacy
}

// ConvertToRunJobConfig converts UnifiedJobConfig back to legacy RunJobConfig
func ConvertToRunJobConfig(unified *UnifiedJobConfig) *RunJobConfigLegacy {
	if unified.Type != JobTypeRun || unified.RunJob == nil {
		return nil
	}

	legacy := &RunJobConfigLegacy{
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
	// Copy job fields individually to avoid copying mutex  
	if unified.RunJob != nil {
		legacy.Schedule = unified.RunJob.Schedule
		legacy.Name = unified.RunJob.Name
		legacy.Command = unified.RunJob.Command
		legacy.Image = unified.RunJob.Image
		legacy.User = unified.RunJob.User
		legacy.TTY = unified.RunJob.TTY
		legacy.Environment = unified.RunJob.Environment
		legacy.Volume = unified.RunJob.Volume
		legacy.Network = unified.RunJob.Network
		legacy.Delete = unified.RunJob.Delete
		legacy.Pull = unified.RunJob.Pull
		legacy.ContainerName = unified.RunJob.ContainerName
		legacy.Hostname = unified.RunJob.Hostname
		legacy.Entrypoint = unified.RunJob.Entrypoint
		legacy.Container = unified.RunJob.Container
		legacy.VolumesFrom = unified.RunJob.VolumesFrom
		legacy.MaxRuntime = unified.RunJob.MaxRuntime
		legacy.HistoryLimit = unified.RunJob.HistoryLimit
		legacy.MaxRetries = unified.RunJob.MaxRetries
		legacy.RetryDelayMs = unified.RunJob.RetryDelayMs
		legacy.RetryExponential = unified.RunJob.RetryExponential
		legacy.RetryMaxDelayMs = unified.RunJob.RetryMaxDelayMs
		legacy.Dependencies = unified.RunJob.Dependencies
		legacy.OnSuccess = unified.RunJob.OnSuccess
		legacy.OnFailure = unified.RunJob.OnFailure
		legacy.AllowParallel = unified.RunJob.AllowParallel
	}
	return legacy
}

// ConvertToRunServiceConfig converts UnifiedJobConfig back to legacy RunServiceConfig
func ConvertToRunServiceConfig(unified *UnifiedJobConfig) *RunServiceConfigLegacy {
	if unified.Type != JobTypeService || unified.RunServiceJob == nil {
		return nil
	}

	legacy := &RunServiceConfigLegacy{
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
	// Copy job fields individually to avoid copying mutex
	if unified.RunServiceJob != nil {
		legacy.Schedule = unified.RunServiceJob.Schedule
		legacy.Name = unified.RunServiceJob.Name
		legacy.Command = unified.RunServiceJob.Command
		legacy.Image = unified.RunServiceJob.Image
		legacy.User = unified.RunServiceJob.User
		legacy.TTY = unified.RunServiceJob.TTY
		legacy.Delete = unified.RunServiceJob.Delete
		legacy.Network = unified.RunServiceJob.Network
		legacy.MaxRuntime = unified.RunServiceJob.MaxRuntime
		legacy.HistoryLimit = unified.RunServiceJob.HistoryLimit
		legacy.MaxRetries = unified.RunServiceJob.MaxRetries
		legacy.RetryDelayMs = unified.RunServiceJob.RetryDelayMs
		legacy.RetryExponential = unified.RunServiceJob.RetryExponential
		legacy.RetryMaxDelayMs = unified.RunServiceJob.RetryMaxDelayMs
		legacy.Dependencies = unified.RunServiceJob.Dependencies
		legacy.OnSuccess = unified.RunServiceJob.OnSuccess
		legacy.OnFailure = unified.RunServiceJob.OnFailure
		legacy.AllowParallel = unified.RunServiceJob.AllowParallel
	}
	return legacy
}

// ConvertToLocalJobConfig converts UnifiedJobConfig back to legacy LocalJobConfig
func ConvertToLocalJobConfig(unified *UnifiedJobConfig) *LocalJobConfigLegacy {
	if unified.Type != JobTypeLocal || unified.LocalJob == nil {
		return nil
	}

	legacy := &LocalJobConfigLegacy{
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
	// Copy job fields individually to avoid copying mutex
	if unified.LocalJob != nil {
		legacy.Schedule = unified.LocalJob.Schedule
		legacy.Name = unified.LocalJob.Name
		legacy.Command = unified.LocalJob.Command
		legacy.Dir = unified.LocalJob.Dir
		legacy.Environment = unified.LocalJob.Environment
		legacy.HistoryLimit = unified.LocalJob.HistoryLimit
		legacy.MaxRetries = unified.LocalJob.MaxRetries
		legacy.RetryDelayMs = unified.LocalJob.RetryDelayMs
		legacy.RetryExponential = unified.LocalJob.RetryExponential
		legacy.RetryMaxDelayMs = unified.LocalJob.RetryMaxDelayMs
		legacy.Dependencies = unified.LocalJob.Dependencies
		legacy.OnSuccess = unified.LocalJob.OnSuccess
		legacy.OnFailure = unified.LocalJob.OnFailure
		legacy.AllowParallel = unified.LocalJob.AllowParallel
	}
	return legacy
}

// ConvertToComposeJobConfig converts UnifiedJobConfig back to legacy ComposeJobConfig
func ConvertToComposeJobConfig(unified *UnifiedJobConfig) *ComposeJobConfigLegacy {
	if unified.Type != JobTypeCompose || unified.ComposeJob == nil {
		return nil
	}

	legacy := &ComposeJobConfigLegacy{
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
	// Copy job fields individually to avoid copying mutex
	if unified.ComposeJob != nil {
		legacy.Schedule = unified.ComposeJob.Schedule
		legacy.Name = unified.ComposeJob.Name
		legacy.Command = unified.ComposeJob.Command
		legacy.File = unified.ComposeJob.File
		legacy.Service = unified.ComposeJob.Service
		legacy.Exec = unified.ComposeJob.Exec
		legacy.HistoryLimit = unified.ComposeJob.HistoryLimit
		legacy.MaxRetries = unified.ComposeJob.MaxRetries
		legacy.RetryDelayMs = unified.ComposeJob.RetryDelayMs
		legacy.RetryExponential = unified.ComposeJob.RetryExponential
		legacy.RetryMaxDelayMs = unified.ComposeJob.RetryMaxDelayMs
		legacy.Dependencies = unified.ComposeJob.Dependencies
		legacy.OnSuccess = unified.ComposeJob.OnSuccess
		legacy.OnFailure = unified.ComposeJob.OnFailure
		legacy.AllowParallel = unified.ComposeJob.AllowParallel
	}
	return legacy
}

// ConvertLegacyJobMaps converts all legacy job maps to a unified job map
// This enables the transition from 5 separate maps to a single unified approach
func ConvertLegacyJobMaps(
	execJobs map[string]*ExecJobConfigLegacy,
	runJobs map[string]*RunJobConfigLegacy,
	serviceJobs map[string]*RunServiceConfigLegacy,
	localJobs map[string]*LocalJobConfigLegacy,
	composeJobs map[string]*ComposeJobConfigLegacy,
) map[string]*UnifiedJobConfig {
	unified := make(map[string]*UnifiedJobConfig)

	// Convert exec jobs
	for name, job := range execJobs {
		unified[name] = ConvertFromExecJobConfig(job)
	}

	// Convert run jobs
	for name, job := range runJobs {
		unified[name] = ConvertFromRunJobConfig(job)
	}

	// Convert service jobs
	for name, job := range serviceJobs {
		unified[name] = ConvertFromRunServiceConfig(job)
	}

	// Convert local jobs
	for name, job := range localJobs {
		unified[name] = ConvertFromLocalJobConfig(job)
	}

	// Convert compose jobs
	for name, job := range composeJobs {
		unified[name] = ConvertFromComposeJobConfig(job)
	}

	return unified
}
