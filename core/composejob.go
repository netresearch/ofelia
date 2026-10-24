// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gobs/args"

	"github.com/netresearch/ofelia/config"
	dockeradapter "github.com/netresearch/ofelia/core/adapters/docker"
	"github.com/netresearch/ofelia/core/domain"
)

// ComposeExecutionLabel is the container label a `compose run` job sets to
// the execution ID, so cancellation can find the run container — and only
// that one, never a dependency service Compose started alongside it.
const ComposeExecutionLabel = "ofelia.execution"

// ErrComposeTerminationUnconfirmed reports that a canceled `compose run`
// may still be running: Ofelia stopped waiting, but could not confirm
// through the Docker API that the run container is gone.
var ErrComposeTerminationUnconfirmed = errors.New("compose run termination unconfirmed")

var (
	errComposeNoProvider       = errors.New("no Docker provider configured")
	errComposeContainersRemain = errors.New("run containers still present")
)

const (
	// composeStopTimeout is the grace period between the stop signal and
	// SIGKILL when a canceled run container is stopped — the same 10s
	// default RunJob uses when stop-timeout is unset.
	composeStopTimeout = 10 * time.Second
	// composeCLIExitTimeout bounds the wait for the compose CLI to exit
	// after SIGTERM before it is killed.
	composeCLIExitTimeout = 10 * time.Second
	// composeVerifyTimeout bounds the wait for the run containers to
	// disappear after removal was requested.
	composeVerifyTimeout  = 5 * time.Second
	composeVerifyInterval = 250 * time.Millisecond
)

// composeWaitDelay bounds how long Wait keeps copying output after the CLI
// exited, when a process it started still holds the output pipes. A var so
// tests can shorten it.
var composeWaitDelay = composeCLIExitTimeout

// ComposeJob runs a command through the `docker compose` CLI on the host
// Ofelia itself runs on — `compose run --rm <service>` by default, or
// `compose exec <service>` when Exec is set. Unlike the other job types it
// shells out instead of talking to the Docker API, so the docker binary
// must be on Ofelia's PATH. Provider is used only to find and stop the run
// container when a `compose run` execution is canceled.
type ComposeJob struct {
	BareJob  `mapstructure:",squash"`
	Provider DockerProvider `json:"-"`
	File     string         `default:"compose.yml" gcfg:"file" mapstructure:"file" hash:"true"`
	Service  string         `gcfg:"service" mapstructure:"service" hash:"true"`
	Exec     bool           `default:"false" gcfg:"exec" mapstructure:"exec" hash:"true"`
	// Project is set only for jobs loaded from a registered Compose project
	// ([compose-project] in the INI, see ADR-004). It cannot be set on a
	// [job-compose] section: the project context lives in the registration.
	Project ComposeProjectContext `mapstructure:"-"`
}

// ComposeProjectContext is the project selection a job loaded from a
// registered Compose project runs with. When Files is set it replaces File,
// and Service names the Compose job. All fields take part in the job hash, so
// a changed project replaces the job on reload.
type ComposeProjectContext struct {
	Files            []string `hash:"true"`
	Profiles         []string `hash:"true"`
	EnvFiles         []string `hash:"true"`
	ProjectName      string   `hash:"true"`
	ProjectDirectory string   `hash:"true"`
	// Revision identifies the job's definition in the loaded configuration;
	// each execution logs it.
	Revision string `hash:"true"`
	// TimeZone is the zone the schedule is evaluated in, as resolved at load.
	TimeZone string `hash:"true"`
}

// NewComposeJob returns an empty ComposeJob; the caller fills in File,
// Service, Provider and the embedded BareJob.
func NewComposeJob() *ComposeJob { return &ComposeJob{} }

