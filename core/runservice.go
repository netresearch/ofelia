// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gobs/args"

	"github.com/netresearch/ofelia/core/domain"
)

// Note: The ServiceJob is loosely inspired by https://github.com/alexellis/jaas/

// RunServiceJob runs a command as a one-shot Docker Swarm service: it creates
// a service whose task has restart condition "none", polls that task until it
// reaches a terminal state, and removes the service again unless Delete is
// "false". The provider must be connected to a daemon acting as a Swarm
// manager. Only the task's exit code is observed — unlike RunJob, no
// container output is captured into the execution streams.
type RunServiceJob struct {
	BareJob  `mapstructure:",squash"`
	Provider DockerProvider `json:"-"` // SDK-based Docker provider
	// User specifies the user to run the service as.
	// If not set, uses the global default-user setting (default: "nobody").
	// Set to "default" to explicitly use the container's default user, overriding global setting.
	User string `hash:"true"`
	TTY  bool   `default:"false" hash:"true"`
	// do not use bool values with "default:true" because if
	// user would set it to "false" explicitly, it still will be
	// changed to "true" https://github.com/netresearch/ofelia/issues/135
	// so lets use strings here as workaround
	Delete      string        `default:"true" hash:"true"`
	Image       string        `hash:"true"`
	Network     string        `hash:"true"`
	Hostname    string        `hash:"true"`
	Dir         string        `hash:"true"`
	Volume      []string      `hash:"true"`
	Environment []string      `mapstructure:"environment" hash:"true"`
	EnvFile     []string      `gcfg:"env-file" mapstructure:"env-file," hash:"true"`
	EnvFrom     []string      `gcfg:"env-from" mapstructure:"env-from," hash:"true"`
	Annotations []string      `mapstructure:"annotations" hash:"true"`
	MaxRuntime  time.Duration `gcfg:"max-runtime" mapstructure:"max-runtime"`
}

// NewRunServiceJob returns a RunServiceJob bound to provider, which is the
// Docker connection the swarm service is created and polled on. The caller
// sets the remaining fields; Image is mandatory (see Validate).
func NewRunServiceJob(provider DockerProvider) *RunServiceJob {
	return &RunServiceJob{Provider: provider}
}

// GetMaxRuntime exposes the configured per-job maximum runtime so the
// scheduler can wrap the per-run context with a deadline (issue #638).
// A zero value means "no per-job override"; the scheduler falls back to
// its default bound (typically 24h, mirroring `[global] max-runtime`).
func (j *RunServiceJob) GetMaxRuntime() time.Duration {
	return j.MaxRuntime
}

// InitializeRuntimeFields initializes fields that depend on the Docker provider.
// This should be called after the Provider field is set.
func (j *RunServiceJob) InitializeRuntimeFields() {
	// No additional initialization needed with DockerProvider
}

// Validate checks that the job configuration is valid.
// For job-service-run, Image is required.
func (j *RunServiceJob) Validate() error {
	if j.Image == "" {
		return ErrImageRequired
	}
	return nil
}

// Run pulls the image, creates the swarm service and blocks while polling its
// tasks every 500ms until one reaches a terminal state. It returns nil for
// task exit code 0, ErrUnexpected for -1 and for ExitCodeSwarmError (no
// terminal task observed), NonZeroExitError otherwise, and ErrMaxTimeRunning
// when MaxRuntime elapses first.
//
// The service is removed before Run returns unless Delete is "false"; if the
// run context has already expired, removal runs on a fresh context
// (jobCleanupTimeout) so no orphan service is left scheduled — see issue
// #655. A failed removal is returned as the error only when the run itself
// succeeded; otherwise the run's own error wins and the removal failure is
// logged.
func (j *RunServiceJob) Run(ctx *Context) error {
	// Use the (deadline-bounded) middleware-chain context for cancellation
	// propagation. The fallback to context.Background() lives in
	// (*Context).RunContext so legacy *Context{} literals stay safe.
	// See issue #638.
	runCtx := ctx.RunContext()

	// Pull image using the provider
	if err := j.Provider.EnsureImage(runCtx, j.Image, true); err != nil {
		return fmt.Errorf("ensuring image: %w", err)
	}

	svcID, err := j.buildService(runCtx)
	if err != nil {
		return err
	}

	ctx.Logger.Info(fmt.Sprintf("Created service %s for job %s", svcID, j.Name))

	watchErr := j.watchContainer(runCtx, ctx, svcID)

	// If the per-run context already fired (wrapper-level deadline from
	// boundJobContext / per-job MaxRuntime), the swarm service is left
	// scheduled because the SDK calls bailed out on cancellation rather
	// than completing teardown. Issue a best-effort RemoveService on a
	// fresh context so the operator does not get a phantom task. See
	// issue #655.
	deleteCtx := runCtx
	if runCtx.Err() != nil {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), jobCleanupTimeout)
		defer cancelCleanup()
		deleteCtx = cleanupCtx
	}

	if delErr := j.deleteService(deleteCtx, ctx, svcID); delErr != nil {
		if watchErr == nil {
			return delErr
		}
		// watchErr (typically MaxRuntime / cancellation) takes precedence as
		// the surfaced return; log delErr so a failed RemoveService isn't
		// silently swallowed and orphan services are observable.
		ctx.Log(fmt.Sprintf("RunServiceJob: cleanup deleteService failed (watchErr=%v): %v", watchErr, delErr))
	}
	return watchErr
}

