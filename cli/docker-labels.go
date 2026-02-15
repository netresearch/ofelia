package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mitchellh/mapstructure"

	"github.com/netresearch/ofelia/core"
)

const (
	labelPrefix = "ofelia"

	requiredLabel       = labelPrefix + ".enabled"
	requiredLabelFilter = requiredLabel + "=true"
	serviceLabel        = labelPrefix + ".service"

	// dockerComposeServiceLabel is the label Docker Compose sets on containers
	// to indicate the service name from docker-compose.yml
	dockerComposeServiceLabel = "com.docker.compose.service"
)

func (c *Config) buildFromDockerContainers(containers []DockerContainerInfo) error {
	execJobs, localJobs, runJobs, serviceJobs, composeJobs, globals := c.splitContainersLabelsIntoJobMapsByType(containers)

	if len(globals) > 0 {
		if err := mapstructure.WeakDecode(globals, &c.Global); err != nil {
			return fmt.Errorf("decode global labels: %w", err)
		}
	}

	// Security check: filter out host-based jobs from container labels unless explicitly allowed
	if !c.Global.AllowHostJobsFromLabels {
		if len(localJobs) > 0 {
			c.logger.Errorf("SECURITY POLICY VIOLATION: Cannot sync %d host-based local jobs from container labels. "+
				"Host job execution from container labels is disabled for security. "+
				"This prevents container-to-host privilege escalation attacks.", len(localJobs))
			localJobs = make(map[string]map[string]interface{})
		}
		if len(composeJobs) > 0 {
			c.logger.Errorf("SECURITY POLICY VIOLATION: Cannot sync %d host-based compose jobs from container labels. "+
				"Host job execution from container labels is disabled for security. "+
				"This prevents container-to-host privilege escalation attacks.", len(composeJobs))
			composeJobs = make(map[string]map[string]interface{})
		}
	}

	decodeInto := func(src map[string]map[string]interface{}, dst any) error {
		if len(src) == 0 {
			return nil
		}
		return mapstructure.WeakDecode(src, dst)
	}

	if err := decodeInto(execJobs, &c.ExecJobs); err != nil {
		return fmt.Errorf("decode exec jobs: %w", err)
	}
	if err := decodeInto(localJobs, &c.LocalJobs); err != nil {
		return fmt.Errorf("decode local jobs: %w", err)
	}
	if err := decodeInto(serviceJobs, &c.ServiceJobs); err != nil {
		return fmt.Errorf("decode service jobs: %w", err)
	}
	if err := decodeInto(runJobs, &c.RunJobs); err != nil {
		return fmt.Errorf("decode run jobs: %w", err)
	}
	if err := decodeInto(composeJobs, &c.ComposeJobs); err != nil {
		return fmt.Errorf("decode compose jobs: %w", err)
	}

	markJobSource(c.ExecJobs, JobSourceLabel)
	markJobSource(c.LocalJobs, JobSourceLabel)
	markJobSource(c.ServiceJobs, JobSourceLabel)
	markJobSource(c.RunJobs, JobSourceLabel)
	markJobSource(c.ComposeJobs, JobSourceLabel)

	return nil
}

// Returns true if the job type is allowed to run on a service/non-service container.
// `job-local` and `job-service-run` can only run on service containers.
func checkJobTypeAllowedOnServiceContainer(jobType string, jobName string, containerName string, isService bool, logger core.Logger) bool {
	// Any job type can run on a service container.
	if isService {
		return true
	}

	switch jobType {
	case jobLocal, jobServiceRun:
		// `job-local` and `job-service-run` can only run on service containers.
		logger.Debugf("Container %s. Job %s (%s) can be run only on service containers. Skipping", containerName, jobName, jobType)
		return false
	case jobRun, jobExec, jobCompose:
		// Other job types can run on non-service containers.
		return true
	}

	logger.Warningf("Unknown job type %s found in container %s. Skipping", jobType, containerName)
	return false
}