// Run executes the compose command and blocks until the child process exits.
// File, Service and the parsed command arguments are checked for command
// injection first, so a malformed job fails before any process is started.
// Errors cover a rejected argument, a docker binary missing from PATH, and a
// non-zero exit status. Output is streamed to the execution's stdout/stderr
// streams.
//
// In run mode the CLI is bound to the run context. Killing the CLI does not
// stop the container it started, so on cancellation or a max-runtime
// deadline Run stops the containers labeled with this execution's ID
// (ComposeExecutionLabel) with a 10s grace period, signals the CLI, removes
// the labeled containers again after the CLI exited — catching one created
// while cancellation was in progress — and confirms that none remain. It
// then returns ErrMaxTimeRunning, wrapped together with
// ErrComposeTerminationUnconfirmed when the Docker API could not confirm
// that the run container is gone.
//
// In exec mode the command runs inside an existing service container. The
// Docker Engine API has no way to stop an exec'd process from outside (see
// ExecJob and issue #655), so the child process is not tied to the run
// context: canceling the run, or a max-runtime deadline, does not stop it.
// Enforce a hard limit inside the command (e.g. `timeout 30s ...`).
func (j *ComposeJob) Run(ctx *Context) error {
	cmd, err := j.buildCommand(ctx)
	if err != nil {
		return fmt.Errorf("compose build command: %w", err)
	}
	if j.Project.Revision != "" {
		ctx.Log(fmt.Sprintf("compose project %s, job revision %s, schedule time zone %s",
			j.Project.ProjectName, j.Project.Revision, j.Project.TimeZone))
	}
	if j.Exec {
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("compose run: %w", err)
		}
		return nil
	}
	return j.runWithCancellation(ctx, cmd)
}

func (j *ComposeJob) runWithCancellation(ctx *Context, cmd *exec.Cmd) error {
	base := os.Environ()
	env, err := composeChildEnv(base)
	if err != nil {
		return fmt.Errorf("compose docker endpoint: %w", err)
	}
	cmd.Env = env
	if name := ignoredDockerContext(base); name != "" {
		ctx.Warn(fmt.Sprintf("compose run ignores Docker context %q and uses %s, the endpoint Ofelia's "+
			"Docker client uses; set DOCKER_HOST to choose the daemon", name, envValue(env, "DOCKER_HOST")))
	}

	// Without a bound, Wait — and the cancellation path that reads its result
	// after killing the CLI — blocks for as long as any process the CLI
	// started keeps the output pipes open.
	cmd.WaitDelay = composeWaitDelay
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("compose run: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	runCtx := ctx.RunContext()
	select {
	case err := <-done:
		return composeExitError(ctx, err)
	case <-runCtx.Done():
	}

	// The CLI may have exited in the same instant the context fired; a
	// finished run keeps its own result.
	select {
	case err := <-done:
		return composeExitError(ctx, err)
	default:
	}

	return j.cancelRun(ctx, cmd, done)
}

// composeExitError classifies the CLI's exit. exec.ErrWaitDelay means the CLI
// itself succeeded and only output still arriving after composeWaitDelay was
// dropped, so the run counts as successful with a warning.
func composeExitError(ctx *Context, err error) error {
	if errors.Is(err, exec.ErrWaitDelay) {
		ctx.Warn("compose run: output after the CLI exited was not collected: " + err.Error())
		return nil
	}
	if err != nil {
		return fmt.Errorf("compose run: %w", err)
	}
	return nil
}

// cancelRun stops the run container, ends the CLI and confirms cleanup. It
// uses a fresh context because the run context has already expired; reusing
// it would turn every cleanup call into an immediate failure (issue #655).
func (j *ComposeJob) cancelRun(ctx *Context, cmd *exec.Cmd, done <-chan error) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), jobCleanupTimeout+composeStopTimeout)
	defer cancel()

	label := ComposeExecutionLabel + "=" + ctx.Execution.ID
	var cleanupErrs []error

	// Stop first: once its container exits, the CLI returns on its own.
	if err := j.stopLabeled(cleanupCtx, ctx, label); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}

	if exitErr := stopCLI(cmd, done); exitErr != nil {
		ctx.Log("compose CLI exited after cancellation: " + exitErr.Error())
	}

	// A container created while cancellation was in progress shows up only
	// now; the CLI is gone, so nothing creates another one after this.
	if err := j.stopLabeled(cleanupCtx, ctx, label); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}
	if err := j.removeLabeled(cleanupCtx, label); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}

	if len(cleanupErrs) > 0 {
		return fmt.Errorf("%w: %w: %w", ErrMaxTimeRunning, ErrComposeTerminationUnconfirmed, errors.Join(cleanupErrs...))
	}
	return ErrMaxTimeRunning
}

// stopCLI asks the compose CLI to exit, kills it when it does not within
// composeCLIExitTimeout, and returns the CLI's exit error. After a
// cancellation that error describes the signal, not the job's outcome.
func stopCLI(cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case err := <-done:
		return err
	case <-time.After(composeCLIExitTimeout):
	}
	_ = cmd.Process.Kill()
	return <-done
}

