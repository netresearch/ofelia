package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mitchellh/mapstructure"
	ini "gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/core"
)

// Constants for job types and labels
const (
	jobExec       = "job-exec"
	jobRun        = "job-run"
	jobServiceRun = "job-service-run"
	jobLocal      = "job-local"
	jobCompose    = "job-compose"

	labelPrefix      = "ofelia"
	requiredLabel    = labelPrefix + ".enabled"
	serviceLabel     = labelPrefix + ".service"
)

// ConfigurationParser handles parsing of unified job configurations from various sources
type ConfigurationParser struct {
	logger core.Logger
}

// NewConfigurationParser creates a new configuration parser
func NewConfigurationParser(logger core.Logger) *ConfigurationParser {
	return &ConfigurationParser{
		logger: logger,
	}
}

// ParseINI parses INI file content and returns unified job configurations
func (p *ConfigurationParser) ParseINI(cfg *ini.File) (map[string]*UnifiedJobConfig, error) {
	jobs := make(map[string]*UnifiedJobConfig)

	for _, section := range cfg.Sections() {
		name := strings.TrimSpace(section.Name())
		
		var jobType JobType
		var jobName string
		
		switch {
		case strings.HasPrefix(name, jobExec):
			jobType = JobTypeExec
			jobName = parseJobName(name, jobExec)
		case strings.HasPrefix(name, jobRun):
			jobType = JobTypeRun
			jobName = parseJobName(name, jobRun)
		case strings.HasPrefix(name, jobServiceRun):
			jobType = JobTypeService
			jobName = parseJobName(name, jobServiceRun)
		case strings.HasPrefix(name, jobLocal):
			jobType = JobTypeLocal
			jobName = parseJobName(name, jobLocal)
		case strings.HasPrefix(name, jobCompose):
			jobType = JobTypeCompose
			jobName = parseJobName(name, jobCompose)
		default:
			continue // Skip non-job sections
		}

		// Create unified job configuration
		unifiedJob := NewUnifiedJobConfig(jobType)
		unifiedJob.SetJobSource(JobSourceINI)

		// Parse section into the appropriate job type
		if err := p.parseINISection(section, unifiedJob); err != nil {
			return nil, fmt.Errorf("failed to parse %s job %q: %w", jobType, jobName, err)
		}

		jobs[jobName] = unifiedJob
	}

	return jobs, nil
}

// parseINISection parses an INI section into a unified job configuration
func (p *ConfigurationParser) parseINISection(section *ini.Section, job *UnifiedJobConfig) error {
	sectionMap := sectionToMap(section)

	// Parse into the appropriate core job type based on the unified job type
	switch job.Type {
	case JobTypeExec:
		if err := mapstructure.WeakDecode(sectionMap, job.ExecJob); err != nil {
			return fmt.Errorf("failed to decode exec job: %w", err)
		}
	case JobTypeRun:
		if err := mapstructure.WeakDecode(sectionMap, job.RunJob); err != nil {
			return fmt.Errorf("failed to decode run job: %w", err)
		}
	case JobTypeService:
		if err := mapstructure.WeakDecode(sectionMap, job.RunServiceJob); err != nil {
			return fmt.Errorf("failed to decode service job: %w", err)
		}
	case JobTypeLocal:
		if err := mapstructure.WeakDecode(sectionMap, job.LocalJob); err != nil {
			return fmt.Errorf("failed to decode local job: %w", err)
		}
	case JobTypeCompose:
		if err := mapstructure.WeakDecode(sectionMap, job.ComposeJob); err != nil {
			return fmt.Errorf("failed to decode compose job: %w", err)
		}
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}

	// Parse middleware configuration (common to all job types)
	if err := mapstructure.WeakDecode(sectionMap, &job.MiddlewareConfig); err != nil {
		return fmt.Errorf("failed to decode middleware config: %w", err)
	}

	return nil
}

