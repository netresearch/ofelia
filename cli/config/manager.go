package config

import (
	"fmt"
	"reflect"
	"sync"

	defaults "github.com/creasty/defaults"
	docker "github.com/fsouza/go-dockerclient"

	"github.com/netresearch/ofelia/core"
)

// UnifiedConfigManager manages the unified job configuration system
// This replaces the complex job management logic scattered throughout config.go
type UnifiedConfigManager struct {
	// Unified job storage (replaces 5 separate maps)
	jobs map[string]*UnifiedJobConfig

	// Core dependencies
	scheduler         *core.Scheduler
	dockerHandler     DockerHandlerInterface // Interface for testability
	middlewareBuilder *MiddlewareBuilder
	logger            core.Logger

	// Thread safety
	mutex sync.RWMutex
}

// DockerHandlerInterface defines the interface for Docker operations
// This makes the manager testable by allowing dependency injection
type DockerHandlerInterface interface {
	GetInternalDockerClient() *docker.Client
	GetDockerLabels() (map[string]map[string]string, error)
}

// NewUnifiedConfigManager creates a new unified configuration manager
func NewUnifiedConfigManager(logger core.Logger) *UnifiedConfigManager {
	return &UnifiedConfigManager{
		jobs:              make(map[string]*UnifiedJobConfig),
		middlewareBuilder: NewMiddlewareBuilder(),
		logger:            logger,
	}
}

// SetScheduler sets the scheduler for job management
func (m *UnifiedConfigManager) SetScheduler(scheduler *core.Scheduler) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.scheduler = scheduler
}

// SetDockerHandler sets the Docker handler for container operations
func (m *UnifiedConfigManager) SetDockerHandler(handler DockerHandlerInterface) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.dockerHandler = handler
}

// GetJob returns a job by name (thread-safe)
func (m *UnifiedConfigManager) GetJob(name string) (*UnifiedJobConfig, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	job, exists := m.jobs[name]
	return job, exists
}

// ListJobs returns all jobs (thread-safe copy)
func (m *UnifiedConfigManager) ListJobs() map[string]*UnifiedJobConfig {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]*UnifiedJobConfig, len(m.jobs))
	for name, job := range m.jobs {
		result[name] = job
	}
	return result
}

// ListJobsByType returns jobs filtered by type
func (m *UnifiedConfigManager) ListJobsByType(jobType JobType) map[string]*UnifiedJobConfig {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]*UnifiedJobConfig)
	for name, job := range m.jobs {
		if job.Type == jobType {
			result[name] = job
		}
	}
	return result
}

// AddJob adds or updates a job in the manager
func (m *UnifiedConfigManager) AddJob(name string, job *UnifiedJobConfig) error {
	if job == nil {
		return fmt.Errorf("cannot add nil job")
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Set defaults and prepare the job
	if err := m.prepareJob(name, job); err != nil {
		return fmt.Errorf("failed to prepare job %q: %w", name, err)
	}

	// Build middlewares
	job.buildMiddlewares()

	// Add to scheduler if available
	if m.scheduler != nil {
		if err := m.scheduler.AddJob(job); err != nil {
			return fmt.Errorf("failed to add job %q to scheduler: %w", name, err)
		}
	}

	// Store in manager
	m.jobs[name] = job

	m.logger.Debugf("Added %s job: %s", job.Type, name)
	return nil
}

// RemoveJob removes a job from the manager
func (m *UnifiedConfigManager) RemoveJob(name string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	job, exists := m.jobs[name]
	if !exists {
		return fmt.Errorf("job %q not found", name)
	}

	// Remove from scheduler if available
	if m.scheduler != nil {
		if err := m.scheduler.RemoveJob(job); err != nil {
			m.logger.Errorf("Failed to remove job %q from scheduler: %v", name, err)
		}
	}

	// Remove from manager
	delete(m.jobs, name)

	m.logger.Debugf("Removed %s job: %s", job.Type, name)
	return nil
}

// SyncJobs synchronizes jobs from external sources (INI files, Docker labels)
// This replaces the complex syncJobMap logic
func (m *UnifiedConfigManager) SyncJobs(
	parsed map[string]*UnifiedJobConfig,
	source JobSource,
) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Remove jobs that no longer exist in the source
	for name, job := range m.jobs {
		if source != "" && job.JobSource != source && job.JobSource != "" {
			continue // Skip jobs from different sources
		}

		if _, exists := parsed[name]; !exists {
			m.removeJobUnsafe(name, job)
		}
	}

	// Add or update jobs from the parsed configuration
	for name, job := range parsed {
		if err := m.syncSingleJob(name, job, source); err != nil {
			m.logger.Errorf("Failed to sync job %q: %v", name, err)
			continue
		}
	}

	return nil
}