func (j *RunServiceJob) buildService(ctx context.Context) (string, error) {
	maxAttempts := uint64(1)

	// Add annotations as service labels (swarm services use Labels for metadata)
	defaults := getDefaultAnnotations(j.Name, "service")
	annotations := mergeAnnotations(j.Annotations, defaults)

	// Resolve environment from env-file, env-from, and explicit environment
	mergedEnv, err := ResolveJobEnvironment(ctx, j.EnvFile, j.EnvFrom, j.Environment, j.Provider, nil)
	if err != nil {
		return "", err
	}

	spec := domain.ServiceSpec{
		Labels: annotations,
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image:    j.Image,
				Env:      mergedEnv,
				User:     j.User,
				Hostname: j.Hostname,
				Dir:      j.Dir,
				TTY:      j.TTY,
			},
			RestartPolicy: &domain.ServiceRestartPolicy{
				Condition:   domain.RestartConditionNone,
				MaxAttempts: &maxAttempts,
			},
		},
	}

	// Convert volume bind strings to service mounts
	for _, v := range j.Volume {
		m, err := parseVolumeMount(v)
		if err != nil {
			return "", fmt.Errorf("volume config: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Mounts = append(
			spec.TaskTemplate.ContainerSpec.Mounts, m)
	}

	// For a service to interact with other services in a stack,
	// we need to attach it to the same network
	if j.Network != "" {
		spec.TaskTemplate.Networks = []domain.NetworkAttachment{
			{Target: j.Network},
		}
	}

	if j.Command != "" {
		spec.TaskTemplate.ContainerSpec.Command = args.GetArgs(j.Command)
	}

	serviceID, err := j.Provider.CreateService(ctx, spec, domain.ServiceCreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create service: %w", err)
	}

	return serviceID, nil
}

// Exit codes for swarm service execution states
// These are Ofelia-specific codes, not from Docker Swarm API
// They indicate failure modes that don't map to container exit codes
const (
	// ExitCodeSwarmError marks a swarm orchestration error (task not found,
	// service unavailable) rather than an exit code reported by a task. It is
	// also the initial value the watcher holds, so it is what remains when the
	// watcher stops before any task reached a terminal state; watchContainer
	// maps it to ErrUnexpected.
	ExitCodeSwarmError = -999
	// ExitCodeTimeout marks max runtime exceeded before task completion. No
	// code path assigns it today — the timeout path returns ErrMaxTimeRunning
	// instead of an exit code.
	ExitCodeTimeout = -998
)

