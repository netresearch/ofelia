package config

import (
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
)

// JobType represents the different types of jobs that can be scheduled
type JobType string

const (
	JobTypeExec    JobType = "exec"
	JobTypeRun     JobType = "run"
	JobTypeService JobType = "service-run"
	JobTypeLocal   JobType = "local"
	JobTypeCompose JobType = "compose"
)

// JobSource indicates where a job configuration originated from
type JobSource string

const (
	JobSourceINI   JobSource = "ini"
	JobSourceLabel JobSource = "label"
)

// MiddlewareConfig contains all common middleware configurations
// This replaces the duplication across all 5 job config types
type MiddlewareConfig struct {
	middlewares.OverlapConfig `mapstructure:",squash"`
	middlewares.SlackConfig   `mapstructure:",squash"`
	middlewares.SaveConfig    `mapstructure:",squash"`
	middlewares.MailConfig    `mapstructure:",squash"`
}

// UnifiedJobConfig represents a unified configuration for all job types
// This eliminates the need for separate ExecJobConfig, RunJobConfig, etc.
type UnifiedJobConfig struct {
	// Common configuration
	Type      JobType   `json:"type" mapstructure:"type"`
	JobSource JobSource `json:"-" mapstructure:"-"`

	// Common middleware configuration (previously duplicated 5 times)
	MiddlewareConfig `mapstructure:",squash"`

	// Core job configurations (embedded via union)
	ExecJob       *core.ExecJob       `json:"exec_job,omitempty" mapstructure:",squash"`
	RunJob        *core.RunJob        `json:"run_job,omitempty" mapstructure:",squash"`
	RunServiceJob *core.RunServiceJob `json:"service_job,omitempty" mapstructure:",squash"`
	LocalJob      *core.LocalJob      `json:"local_job,omitempty" mapstructure:",squash"`
	ComposeJob    *core.ComposeJob    `json:"compose_job,omitempty" mapstructure:",squash"`
}

// GetCoreJob returns the appropriate core job based on the job type
func (u *UnifiedJobConfig) GetCoreJob() core.Job {
	switch u.Type {
	case JobTypeExec:
		return u.ExecJob
	case JobTypeRun:
		return u.RunJob
	case JobTypeService:
		return u.RunServiceJob
	case JobTypeLocal:
		return u.LocalJob
	case JobTypeCompose:
		return u.ComposeJob
	default:
		return nil
	}
}

// GetName returns the job name from the appropriate core job
func (u *UnifiedJobConfig) GetName() string {
	if job := u.GetCoreJob(); job != nil {
		return job.GetName()
	}
	return ""
}

// GetSchedule returns the schedule from the appropriate core job
func (u *UnifiedJobConfig) GetSchedule() string {
	if job := u.GetCoreJob(); job != nil {
		return job.GetSchedule()
	}
	return ""
}

// GetCommand returns the command from the appropriate core job
func (u *UnifiedJobConfig) GetCommand() string {
	if job := u.GetCoreJob(); job != nil {
		return job.GetCommand()
	}
	return ""
}

// GetJobSource implements the jobConfig interface
func (u *UnifiedJobConfig) GetJobSource() JobSource {
	return u.JobSource
}

// SetJobSource implements the jobConfig interface
func (u *UnifiedJobConfig) SetJobSource(source JobSource) {
	u.JobSource = source
}

// Hash returns a hash of the job configuration for change detection
func (u *UnifiedJobConfig) Hash() (string, error) {
	if job := u.GetCoreJob(); job != nil {
		return job.Hash()
	}
	return "", nil
}

// Run implements the core.Job interface by delegating to the appropriate job type
func (u *UnifiedJobConfig) Run(ctx *core.Context) error {
	job := u.GetCoreJob()
	if job == nil {
		return core.ErrUnexpected
	}
	return job.Run(ctx)
}

// Use implements the core.Job interface for middleware support
func (u *UnifiedJobConfig) Use(middlewares ...core.Middleware) {
	if job := u.GetCoreJob(); job != nil {
		job.Use(middlewares...)
	}
}

// Middlewares implements the core.Job interface
func (u *UnifiedJobConfig) Middlewares() []core.Middleware {
	if job := u.GetCoreJob(); job != nil {
		return job.Middlewares()
	}
	return nil
}

// ResetMiddlewares implements the jobConfig interface
func (u *UnifiedJobConfig) ResetMiddlewares(middlewares ...core.Middleware) {
	if job := u.GetCoreJob(); job != nil {
		job.Use(middlewares...)
	}
}

// GetCronJobID implements the core.Job interface
func (u *UnifiedJobConfig) GetCronJobID() int {
	if job := u.GetCoreJob(); job != nil {
		return job.GetCronJobID()
	}
	return 0
}

// SetCronJobID implements the core.Job interface
func (u *UnifiedJobConfig) SetCronJobID(id int) {
	if job := u.GetCoreJob(); job != nil {
		job.SetCronJobID(id)
	}
}

// GetHistory implements the core.Job interface
func (u *UnifiedJobConfig) GetHistory() []*core.Execution {
	if job := u.GetCoreJob(); job != nil {
		return job.GetHistory()
	}
	return nil
}

// Running implements the core.Job interface
func (u *UnifiedJobConfig) Running() int32 {
	if job := u.GetCoreJob(); job != nil {
		return job.Running()
	}
	return 0
}

// NotifyStart implements the core.Job interface
func (u *UnifiedJobConfig) NotifyStart() {
	if job := u.GetCoreJob(); job != nil {
		job.NotifyStart()
	}
}

// NotifyStop implements the core.Job interface
func (u *UnifiedJobConfig) NotifyStop() {
	if job := u.GetCoreJob(); job != nil {
		job.NotifyStop()
	}
}

// buildMiddlewares builds and applies middlewares to the job
// This replaces 5 duplicate buildMiddlewares() methods
func (u *UnifiedJobConfig) buildMiddlewares() {
	coreJob := u.GetCoreJob()
	if coreJob == nil {
		return
	}

	// Apply all middleware configurations (previously duplicated 5 times)
	coreJob.Use(middlewares.NewOverlap(&u.MiddlewareConfig.OverlapConfig))
	coreJob.Use(middlewares.NewSlack(&u.MiddlewareConfig.SlackConfig))
	coreJob.Use(middlewares.NewSave(&u.MiddlewareConfig.SaveConfig))
	coreJob.Use(middlewares.NewMail(&u.MiddlewareConfig.MailConfig))
}

// NewUnifiedJobConfig creates a new unified job configuration of the specified type
func NewUnifiedJobConfig(jobType JobType) *UnifiedJobConfig {
	config := &UnifiedJobConfig{
		Type: jobType,
	}

	// Initialize the appropriate core job based on type
	switch jobType {
	case JobTypeExec:
		config.ExecJob = &core.ExecJob{}
	case JobTypeRun:
		config.RunJob = &core.RunJob{}
	case JobTypeService:
		config.RunServiceJob = &core.RunServiceJob{}
	case JobTypeLocal:
		config.LocalJob = &core.LocalJob{}
	case JobTypeCompose:
		config.ComposeJob = &core.ComposeJob{}
	}

	return config
}

