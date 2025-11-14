package core

import (
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/gobs/args"
)

var dockercfg *docker.AuthConfigurations

func init() {
	dockercfg, _ = docker.NewAuthConfigurationsFromDockerCfg()
}

type RunJob struct {
	BareJob   `mapstructure:",squash"`
	Client    *docker.Client    `json:"-"`
	monitor   *ContainerMonitor `json:"-"` // Container monitor for efficient watching
	dockerOps *DockerOperations `json:"-"` // High-level Docker operations wrapper
	User      string            `default:"nobody" hash:"true"`

	// ContainerName specifies the name of the container to be created. If
	// nil, the job name will be used. If set to an empty string, Docker
	// will assign a random name.
	ContainerName *string `gcfg:"container-name" mapstructure:"container-name" hash:"true"`

	TTY bool `default:"false" hash:"true"`

	// do not use bool values with "default:true" because if
	// user would set it to "false" explicitly, it still will be
	// changed to "true" https://github.com/netresearch/ofelia/issues/135
	// so lets use strings here as workaround
	Delete string `default:"true" hash:"true"`
	Pull   string `default:"true" hash:"true"`

	Image       string   `hash:"true"`
	Network     string   `hash:"true"`
	Hostname    string   `hash:"true"`
	Entrypoint  *string  `hash:"true"`
	Container   string   `hash:"true"`
	Volume      []string `hash:"true"`
	VolumesFrom []string `gcfg:"volumes-from" mapstructure:"volumes-from," hash:"true"`
	Environment []string `mapstructure:"environment" hash:"true"`
	Annotations []string `mapstructure:"annotations" hash:"true"`

	MaxRuntime time.Duration `gcfg:"max-runtime" mapstructure:"max-runtime"`

	containerID string
	mu          sync.RWMutex // Protect containerID access
}

func NewRunJob(c *docker.Client) *RunJob {
	// Create a logger for the monitor (will be set properly when job runs)
	logger := &SimpleLogger{}
	monitor := NewContainerMonitor(c, logger)

	// Check for Docker events configuration
	if useEvents := os.Getenv("OFELIA_USE_DOCKER_EVENTS"); useEvents != "" {
		// Default is true, so only disable if explicitly set to false
		if useEvents == "false" || useEvents == "0" || useEvents == "no" {
			monitor.SetUseEventsAPI(false)
		}
	}

	// Initialize Docker operations wrapper
	dockerOps := NewDockerOperations(c, logger, nil)

	return &RunJob{
		Client:    c,
		monitor:   monitor,
		dockerOps: dockerOps,
	}
}

// InitializeRuntimeFields initializes fields that depend on the Docker client
// This should be called after the Client field is set, typically during configuration loading
func (j *RunJob) InitializeRuntimeFields() {
	if j.Client == nil {
		return // Cannot initialize without client
	}

	// Only initialize if not already done
	if j.monitor == nil {
		logger := &SimpleLogger{} // Will be set properly when job runs
		j.monitor = NewContainerMonitor(j.Client, logger)

		// Check for Docker events configuration
		if useEvents := os.Getenv("OFELIA_USE_DOCKER_EVENTS"); useEvents != "" {
			// Default is true, so only disable if explicitly set to false
			if useEvents == "false" || useEvents == "0" || useEvents == "no" {
				j.monitor.SetUseEventsAPI(false)
			}
		}
	}

	if j.dockerOps == nil {
		logger := &SimpleLogger{} // Will be set properly when job runs
		j.dockerOps = NewDockerOperations(j.Client, logger, nil)
	}
}

func (j *RunJob) setContainerID(id string) {
	j.mu.Lock()
	j.containerID = id
	j.mu.Unlock()
}

func (j *RunJob) getContainerID() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.containerID
}

func entrypointSlice(ep *string) []string {
	if ep == nil {
		return nil
	}
	return args.GetArgs(*ep)
}

func (j *RunJob) Run(ctx *Context) error {
	pull, _ := strconv.ParseBool(j.Pull)

	if j.Image != "" && j.Container == "" {
		if err := j.ensureImageAvailable(ctx, pull); err != nil {
			return err
		}
	}

	container, err := j.createOrInspectContainer()
	if err != nil {
		return err
	}
	if container != nil {
		j.setContainerID(container.ID)
	}

	created := j.Container == ""
	if created {
		defer func() {
			if delErr := j.deleteContainer(); delErr != nil {
				ctx.Warn("failed to delete container: " + delErr.Error())
			}
		}()
	}

	return j.startAndWait(ctx)
}

// ensureImageAvailable pulls or verifies the image presence according to Pull option.
func (j *RunJob) ensureImageAvailable(ctx *Context, pull bool) error {
	// Update dockerOps with current context logger and metrics
	imageOps := j.dockerOps.NewImageOperations()
	imageOps.logger = ctx.Logger
	if ctx.Scheduler != nil && ctx.Scheduler.metricsRecorder != nil {
		imageOps.metricsRecorder = ctx.Scheduler.metricsRecorder
	}

	if err := imageOps.EnsureImage(j.Image, pull); err != nil {
		return err
	}

	ctx.Log("Image " + j.Image + " is available")
	return nil
}

