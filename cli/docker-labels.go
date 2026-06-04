// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
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

// globalLabelAllowList defines global config keys that may be set via Docker labels.
// Keys NOT in this list are blocked to prevent privilege escalation (e.g., a container
// enabling host job execution or disabling web authentication via labels).
// See: https://github.com/netresearch/ofelia/issues/486
var globalLabelAllowList = map[string]bool{
	// Logging & scheduling
	"log-level":                true,
	"max-runtime":              true,
	"notification-cooldown":    true,
	"enable-strict-validation": true,

	// Slack notifications
	DeprecatedSlackWebhook: true,
	"slack-only-on-error":  true,

	// Email notifications
	"smtp-host":            true,
	"smtp-port":            true,
	"smtp-user":            true,
	"smtp-password":        true,
	"smtp-tls-skip-verify": true,
	"smtp-tls-policy":      true,
	"email-to":             true,
	"email-from":           true,
	"email-subject":        true,
	"mail-only-on-error":   true,

	// Save middleware
	"save-only-on-error":      true,
	"restore-history":         true,
	"restore-history-max-age": true,

	// Webhook global settings — operator-tunable, non-SSRF-sensitive keys
	// are exposed via labels. The SSRF-sensitive keys (webhook-allowed-hosts,
	// webhook-allow-remote-presets, webhook-trusted-preset-sources,
	// webhook-preset-cache-dir) stay INI-only to prevent containers from
	// widening the network egress surface or redirecting preset loading.
	// webhook-preset-cache-ttl is exposed because narrowing or widening a
	// cache TTL cannot widen the egress surface — see #486, #620, #640, plus
	// the negative regression test in
	// TestGlobalLabelAllowList_OmitsSSRFSensitiveWebhookKeys.
	webhookGlobalKeyWebhooks:       true,
	webhookGlobalKeyPresetCacheTTL: true,
	webhookGlobalKeyDefaultPreset:  true,

	// Legacy unprefixed form left behind by #618 on the label side. Kept in
	// the allow-list so values still reach applyGlobalWebhookLabels, which
	// logs a one-shot deprecation warning and maps it to the canonical
	// name. Remove after the deprecation window closes.
	legacyLabelKeyWebhooks: true,
}

// warnUnknownGlobalLabelKeys emits a single warning listing decoder UnusedKeys
// after stripping the legacy webhook aliases that applyGlobalWebhookLabels
// handles outside the decoder. Without the filter, every container reconcile
// would log the legacy keys as unknown alongside the one-shot deprecation
// warning.
func warnUnknownGlobalLabelKeys(logger *slog.Logger, unused []string) {
	if logger == nil || len(unused) == 0 {
		return
	}
	filtered := make([]string, 0, len(unused))
	for _, k := range unused {
		if _, isLegacy := legacyWebhookLabelAliases[k]; isLegacy {
			continue
		}
		filtered = append(filtered, k)
	}
	if len(filtered) == 0 {
		return
	}
	logger.Warn("Unknown global label keys (possible typo)", "keys", filtered)
}

