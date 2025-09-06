package cli

import (
	"context"
	"fmt"
	"time"

	docker "github.com/fsouza/go-dockerclient"

	"github.com/netresearch/ofelia/cli/config"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/middlewares"
)

// UnifiedConfig represents the new unified configuration approach
// This provides a bridge between the old Config struct and the new unified system
type UnifiedConfig struct {
	Global struct {
		middlewares.SlackConfig `mapstructure:",squash"`
		middlewares.SaveConfig  `mapstructure:",squash"`
		middlewares.MailConfig  `mapstructure:",squash"`
		LogLevel                string        `gcfg:"log-level" mapstructure:"log-level"`
		EnableWeb               bool          `gcfg:"enable-web" mapstructure:"enable-web" default:"false"`
		WebAddr                 string        `gcfg:"web-address" mapstructure:"web-address" default:":8081"`
		EnablePprof             bool          `gcfg:"enable-pprof" mapstructure:"enable-pprof" default:"false"`
		PprofAddr               string        `gcfg:"pprof-address" mapstructure:"pprof-address" default:"127.0.0.1:8080"`
		MaxRuntime              time.Duration `gcfg:"max-runtime" mapstructure:"max-runtime" default:"24h"`
		AllowHostJobsFromLabels bool          `gcfg:"allow-host-jobs-from-labels" mapstructure:"allow-host-jobs-from-labels" default:"false"`
	}
	Docker DockerConfig

	// Unified job management
	configManager *config.UnifiedConfigManager
	parser        *config.ConfigurationParser

	// Metadata
	configPath    string
	configFiles   []string
	configModTime time.Time

	// Dependencies
	sh            *core.Scheduler
	dockerHandler *DockerHandler
	logger        core.Logger
}

// NewUnifiedConfig creates a new unified configuration instance
func NewUnifiedConfig(logger core.Logger) *UnifiedConfig {
	uc := &UnifiedConfig{
		configManager: config.NewUnifiedConfigManager(logger),
		parser:        config.NewConfigurationParser(logger),
		logger:        logger,
	}
	return uc
}

// InitializeApp initializes the unified configuration system
func (uc *UnifiedConfig) InitializeApp() error {
	uc.sh = core.NewScheduler(uc.logger)
	uc.buildSchedulerMiddlewares(uc.sh)

	if err := uc.initDockerHandler(); err != nil {
		return err
	}

	// Set dependencies in the config manager
	uc.configManager.SetScheduler(uc.sh)
	uc.configManager.SetDockerHandler(uc.dockerHandler)

	// Load jobs from Docker labels
	if err := uc.mergeJobsFromDockerLabels(); err != nil {
		return fmt.Errorf("failed to load jobs from Docker labels: %w", err)
	}

	return nil
}

// initDockerHandler initializes the Docker handler
func (uc *UnifiedConfig) initDockerHandler() error {
	var err error
	uc.dockerHandler, err = newDockerHandler(context.Background(), uc, uc.logger, &uc.Docker, nil)
	return err
}

// mergeJobsFromDockerLabels loads and merges jobs from Docker container labels
func (uc *UnifiedConfig) mergeJobsFromDockerLabels() error {
	dockerLabels, err := uc.dockerHandler.GetDockerLabels()
	if err != nil {
		uc.logger.Errorf("Failed to get Docker labels: %v", err)
		return nil // Non-fatal error
	}

	// Parse Docker labels into unified job configurations
	labelJobs, err := uc.parser.ParseDockerLabels(dockerLabels, uc.Global.AllowHostJobsFromLabels)
	if err != nil {
		return fmt.Errorf("failed to parse Docker labels: %w", err)
	}

	// Sync the parsed jobs
	if err := uc.configManager.SyncJobs(labelJobs, config.JobSourceLabel); err != nil {
		return fmt.Errorf("failed to sync jobs from Docker labels: %w", err)
	}

	uc.logger.Debugf("Merged %d jobs from Docker labels", len(labelJobs))
	return nil
}

// buildSchedulerMiddlewares builds middlewares for the scheduler
func (uc *UnifiedConfig) buildSchedulerMiddlewares(sh *core.Scheduler) {
	builder := config.NewMiddlewareBuilder()
	builder.BuildSchedulerMiddlewares(sh, &uc.Global.SlackConfig, &uc.Global.SaveConfig, &uc.Global.MailConfig)
}

// GetJobCount returns the total number of managed jobs
func (uc *UnifiedConfig) GetJobCount() int {
	return uc.configManager.GetJobCount()
}

// GetJobCountByType returns the number of jobs by type
func (uc *UnifiedConfig) GetJobCountByType() map[config.JobType]int {
	return uc.configManager.GetJobCountByType()
}

// ListJobs returns all jobs
func (uc *UnifiedConfig) ListJobs() map[string]*config.UnifiedJobConfig {
	return uc.configManager.ListJobs()
}

