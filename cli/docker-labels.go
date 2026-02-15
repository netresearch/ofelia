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

// Returns true if specified job can be run on a service container, logs debug message if not.
func canRunServiceJob(jobType string, jobName string, containerName string, isService bool, logger core.Logger) bool {
	switch jobType {
	case jobLocal, jobServiceRun:
		if !isService {
			logger.Debugf("Container %s. Job %s (%s) can be run only on service containers. Skipping", containerName, jobName, jobType)
		}
		return isService
	case jobRun, jobExec, jobCompose:
		return true
	}
	logger.Warningf("Unknown job type %s found in container. Skipping", jobType, containerName)
	return false
}

// Returns true if specified job can be run on a stopped container, logs debug message if not.
func canRunJobInStoppedContainer(jobType string, jobName string, containerName string, isRunning bool, logger core.Logger) bool {
	switch jobType {
	case jobExec, jobLocal, jobServiceRun, jobCompose:
		if !isRunning {
			logger.Debugf(
				"Container %s is stopped, skipping job %s (%s) from stopped container: only job-run allowed on stopped containers",
				containerName,
				jobName,
				jobType,
			)
		}
		return isRunning
	case jobRun:
		return true
	}
	logger.Warningf("Unknown job type %s found in container. Skipping", jobType, containerName)
	return false
}

// Returns true if specified job can be run on a container, logs debug message if not.
func canRunJobOnContainer(jobType string, jobName string, containerName string, isRunning bool, isService bool, logger core.Logger) bool {
	return canRunServiceJob(jobType, jobName, containerName, isService, logger) &&
		canRunJobInStoppedContainer(jobType, jobName, containerName, isRunning, logger)
}

// applyLabelToJobMaps updates the appropriate job map for the given label.
// It is extracted to keep splitContainersLabelsIntoJobMapsByType cyclomatic complexity under the linter limit.
func applyLabelToJobMaps(
	jobType, jobName, jobParam, scopedName, containerName, paramValue string,
	isService bool,
	execJobs, localJobs, containerRunJobs, serviceJobs, composeJobs map[string]map[string]interface{},
) {
	switch {
	case jobType == jobExec:
		ensureJob(execJobs, scopedName)
		setJobParam(execJobs[scopedName], jobParam, paramValue)
		if !isService && execJobs[scopedName]["container"] == nil {
			execJobs[scopedName]["container"] = containerName
		}
	case jobType == jobLocal && isService:
		ensureJob(localJobs, jobName)
		setJobParam(localJobs[jobName], jobParam, paramValue)
	case jobType == jobServiceRun && isService:
		ensureJob(serviceJobs, jobName)
		setJobParam(serviceJobs[jobName], jobParam, paramValue)
	case jobType == jobRun:
		ensureJob(containerRunJobs, jobName)
		setJobParam(containerRunJobs[jobName], jobParam, paramValue)
		if !isService && containerRunJobs[jobName]["container"] == nil {
			containerRunJobs[jobName]["container"] = containerName
		}
	case jobType == jobCompose:
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

		// New run jobs for the container
		// We merge them into the existing run jobs later based on the container running state.
		// This helps us to avoid duplicate job definitions for the same job name and prefer jobs from the running container.
		containerRunJobs := make(map[string]map[string]interface{})

		for k, v := range labelSet {
			parts := strings.Split(k, ".")
			if len(parts) < 4 {
				if isService {
					globalConfigs[parts[1]] = v
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

			applyLabelToJobMaps(jobType, jobName, jobParam, scopedName, containerName, v, isService,
				execJobs, localJobs, containerRunJobs, serviceJobs, composeJobs)
		}

		// Merge new run jobs into existing run jobs
		// If the container is running, use the new run jobs and overwrite the existing run jobs.
		// If the container is stopped, use the existing run jobs if possible and do not overwrite them.
		runJobs = mergeJobMaps(runJobs, containerRunJobs, containerRunning)
	}
	return
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