func (j *ComposeJob) listLabeled(ctx context.Context, label string) ([]domain.Container, error) {
	if j.Provider == nil {
		return nil, errComposeNoProvider
	}
	containers, err := j.Provider.ListContainers(ctx, domain.ListOptions{
		All:     true,
		Filters: map[string][]string{"label": {label}},
	})
	if err != nil {
		return nil, fmt.Errorf("listing run containers: %w", err)
	}
	return containers, nil
}

func (j *ComposeJob) stopLabeled(cleanupCtx context.Context, ctx *Context, label string) error {
	containers, err := j.listLabeled(cleanupCtx, label)
	if err != nil {
		return err
	}
	timeout := composeStopTimeout
	var errs []error
	for _, c := range containers {
		ctx.Log("Stopping compose run container " + c.ID)
		err := j.Provider.StopContainer(cleanupCtx, c.ID, domain.StopOptions{Timeout: &timeout})
		if err != nil && !domain.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("stopping container %s: %w", c.ID, err))
		}
	}
	return errors.Join(errs...)
}

// removeLabeled removes the labeled containers that `--rm` has not removed
// yet and waits until the daemon lists none.
func (j *ComposeJob) removeLabeled(cleanupCtx context.Context, label string) error {
	deadline := time.Now().Add(composeVerifyTimeout)
	for {
		containers, err := j.listLabeled(cleanupCtx, label)
		if err != nil {
			return err
		}
		if len(containers) == 0 {
			return nil
		}
		for _, c := range containers {
			// Errors are not final here: a removal already in progress
			// (from `--rm`) conflicts, and the next listing decides.
			_ = j.Provider.RemoveContainer(cleanupCtx, c.ID, true)
		}
		if time.Now().After(deadline) {
			ids := make([]string, 0, len(containers))
			for _, c := range containers {
				ids = append(ids, c.ID)
			}
			return fmt.Errorf("%w: %s", errComposeContainersRemain, strings.Join(ids, ", "))
		}
		select {
		case <-cleanupCtx.Done():
			return fmt.Errorf("waiting for run container removal: %w", cleanupCtx.Err())
		case <-time.After(composeVerifyInterval):
		}
	}
}

// composeChildEnv returns the environment for the compose CLI. It points
// DOCKER_HOST at the endpoint Ofelia's own Docker client resolves, so the
// container the CLI starts lives on the daemon that cancellation searches.
// DOCKER_CONTEXT is dropped: the SDK client ignores Docker contexts, and the
// CLI would otherwise follow a context to another daemon. DOCKER_HOST takes
// precedence over a context set in the CLI's config file.
func composeChildEnv(base []string) ([]string, error) {
	host, err := dockeradapter.ResolveHost()
	if err != nil {
		return nil, fmt.Errorf("resolving Docker host: %w", err)
	}
	host, tlsVerify := cliDockerHost(host)

	env := make([]string, 0, len(base)+2)
	hasTLSVerify := false
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "DOCKER_HOST", "DOCKER_CONTEXT":
			continue
		case "DOCKER_TLS_VERIFY":
			hasTLSVerify = true
		}
		env = append(env, kv)
	}
	env = append(env, "DOCKER_HOST="+host)
	if tlsVerify && !hasTLSVerify {
		env = append(env, "DOCKER_TLS_VERIFY=1")
	}
	return env, nil
}