func (c *Config) buildFromDockerContainers(containers []DockerContainerInfo) error {
	execJobs, localJobs, runJobs, serviceJobs, composeJobs, globals, webhookLabels := c.splitContainersLabelsIntoJobMapsByType(containers)

	if len(globals) > 0 {
		result, err := decodeWithMetadata(globals, &c.Global)
		if err != nil {
			return fmt.Errorf("decode global labels: %w", err)
		}
		warnUnknownGlobalLabelKeys(c.logger, result.UnusedKeys)

		// Apply global webhook label selector (e.g., ofelia.webhook-webhooks=discord).
		applyGlobalWebhookLabels(c, globals)
	}

	// Build webhook configs from labels (e.g., ofelia.webhook.slack-alerts.preset=slack)
	buildWebhookConfigsFromLabels(c, webhookLabels)

	// Security check: filter out host-based jobs from container labels unless explicitly allowed
	if !c.Global.AllowHostJobsFromLabels {
		if len(localJobs) > 0 {
			c.logger.Error(fmt.Sprintf("SECURITY POLICY VIOLATION: Cannot sync %d host-based local jobs from container labels. "+
				"Host job execution from container labels is disabled for security. "+
				"This prevents container-to-host privilege escalation attacks.", len(localJobs)))
			localJobs = make(map[string]map[string]any)
		}
		if len(composeJobs) > 0 {
			c.logger.Error(fmt.Sprintf("SECURITY POLICY VIOLATION: Cannot sync %d host-based compose jobs from container labels. "+
				"Host job execution from container labels is disabled for security. "+
				"This prevents container-to-host privilege escalation attacks.", len(composeJobs)))
			composeJobs = make(map[string]map[string]any)
		}
		// job-run / job-service-run privilege-escalation: a label-defined
		// container-spawning job can specify `volume=/:/host:rw` to mount
		// the host filesystem into the spawned container, or
		// `volumes-from=<donor>` to inherit a donor's bind mounts (e.g.
		// ofelia's own /var/run/docker.sock) — same class of escalation as
		// job-local / job-compose. Filter per-job (not per-batch) so
		// legitimate jobs with named/anonymous volumes and no volumes-from
		// survive while the policy still blocks host mounts and
		// volumes-from inheritance. Applies to BOTH job-run and
		// job-service-run because RunServiceJob.Volume has the same shape
		// and lands at the same Docker SDK call. See
		// https://github.com/netresearch/ofelia/issues/462.
		runJobs = filterJobsWithHostEscalation(runJobs, "job-run", c.logger)
		serviceJobs = filterJobsWithHostEscalation(serviceJobs, "job-service-run", c.logger)
	}

	decodeInto := func(src map[string]map[string]any, dst any) error {
		if len(src) == 0 {
			return nil
		}
		return weakDecodeConsistent(src, dst)
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
func checkJobTypeAllowedOnServiceContainer(jobType string, jobName string, containerName string, isService bool, logger *slog.Logger) bool {
	// Any job type can run on a service container.
	if isService {
		return true
	}

	switch jobType {
	case jobLocal, jobServiceRun:
		// `job-local` and `job-service-run` can only run on service containers.
		logger.Debug(fmt.Sprintf("Container %s. Job %s (%s) can be run only on service containers. Skipping",
			containerName, jobName, jobType))
		return false
	case jobRun, jobExec, jobCompose:
		// Other job types can run on non-service containers.
		return true
	}

	logger.Warn(fmt.Sprintf("Unknown job type %s found in container %s. Skipping", jobType, containerName))
	return false
}

// Returns true if the job type is allowed to run on a stopped/running container.
// Only the `job-run` type can run on stopped containers.
func checkJobTypeAllowedOnStoppedContainer(jobType string, jobName string, containerName string, isRunning bool, logger *slog.Logger) bool {
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
		logger.Debug(fmt.Sprintf(
			"Container %s is stopped, skipping job %s (%s) from stopped container: only job-run allowed on stopped containers",
			containerName, jobName, jobType))

		return false
	}

	logger.Warn(fmt.Sprintf("Unknown job type %s found in container %s. Skipping", jobType, containerName))
	return false
}

// Returns true if specified job can be run on a container, logs debug message if not.
func canRunJobOnContainer(jobType string, jobName string, containerName string, isRunning bool, isService bool, logger *slog.Logger) bool {
	return checkJobTypeAllowedOnServiceContainer(jobType, jobName, containerName, isService, logger) &&
		checkJobTypeAllowedOnStoppedContainer(jobType, jobName, containerName, isRunning, logger)
}

// applyJobParameterToJobMaps updates the appropriate job map for the given parameter.
// It is extracted to keep splitContainersLabelsIntoJobMapsByType cyclomatic complexity under the linter limit.
func applyJobParameterToJobMaps(
	jobType, jobName, jobParam, paramValue string,
	scopedJobName string,
	execJobs, localJobs, containerRunJobs, serviceJobs, composeJobs map[string]map[string]any,
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

	slices.SortStableFunc(sortedContainers, func(left, right DockerContainerInfo) int {
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
// dockerLabelJobMaps groups the per-type job maps and config maps produced
// while scanning a set of Docker containers' labels.
type dockerLabelJobMaps struct {
	execJobs, localJobs, runJobs, serviceJobs, composeJobs map[string]map[string]any
	globalConfigs                                          map[string]any
	webhookConfigs                                         map[string]map[string]string
}

// containerScan holds the state for the container currently being scanned,
// including its temporary run/exec job maps (merged into the scan-wide maps
// once the container's labels have all been processed).
type containerScan struct {
	name      string
	running   bool
	isService bool
	labels    map[string]string
	runJobs   map[string]map[string]any
	execJobs  map[string]map[string]any
}

func (c *Config) splitContainersLabelsIntoJobMapsByType(containers []DockerContainerInfo) (
	execJobs, localJobs, runJobs, serviceJobs, composeJobs map[string]map[string]any,
	globalConfigs map[string]any,
	webhookConfigs map[string]map[string]string,
) {
	m := &dockerLabelJobMaps{
		execJobs:       make(map[string]map[string]any),
		localJobs:      make(map[string]map[string]any),
		runJobs:        make(map[string]map[string]any),
		serviceJobs:    make(map[string]map[string]any),
		composeJobs:    make(map[string]map[string]any),
		globalConfigs:  make(map[string]any),
		webhookConfigs: make(map[string]map[string]string),
	}

	for _, containerInfo := range sortContainers(containers) {
		c.scanContainerLabels(m, containerInfo)
	}

	return m.execJobs, m.localJobs, m.runJobs, m.serviceJobs, m.composeJobs, m.globalConfigs, m.webhookConfigs
}

// scanContainerLabels processes every label on a single container, then merges
// the container's run/exec jobs into the scan-wide maps.
func (c *Config) scanContainerLabels(m *dockerLabelJobMaps, containerInfo DockerContainerInfo) {
	cs := &containerScan{
		name:      containerInfo.Name,
		running:   containerInfo.State.Running,
		isService: hasServiceLabel(containerInfo.Labels),
		labels:    containerInfo.Labels,
		// New jobs for the container, merged into the existing jobs later so a
		// set `image` parameter takes precedence over the default propagated
		// `container` parameter.
		runJobs:  make(map[string]map[string]any),
		execJobs: make(map[string]map[string]any),
	}

	for k, jobParamValue := range cs.labels {
		c.processContainerLabel(m, cs, k, jobParamValue)
	}

	if !cs.isService {
		addContainerNameToJobsIfNeeded(cs.execJobs, cs.name)
		addContainerNameToJobsIfNeeded(cs.runJobs, cs.name)
	}

	// `job-exec` - do not overwrite existing jobs with new jobs, only add new
	// jobs. There could not be dupes since the scoped name must be unique.
	m.execJobs = mergeJobMaps(m.execJobs, cs.execJobs, false)
	// `job-run` - overwrite existing jobs with new jobs if the container is running.
	m.runJobs = mergeJobMaps(m.runJobs, cs.runJobs, cs.running)
}

// processContainerLabel applies a single Docker label key/value to the
// appropriate job or config map for the container being scanned.
func (c *Config) processContainerLabel(m *dockerLabelJobMaps, cs *containerScan, k, jobParamValue string) {
	if k == requiredLabel || k == serviceLabel || k == dockerComposeServiceLabel {
		// Do not track internal labels as global config parameters.
		return
	}

	parts := strings.Split(k, ".")
	if len(parts) < 2 {
		return
	}
	if len(parts) < 4 {
		if cs.isService {
			c.filterGlobalLabelKey(parts[1], jobParamValue, cs.name, m.globalConfigs)
		}
		return
	}
	jobType, jobName, jobParam := parts[1], parts[2], parts[3]

	// Intercept webhook labels (e.g., ofelia.webhook.slack-alerts.preset=slack).
	// Webhook labels are only processed from service containers.
	if jobType == "webhook" {
		if cs.isService {
			if _, ok := m.webhookConfigs[jobName]; !ok {
				m.webhookConfigs[jobName] = make(map[string]string)
			}
			m.webhookConfigs[jobName][jobParam] = jobParamValue
		}
		return
	}

	scopedName := jobName
	if jobType == jobExec {
		// Use Docker Compose service name if available, fallback to container name.
		scopedName = getJobPrefix(cs.labels, cs.name) + "." + jobName
	}

	if !canRunJobOnContainer(jobType, jobName, cs.name, cs.running, cs.isService, c.logger) {
		return
	}

	applyJobParameterToJobMaps(jobType, jobName, jobParam, jobParamValue, scopedName,
		cs.execJobs, m.localJobs, cs.runJobs, m.serviceJobs, m.composeJobs)
}

func addContainerNameToJobsIfNeeded(jobs map[string]map[string]any, containerName string) {
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
	maps.Copy(result, left)
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

// filterGlobalLabelKey checks if a global config key from a Docker label is in the allow-list.
// Allowed keys are added to globalConfigs; blocked keys emit a security warning.
func (c *Config) filterGlobalLabelKey(key, value, containerName string, globalConfigs map[string]any) {
	if globalLabelAllowList[key] {
		globalConfigs[key] = value
		return
	}
	if c.logger != nil {
		c.logger.Warn(fmt.Sprintf(
			"SECURITY: Blocked global config key %q from Docker labels on container %q (only settable via config file)",
			key, containerName,
		))
	}
}

func hasServiceLabel(labels map[string]string) bool {
	for k, v := range labels {
		if k == serviceLabel && v == "true" { //nolint:goconst // Docker labels are stringly-typed — value is the literal "true"
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

func ensureJob(m map[string]map[string]any, name string) {
	if _, ok := m[name]; !ok {
		m[name] = make(map[string]any)
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

func setJobParam(params map[string]any, paramName, paramVal string) {
	switch strings.ToLower(paramName) {
	case "volume", "environment", "volumes-from", "depends-on", "on-success", "on-failure", "env-file", "env-from":
		arr := []string{} // allow providing JSON arr of volume mounts or dependency lists
		if err := json.Unmarshal([]byte(paramVal), &arr); err == nil {
			params[paramName] = arr
			return
		}
	}

	params[paramName] = paramVal
}

// isHostVolumeMount reports whether a Docker --volume spec mounts a host
// filesystem path into the container. Docker's bind-mount syntax is
// `source:target[:options]`; a host mount has the source as an absolute
// path ("/foo"), a relative path ("./foo"), or a home-relative path
// ("~/foo"). Named volumes ("my-vol:/data") and anonymous volumes
// ("/data" with no colon source/target separator) are NOT host mounts.
//
// The predicate errs on the side of blocking: any source starting with
// `/`, `.`, or `~` is treated as a host mount even though Docker'd valid
// named-volume regex `[a-zA-Z0-9][a-zA-Z0-9_.-]+` technically allows a
// volume name with an embedded `.` (e.g. `.hidden`). False positives
// here cost the operator a renamed volume; false negatives cost
// container-to-host escape, so the asymmetry is intentional.
//
// Whitespace and tab prefixes are stripped before the byte check so a
// crafted spec like `" /host:/container"` cannot bypass the predicate
// even if a future Docker daemon decides to be lenient about them.
//
// Examples:
//
//	"/host:/container"        -> true   (absolute host path)
//	"/host:/container:ro"     -> true   (with options)
//	"./relative:/container"   -> true   (relative host path)
//	"~/home:/container"       -> true   (home-relative)
//	"named-vol:/container"    -> false  (named volume)
//	"/container"              -> false  (anonymous volume target, no source)
//	"/:/host:rw"              -> true   (root mount — the original #462 vector)
//
// Note: Windows-style paths ("C:\foo:/container") are not handled — Ofelia
// targets Linux Docker daemons in production. A Windows operator using
// container labels for cross-host execution would already have other
// portability concerns; revisit if that use case emerges.
func isHostVolumeMount(spec string) bool {
	parts := strings.Split(spec, ":")
	if len(parts) < 2 {
		return false // anonymous volume target — no source, just a path inside the container
	}
	src := strings.TrimSpace(parts[0])
	if src == "" {
		return false // malformed (":foo:bar" or "  :foo:bar")
	}
	switch src[0] {
	case '/', '.', '~':
		return true
	default:
		return false
	}
}

// filterJobsWithHostEscalation returns container-spawning jobs (job-run
// or job-service-run) with any entry that represents a container-to-host
// privilege-escalation vector removed, emitting one SECURITY POLICY
// VIOLATION log per dropped job that names the job-type, the job name,
// the vector class, and the offending specs. Used to extend the
// AllowHostJobsFromLabels policy beyond job-local / job-compose, closing
// the escalation vectors tracked in
// https://github.com/netresearch/ofelia/issues/462.
//
// Applied to both job-run (core.RunJob.Volume / VolumesFrom) and
// job-service-run (core.RunServiceJob.Volume — Swarm services that mount
// the host filesystem are the same escape vector despite the different
// orchestration layer; the spec format is identical and lands at the
// same Docker SDK volume parsing). The jobType arg is the operator-
// facing label namespace ("job-run" / "job-service-run") used only in
// the violation log for triage.
//
// Currently filters two vector classes:
//
//  1. "volume" — direct host bind mounts (e.g. /:/host:rw). Per-spec
//     check so legitimate named/anonymous volumes survive.
//
//  2. "volumes-from" — indirect inheritance of a donor container's
//     bind mounts. We cannot inspect the donor at filter time, so any
//     non-empty volumes-from is treated as a violation: the donor might
//     have /var/run/docker.sock bind-mounted (ofelia itself usually does),
//     which would give the spawned container full Docker-daemon access.
//
// The job-local and job-compose entries in the same security block drop
// wholesale (a single host job is a host job); for the container-
// spawning jobs we filter per-job because legitimate use cases (named
// volumes, anonymous volumes, and no volumes-from) are common — an
// attacker controlling one container's labels should not be able to
// silently disable unrelated legitimate jobs on the same container.
//
// The "volume" key in each job map can be either a single string (single
// label like `ofelia.job-run.foo.volume=/host:/container`) or a []string
// (JSON array like `ofelia.job-run.foo.volume=["/host:/container","other:/v"]`).
// Both shapes are handled. Unexpected shapes are fail-closed: a future
// refactor that delivers `[]any` or similar will drop the job rather
// than silently bypass the security check.
func filterJobsWithHostEscalation(jobs map[string]map[string]any, jobType string, logger *slog.Logger) map[string]map[string]any {
	if len(jobs) == 0 {
		return jobs
	}
	filtered := make(map[string]map[string]any, len(jobs))
	for name, job := range jobs {
		hostMounts, volSafe := extractHostVolumeMounts(job["volume"], logger, name)
		volsFrom, vfSafe := extractVolumesFromSpecs(job["volumes-from"], logger, name)
		if volSafe && vfSafe && len(hostMounts) == 0 && len(volsFrom) == 0 {
			filtered[name] = job
			continue
		}
		var vectors []string
		if len(hostMounts) > 0 {
			vectors = append(vectors, fmt.Sprintf("volume=%q", hostMounts))
		}
		if len(volsFrom) > 0 {
			vectors = append(vectors, fmt.Sprintf("volumes-from=%q", volsFrom))
		}
		if !volSafe {
			vectors = append(vectors, "volume=<unexpected-type>")
		}
		if !vfSafe {
			vectors = append(vectors, "volumes-from=<unexpected-type>")
		}
		logger.Error(fmt.Sprintf("SECURITY POLICY VIOLATION: dropping %s %q with host-escalation vectors %v from container labels. "+
			"Host bind mounts and volumes-from inheritance enable container-to-host privilege escalation "+
			"(volumes-from inherits the donor container's mounts, e.g. /var/run/docker.sock). "+
			"Set [global] allow-host-jobs-from-labels=true in INI to permit (NOT recommended for multi-tenant hosts). "+
			"See https://github.com/netresearch/ofelia/issues/462.",
			jobType, name, vectors))
	}
	return filtered
}

// extractHostVolumeMounts walks the "volume" param value and returns the
// host-mount specs it contains. The bool return is true when the value's
// type was recognized; false signals a fail-closed condition (an
// unexpected type should drop the job rather than silently bypass the
// security check).
//
// Recognized shapes:
//   - nil          -> ([], true)   no volumes declared
//   - string       -> per-spec scan
//   - []string     -> per-spec scan
//   - anything else -> ([], false) — fail closed, log at WARN
func extractHostVolumeMounts(v any, logger *slog.Logger, jobName string) ([]string, bool) {
	if v == nil {
		return nil, true
	}
	var hostMounts []string
	switch specs := v.(type) {
	case string:
		if isHostVolumeMount(specs) {
			hostMounts = append(hostMounts, specs)
		}
		return hostMounts, true
	case []string:
		for _, spec := range specs {
			if isHostVolumeMount(spec) {
				hostMounts = append(hostMounts, spec)
			}
		}
		return hostMounts, true
	default:
		logger.Warn(fmt.Sprintf("unexpected type %T for job-run %q volume key; treating as security violation (fail-closed). "+
			"This usually means a code change broke the setJobParam contract — please file an issue.",
			v, jobName))
		return nil, false
	}
}

// extractVolumesFromSpecs walks the "volumes-from" param value and
// returns the donor container references it contains. Same shape
// handling and fail-closed contract as extractHostVolumeMounts. Any
// non-empty value is a violation because we cannot inspect the donor's
// mounts at filter time — a donor with a host bind would silently
// inherit those mounts into the spawned container.
func extractVolumesFromSpecs(v any, logger *slog.Logger, jobName string) ([]string, bool) {
	if v == nil {
		return nil, true
	}
	switch specs := v.(type) {
	case string:
		if specs == "" {
			return nil, true
		}
		return []string{specs}, true
	case []string:
		// Filter out empty strings the JSON array shorthand might have
		// produced; an empty donor name is a no-op for Docker.
		out := make([]string, 0, len(specs))
		for _, s := range specs {
			if s != "" {
				out = append(out, s)
			}
		}
		return out, true
	default:
		logger.Warn(fmt.Sprintf("unexpected type %T for job-run %q volumes-from key; treating as security violation (fail-closed). "+
			"This usually means a code change broke the setJobParam contract — please file an issue.",
			v, jobName))
		return nil, false
	}
}