func (j *RunServiceJob) watchContainer(ctx context.Context, jobCtx *Context, svcID string) error {
	exitCode := ExitCodeSwarmError

	jobCtx.Logger.Info(fmt.Sprintf("Checking for service ID %s (%s) termination", svcID, j.Name))

	svc, err := j.Provider.InspectService(ctx, svcID)
	if err != nil {
		return fmt.Errorf("inspect service %s: %w", svcID, err)
	}

	startTime := time.Now()

	const watchDuration = time.Millisecond * 500 // Optimized from 100ms to reduce CPU usage
	ticker := time.NewTicker(watchDuration)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer func() {
			ticker.Stop()
			wg.Done()
		}()
		for {
			// Honor the wrapper-level deadline (#651's boundJobContext)
			// in addition to the per-job MaxRuntime check below — the
			// global default of 24h means any service job left running
			// past that bound used to spin in this loop forever even
			// though the SDK calls themselves had bailed. See #655.
			select {
			case <-ctx.Done():
				err = ctx.Err()
				return
			case <-ticker.C:
			}

			if j.MaxRuntime > 0 && time.Since(startTime) > j.MaxRuntime {
				err = ErrMaxTimeRunning
				return
			}

			taskExitCode, found := j.findTaskStatus(ctx, jobCtx, svc.ID)
			if found {
				exitCode = taskExitCode
				return
			}
		}
	}()

	wg.Wait()

	// When the wrapper-level deadline fires before the watcher observes a
	// terminal task state, exitCode is left at the ExitCodeSwarmError sentinel
	// — the watcher returned via ctx.Done(), not via observed completion.
	// Logging "completed with exit code -999" misleads operators into
	// thinking the swarm task itself failed; surface the cancellation
	// reason instead. See PR #659 review on exit-code log line at line 216.
	if err != nil {
		jobCtx.Logger.Info(fmt.Sprintf("Service ID %s (%s) was canceled before completion: %v", svcID, j.Name, err))
		return err
	}

	jobCtx.Logger.Info(fmt.Sprintf("Service ID %s (%s) has completed with exit code %d", svcID, j.Name, exitCode))

	switch exitCode {
	case 0:
		return nil
	case -1, ExitCodeSwarmError:
		return ErrUnexpected
	default:
		return NonZeroExitError{ExitCode: exitCode}
	}
}

func (j *RunServiceJob) findTaskStatus(ctx context.Context, jobCtx *Context, serviceID string) (int, bool) {
	taskFilters := map[string][]string{
		"service": {serviceID},
	}

	tasks, err := j.Provider.ListTasks(ctx, domain.TaskListOptions{
		Filters: taskFilters,
	})
	if err != nil {
		jobCtx.Logger.Error(fmt.Sprintf("Failed to find task for service %s. Considering the task terminated: %s", serviceID, err.Error()))
		return 0, false
	}

	if len(tasks) == 0 {
		// That task is gone now (maybe someone else removed it. Our work here is done
		return 0, true
	}

	exitCode := 1
	var done bool
	stopStates := []domain.TaskState{
		domain.TaskStateComplete,
		domain.TaskStateFailed,
		domain.TaskStateRejected,
	}

	for _, task := range tasks {

		stop := slices.Contains(stopStates, task.Status.State)

		if stop {
			if task.Status.ContainerStatus != nil {
				exitCode = task.Status.ContainerStatus.ExitCode
			}

			if exitCode == 0 && task.Status.State == domain.TaskStateRejected {
				exitCode = 255 // force non-zero exit for task rejected
			}
			done = true
			break
		}
	}
	return exitCode, done
}

func (j *RunServiceJob) deleteService(ctx context.Context, jobCtx *Context, svcID string) error {
	if shouldDelete, _ := strconv.ParseBool(j.Delete); !shouldDelete {
		return nil
	}

	err := j.Provider.RemoveService(ctx, svcID)
	// Check if service was already removed (not found error)
	if err != nil {
		// Log warning but don't return error if service is already gone
		if isNotFoundError(err) {
			jobCtx.Logger.Warn(fmt.Sprintf("Service %s cannot be removed. An error may have happened, "+
				"or it might have been removed by another process", svcID))
			return nil
		}
		return fmt.Errorf("remove service %s: %w", svcID, err)
	}
	return nil
}

// isNotFoundError checks if the error indicates a resource was not found.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check for common "not found" error patterns
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such") || strings.Contains(errStr, "404")
}

// parseVolumeMount parses a Docker volume string (source:target[:ro|rw])
// into a domain.ServiceMount. Returns an error for malformed input.
// Sources starting with / or . are bind mounts; others are named volumes.
func parseVolumeMount(bind string) (domain.ServiceMount, error) {
	parts := strings.SplitN(bind, ":", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return domain.ServiceMount{}, fmt.Errorf("%w: %q", ErrInvalidVolume, bind)
	}

	m := domain.ServiceMount{
		Type:   domain.MountTypeBind,
		Source: parts[0],
		Target: parts[1],
	}
	if len(parts) >= 3 {
		for opt := range strings.SplitSeq(parts[2], ",") {
			if opt == "ro" {
				m.ReadOnly = true
			}
		}
	}
	// Paths (absolute or relative) are bind mounts; bare names are volumes
	if !strings.HasPrefix(m.Source, "/") && !strings.HasPrefix(m.Source, ".") {
		m.Type = domain.MountTypeVolume
	}
	return m, nil
}