// ListJobsByType returns jobs filtered by type
func (uc *UnifiedConfig) ListJobsByType(jobType config.JobType) map[string]*config.UnifiedJobConfig {
	return uc.configManager.ListJobsByType(jobType)
}

// GetJob returns a specific job by name
func (uc *UnifiedConfig) GetJob(name string) (*config.UnifiedJobConfig, bool) {
	return uc.configManager.GetJob(name)
}

// dockerLabelsUpdate implements the dockerLabelsUpdate interface
// This method is called when Docker labels are updated
func (uc *UnifiedConfig) dockerLabelsUpdate(labels map[string]map[string]string) {
	uc.logger.Debugf("dockerLabelsUpdate started")

	// Parse labels into unified job configurations
	parsedJobs, err := uc.parser.ParseDockerLabels(labels, !uc.Global.AllowHostJobsFromLabels)
	if err != nil {
		uc.logger.Errorf("Failed to parse Docker labels: %v", err)
		return
	}

	// Add parsed jobs to config manager and sync with scheduler
	for name, job := range parsedJobs {
		if err := uc.configManager.AddJob(name, job); err != nil {
			uc.logger.Errorf("Failed to add job %q from Docker labels: %v", name, err)
			continue
		}

		if job.GetJobSource() == config.JobSourceLabel {
			uc.sh.AddJob(job)
		}
	}

	uc.logger.Debugf("dockerLabelsUpdate completed")
}

// Conversion methods for backward compatibility with legacy Config

// ToLegacyConfig converts the unified configuration to the legacy Config struct
// This maintains backward compatibility for code that still expects the old structure
func (uc *UnifiedConfig) ToLegacyConfig() *Config {
	legacy := &Config{
		Global:        uc.Global,
		Docker:        uc.Docker,
		configPath:    uc.configPath,
		configFiles:   uc.configFiles,
		configModTime: uc.configModTime,
		sh:            uc.sh,
		dockerHandler: uc.dockerHandler,
		logger:        uc.logger,
		ExecJobs:      make(map[string]*ExecJobConfig),
		RunJobs:       make(map[string]*RunJobConfig),
		ServiceJobs:   make(map[string]*RunServiceConfig),
		LocalJobs:     make(map[string]*LocalJobConfig),
		ComposeJobs:   make(map[string]*ComposeJobConfig),
	}

	// Convert unified jobs back to legacy job maps
	allJobs := uc.configManager.ListJobs()
	for name, unifiedJob := range allJobs {
		switch unifiedJob.Type {
		case config.JobTypeExec:
			if legacyJob := config.ConvertToExecJobConfig(unifiedJob); legacyJob != nil {
				// Convert from config.ExecJobConfigLegacy to cli.ExecJobConfig
				cliJob := &ExecJobConfig{
					OverlapConfig: legacyJob.OverlapConfig,
					SlackConfig:   legacyJob.SlackConfig,
					SaveConfig:    legacyJob.SaveConfig,
					MailConfig:    legacyJob.MailConfig,
					JobSource:     JobSource(legacyJob.JobSource),
				}
				// Copy job fields individually to avoid copying mutex
				cliJob.Schedule = legacyJob.Schedule
				cliJob.Name = legacyJob.Name
				cliJob.Command = legacyJob.Command
				cliJob.Container = legacyJob.Container
				cliJob.User = legacyJob.User
				cliJob.TTY = legacyJob.TTY
				cliJob.Environment = legacyJob.Environment
				cliJob.HistoryLimit = legacyJob.HistoryLimit
				cliJob.MaxRetries = legacyJob.MaxRetries
				cliJob.RetryDelayMs = legacyJob.RetryDelayMs
				cliJob.RetryExponential = legacyJob.RetryExponential
				cliJob.RetryMaxDelayMs = legacyJob.RetryMaxDelayMs
				cliJob.Dependencies = legacyJob.Dependencies
				cliJob.OnSuccess = legacyJob.OnSuccess
				cliJob.OnFailure = legacyJob.OnFailure
				cliJob.AllowParallel = legacyJob.AllowParallel
				legacy.ExecJobs[name] = cliJob
			}
		case config.JobTypeRun:
			if legacyJob := config.ConvertToRunJobConfig(unifiedJob); legacyJob != nil {
				// Convert from config.RunJobConfigLegacy to cli.RunJobConfig
				legacy.RunJobs[name] = &RunJobConfig{
					RunJob:        legacyJob.RunJob,
					OverlapConfig: legacyJob.OverlapConfig,
					SlackConfig:   legacyJob.SlackConfig,
					SaveConfig:    legacyJob.SaveConfig,
					MailConfig:    legacyJob.MailConfig,
					JobSource:     JobSource(legacyJob.JobSource),
				}
			}
		case config.JobTypeService:
			if legacyJob := config.ConvertToRunServiceConfig(unifiedJob); legacyJob != nil {
				// Convert from config.RunServiceConfigLegacy to cli.RunServiceConfig
				legacy.ServiceJobs[name] = &RunServiceConfig{
					RunServiceJob: legacyJob.RunServiceJob,
					OverlapConfig: legacyJob.OverlapConfig,
					SlackConfig:   legacyJob.SlackConfig,
					SaveConfig:    legacyJob.SaveConfig,
					MailConfig:    legacyJob.MailConfig,
					JobSource:     JobSource(legacyJob.JobSource),
				}
			}
		case config.JobTypeLocal:
			if legacyJob := config.ConvertToLocalJobConfig(unifiedJob); legacyJob != nil {
				// Convert from config.LocalJobConfigLegacy to cli.LocalJobConfig
				legacy.LocalJobs[name] = &LocalJobConfig{
					LocalJob:      legacyJob.LocalJob,
					OverlapConfig: legacyJob.OverlapConfig,
					SlackConfig:   legacyJob.SlackConfig,
					SaveConfig:    legacyJob.SaveConfig,
					MailConfig:    legacyJob.MailConfig,
					JobSource:     JobSource(legacyJob.JobSource),
				}
			}
		case config.JobTypeCompose:
			if legacyJob := config.ConvertToComposeJobConfig(unifiedJob); legacyJob != nil {
				// Convert from config.ComposeJobConfigLegacy to cli.ComposeJobConfig
				legacy.ComposeJobs[name] = &ComposeJobConfig{
					ComposeJob:    legacyJob.ComposeJob,
					OverlapConfig: legacyJob.OverlapConfig,
					SlackConfig:   legacyJob.SlackConfig,
					SaveConfig:    legacyJob.SaveConfig,
					MailConfig:    legacyJob.MailConfig,
					JobSource:     JobSource(legacyJob.JobSource),
				}
			}
		}
	}

	return legacy
}

