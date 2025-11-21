package core

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/gobs/args"
)

// Note: The ServiceJob is loosely inspired by https://github.com/alexellis/jaas/

type RunServiceJob struct {
	BareJob `mapstructure:",squash"`
	Client  *docker.Client `json:"-"`
	User    string         `default:"nobody" hash:"true"`
	TTY     bool           `default:"false" hash:"true"`
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

func NewRunServiceJob(c *docker.Client) *RunServiceJob {
	return &RunServiceJob{Client: c}
}

func (j *RunServiceJob) Run(ctx *Context) error {
	// Use Docker operations abstraction for image pulling
	dockerOps := NewDockerOperations(j.Client, ctx.Logger, nil)
	if ctx.Scheduler != nil && ctx.Scheduler.metricsRecorder != nil {
		dockerOps.metricsRecorder = ctx.Scheduler.metricsRecorder
	}

	imageOps := dockerOps.NewImageOperations()
	if err := imageOps.PullImage(j.Image); err != nil {
		return err
	}

	svc, err := j.buildService()
	if err != nil {
		return err
	}

	ctx.Logger.Noticef("Created service %s for job %s\n", svc.ID, j.Name)

	if err := j.watchContainer(ctx, svc.ID); err != nil {
		return err
	}

	return j.deleteService(ctx, svc.ID)
}

func (j *RunServiceJob) buildService() (*swarm.Service, error) {
	// createOptions := types.ServiceCreateOptions{}

	maxAttempts := uint64(1)
	createSvcOpts := docker.CreateServiceOptions{}

	createSvcOpts.ServiceSpec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Image: j.Image,
	}

	// Add annotations as service labels (swarm services use Labels for metadata)
	defaults := getDefaultAnnotations(j.Name, "service")
	annotations := mergeAnnotations(j.Annotations, defaults)
	createSvcOpts.ServiceSpec.Labels = annotations

	// Make the service run once and not restart
	createSvcOpts.ServiceSpec.TaskTemplate.RestartPolicy = &swarm.RestartPolicy{
		MaxAttempts: &maxAttempts,
		Condition:   swarm.RestartPolicyConditionNone,
	}

	// For a service to interact with other services in a stack,
	// we need to attach it to the same network
	if j.Network != "" {
		// Prefer attaching via TaskTemplate Networks when available
		createSvcOpts.ServiceSpec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{
			{Target: j.Network},
		}
	}

	if j.Command != "" {
		createSvcOpts.ServiceSpec.TaskTemplate.ContainerSpec.Command = args.GetArgs(j.Command)
	}

	svc, err := j.Client.CreateService(createSvcOpts)
	if err != nil {
		return nil, fmt.Errorf("create service: %w", err)
	}

	return svc, nil
}

const (
	// Exit codes for swarm service execution states
	// These are Ofelia-specific codes, not from Docker Swarm API
	// They indicate failure modes that don't map to container exit codes
	ExitCodeSwarmError = -999 // Swarm orchestration error (task not found, service unavailable)
	ExitCodeTimeout    = -998 // Max runtime exceeded before task completion
)

func (j *RunServiceJob) watchContainer(ctx *Context, svcID string) error {
	exitCode := ExitCodeSwarmError

	ctx.Logger.Noticef("Checking for service ID %s (%s) termination\n", svcID, j.Name)

	svc, err := j.Client.InspectService(svcID)
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

			taskExitCode, found := j.findTaskStatus(ctx, svc.ID)
			if found {
				exitCode = taskExitCode
				return
			}
		}
	}()

	wg.Wait()

	ctx.Logger.Noticef("Service ID %s (%s) has completed with exit code %d\n", svcID, j.Name, exitCode)
	return err
}

func (j *RunServiceJob) findTaskStatus(ctx *Context, taskID string) (int, bool) {
	taskFilters := make(map[string][]string)
	taskFilters["service"] = []string{taskID}

	tasks, err := j.Client.ListTasks(docker.ListTasksOptions{
		Filters: taskFilters,
	})
	if err != nil {
		ctx.Logger.Errorf("Failed to find task ID %s. Considering the task terminated: %s\n", taskID, err.Error())
		return 0, false
	}

	if len(tasks) == 0 {
		// That task is gone now (maybe someone else removed it. Our work here is done
		return 0, true
	}

	exitCode := 1
	var done bool
	stopStates := []swarm.TaskState{
		swarm.TaskStateComplete,
		swarm.TaskStateFailed,
		swarm.TaskStateRejected,
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

			exitCode = task.Status.ContainerStatus.ExitCode

			if exitCode == 0 && task.Status.State == swarm.TaskStateRejected {
				exitCode = 255 // force non-zero exit for task rejected
			}
			done = true
			break
		}
	}
	return exitCode, done
}

func (j *RunServiceJob) deleteService(ctx *Context, svcID string) error {
	if shouldDelete, _ := strconv.ParseBool(j.Delete); !shouldDelete {
		return nil
	}

	err := j.Client.RemoveService(docker.RemoveServiceOptions{
		ID: svcID,
	})

	var noSvc *docker.NoSuchService
	if errors.As(err, &noSvc) {
		ctx.Logger.Warningf("Service %s cannot be removed. An error may have happened, "+
			"or it might have been removed by another process", svcID)
		return nil
	}

	if err != nil {
		return fmt.Errorf("remove service %s: %w", svcID, err)
	}
	return nil
}
