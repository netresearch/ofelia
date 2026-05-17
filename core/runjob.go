// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

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
	EnvFile     []string `gcfg:"env-file" mapstructure:"env-file," hash:"true"`
	EnvFrom     []string `gcfg:"env-from" mapstructure:"env-from," hash:"true"`
	WorkingDir  string   `gcfg:"working-dir" mapstructure:"working-dir" hash:"true"`
	Annotations []string `mapstructure:"annotations" hash:"true"`

	// StopSignal is the signal sent to the main process when Ofelia
	// stops the container after execution. Accepts the canonical name
	// ("SIGINT") or the bare suffix ("INT"); empty defaults to whatever
	// the container image declared via STOPSIGNAL (which itself falls
	// back to SIGTERM). Useful for apps with signal handlers — Node.js
	// (SIGINT), Java thread-dump-on-quit (SIGQUIT), or custom cleanup
	// handlers (SIGUSR1/SIGUSR2). Requires Docker API v1.42+ (Docker
	// Engine 20.10+, released 2020-12). See
	// https://github.com/netresearch/ofelia/issues/234.
	StopSignal string `gcfg:"stop-signal" mapstructure:"stop-signal" hash:"true"`

	// StopTimeout is the grace period between the stop signal (see
	// StopSignal) and the daemon's escalation to SIGKILL. Honored only
	// by Ofelia's deadline-cleanup path today (cleanupOnDeadline);
	// other shutdown paths inherit the daemon's default (typically 10s,
	// matching the legacy hardcoded value this field replaces).
	// Zero leaves the previous 10s default in effect, so unconfigured
	// jobs see no behavior change. Pair with StopSignal to give apps
	// enough time to flush state before SIGKILL — Node.js graceful
	// shutdown often needs >10s, Java thread-dump on SIGQUIT can take
	// 5-30s depending on heap size. See #234.
	StopTimeout time.Duration `gcfg:"stop-timeout" mapstructure:"stop-timeout" hash:"true"`

	MaxRuntime time.Duration `gcfg:"max-runtime" mapstructure:"max-runtime"`

	containerID string
	mu          sync.RWMutex // Protect containerID access
}

func NewRunJob(provider DockerProvider) *RunJob {
	return &RunJob{
		Provider: provider,
	}
}

// GetMaxRuntime exposes the configured per-job maximum runtime so the
// scheduler can wrap the per-run context with a deadline (issue #638).
// A zero value means "no per-job override"; the scheduler falls back to
// its default bound (typically 24h, mirroring `[global] max-runtime`).
func (j *RunJob) GetMaxRuntime() time.Duration {
	return j.MaxRuntime
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
		return ErrImageOrContainer
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

// jobCleanupTimeout bounds best-effort container/service teardown when
// the per-run context has already expired (issue #655). 30s mirrors
// DefaultStopTimeout used elsewhere for graceful shutdown — long enough
// for a healthy daemon, short enough to fail loudly on a wedged one.
const jobCleanupTimeout = 30 * time.Second

func (j *RunJob) Run(ctx *Context) error {
	pull, _ := strconv.ParseBool(j.Pull)
	// Use the (deadline-bounded) middleware-chain context for cancellation
	// propagation. This ensures scheduler shutdown, job removal, and
	// scheduler-level max-runtime cancellation reach the Docker API
	// calls. The fallback to context.Background() lives in
	// (*Context).RunContext so legacy *Context{} literals stay safe.
	// See issue #638.
	runCtx := ctx.RunContext()

	if j.Image != "" && j.Container == "" {
		if err := j.ensureImageAvailable(runCtx, ctx, pull); err != nil {
			return err
		}
	}

	containerID, err := j.createOrInspectContainer(runCtx)
	if err != nil {
		return err
	}
	j.setContainerID(containerID)

	created := j.Container == ""
	if created {
		defer j.cleanupAfterRun(runCtx, ctx)
	}

	return j.startAndWait(runCtx, ctx)
}

// cleanupAfterRun handles deferred container teardown. When the per-run
// context already fired (wrapper-level deadline from boundJobContext or
// per-job MaxRuntime), WaitContainer returned early on cancellation so
// the container is almost certainly still running. Issue an explicit
// best-effort Stop on a fresh context BEFORE Remove so we don't race
// the daemon trying to delete a live container, and so the Remove call
// itself runs on a non-expired context. The fresh background-derived
// context is intentional: reusing the expired parent would no-op the
// cleanup, which is precisely the bug being fixed. See issue #655.
func (j *RunJob) cleanupAfterRun(runCtx context.Context, ctx *Context) {
	if runCtx.Err() == nil {
		// Happy path: parent context still alive — reuse it so any
		// caller-set deadline applies to teardown too.
		if delErr := j.deleteContainer(runCtx); delErr != nil {
			ctx.Warn("failed to delete container: " + delErr.Error())
		}
		return
	}
	// Parent expired — fall through to the fresh-context cleanup path.
	j.cleanupOnDeadline(runCtx, ctx)
}

// cleanupOnDeadline issues stop+remove on a fresh background-derived
// context because the per-run parent already expired. The expired
// runCtx is accepted for symmetry/diagnostics but intentionally NOT
// used as the parent of the new context — that would no-op the
// cleanup, which is precisely the bug being fixed. See issue #655.
//
//nolint:contextcheck // intentional fresh ctx; parent already expired
func (j *RunJob) cleanupOnDeadline(_ context.Context, ctx *Context) {
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), jobCleanupTimeout)
	defer cancelCleanup()
	stopTimeout := 10 * time.Second
	if j.StopTimeout > 0 {
		stopTimeout = j.StopTimeout
	}
	if stopErr := j.stopContainer(cleanupCtx, stopTimeout); stopErr != nil {
		ctx.Warn("failed to stop container after deadline: " + stopErr.Error())
	}
	if delErr := j.deleteContainer(cleanupCtx); delErr != nil {
		ctx.Warn("failed to delete container: " + delErr.Error())
	}
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

	// Resolve environment from env-file, env-from, and explicit environment
	mergedEnv, err := ResolveJobEnvironment(ctx, j.EnvFile, j.EnvFrom, j.Environment, j.Provider, nil)
	if err != nil {
		return "", err
	}

	// Build container configuration using domain types
	config := &domain.ContainerConfig{
		Image:        j.Image,
		Cmd:          args.GetArgs(j.Command),
		Entrypoint:   entrypointSlice(j.Entrypoint),
		Env:          mergedEnv,
		WorkingDir:   j.WorkingDir,
		User:         j.User,
		Hostname:     j.Hostname,
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          j.TTY,
		Name:         name,
		Labels:       annotations,
		HostConfig: &domain.HostConfig{
			Binds:       j.Volume,
			VolumesFrom: j.VolumesFrom,
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

func (j *RunJob) stopContainer(ctx context.Context, timeout time.Duration) error {
	opts := domain.StopOptions{Timeout: &timeout, Signal: j.StopSignal}
	if err := j.Provider.StopContainer(ctx, j.getContainerID(), opts); err != nil {
		return fmt.Errorf("stopping container: %w", err)
	}
	return nil
}

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
