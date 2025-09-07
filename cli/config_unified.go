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
		AllowHostJobsFromLabels bool          `gcfg:"allow-host-jobs-from-labels" mapstructure:"allow-host-jobs-from-labels" default:"false"` //nolint:revive
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
			if err := uc.sh.AddJob(job); err != nil {
				uc.logger.Errorf("Failed to add job %q to scheduler: %v", name, err)
			}
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
				cliJob := &RunJobConfig{
					OverlapConfig: legacyJob.OverlapConfig,
					SlackConfig:   legacyJob.SlackConfig,
					SaveConfig:    legacyJob.SaveConfig,
					MailConfig:    legacyJob.MailConfig,
					JobSource:     JobSource(legacyJob.JobSource),
				}
				// Copy RunJob fields individually to avoid copying mutex from BareJob
				cliJob.RunJob.Schedule = legacyJob.Schedule
				cliJob.RunJob.Name = legacyJob.Name
				cliJob.RunJob.Command = legacyJob.Command
				cliJob.RunJob.HistoryLimit = legacyJob.HistoryLimit
				cliJob.RunJob.MaxRetries = legacyJob.MaxRetries
				cliJob.RunJob.RetryDelayMs = legacyJob.RetryDelayMs
				cliJob.RunJob.RetryExponential = legacyJob.RetryExponential
				cliJob.RunJob.RetryMaxDelayMs = legacyJob.RetryMaxDelayMs
				cliJob.RunJob.Dependencies = legacyJob.Dependencies
				cliJob.RunJob.OnSuccess = legacyJob.OnSuccess
				cliJob.RunJob.OnFailure = legacyJob.OnFailure
				cliJob.RunJob.AllowParallel = legacyJob.AllowParallel
				// RunJob-specific fields
				cliJob.RunJob.User = legacyJob.User
				cliJob.RunJob.ContainerName = legacyJob.ContainerName
				cliJob.RunJob.TTY = legacyJob.TTY
				cliJob.RunJob.Delete = legacyJob.Delete
				cliJob.RunJob.Pull = legacyJob.Pull
				cliJob.RunJob.Image = legacyJob.Image
				cliJob.RunJob.Network = legacyJob.Network
				cliJob.RunJob.Hostname = legacyJob.Hostname
				cliJob.RunJob.Entrypoint = legacyJob.Entrypoint
				cliJob.RunJob.Container = legacyJob.Container
				cliJob.RunJob.Volume = legacyJob.Volume
				cliJob.RunJob.VolumesFrom = legacyJob.VolumesFrom
				cliJob.RunJob.Environment = legacyJob.Environment
				cliJob.RunJob.MaxRuntime = legacyJob.MaxRuntime
				legacy.RunJobs[name] = cliJob
			}
		case config.JobTypeService:
			if legacyJob := config.ConvertToRunServiceConfig(unifiedJob); legacyJob != nil {
				// Convert from config.RunServiceConfigLegacy to cli.RunServiceConfig
				cliJob := &RunServiceConfig{
					OverlapConfig: legacyJob.OverlapConfig,
					SlackConfig:   legacyJob.SlackConfig,
					SaveConfig:    legacyJob.SaveConfig,
					MailConfig:    legacyJob.MailConfig,
					JobSource:     JobSource(legacyJob.JobSource),
				}
				// Copy RunServiceJob fields individually to avoid copying mutex from BareJob
				cliJob.RunServiceJob.Schedule = legacyJob.Schedule
				cliJob.RunServiceJob.Name = legacyJob.Name
				cliJob.RunServiceJob.Command = legacyJob.Command
				cliJob.RunServiceJob.HistoryLimit = legacyJob.HistoryLimit
				cliJob.RunServiceJob.MaxRetries = legacyJob.MaxRetries
				cliJob.RunServiceJob.RetryDelayMs = legacyJob.RetryDelayMs
				cliJob.RunServiceJob.RetryExponential = legacyJob.RetryExponential
				cliJob.RunServiceJob.RetryMaxDelayMs = legacyJob.RetryMaxDelayMs
				cliJob.RunServiceJob.Dependencies = legacyJob.Dependencies
				cliJob.RunServiceJob.OnSuccess = legacyJob.OnSuccess
				cliJob.RunServiceJob.OnFailure = legacyJob.OnFailure
				cliJob.RunServiceJob.AllowParallel = legacyJob.AllowParallel
				// RunServiceJob-specific fields
				cliJob.RunServiceJob.User = legacyJob.User
				cliJob.RunServiceJob.TTY = legacyJob.TTY
				cliJob.RunServiceJob.Delete = legacyJob.Delete
				cliJob.RunServiceJob.Image = legacyJob.Image
				cliJob.RunServiceJob.Network = legacyJob.Network
				cliJob.RunServiceJob.MaxRuntime = legacyJob.MaxRuntime
				legacy.ServiceJobs[name] = cliJob
			}
		case config.JobTypeLocal:
			if legacyJob := config.ConvertToLocalJobConfig(unifiedJob); legacyJob != nil {
				// Convert from config.LocalJobConfigLegacy to cli.LocalJobConfig
				cliJob := &LocalJobConfig{
					OverlapConfig: legacyJob.OverlapConfig,
					SlackConfig:   legacyJob.SlackConfig,
					SaveConfig:    legacyJob.SaveConfig,
					MailConfig:    legacyJob.MailConfig,
					JobSource:     JobSource(legacyJob.JobSource),
				}
				// Copy LocalJob fields individually to avoid copying mutex from BareJob
				cliJob.LocalJob.Schedule = legacyJob.Schedule
				cliJob.LocalJob.Name = legacyJob.Name
				cliJob.LocalJob.Command = legacyJob.Command
				cliJob.LocalJob.HistoryLimit = legacyJob.HistoryLimit
				cliJob.LocalJob.MaxRetries = legacyJob.MaxRetries
				cliJob.LocalJob.RetryDelayMs = legacyJob.RetryDelayMs
				cliJob.LocalJob.RetryExponential = legacyJob.RetryExponential
				cliJob.LocalJob.RetryMaxDelayMs = legacyJob.RetryMaxDelayMs
				cliJob.LocalJob.Dependencies = legacyJob.Dependencies
				cliJob.LocalJob.OnSuccess = legacyJob.OnSuccess
				cliJob.LocalJob.OnFailure = legacyJob.OnFailure
				cliJob.LocalJob.AllowParallel = legacyJob.AllowParallel
				// LocalJob-specific fields
				cliJob.LocalJob.Dir = legacyJob.Dir
				cliJob.LocalJob.Environment = legacyJob.Environment
				legacy.LocalJobs[name] = cliJob
			}
		case config.JobTypeCompose:
			if legacyJob := config.ConvertToComposeJobConfig(unifiedJob); legacyJob != nil {
				// Convert from config.ComposeJobConfigLegacy to cli.ComposeJobConfig
				cliJob := &ComposeJobConfig{
					OverlapConfig: legacyJob.OverlapConfig,
					SlackConfig:   legacyJob.SlackConfig,
					SaveConfig:    legacyJob.SaveConfig,
					MailConfig:    legacyJob.MailConfig,
					JobSource:     JobSource(legacyJob.JobSource),
				}
				// Copy ComposeJob fields individually to avoid copying mutex from BareJob
				cliJob.ComposeJob.Schedule = legacyJob.Schedule
				cliJob.ComposeJob.Name = legacyJob.Name
				cliJob.ComposeJob.Command = legacyJob.Command
				cliJob.ComposeJob.HistoryLimit = legacyJob.HistoryLimit
				cliJob.ComposeJob.MaxRetries = legacyJob.MaxRetries
				cliJob.ComposeJob.RetryDelayMs = legacyJob.RetryDelayMs
				cliJob.ComposeJob.RetryExponential = legacyJob.RetryExponential
				cliJob.ComposeJob.RetryMaxDelayMs = legacyJob.RetryMaxDelayMs
				cliJob.ComposeJob.Dependencies = legacyJob.Dependencies
				cliJob.ComposeJob.OnSuccess = legacyJob.OnSuccess
				cliJob.ComposeJob.OnFailure = legacyJob.OnFailure
				cliJob.ComposeJob.AllowParallel = legacyJob.AllowParallel
				// ComposeJob-specific fields
				cliJob.ComposeJob.File = legacyJob.File
				cliJob.ComposeJob.Service = legacyJob.Service
				cliJob.ComposeJob.Exec = legacyJob.Exec
				legacy.ComposeJobs[name] = cliJob
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
		legacyJob := &config.ExecJobConfigLegacy{
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
		// Copy ExecJob fields individually to avoid copying mutex from BareJob
		legacyJob.Schedule = job.ExecJob.Schedule
		legacyJob.Name = job.ExecJob.Name
		legacyJob.Command = job.ExecJob.Command
		legacyJob.HistoryLimit = job.ExecJob.HistoryLimit
		legacyJob.MaxRetries = job.ExecJob.MaxRetries
		legacyJob.RetryDelayMs = job.ExecJob.RetryDelayMs
		legacyJob.RetryExponential = job.ExecJob.RetryExponential
		legacyJob.RetryMaxDelayMs = job.ExecJob.RetryMaxDelayMs
		legacyJob.Dependencies = job.ExecJob.Dependencies
		legacyJob.OnSuccess = job.ExecJob.OnSuccess
		legacyJob.OnFailure = job.ExecJob.OnFailure
		legacyJob.AllowParallel = job.ExecJob.AllowParallel
		// ExecJob-specific fields
		legacyJob.Container = job.ExecJob.Container
		legacyJob.User = job.ExecJob.User
		legacyJob.TTY = job.ExecJob.TTY
		legacyJob.Environment = job.ExecJob.Environment
		result[name] = legacyJob
	}
	return result
}