// ParseDockerLabels parses Docker labels and returns unified job configurations
func (p *ConfigurationParser) ParseDockerLabels(
	labels map[string]map[string]string,
	allowHostJobs bool,
) (map[string]*UnifiedJobConfig, error) {
	// Split labels by type using the existing logic
	execJobs, localJobs, runJobs, serviceJobs, composeJobs := p.splitLabelsByType(labels)

	// Security enforcement: block host-based jobs if not allowed
	if !allowHostJobs {
		if len(localJobs) > 0 {
			p.logger.Errorf("SECURITY POLICY VIOLATION: Blocked %d local jobs from Docker labels. "+
				"Host job execution from container labels is disabled for security.", len(localJobs))
			localJobs = make(map[string]map[string]interface{})
		}
		if len(composeJobs) > 0 {
			p.logger.Errorf("SECURITY POLICY VIOLATION: Blocked %d compose jobs from Docker labels. "+
				"Host job execution from container labels is disabled for security.", len(composeJobs))
			composeJobs = make(map[string]map[string]interface{})
		}
	} else {
		if len(localJobs) > 0 {
			p.logger.Warningf("SECURITY WARNING: Processing %d local jobs from Docker labels. "+
				"This allows containers to execute arbitrary commands on the host system.", len(localJobs))
		}
		if len(composeJobs) > 0 {
			p.logger.Warningf("SECURITY WARNING: Processing %d compose jobs from Docker labels. "+
				"This allows containers to execute Docker Compose operations on the host system.", len(composeJobs))
		}
	}

	// Convert parsed label data to unified job configurations
	jobs := make(map[string]*UnifiedJobConfig)

	// Convert each job type
	if err := p.convertLabelJobs(execJobs, JobTypeExec, jobs); err != nil {
		return nil, fmt.Errorf("failed to convert exec jobs: %w", err)
	}
	if err := p.convertLabelJobs(runJobs, JobTypeRun, jobs); err != nil {
		return nil, fmt.Errorf("failed to convert run jobs: %w", err)
	}
	if err := p.convertLabelJobs(serviceJobs, JobTypeService, jobs); err != nil {
		return nil, fmt.Errorf("failed to convert service jobs: %w", err)
	}
	if err := p.convertLabelJobs(localJobs, JobTypeLocal, jobs); err != nil {
		return nil, fmt.Errorf("failed to convert local jobs: %w", err)
	}
	if err := p.convertLabelJobs(composeJobs, JobTypeCompose, jobs); err != nil {
		return nil, fmt.Errorf("failed to convert compose jobs: %w", err)
	}

	return jobs, nil
}

// convertLabelJobs converts parsed label data to unified job configurations
func (p *ConfigurationParser) convertLabelJobs(
	labelJobs map[string]map[string]interface{},
	jobType JobType,
	targetMap map[string]*UnifiedJobConfig,
) error {
	for jobName, jobData := range labelJobs {
		unifiedJob := NewUnifiedJobConfig(jobType)
		unifiedJob.SetJobSource(JobSourceLabel)

		// Decode into the appropriate core job type
		switch jobType {
		case JobTypeExec:
			if err := mapstructure.WeakDecode(jobData, unifiedJob.ExecJob); err != nil {
				return fmt.Errorf("failed to decode exec job %q: %w", jobName, err)
			}
		case JobTypeRun:
			if err := mapstructure.WeakDecode(jobData, unifiedJob.RunJob); err != nil {
				return fmt.Errorf("failed to decode run job %q: %w", jobName, err)
			}
		case JobTypeService:
			if err := mapstructure.WeakDecode(jobData, unifiedJob.RunServiceJob); err != nil {
				return fmt.Errorf("failed to decode service job %q: %w", jobName, err)
			}
		case JobTypeLocal:
			if err := mapstructure.WeakDecode(jobData, unifiedJob.LocalJob); err != nil {
				return fmt.Errorf("failed to decode local job %q: %w", jobName, err)
			}
		case JobTypeCompose:
			if err := mapstructure.WeakDecode(jobData, unifiedJob.ComposeJob); err != nil {
				return fmt.Errorf("failed to decode compose job %q: %w", jobName, err)
			}
		}

		// Decode middleware configuration
		if err := mapstructure.WeakDecode(jobData, &unifiedJob.MiddlewareConfig); err != nil {
			return fmt.Errorf("failed to decode middleware config for job %q: %w", jobName, err)
		}

		targetMap[jobName] = unifiedJob
	}

	return nil
}

