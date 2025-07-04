package cli

import (
	"encoding/json"
	"strings"

	"github.com/mitchellh/mapstructure"
)

const (
	labelPrefix = "ofelia"

	requiredLabel       = labelPrefix + ".enabled"
	requiredLabelFilter = requiredLabel + "=true"
	serviceLabel        = labelPrefix + ".service"
)

var globalLabelKeys = map[string]struct{}{
	"smtp-host":            {},
	"smtp-port":            {},
	"smtp-user":            {},
	"smtp-password":        {},
	"smtp-tls-skip-verify": {},
	"email-to":             {},
	"email-from":           {},
	"mail-only-on-error":   {},
	"save-folder":          {},
	"save-only-on-error":   {},
	"slack-webhook":        {},
	"slack-only-on-error":  {},
	"log-level":            {},
	"enable-web":           {},
	"web-address":          {},
	"enable-pprof":         {},
	"pprof-address":        {},
	"max-runtime":          {},
}

func (c *Config) buildFromDockerLabels(labels map[string]map[string]string) error {
	execJobs := make(map[string]map[string]interface{})
	localJobs := make(map[string]map[string]interface{})
	runJobs := make(map[string]map[string]interface{})
	serviceJobs := make(map[string]map[string]interface{})
	composeJobs := make(map[string]map[string]interface{})
	globalConfigs := make(map[string]interface{})

	for containerName, l := range labels {
		isServiceContainer := func() bool {
			for k, v := range l {
				if k == serviceLabel {
					return v == "true"
				}
			}
			return false
		}()

		for k, v := range l {
			if k == requiredLabel || k == serviceLabel {
				continue
			}
			parts := strings.Split(k, ".")
			if len(parts) < 4 {
				if isServiceContainer {
					if _, ok := globalLabelKeys[parts[1]]; ok {
						globalConfigs[parts[1]] = v
					} else if c.logger != nil {
						c.logger.Warningf("unknown label %q", k)
					}
				} else if c.logger != nil {
					c.logger.Warningf("unknown label %q", k)
				}

				continue
			}

			jobType, jobName, jobParam := parts[1], parts[2], parts[3]
			scopedJobName := jobName
			if jobType == jobExec {
				scopedJobName = containerName + "." + jobName
			}
			switch {
			case jobType == jobExec: // only job exec can be provided on the non-service container
				if _, ok := execJobs[scopedJobName]; !ok {
					execJobs[scopedJobName] = make(map[string]interface{})
				}

				setJobParam(execJobs[scopedJobName], jobParam, v)
				// since this label was placed not on the service container
				// this means we need to `exec` command in this container
				if !isServiceContainer {
					execJobs[scopedJobName]["container"] = containerName
				}
			case jobType == jobLocal && isServiceContainer:
				if _, ok := localJobs[jobName]; !ok {
					localJobs[jobName] = make(map[string]interface{})
				}
				setJobParam(localJobs[jobName], jobParam, v)
			case jobType == jobServiceRun && isServiceContainer:
				if _, ok := serviceJobs[jobName]; !ok {
					serviceJobs[jobName] = make(map[string]interface{})
				}
				setJobParam(serviceJobs[jobName], jobParam, v)
			case jobType == jobRun:
				if _, ok := runJobs[jobName]; !ok {
					runJobs[jobName] = make(map[string]interface{})
				}
				setJobParam(runJobs[jobName], jobParam, v)
			case jobType == jobCompose:
				if _, ok := composeJobs[jobName]; !ok {
					composeJobs[jobName] = make(map[string]interface{})
				}
				setJobParam(composeJobs[jobName], jobParam, v)
			default:
				if c.logger != nil {
					c.logger.Warningf("unknown label %q", k)
				}
			}
		}
	}

	if len(globalConfigs) > 0 {
		if err := mapstructure.WeakDecode(globalConfigs, &c.Global); err != nil {
			return err
		}
	}

	if len(execJobs) > 0 {
		if err := mapstructure.WeakDecode(execJobs, &c.ExecJobs); err != nil {
			return err
		}
		markJobSource(c.ExecJobs, JobSourceLabel)
	}

	if len(localJobs) > 0 {
		if err := mapstructure.WeakDecode(localJobs, &c.LocalJobs); err != nil {
			return err
		}
		markJobSource(c.LocalJobs, JobSourceLabel)
	}

	if len(serviceJobs) > 0 {
		if err := mapstructure.WeakDecode(serviceJobs, &c.ServiceJobs); err != nil {
			return err
		}
		markJobSource(c.ServiceJobs, JobSourceLabel)
	}

	if len(runJobs) > 0 {
		if err := mapstructure.WeakDecode(runJobs, &c.RunJobs); err != nil {
			return err
		}
		markJobSource(c.RunJobs, JobSourceLabel)
	}

	if len(composeJobs) > 0 {
		if err := mapstructure.WeakDecode(composeJobs, &c.ComposeJobs); err != nil {
			return err
		}
		markJobSource(c.ComposeJobs, JobSourceLabel)
	}

	return nil
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
