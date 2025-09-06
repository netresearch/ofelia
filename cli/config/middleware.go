package config

import (
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
)

// MiddlewareBuilder provides centralized middleware building functionality
// This replaces the duplicate buildMiddlewares() methods across all job types
type MiddlewareBuilder struct{}

// NewMiddlewareBuilder creates a new middleware builder
func NewMiddlewareBuilder() *MiddlewareBuilder {
	return &MiddlewareBuilder{}
}

// BuildMiddlewares builds and applies middlewares to a job using the middleware configuration
// This method replaces 5 identical buildMiddlewares() methods (lines 540-545, 572-577, etc.)
func (b *MiddlewareBuilder) BuildMiddlewares(job core.Job, config *MiddlewareConfig) {
	if job == nil || config == nil {
		return
	}

	// Apply all middleware configurations in consistent order
	// This logic was previously duplicated 5 times across different job types
	job.Use(middlewares.NewOverlap(&config.OverlapConfig))
	job.Use(middlewares.NewSlack(&config.SlackConfig))
	job.Use(middlewares.NewSave(&config.SaveConfig))
	job.Use(middlewares.NewMail(&config.MailConfig))
}

// BuildSchedulerMiddlewares builds middlewares for the global scheduler
// This centralizes the scheduler middleware building logic
func (b *MiddlewareBuilder) BuildSchedulerMiddlewares(
	scheduler *core.Scheduler,
	slackConfig *middlewares.SlackConfig,
	saveConfig *middlewares.SaveConfig,
	mailConfig *middlewares.MailConfig,
) {
	if scheduler == nil {
		return
	}

	// Apply global middlewares in consistent order
	scheduler.Use(middlewares.NewSlack(slackConfig))
	scheduler.Use(middlewares.NewSave(saveConfig))
	scheduler.Use(middlewares.NewMail(mailConfig))
}

// ResetJobMiddlewares resets and rebuilds middlewares for a job
// This provides a centralized way to handle middleware updates
func (b *MiddlewareBuilder) ResetJobMiddlewares(
	job core.Job,
	middlewareConfig *MiddlewareConfig,
	schedulerMiddlewares []core.Middleware,
) {
	if job == nil {
		return
	}

	// Reset to clean state
	if resetter, ok := job.(interface{ ResetMiddlewares(...core.Middleware) }); ok {
		resetter.ResetMiddlewares()
	}

	// Rebuild job-specific middlewares
	b.BuildMiddlewares(job, middlewareConfig)

	// Apply scheduler middlewares
	if schedulerMiddlewares != nil {
		job.Use(schedulerMiddlewares...)
	}
}

// ValidateMiddlewareConfig validates middleware configuration settings
func (b *MiddlewareBuilder) ValidateMiddlewareConfig(config *MiddlewareConfig) error {
	// Add validation logic for middleware configurations
	// This can be extended to validate specific middleware settings
	return nil
}

// GetActiveMiddlewareNames returns the names of active middlewares based on configuration
// This helps with debugging and monitoring which middlewares are enabled
func (b *MiddlewareBuilder) GetActiveMiddlewareNames(config *MiddlewareConfig) []string {
	var active []string

	if config == nil {
		return active
	}

	// Check which middlewares would be active based on configuration
	if !middlewares.IsEmpty(&config.OverlapConfig) {
		active = append(active, "overlap")
	}
	if !middlewares.IsEmpty(&config.SlackConfig) {
		active = append(active, "slack")
	}
	if !middlewares.IsEmpty(&config.SaveConfig) {
		active = append(active, "save")
	}
	if !middlewares.IsEmpty(&config.MailConfig) {
		active = append(active, "mail")
	}

	return active
}