// splitLabelsByType partitions label maps and parses values into per-type maps
// This is adapted from the existing docker-labels.go logic
func (p *ConfigurationParser) splitLabelsByType(labels map[string]map[string]string) (
	execJobs, localJobs, runJobs, serviceJobs, composeJobs map[string]map[string]interface{},
) {
	execJobs = make(map[string]map[string]interface{})
	localJobs = make(map[string]map[string]interface{})
	runJobs = make(map[string]map[string]interface{})
	serviceJobs = make(map[string]map[string]interface{})
	composeJobs = make(map[string]map[string]interface{})

	for containerName, labelSet := range labels {
		// Check if this container has the required label
		if enabled, exists := labelSet[requiredLabel]; !exists || enabled != "true" {
			continue
		}

		isService := hasServiceLabel(labelSet)
		
		for k, v := range labelSet {
			parts := strings.Split(k, ".")
			if len(parts) < 4 || parts[0] != "ofelia" {
				continue
			}
			
			jobType, jobName, jobParam := parts[1], parts[2], parts[3]
			scopedName := jobName
			
			// For exec jobs, include container name in scope
			if jobType == "job-exec" {
				scopedName = containerName + "." + jobName
			}

			switch {
			case jobType == "job-exec":
				ensureJob(execJobs, scopedName)
				setJobParam(execJobs[scopedName], jobParam, v)
				if !isService {
					execJobs[scopedName]["container"] = containerName
				}
			case jobType == "job-local" && isService:
				ensureJob(localJobs, jobName)
				setJobParam(localJobs[jobName], jobParam, v)
			case jobType == "job-service-run" && isService:
				ensureJob(serviceJobs, jobName)
				setJobParam(serviceJobs[jobName], jobParam, v)
			case jobType == "job-run":
				ensureJob(runJobs, jobName)
				setJobParam(runJobs[jobName], jobParam, v)
			case jobType == "job-compose":
				ensureJob(composeJobs, jobName)
				setJobParam(composeJobs[jobName], jobParam, v)
			}
		}
	}

	return
}

// Helper functions

func parseJobName(section, prefix string) string {
	s := strings.TrimPrefix(section, prefix)
	s = strings.TrimSpace(s)
	return strings.Trim(s, "\"")
}

func sectionToMap(section *ini.Section) map[string]interface{} {
	m := make(map[string]interface{})
	for _, key := range section.Keys() {
		vals := key.ValueWithShadows()
		switch {
		case len(vals) > 1:
			cp := make([]string, len(vals))
			copy(cp, vals)
			m[key.Name()] = cp
		case len(vals) == 1:
			m[key.Name()] = vals[0]
		default:
			m[key.Name()] = ""
		}
	}
	return m
}

func hasServiceLabel(labels map[string]string) bool {
	for k, v := range labels {
		if k == serviceLabel && v == "true" {
			return true
		}
	}
	return false
}

func ensureJob(m map[string]map[string]interface{}, name string) {
	if _, ok := m[name]; !ok {
		m[name] = make(map[string]interface{})
	}
}

func setJobParam(params map[string]interface{}, paramName, paramVal string) {
	switch strings.ToLower(paramName) {
	case "volume", "environment", "volumes-from":
		arr := []string{}
		if err := json.Unmarshal([]byte(paramVal), &arr); err == nil {
			params[paramName] = arr
			return
		}
	}
	params[paramName] = paramVal
}