// FromLegacyConfig converts a legacy Config struct to the unified configuration
func (uc *UnifiedConfig) FromLegacyConfig(legacy *Config) {
	uc.Global = legacy.Global
	uc.Docker = legacy.Docker
	uc.configPath = legacy.configPath
	uc.configFiles = legacy.configFiles
	uc.configModTime = legacy.configModTime
	uc.sh = legacy.sh
	uc.dockerHandler = legacy.dockerHandler
	uc.logger = legacy.logger

	// Convert legacy job maps to unified jobs
	unifiedJobs := config.ConvertLegacyJobMaps(
		convertExecJobs(legacy.ExecJobs),
		convertRunJobs(legacy.RunJobs),
		convertServiceJobs(legacy.ServiceJobs),
		convertLocalJobs(legacy.LocalJobs),
		convertComposeJobs(legacy.ComposeJobs),
	)

	// Add all jobs to the manager
	for name, job := range unifiedJobs {
		if err := uc.configManager.AddJob(name, job); err != nil {
			uc.logger.Errorf("Failed to add job %q during legacy conversion: %v", name, err)
		}
	}
}

// Helper conversion functions

func convertExecJobs(legacy map[string]*ExecJobConfig) map[string]*config.ExecJobConfigLegacy {
	result := make(map[string]*config.ExecJobConfigLegacy)
	for name, job := range legacy {
		result[name] = &config.ExecJobConfigLegacy{
			ExecJob:       job.ExecJob,
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
	}
	return result
}

func convertRunJobs(legacy map[string]*RunJobConfig) map[string]*config.RunJobConfigLegacy {
	result := make(map[string]*config.RunJobConfigLegacy)
	for name, job := range legacy {
		result[name] = &config.RunJobConfigLegacy{
			RunJob:        job.RunJob,
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
	}
	return result
}

func convertServiceJobs(legacy map[string]*RunServiceConfig) map[string]*config.RunServiceConfigLegacy {
	result := make(map[string]*config.RunServiceConfigLegacy)
	for name, job := range legacy {
		result[name] = &config.RunServiceConfigLegacy{
			RunServiceJob: job.RunServiceJob,
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
	}
	return result
}

func convertLocalJobs(legacy map[string]*LocalJobConfig) map[string]*config.LocalJobConfigLegacy {
	result := make(map[string]*config.LocalJobConfigLegacy)
	for name, job := range legacy {
		result[name] = &config.LocalJobConfigLegacy{
			LocalJob:      job.LocalJob,
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
	}
	return result
}

func convertComposeJobs(legacy map[string]*ComposeJobConfig) map[string]*config.ComposeJobConfigLegacy {
	result := make(map[string]*config.ComposeJobConfigLegacy)
	for name, job := range legacy {
		result[name] = &config.ComposeJobConfigLegacy{
			ComposeJob:    job.ComposeJob,
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
	}
	return result
}

// DockerHandlerAdapter implements the DockerHandlerInterface for the UnifiedConfigManager
type DockerHandlerAdapter struct {
	handler *DockerHandler
}

func (da *DockerHandlerAdapter) GetInternalDockerClient() *docker.Client {
	return da.handler.GetInternalDockerClient()
}

func (da *DockerHandlerAdapter) GetDockerLabels() (map[string]map[string]string, error) {
	return da.handler.GetDockerLabels()
}
