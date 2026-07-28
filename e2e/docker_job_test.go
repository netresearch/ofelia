//go:build e2e && unix
// +build e2e,unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// dockerConfigOnce returns an INI fragment that schedules a single one-shot
// execution of the given job: far-future cron + `run-on-startup = true`.
// This is important because we set `delete = false` + a fixed container-name
// so we can inspect the container's logs afterwards — a repeating schedule
// would try to create a second container with the same name on tick #2 and
// produce spurious `name already in use` failures in the logs.
func dockerConfigOnce(jobName, containerName, image, command string) string {
	return fmt.Sprintf(`[global]
  log-level = info

[job-run %q]
  # 2099-01-01 — effectively never fires; the run-on-startup trigger is
  # what drives this test. Single execution → deterministic state.
  schedule = 0 0 1 1 1
  run-on-startup = true
  image = %s
  container-name = %q
  delete = false
  command = %s
`, jobName, image, containerName, command)
}

// TestE2E_DockerRunJob_SpawnsContainer is the canonical end-to-end docker
// scenario: ofelia runs a real alpine container, the container prints a
// marker, and the container logs contain that marker (verified via the
// docker CLI).
//
// Configured for a single execution (see `dockerConfigOnce`) because we
// preserve the container (`delete = false`) to read its logs — repeating
// schedules would collide on the fixed container name.
// Skipped automatically when docker is not available.
func TestE2E_DockerRunJob_SpawnsContainer(t *testing.T) {
	t.Parallel()

	if !dockerAvailable(t) {
		t.Skip("docker not available; skipping docker e2e test")
	}

	// Pre-pull the image so the scheduled window doesn't race with registry
	// latency on cold CI runners.
	pullCtx, pullCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer pullCancel()
	if out, err := exec.CommandContext(pullCtx, "docker", "pull", "alpine:3.20").CombinedOutput(); err != nil {
		t.Fatalf("docker pull alpine:3.20: %v\n%s", err, out)
	}

	// Container name used to locate + clean up the container after the job.
	// Timestamped to avoid collisions across parallel test runs.
	containerName := fmt.Sprintf("ofelia-e2e-run-%d", time.Now().UnixNano())
	t.Cleanup(func() { dockerRemove(t, containerName) })

	configPath := writeConfig(t, dockerConfigOnce(
		"e2e-docker",
		containerName,
		"alpine:3.20",
		`sh -c "echo OFELIA_E2E_DOCKER_MARKER"`,
	))
	daemon := startDaemon(t, configPath)
	t.Cleanup(func() { daemon.shutdown(t, 30*time.Second) })

	// Wait for the job to finish; the log line appears after image resolve +
	// container run + log collection, so allow 30s on cold runners.
	if err := daemon.waitForLog(`Job \"e2e-docker\"`, 30*time.Second); err != nil {
		t.Fatalf("no docker execution log observed: %v\nstdout=%s",
			err, daemon.stdout.String())
	}
	if err := daemon.waitForLog("Finished in", 30*time.Second); err != nil {
		t.Fatalf("docker job did not finish: %v\nstdout=%s",
			err, daemon.stdout.String())
	}

	// Now that the container exists (delete=false), fetch its logs and
	// verify the marker made it all the way through the docker runtime.
	logs := dockerLogs(t, containerName)
	if !strings.Contains(logs, "OFELIA_E2E_DOCKER_MARKER") {
		t.Fatalf("marker missing from container logs; got: %q", logs)
	}

	// Belt-and-suspenders: ofelia's stream forwarder should also have picked
	// it up (confirms the stdout-copy plumbing works).
	if !bytes.Contains(daemon.stdout.Bytes(), []byte("OFELIA_E2E_DOCKER_MARKER")) {
		t.Errorf("marker missing from ofelia's captured StdOut stream\nstdout=%s",
			daemon.stdout.String())
	}
}

// TestE2E_DockerRunJob_FailingContainerMarkedFailed asserts that a non-zero
// exit code in the container is surfaced by ofelia as a failed execution.
// Regression guard: silent failures would let broken cron jobs look healthy.
func TestE2E_DockerRunJob_FailingContainerMarkedFailed(t *testing.T) {
	t.Parallel()

	if !dockerAvailable(t) {
		t.Skip("docker not available; skipping docker e2e test")
	}

	pullCtx, pullCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer pullCancel()
	if out, err := exec.CommandContext(pullCtx, "docker", "pull", "alpine:3.20").CombinedOutput(); err != nil {
		t.Fatalf("docker pull alpine:3.20: %v\n%s", err, out)
	}

	containerName := fmt.Sprintf("ofelia-e2e-fail-%d", time.Now().UnixNano())
	t.Cleanup(func() { dockerRemove(t, containerName) })

	configPath := writeConfig(t, dockerConfigOnce(
		"e2e-docker-fail",
		containerName,
		"alpine:3.20",
		`sh -c "exit 42"`,
	))
	daemon := startDaemon(t, configPath)
	t.Cleanup(func() { daemon.shutdown(t, 30*time.Second) })

	if err := daemon.waitForLog(`failed: true`, 30*time.Second); err != nil {
		t.Fatalf("expected a 'failed: true' log line within 30s but none appeared: %v\nstdout=%s",
			err, daemon.stdout.String())
	}
}