// createOrInspectContainer creates a new container when needed or inspects an existing one.
func (j *RunJob) createOrInspectContainer() (*docker.Container, error) {
	if j.Image != "" && j.Container == "" {
		return j.buildContainer()
	}

	containerOps := j.dockerOps.NewContainerLifecycle()
	return containerOps.InspectContainer(j.Container)
}

// startAndWait starts the container, waits for completion and tails logs.
func (j *RunJob) startAndWait(ctx *Context) error {
	startTime := time.Now()
	if err := j.startContainer(); err != nil {
		return err
	}
	err := j.watchContainer()
	if errors.Is(err, ErrUnexpected) {
		return err
	}
	logsOps := j.dockerOps.NewLogsOperations()
	if logsErr := logsOps.GetLogsSince(j.getContainerID(), startTime, true, true,
		ctx.Execution.OutputStream, ctx.Execution.ErrorStream); logsErr != nil {
		ctx.Warn("failed to fetch container logs: " + logsErr.Error())
	}
	return err
}

func (j *RunJob) buildContainer() (*docker.Container, error) {
	name := j.Name
	if j.ContainerName != nil {
		name = *j.ContainerName
	}

	// Merge user annotations with default Ofelia annotations
	defaults := getDefaultAnnotations(j.Name, "run")
	annotations := mergeAnnotations(j.Annotations, defaults)

	containerOps := j.dockerOps.NewContainerLifecycle()
	opts := docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image:        j.Image,
			AttachStdin:  false,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          j.TTY,
			Cmd:          args.GetArgs(j.Command),
			Entrypoint:   entrypointSlice(j.Entrypoint),
			User:         j.User,
			Env:          j.Environment,
			Hostname:     j.Hostname,
		},
		NetworkingConfig: &docker.NetworkingConfig{},
		HostConfig: &docker.HostConfig{
			Binds:       j.Volume,
			VolumesFrom: j.VolumesFrom,
			Annotations: annotations,
		},
	}

	c, err := containerOps.CreateContainer(opts)
	if err != nil {
		return c, err
	}

	// Connect to network if specified
	if j.Network != "" {
		networkOps := j.dockerOps.NewNetworkOperations()
		networks, err := networkOps.FindNetworkByName(j.Network)
		if err == nil {
			for _, network := range networks {
				if err := networkOps.ConnectContainerToNetwork(c.ID, network.ID); err != nil {
					return c, err
				}
			}
		}
	}

	return c, nil
}

func (j *RunJob) startContainer() error {
	containerOps := j.dockerOps.NewContainerLifecycle()
	return containerOps.StartContainer(j.getContainerID(), &docker.HostConfig{})
}

//nolint:unused // used in integration tests via build tags
func (j *RunJob) stopContainer(timeout uint) error {
	containerOps := j.dockerOps.NewContainerLifecycle()
	return containerOps.StopContainer(j.getContainerID(), timeout)
}

//nolint:unused // used in integration tests via build tags
func (j *RunJob) getContainer() (*docker.Container, error) {
	containerOps := j.dockerOps.NewContainerLifecycle()
	return containerOps.InspectContainer(j.getContainerID())
}

func (j *RunJob) watchContainer() error {
	// Use the efficient container monitor
	if j.monitor == nil {
		// Fallback to old polling method if monitor not available
		return j.watchContainerLegacy()
	}

	state, err := j.monitor.WaitForContainer(j.getContainerID(), j.MaxRuntime)
	if err != nil {
		return err
	}

	switch state.ExitCode {
	case 0:
		return nil
	case -1:
		return ErrUnexpected
	default:
		return NonZeroExitError{ExitCode: state.ExitCode}
	}
}

// watchContainerLegacy is the old polling method kept for backward compatibility
func (j *RunJob) watchContainerLegacy() error {
	const watchDuration = time.Millisecond * 500 // Optimized from 100ms to reduce CPU usage
	var s docker.State
	var r time.Duration
	for {
		time.Sleep(watchDuration)
		r += watchDuration

		if j.MaxRuntime > 0 && r > j.MaxRuntime {
			return ErrMaxTimeRunning
		}

		containerOps := j.dockerOps.NewContainerLifecycle()
		c, err := containerOps.InspectContainer(j.getContainerID())
		if err != nil {
			return err
		}

		if !c.State.Running {
			s = c.State
			break
		}
	}

	switch s.ExitCode {
	case 0:
		return nil
	case -1:
		return ErrUnexpected
	default:
		return NonZeroExitError{ExitCode: s.ExitCode}
	}
}

func (j *RunJob) deleteContainer() error {
	if shouldDelete, _ := strconv.ParseBool(j.Delete); !shouldDelete {
		return nil
	}

	containerOps := j.dockerOps.NewContainerLifecycle()
	return containerOps.RemoveContainer(j.getContainerID(), false)
}
