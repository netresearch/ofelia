package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gobs/args"

	"github.com/netresearch/ofelia/core/domain"
)

// Note: The ServiceJob is loosely inspired by https://github.com/alexellis/jaas/

type RunServiceJob struct {
	BareJob  `mapstructure:",squash"`
	Provider DockerProvider `json:"-"` // SDK-based Docker provider
	// User specifies the user to run the service as.
	// If empty, uses the global default-user setting (default: "nobody").
	// Set explicitly to "" in config to use the container's default user.
	User string `hash:"true"`
	TTY  bool   `default:"false" hash:"true"`
	// do not use bool values with "default:true" because if
	// user would set it to "false" explicitly, it still will be
	// changed to "true" https://github.com/netresearch/ofelia/issues/135
	// so lets use strings here as workaround
	Delete      string        `default:"true" hash:"true"`
	Image       string        `hash:"true"`
	Network     string        `hash:"true"`
	Annotations []string      `mapstructure:"annotations" hash:"true"`
	MaxRuntime  time.Duration `gcfg:"max-runtime" mapstructure:"max-runtime"`
}

func NewRunServiceJob(provider DockerProvider) *RunServiceJob {
	return &RunServiceJob{Provider: provider}
}

// InitializeRuntimeFields initializes fields that depend on the Docker provider.
// This should be called after the Provider field is set.
func (j *RunServiceJob) InitializeRuntimeFields() {
	// No additional initialization needed with DockerProvider
}

func (j *RunServiceJob) Run(ctx *Context) error {
	bgCtx := context.Background()

	// Pull image using the provider
	if err := j.Provider.EnsureImage(bgCtx, j.Image, true); err != nil {
		return fmt.Errorf("ensuring image: %w", err)
	}

	svcID, err := j.buildService(bgCtx)
	if err != nil {
		return err
	}

	ctx.Logger.Noticef("Created service %s for job %s\n", svcID, j.Name)

	if err := j.watchContainer(bgCtx, ctx, svcID); err != nil {
		return err
	}

	return j.deleteService(bgCtx, ctx, svcID)
}

func (j *RunServiceJob) buildService(ctx context.Context) (string, error) {
	maxAttempts := uint64(1)

	// Add annotations as service labels (swarm services use Labels for metadata)
	defaults := getDefaultAnnotations(j.Name, "service")
	annotations := mergeAnnotations(j.Annotations, defaults)

	spec := domain.ServiceSpec{
		Labels: annotations,
		TaskTemplate: domain.TaskSpec{
			ContainerSpec: domain.ContainerSpec{
				Image: j.Image,
				User:  j.User,
				TTY:   j.TTY,
			},
			RestartPolicy: &domain.ServiceRestartPolicy{
				Condition:   domain.RestartConditionNone,
				MaxAttempts: &maxAttempts,
			},
		},
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

const (
	// Exit codes for swarm service execution states
	// These are Ofelia-specific codes, not from Docker Swarm API
	// They indicate failure modes that don't map to container exit codes
	ExitCodeSwarmError = -999 // Swarm orchestration error (task not found, service unavailable)
	ExitCodeTimeout    = -998 // Max runtime exceeded before task completion
)

func (j *RunServiceJob) watchContainer(ctx context.Context, jobCtx *Context, svcID string) error {
	exitCode := ExitCodeSwarmError

	jobCtx.Logger.Noticef("Checking for service ID %s (%s) termination\n", svcID, j.Name)

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
		for range ticker.C {
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

	jobCtx.Logger.Noticef("Service ID %s (%s) has completed with exit code %d\n", svcID, j.Name, exitCode)
	return err
}

func (j *RunServiceJob) findTaskStatus(ctx context.Context, jobCtx *Context, serviceID string) (int, bool) {
	taskFilters := map[string][]string{
		"service": {serviceID},
	}

	tasks, err := j.Provider.ListTasks(ctx, domain.TaskListOptions{
		Filters: taskFilters,
	})
	if err != nil {
		jobCtx.Logger.Errorf("Failed to find task for service %s. Considering the task terminated: %s\n", serviceID, err.Error())
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

		stop := false
		for _, stopState := range stopStates {
			if task.Status.State == stopState {
				stop = true
				break
			}
		}

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
			jobCtx.Logger.Warningf("Service %s cannot be removed. An error may have happened, "+
				"or it might have been removed by another process", svcID)
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