// Returns true if the job type is allowed to run on a stopped/running container.
// Only the `job-run` type can run on stopped containers.
func checkJobTypeAllowedOnStoppedContainer(jobType string, jobName string, containerName string, isRunning bool, logger core.Logger) bool {
	// Any job type can run on a running container.
	if isRunning {
		return true
	}

	switch jobType {
	case jobRun:
		// The `job-run` job type can run on stopped containers.
		return true

	case jobExec, jobLocal, jobServiceRun, jobCompose:
		// Other job types cannot run on stopped containers.
		logger.Debugf(
			"Container %s is stopped, skipping job %s (%s) from stopped container: only job-run allowed on stopped containers",
			containerName, jobName, jobType)

		return false
	}

	logger.Warningf("Unknown job type %s found in container %s. Skipping", jobType, containerName)
	return false
}

// Returns true if specified job can be run on a container, logs debug message if not.
func canRunJobOnContainer(jobType string, jobName string, containerName string, isRunning bool, isService bool, logger core.Logger) bool {
	return checkJobTypeAllowedOnServiceContainer(jobType, jobName, containerName, isService, logger) &&
		checkJobTypeAllowedOnStoppedContainer(jobType, jobName, containerName, isRunning, logger)
}

// applyJobParameterToJobMaps updates the appropriate job map for the given parameter.
// It is extracted to keep splitContainersLabelsIntoJobMapsByType cyclomatic complexity under the linter limit.
func applyJobParameterToJobMaps(
	jobType, jobName, jobParam, paramValue string,
	scopedJobName string,
	execJobs, localJobs, containerRunJobs, serviceJobs, composeJobs map[string]map[string]interface{},
) {
	switch jobType {
	case jobExec:
		ensureJob(execJobs, scopedJobName)
		setJobParam(execJobs[scopedJobName], jobParam, paramValue)
	case jobLocal:
		ensureJob(localJobs, jobName)
		setJobParam(localJobs[jobName], jobParam, paramValue)
	case jobServiceRun:
		ensureJob(serviceJobs, jobName)
		setJobParam(serviceJobs[jobName], jobParam, paramValue)
	case jobRun:
		ensureJob(containerRunJobs, jobName)
		setJobParam(containerRunJobs[jobName], jobParam, paramValue)
	case jobCompose:
		ensureJob(composeJobs, jobName)
		setJobParam(composeJobs[jobName], jobParam, paramValue)
	}
}

func sortContainers(containers []DockerContainerInfo) []DockerContainerInfo {
	// Sort containers. Order:
	// 1. Running containers first
	// 2. Not running containers second
	// 3. Sort by Created (newest first)
	// 4. Sort by name
	sortedContainers := make([]DockerContainerInfo, len(containers))
	copy(sortedContainers, containers)

	slices.SortStableFunc(sortedContainers, func(left DockerContainerInfo, right DockerContainerInfo) int {
		if left.State.Running != right.State.Running {
			if left.State.Running {
				return -1
			} else {
				return 1
			}
		}

		// Sort containers by Created (newest first)
		// if result is not 0, return result, otherwise sort by name
		createdSortResult := right.Created.Compare(left.Created)
		if createdSortResult != 0 {
			return createdSortResult
		}

		return strings.Compare(left.Name, right.Name)
	})
	return sortedContainers
}