func convertRunJobs(legacy map[string]*RunJobConfig) map[string]*config.RunJobConfigLegacy {
	result := make(map[string]*config.RunJobConfigLegacy)
	for name, job := range legacy {
		legacyJob := &config.RunJobConfigLegacy{
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
		// Copy RunJob fields individually to avoid copying mutex from BareJob
		legacyJob.Schedule = job.RunJob.Schedule
		legacyJob.Name = job.RunJob.Name
		legacyJob.Command = job.RunJob.Command
		legacyJob.HistoryLimit = job.RunJob.HistoryLimit
		legacyJob.MaxRetries = job.RunJob.MaxRetries
		legacyJob.RetryDelayMs = job.RunJob.RetryDelayMs
		legacyJob.RetryExponential = job.RunJob.RetryExponential
		legacyJob.RetryMaxDelayMs = job.RunJob.RetryMaxDelayMs
		legacyJob.Dependencies = job.RunJob.Dependencies
		legacyJob.OnSuccess = job.RunJob.OnSuccess
		legacyJob.OnFailure = job.RunJob.OnFailure
		legacyJob.AllowParallel = job.RunJob.AllowParallel
		// RunJob-specific fields
		legacyJob.User = job.RunJob.User
		legacyJob.ContainerName = job.RunJob.ContainerName
		legacyJob.TTY = job.RunJob.TTY
		legacyJob.Delete = job.RunJob.Delete
		legacyJob.Pull = job.RunJob.Pull
		legacyJob.Image = job.RunJob.Image
		legacyJob.Network = job.RunJob.Network
		legacyJob.Hostname = job.RunJob.Hostname
		legacyJob.Entrypoint = job.RunJob.Entrypoint
		legacyJob.Container = job.RunJob.Container
		legacyJob.Volume = job.RunJob.Volume
		legacyJob.VolumesFrom = job.RunJob.VolumesFrom
		legacyJob.Environment = job.RunJob.Environment
		legacyJob.MaxRuntime = job.RunJob.MaxRuntime
		result[name] = legacyJob
	}
	return result
}

func convertServiceJobs(legacy map[string]*RunServiceConfig) map[string]*config.RunServiceConfigLegacy {
	result := make(map[string]*config.RunServiceConfigLegacy)
	for name, job := range legacy {
		legacyJob := &config.RunServiceConfigLegacy{
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
		// Copy RunServiceJob fields individually to avoid copying mutex from BareJob
		legacyJob.Schedule = job.RunServiceJob.Schedule
		legacyJob.Name = job.RunServiceJob.Name
		legacyJob.Command = job.RunServiceJob.Command
		legacyJob.HistoryLimit = job.RunServiceJob.HistoryLimit
		legacyJob.MaxRetries = job.RunServiceJob.MaxRetries
		legacyJob.RetryDelayMs = job.RunServiceJob.RetryDelayMs
		legacyJob.RetryExponential = job.RunServiceJob.RetryExponential
		legacyJob.RetryMaxDelayMs = job.RunServiceJob.RetryMaxDelayMs
		legacyJob.Dependencies = job.RunServiceJob.Dependencies
		legacyJob.OnSuccess = job.RunServiceJob.OnSuccess
		legacyJob.OnFailure = job.RunServiceJob.OnFailure
		legacyJob.AllowParallel = job.RunServiceJob.AllowParallel
		// RunServiceJob-specific fields
		legacyJob.User = job.RunServiceJob.User
		legacyJob.TTY = job.RunServiceJob.TTY
		legacyJob.Delete = job.RunServiceJob.Delete
		legacyJob.Image = job.RunServiceJob.Image
		legacyJob.Network = job.RunServiceJob.Network
		legacyJob.MaxRuntime = job.RunServiceJob.MaxRuntime
		result[name] = legacyJob
	}
	return result
}

func convertLocalJobs(legacy map[string]*LocalJobConfig) map[string]*config.LocalJobConfigLegacy {
	result := make(map[string]*config.LocalJobConfigLegacy)
	for name, job := range legacy {
		legacyJob := &config.LocalJobConfigLegacy{
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
		// Copy LocalJob fields individually to avoid copying mutex from BareJob
		legacyJob.Schedule = job.LocalJob.Schedule
		legacyJob.Name = job.LocalJob.Name
		legacyJob.Command = job.LocalJob.Command
		legacyJob.HistoryLimit = job.LocalJob.HistoryLimit
		legacyJob.MaxRetries = job.LocalJob.MaxRetries
		legacyJob.RetryDelayMs = job.LocalJob.RetryDelayMs
		legacyJob.RetryExponential = job.LocalJob.RetryExponential
		legacyJob.RetryMaxDelayMs = job.LocalJob.RetryMaxDelayMs
		legacyJob.Dependencies = job.LocalJob.Dependencies
		legacyJob.OnSuccess = job.LocalJob.OnSuccess
		legacyJob.OnFailure = job.LocalJob.OnFailure
		legacyJob.AllowParallel = job.LocalJob.AllowParallel
		// LocalJob-specific fields
		legacyJob.Dir = job.LocalJob.Dir
		legacyJob.Environment = job.LocalJob.Environment
		result[name] = legacyJob
	}
	return result
}

func convertComposeJobs(legacy map[string]*ComposeJobConfig) map[string]*config.ComposeJobConfigLegacy {
	result := make(map[string]*config.ComposeJobConfigLegacy)
	for name, job := range legacy {
		legacyJob := &config.ComposeJobConfigLegacy{
			OverlapConfig: job.OverlapConfig,
			SlackConfig:   job.SlackConfig,
			SaveConfig:    job.SaveConfig,
			MailConfig:    job.MailConfig,
			JobSource:     config.JobSource(job.JobSource),
		}
		// Copy ComposeJob fields individually to avoid copying mutex from BareJob
		legacyJob.Schedule = job.ComposeJob.Schedule
		legacyJob.Name = job.ComposeJob.Name
		legacyJob.Command = job.ComposeJob.Command
		legacyJob.HistoryLimit = job.ComposeJob.HistoryLimit
		legacyJob.MaxRetries = job.ComposeJob.MaxRetries
		legacyJob.RetryDelayMs = job.ComposeJob.RetryDelayMs
		legacyJob.RetryExponential = job.ComposeJob.RetryExponential
		legacyJob.RetryMaxDelayMs = job.ComposeJob.RetryMaxDelayMs
		legacyJob.Dependencies = job.ComposeJob.Dependencies
		legacyJob.OnSuccess = job.ComposeJob.OnSuccess
		legacyJob.OnFailure = job.ComposeJob.OnFailure
		legacyJob.AllowParallel = job.ComposeJob.AllowParallel
		// ComposeJob-specific fields
		legacyJob.File = job.ComposeJob.File
		legacyJob.Service = job.ComposeJob.Service
		legacyJob.Exec = job.ComposeJob.Exec
		result[name] = legacyJob
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
