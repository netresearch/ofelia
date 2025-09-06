package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mitchellh/mapstructure"
)

const (
	labelPrefix = "ofelia"

	requiredLabel       = labelPrefix + ".enabled"
	requiredLabelFilter = requiredLabel + "=true"
	serviceLabel        = labelPrefix + ".service"
)

func (c *Config) buildFromDockerLabels(labels map[string]map[string]string) error {
	execJobs, localJobs, runJobs, serviceJobs, composeJobs, globals := splitLabelsByType(labels)

	if len(globals) > 0 {
		if err := mapstructure.WeakDecode(globals, &c.Global); err != nil {
			return fmt.Errorf("decode global labels: %w", err)
		}
	}

	// SECURITY HARDENING: Hard block host-based jobs from Docker labels unless explicitly allowed
	// This prevents container-to-host privilege escalation attacks through Docker labels
	if !c.Global.AllowHostJobsFromLabels {
		if len(localJobs) > 0 {
			c.logger.Errorf("SECURITY POLICY VIOLATION: Blocked %d local jobs from Docker labels. "+
				"Host job execution from container labels is disabled for security. "+
				"Local jobs allow arbitrary command execution on the host system. "+
				"Set allow-host-jobs-from-labels=true only if you understand the privilege escalation risks.", len(localJobs))
			// Prevent any local job creation by clearing the map
			localJobs = make(map[string]map[string]interface{})
		}
		if len(composeJobs) > 0 {
			c.logger.Errorf("SECURITY POLICY VIOLATION: Blocked %d compose jobs from Docker labels. "+
				"Host job execution from container labels is disabled for security. "+
				"Compose jobs allow arbitrary Docker Compose operations on the host system. "+
				"Set allow-host-jobs-from-labels=true only if you understand the privilege escalation risks.", len(composeJobs))
			// Prevent any compose job creation by clearing the map
			composeJobs = make(map[string]map[string]interface{})
		}
		
		// Log security enforcement action
		if len(localJobs) > 0 || len(composeJobs) > 0 {
			c.logger.Noticef("SECURITY: Container-to-host job execution blocked for security. "+
				"This prevents containers from executing arbitrary commands on the host via labels. "+
				"Only enable allow-host-jobs-from-labels in trusted environments.")
		}
	} else {
		// Warn about security implications when allowing host jobs from labels
		if len(localJobs) > 0 {
			c.logger.Warningf("SECURITY WARNING: Processing %d local jobs from Docker labels. "+
				"This allows containers to execute arbitrary commands on the host system. "+
				"Only enable this in trusted environments with verified container security.", len(localJobs))
		}
		if len(composeJobs) > 0 {
			c.logger.Warningf("SECURITY WARNING: Processing %d compose jobs from Docker labels. "+
				"This allows containers to execute Docker Compose operations on the host system. "+
				"Only enable this in trusted environments with verified container security.", len(composeJobs))
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

// splitLabelsByType partitions label maps and parses values into per-type maps.
func splitLabelsByType(labels map[string]map[string]string) (
	execJobs, localJobs, runJobs, serviceJobs, composeJobs map[string]map[string]interface{},
	globalConfigs map[string]interface{},
) {
	execJobs = make(map[string]map[string]interface{})
	localJobs = make(map[string]map[string]interface{})
	runJobs = make(map[string]map[string]interface{})
	serviceJobs = make(map[string]map[string]interface{})
	composeJobs = make(map[string]map[string]interface{})
	globalConfigs = make(map[string]interface{})

	for containerName, labelSet := range labels {
		isService := hasServiceLabel(labelSet)
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
				scopedName = containerName + "." + jobName
			}
			switch {
			case jobType == jobExec:
				ensureJob(execJobs, scopedName)
				setJobParam(execJobs[scopedName], jobParam, v)
				if !isService {
					execJobs[scopedName]["container"] = containerName
				}
			case jobType == jobLocal && isService:
				ensureJob(localJobs, jobName)
				setJobParam(localJobs[jobName], jobParam, v)
			case jobType == jobServiceRun && isService:
				ensureJob(serviceJobs, jobName)
				setJobParam(serviceJobs[jobName], jobParam, v)
			case jobType == jobRun:
				ensureJob(runJobs, jobName)
				setJobParam(runJobs[jobName], jobParam, v)
			case jobType == jobCompose:
				ensureJob(composeJobs, jobName)
				setJobParam(composeJobs[jobName], jobParam, v)
			}
		}
	}
	return
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
	case "volume", "environment", "volumes-from":
		arr := []string{} // allow providing JSON arr of volume mounts
		if err := json.Unmarshal([]byte(paramVal), &arr); err == nil {
			params[paramName] = arr
			return
		}
	}

	params[paramName] = paramVal
}