// splitContainersLabelsIntoJobMapsByType partitions container labels and parses values into per-type job maps.
func (c *Config) splitContainersLabelsIntoJobMapsByType(containers []DockerContainerInfo) (
	execJobs, localJobs, runJobs, serviceJobs, composeJobs map[string]map[string]interface{},
	globalConfigs map[string]interface{},
) {
	execJobs = make(map[string]map[string]interface{})
	localJobs = make(map[string]map[string]interface{})
	runJobs = make(map[string]map[string]interface{})
	serviceJobs = make(map[string]map[string]interface{})
	composeJobs = make(map[string]map[string]interface{})
	globalConfigs = make(map[string]interface{})

	sortedContainers := sortContainers(containers)

	for _, containerInfo := range sortedContainers {
		containerName := containerInfo.Name
		containerRunning := containerInfo.State.Running
		labelSet := containerInfo.Labels
		isService := hasServiceLabel(labelSet)

		// New jobs for the container
		// We merge them into the existing jobs later.
		// This allows to have the prescedence of the set `image` parameter over the propagated by default `container` parameter.
		containerRunJobs := make(map[string]map[string]interface{})
		containerExecJobs := make(map[string]map[string]interface{})

		for k, jobParamValue := range labelSet {
			parts := strings.Split(k, ".")
			if len(parts) < 4 {
				if isService {
					globalConfigs[parts[1]] = jobParamValue
				}
				continue
			}
			jobType, jobName, jobParam := parts[1], parts[2], parts[3]
			scopedName := jobName
			if jobType == jobExec {
				// Use Docker Compose service name if available, fallback to container name
				jobPrefix := getJobPrefix(labelSet, containerName)
				scopedName = jobPrefix + "." + jobName
			}

			if !canRunJobOnContainer(jobType, jobName, containerName, containerRunning, isService, c.logger) {
				continue
			}

			applyJobParameterToJobMaps(jobType, jobName, jobParam, jobParamValue, scopedName,
				containerExecJobs, localJobs, containerRunJobs, serviceJobs, composeJobs)
		}

		if !isService {
			addContainerNameToJobsIfNeeded(containerExecJobs, containerName)
			addContainerNameToJobsIfNeeded(containerRunJobs, containerName)
		}

		// Merge new jobs into existing jobs
		// `job-exec` - do not overwrite existing jobs with new jobs, only add new jobs.
		// There could not be dupes since the scoped name must be unique.
		execJobs = mergeJobMaps(execJobs, containerExecJobs, false)

		// `job-run` - overwrite existing jobs with new jobs if the container is running.
		runJobs = mergeJobMaps(runJobs, containerRunJobs, containerRunning)
	}
	return
}

func addContainerNameToJobsIfNeeded(jobs map[string]map[string]interface{}, containerName string) {
	for _, job := range jobs {
		if _, hasImage := job["image"]; hasImage {
			continue
		}
		if _, hasContainer := job["container"]; !hasContainer {
			job["container"] = containerName
		}
	}
}

// mergeJobMaps merges two maps into a new map.
// This helps us to avoid duplicate job definitions for the same job name with an option to override the previously defined job.
func mergeJobMaps[K comparable, V any](left map[K]V, right map[K]V, useRightIfExists bool) map[K]V {
	result := make(map[K]V, len(left))
	// Copy left map to new
	for k, v := range left {
		result[k] = v
	}
	// Merge right map into new
	for k, v := range right {
		_, exists := result[k]
		// If right value exists and useRightIfExists is true,
		// or right value does not exist, set new value
		if !exists || useRightIfExists {
			result[k] = v
		}
	}
	return result
}

func hasServiceLabel(labels map[string]string) bool {
	for k, v := range labels {
		if k == serviceLabel && v == "true" {
			return true
		}
	}
	return false
}

// getJobPrefix returns the prefix to use for job names from a container.
// For Docker Compose containers, it uses the service name (com.docker.compose.service label).
// For non-Compose containers, it falls back to the container name.
// This allows cross-container job references using intuitive service names like "database.backup"
// instead of generated container names like "myproject-database-1.backup".
func getJobPrefix(labels map[string]string, containerName string) string {
	if serviceName, ok := labels[dockerComposeServiceLabel]; ok && serviceName != "" {
		return serviceName
	}
	return containerName
}

func ensureJob(m map[string]map[string]interface{}, name string) {
	if _, ok := m[name]; !ok {
		m[name] = make(map[string]interface{})
	}
}

// markJobSource assigns the provided source to all jobs in the map.
//
// The generic type J must implement SetJobSource(JobSource) so the function can
// uniformly tag any job configuration with its origin.
func markJobSource[J interface{ SetJobSource(JobSource) }](m map[string]J, src JobSource) {
	for _, j := range m {
		j.SetJobSource(src)
	}
}

func setJobParam(params map[string]interface{}, paramName, paramVal string) {
	switch strings.ToLower(paramName) {
	case "volume", "environment", "volumes-from", "depends-on", "on-success", "on-failure":
		arr := []string{} // allow providing JSON arr of volume mounts or dependency lists
		if err := json.Unmarshal([]byte(paramVal), &arr); err == nil {
			params[paramName] = arr
			return
		}
	}

	params[paramName] = paramVal
}