// ignoredDockerContext returns the Docker context the CLI would have followed
// with the environment base, or "" when there is none. The CLI follows a
// context only when DOCKER_HOST is unset: DOCKER_CONTEXT first, then
// currentContext in its config file. composeChildEnv always sets DOCKER_HOST,
// so such a context is ignored, and the caller warns about it.
func ignoredDockerContext(base []string) string {
	if envValue(base, "DOCKER_HOST") != "" {
		return ""
	}
	if name := envValue(base, "DOCKER_CONTEXT"); name != "" {
		if name == "default" {
			return ""
		}
		return name
	}
	dir := envValue(base, "DOCKER_CONFIG")
	if dir == "" {
		home := envValue(base, "HOME")
		if home == "" {
			return ""
		}
		dir = filepath.Join(home, ".docker")
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json")) // #nosec G304 -- the docker CLI's own config file
	if err != nil {
		return ""
	}
	var cfg struct {
		CurrentContext string `json:"currentContext"`
	}
	if json.Unmarshal(data, &cfg) != nil || cfg.CurrentContext == "default" {
		return ""
	}
	return cfg.CurrentContext
}

// envValue returns the value of key in env, or "" when it is not set.
func envValue(env []string, key string) string {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}

// cliDockerHost translates an endpoint in the form Ofelia's client accepts
// into the form the docker CLI accepts. The CLI knows tcp, unix, npipe, ssh
// and fd, but not Ofelia's http, https and tcp+tls schemes: those become
// tcp, and the TLS ones request DOCKER_TLS_VERIFY.
func cliDockerHost(host string) (string, bool) {
	scheme, rest, ok := strings.Cut(host, "://")
	if !ok {
		return host, false
	}
	switch scheme {
	case "http":
		return "tcp://" + rest, false
	case "https", "tcp+tls":
		return "tcp://" + rest, true
	default:
		return host, false
	}
}

// projectArgs returns the global compose flags that select the project: one
// -f per file, then --profile, --env-file, -p and --project-directory for a
// job from a registered project, or the single -f File of a [job-compose].
func (j *ComposeJob) projectArgs(validator *config.CommandValidator) ([]string, error) {
	p := j.Project
	if len(p.Files) == 0 {
		if err := validator.ValidateFilePath(j.File); err != nil {
			return nil, fmt.Errorf("invalid compose file path: %w", err)
		}
		return []string{"-f", j.File}, nil
	}
	var out []string
	for _, f := range p.Files {
		if err := validator.ValidateFilePath(f); err != nil {
			return nil, fmt.Errorf("invalid compose file path: %w", err)
		}
		out = append(out, "-f", f)
	}
	for _, profile := range p.Profiles {
		if err := validator.ValidateServiceName(profile); err != nil {
			return nil, fmt.Errorf("invalid compose profile: %w", err)
		}
		out = append(out, "--profile", profile)
	}
	for _, f := range p.EnvFiles {
		if err := validator.ValidateFilePath(f); err != nil {
			return nil, fmt.Errorf("invalid compose env file path: %w", err)
		}
		out = append(out, "--env-file", f)
	}
	if p.ProjectName != "" {
		if err := validator.ValidateServiceName(p.ProjectName); err != nil {
			return nil, fmt.Errorf("invalid compose project name: %w", err)
		}
		out = append(out, "-p", p.ProjectName)
	}
	if p.ProjectDirectory != "" {
		if err := validator.ValidateFilePath(p.ProjectDirectory); err != nil {
			return nil, fmt.Errorf("invalid compose project directory: %w", err)
		}
		out = append(out, "--project-directory", p.ProjectDirectory)
	}
	return out, nil
}

func (j *ComposeJob) buildCommand(ctx *Context) (*exec.Cmd, error) {
	// Validate inputs to prevent command injection
	validator := config.NewCommandValidator()

	// Validate service name
	if err := validator.ValidateServiceName(j.Service); err != nil {
		return nil, fmt.Errorf("invalid service name: %w", err)
	}

	// Build docker compose command
	cmdArgs := []string{"docker", "compose"}
	projectArgs, err := j.projectArgs(validator)
	if err != nil {
		return nil, err
	}
	cmdArgs = append(cmdArgs, projectArgs...)

	if j.Exec {
		cmdArgs = append(cmdArgs, "exec", j.Service)
	} else {
		// -T: Ofelia never has a terminal to attach. The label carries the
		// execution ID, set before launch, so cancellation can find the
		// container before Ofelia learns its ID.
		cmdArgs = append(cmdArgs, "run", "--rm", "-T",
			"-l", ComposeExecutionLabel+"="+ctx.Execution.ID, j.Service)
	}

	// Add command arguments if present
	if j.Command != "" {
		commandArgs := args.GetArgs(j.Command)
		if err := validator.ValidateCommandArgs(commandArgs); err != nil {
			return nil, fmt.Errorf("invalid command arguments: %w", err)
		}
		cmdArgs = append(cmdArgs, commandArgs...)
	}

	bin, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return nil, fmt.Errorf("look path %q: %w", cmdArgs[0], err)
	}

	return &exec.Cmd{
		Path:   bin,
		Args:   cmdArgs,
		Stdout: ctx.Execution.OutputStream,
		Stderr: ctx.Execution.ErrorStream,
	}, nil
}
