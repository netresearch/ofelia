package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gobs/args"

	"github.com/netresearch/ofelia/core/domain"
)

type RunJob struct {
	BareJob  `mapstructure:",squash"`
	Provider DockerProvider `json:"-"` // SDK-based Docker provider
	// User specifies the user to run the container as.
	// If not set, uses the global default-user setting (default: "nobody").
	// Set to "default" to explicitly use the container's default user, overriding global setting.
	User string `hash:"true"`

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

func NewRunJob(provider DockerProvider) *RunJob {
	return &RunJob{
		Provider: provider,
	}
}

// InitializeRuntimeFields initializes fields that depend on the Docker provider.
// This should be called after the Provider field is set.
func (j *RunJob) InitializeRuntimeFields() {
	// No additional initialization needed with DockerProvider
}

// Validate checks that the job configuration is valid.
// For job-run, either Image or Container must be specified.
func (j *RunJob) Validate() error {
	if j.Image == "" && j.Container == "" {
		return errors.New("job-run requires either 'image' (to create a new container) or 'container' (to start an existing container)")
	}
	return nil
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
	bgCtx := context.Background()

	if j.Image != "" && j.Container == "" {
		if err := j.ensureImageAvailable(bgCtx, ctx, pull); err != nil {
			return err
		}
	}

	containerID, err := j.createOrInspectContainer(bgCtx)
	if err != nil {
		return err
	}
	j.setContainerID(containerID)

	created := j.Container == ""
	if created {
		defer func() {
			if delErr := j.deleteContainer(bgCtx); delErr != nil {
				ctx.Warn("failed to delete container: " + delErr.Error())
			}
		}()
	}

	return j.startAndWait(bgCtx, ctx)
}

// ensureImageAvailable pulls or verifies the image presence according to Pull option.
func (j *RunJob) ensureImageAvailable(ctx context.Context, jobCtx *Context, pull bool) error {
	if err := j.Provider.EnsureImage(ctx, j.Image, pull); err != nil {
		return fmt.Errorf("ensuring image: %w", err)
	}

	jobCtx.Log("Image " + j.Image + " is available")
	return nil
}

// createOrInspectContainer creates a new container when needed or inspects an existing one.
func (j *RunJob) createOrInspectContainer(ctx context.Context) (string, error) {
	if j.Image != "" && j.Container == "" {
		return j.buildContainer(ctx)
	}

	container, err := j.Provider.InspectContainer(ctx, j.Container)
	if err != nil {
		return "", fmt.Errorf("inspecting container: %w", err)
	}
	return container.ID, nil
}

// startAndWait starts the container, waits for completion and tails logs.
func (j *RunJob) startAndWait(ctx context.Context, jobCtx *Context) error {
	startTime := time.Now()
	if err := j.startContainer(ctx); err != nil {
		return err
	}

	// Create a context with timeout if MaxRuntime is set
	watchCtx := ctx
	var cancel context.CancelFunc
	if j.MaxRuntime > 0 {
		watchCtx, cancel = context.WithTimeout(ctx, j.MaxRuntime)
		defer cancel()
	}

	err := j.watchContainer(watchCtx)
	if errors.Is(err, ErrUnexpected) {
		return err
	}

	// Get logs since start time
	logsOpts := ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Since:      startTime,
		Follow:     false,
	}
	reader, logsErr := j.Provider.GetContainerLogs(ctx, j.getContainerID(), logsOpts)
	if logsErr != nil {
		jobCtx.Warn("failed to fetch container logs: " + logsErr.Error())
	} else if reader != nil {
		defer reader.Close()
		// Stream logs to execution output
		buf := make([]byte, 32*1024)
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				_, _ = jobCtx.Execution.OutputStream.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
	}
	return err
}

func (j *RunJob) buildContainer(ctx context.Context) (string, error) {
	name := j.Name
	if j.ContainerName != nil {
		name = *j.ContainerName
	}

	// Merge user annotations with default Ofelia annotations
	defaults := getDefaultAnnotations(j.Name, "run")
	annotations := mergeAnnotations(j.Annotations, defaults)

	// Build container configuration using domain types
	config := &domain.ContainerConfig{
		Image:        j.Image,
		Cmd:          args.GetArgs(j.Command),
		Entrypoint:   entrypointSlice(j.Entrypoint),
		Env:          j.Environment,
		User:         j.User,
		Hostname:     j.Hostname,
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          j.TTY,
		Name:         name,
		Labels:       annotations,
		HostConfig: &domain.HostConfig{
			Binds: j.Volume,
		},
	}

	containerID, err := j.Provider.CreateContainer(ctx, config, name)
	if err != nil {
		return "", fmt.Errorf("creating container: %w", err)
	}

	// Connect to network if specified
	if j.Network != "" {
		networks, findErr := j.Provider.FindNetworkByName(ctx, j.Network)
		if findErr == nil {
			for _, network := range networks {
				if connErr := j.Provider.ConnectNetwork(ctx, network.ID, containerID); connErr != nil {
					return containerID, fmt.Errorf("connecting network: %w", connErr)
				}
			}
		}
	}

	return containerID, nil
}

func (j *RunJob) startContainer(ctx context.Context) error {
	if err := j.Provider.StartContainer(ctx, j.getContainerID()); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}
	return nil
}

//nolint:unused // used in integration tests via build tags
func (j *RunJob) stopContainer(ctx context.Context, timeout time.Duration) error {
	if err := j.Provider.StopContainer(ctx, j.getContainerID(), &timeout); err != nil {
		return fmt.Errorf("stopping container: %w", err)
	}
	return nil
}

//nolint:unused // used in integration tests via build tags
func (j *RunJob) getContainer(ctx context.Context) (*domain.Container, error) {
	container, err := j.Provider.InspectContainer(ctx, j.getContainerID())
	if err != nil {
		return nil, fmt.Errorf("getting container: %w", err)
	}
	return container, nil
}

func (j *RunJob) watchContainer(ctx context.Context) error {
	// Use Provider.WaitContainer for efficient waiting
	exitCode, err := j.Provider.WaitContainer(ctx, j.getContainerID())
	if err != nil {
		// Check if it's a context timeout/cancellation (MaxRuntime)
		if ctx.Err() != nil {
			return ErrMaxTimeRunning
		}
		return fmt.Errorf("waiting for container: %w", err)
	}

	switch exitCode {
	case 0:
		return nil
	case -1:
		return ErrUnexpected
	default:
		return NonZeroExitError{ExitCode: int(exitCode)}
	}
}

func (j *RunJob) deleteContainer(ctx context.Context) error {
	if shouldDelete, _ := strconv.ParseBool(j.Delete); !shouldDelete {
		return nil
	}

	if err := j.Provider.RemoveContainer(ctx, j.getContainerID(), false); err != nil {
		return fmt.Errorf("removing container: %w", err)
	}
	return nil
}