// syncSingleJob handles syncing a single job with source prioritization
func (m *UnifiedConfigManager) syncSingleJob(name string, newJob *UnifiedJobConfig, source JobSource) error {
	existing, exists := m.jobs[name]

	if exists {
		// Handle source priority (INI overrides labels)
		switch {
		case existing.JobSource == source:
			// Same source - check for changes
			if m.hasJobChanged(existing, newJob) {
				return m.updateJobUnsafe(name, existing, newJob, source)
			}
			return nil
		case source == JobSourceINI && existing.JobSource == JobSourceLabel:
			m.logger.Warningf("Overriding label-defined %s job %q with INI job", newJob.Type, name)
			return m.replaceJobUnsafe(name, existing, newJob, source)
		case source == JobSourceLabel && existing.JobSource == JobSourceINI:
			m.logger.Warningf("Ignoring label-defined %s job %q because an INI job with the same name exists", newJob.Type, name)
			return nil
		default:
			return nil // Skip - unknown priority case
		}
	}

	// New job - add it
	return m.addJobUnsafe(name, newJob, source)
}

// hasJobChanged checks if a job configuration has changed
func (m *UnifiedConfigManager) hasJobChanged(oldJob, newJob *UnifiedJobConfig) bool {
	oldHash, err1 := oldJob.Hash()
	newHash, err2 := newJob.Hash()

	if err1 != nil || err2 != nil {
		m.logger.Errorf("Failed to calculate job hash for change detection")
		return true // Assume changed if we can't calculate hash
	}

	return oldHash != newHash
}

// prepareJob sets up a job with defaults and required fields
func (m *UnifiedConfigManager) prepareJob(name string, job *UnifiedJobConfig) error {
	// Apply defaults to the unified job
	if err := defaults.Set(job); err != nil {
		return fmt.Errorf("failed to set defaults: %w", err)
	}

	// Set the job name on the core job
	coreJob := job.GetCoreJob()
	if coreJob == nil {
		return fmt.Errorf("core job is nil for type %s", job.Type)
	}

	// Set name using reflection (since core jobs don't have a common SetName interface)
	if err := m.setJobName(coreJob, name); err != nil {
		return fmt.Errorf("failed to set job name: %w", err)
	}

	// Type-specific preparation
	return m.prepareJobByType(job)
}

// setJobName sets the name field on a core job using reflection
func (m *UnifiedConfigManager) setJobName(job core.Job, name string) error {
	jobValue := reflect.ValueOf(job).Elem()
	nameField := jobValue.FieldByName("Name")

	if !nameField.IsValid() || !nameField.CanSet() {
		return fmt.Errorf("cannot set Name field on job")
	}

	nameField.SetString(name)
	return nil
}

// prepareJobByType handles type-specific job preparation
func (m *UnifiedConfigManager) prepareJobByType(job *UnifiedJobConfig) error {
	switch job.Type {
	case JobTypeExec:
		if job.ExecJob != nil && m.dockerHandler != nil {
			job.ExecJob.Client = m.dockerHandler.GetInternalDockerClient()
		}
	case JobTypeRun:
		if job.RunJob != nil && m.dockerHandler != nil {
			job.RunJob.Client = m.dockerHandler.GetInternalDockerClient()
			job.RunJob.InitializeRuntimeFields()
		}
	case JobTypeService:
		if job.RunServiceJob != nil && m.dockerHandler != nil {
			job.RunServiceJob.Client = m.dockerHandler.GetInternalDockerClient()
		}
	case JobTypeLocal:
		// Local jobs don't need special preparation
	case JobTypeCompose:
		// Compose jobs don't need special preparation
	}

	return nil
}

// Thread-unsafe helper methods (called within locks)

func (m *UnifiedConfigManager) removeJobUnsafe(name string, job *UnifiedJobConfig) {
	if m.scheduler != nil {
		_ = m.scheduler.RemoveJob(job)
	}
	delete(m.jobs, name)
}

func (m *UnifiedConfigManager) updateJobUnsafe(name string, oldJob, newJob *UnifiedJobConfig, source JobSource) error {
	// Remove old job
	if m.scheduler != nil {
		_ = m.scheduler.RemoveJob(oldJob)
	}

	// Prepare and add new job
	if err := m.prepareJob(name, newJob); err != nil {
		return err
	}

	newJob.SetJobSource(source)
	newJob.buildMiddlewares()

	if m.scheduler != nil {
		if err := m.scheduler.AddJob(newJob); err != nil {
			return err
		}
	}

	m.jobs[name] = newJob
	return nil
}

func (m *UnifiedConfigManager) replaceJobUnsafe(name string, oldJob, newJob *UnifiedJobConfig, source JobSource) error {
	return m.updateJobUnsafe(name, oldJob, newJob, source)
}

func (m *UnifiedConfigManager) addJobUnsafe(name string, job *UnifiedJobConfig, source JobSource) error {
	if err := m.prepareJob(name, job); err != nil {
		return err
	}

	job.SetJobSource(source)
	job.buildMiddlewares()

	if m.scheduler != nil {
		if err := m.scheduler.AddJob(job); err != nil {
			return err
		}
	}

	m.jobs[name] = job
	return nil
}

// GetJobCount returns the total number of managed jobs
func (m *UnifiedConfigManager) GetJobCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.jobs)
}

// GetJobCountByType returns the number of jobs by type
func (m *UnifiedConfigManager) GetJobCountByType() map[JobType]int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	counts := make(map[JobType]int)
	for _, job := range m.jobs {
		counts[job.Type]++
	}
	return counts
}
