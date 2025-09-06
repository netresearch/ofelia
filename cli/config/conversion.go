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
	
	return &ExecJobConfigLegacy{
		ExecJob:       *unified.ExecJob,
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
}

// ConvertToRunJobConfig converts UnifiedJobConfig back to legacy RunJobConfig
func ConvertToRunJobConfig(unified *UnifiedJobConfig) *RunJobConfigLegacy {
	if unified.Type != JobTypeRun || unified.RunJob == nil {
		return nil
	}
	
	return &RunJobConfigLegacy{
		RunJob:        *unified.RunJob,
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
}

// ConvertToRunServiceConfig converts UnifiedJobConfig back to legacy RunServiceConfig
func ConvertToRunServiceConfig(unified *UnifiedJobConfig) *RunServiceConfigLegacy {
	if unified.Type != JobTypeService || unified.RunServiceJob == nil {
		return nil
	}
	
	return &RunServiceConfigLegacy{
		RunServiceJob: *unified.RunServiceJob,
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
}

// ConvertToLocalJobConfig converts UnifiedJobConfig back to legacy LocalJobConfig
func ConvertToLocalJobConfig(unified *UnifiedJobConfig) *LocalJobConfigLegacy {
	if unified.Type != JobTypeLocal || unified.LocalJob == nil {
		return nil
	}
	
	return &LocalJobConfigLegacy{
		LocalJob:      *unified.LocalJob,
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
}

// ConvertToComposeJobConfig converts UnifiedJobConfig back to legacy ComposeJobConfig
func ConvertToComposeJobConfig(unified *UnifiedJobConfig) *ComposeJobConfigLegacy {
	if unified.Type != JobTypeCompose || unified.ComposeJob == nil {
		return nil
	}
	
	return &ComposeJobConfigLegacy{
		ComposeJob:    *unified.ComposeJob,
		OverlapConfig: unified.MiddlewareConfig.OverlapConfig,
		SlackConfig:   unified.MiddlewareConfig.SlackConfig,
		SaveConfig:    unified.MiddlewareConfig.SaveConfig,
		MailConfig:    unified.MiddlewareConfig.MailConfig,
		JobSource:     unified.JobSource,
	}
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