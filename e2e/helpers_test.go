//go:build e2e && unix
// +build e2e,unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

// Package e2e exercises the compiled ofelia binary as a real subprocess:
// build binary → spawn with INI config → observe stdout/stderr, file system
// side-effects and container logs. These tests complement the in-process
// integration tests by verifying the full pipeline (parse config → schedule
// → fire → execute → shutdown).
package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

// buildBinaryOnce caches the compiled ofelia binary path across tests in the
// same `go test` process to avoid re-invoking `go build` per-test (which is
// slow and multiplies CI cost). cachedBuildErr is a captured build error —
// not a sentinel — hence the `nolint:errname` suppression.
var (
	buildBinaryOnce sync.Once
	cachedBinPath   string
	cachedBuildErr  error //nolint:errname // captured build error, not a sentinel
)

// buildBinary compiles the ofelia binary with race detection enabled into a
// temp directory and returns its absolute path. The binary is shared by all
// e2e tests in a single `go test` invocation.
//
// We intentionally compile with `-race` so that e2e tests double as real-world
// race-condition detectors — if the scheduler or shutdown logic has a data
// race, the binary itself will report it on SIGTERM.
func buildBinary(t *testing.T) string {
	t.Helper()

	buildBinaryOnce.Do(func() {
		// Resolve repo root from this test file's location (e2e/ → ..).
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			cachedBuildErr = errors.New("cannot resolve test source path")
			return
		}
		repoRoot := filepath.Dir(filepath.Dir(thisFile))

		tmpDir, err := os.MkdirTemp("", "ofelia-e2e-bin-")
		if err != nil {
			cachedBuildErr = fmt.Errorf("mkdir temp: %w", err)
			return
		}
		binPath := filepath.Join(tmpDir, "ofelia")

		// Use -race so the real binary surfaces data races during e2e runs.
		// Timeout: go build usually completes in <30s on CI.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "go", "build", "-race", "-o", binPath, ".")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			cachedBuildErr = fmt.Errorf("build ofelia: %w\n%s", err, out)
			return
		}
		cachedBinPath = binPath
	})

	if cachedBuildErr != nil {
		t.Fatalf("build binary: %v", cachedBuildErr)
	}
	return cachedBinPath
}

// daemonProcess wraps a running ofelia daemon subprocess and provides helpers
// to observe its behavior (log scraping, stdout/stderr capture) and terminate
// it cleanly.
type daemonProcess struct {
	cmd      *exec.Cmd
	stdout   *syncBuffer
	stderr   *syncBuffer
	done     chan struct{}
	waitErr  error
	waitOnce sync.Once
}

// startDaemon launches `ofelia daemon --config=<configPath>` as a child
// process and returns a handle to it. The caller MUST defer `p.shutdown(t)`.
func startDaemon(t *testing.T, configPath string, extraArgs ...string) *daemonProcess {
	t.Helper()

	bin := buildBinary(t)

	args := append([]string{"daemon", "--config=" + configPath}, extraArgs...)
	cmd := exec.Command(bin, args...)

	// Put the child in its own process group so SIGTERM to the child does not
	// leak to the test runner, and so we can reliably kill any stragglers.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout := &syncBuffer{}
	stderr := &syncBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start ofelia daemon: %v", err)
	}

	dp := &daemonProcess{
		cmd:    cmd,
		stdout: stdout,
		stderr: stderr,
		done:   make(chan struct{}),
	}

	go func() {
		dp.waitErr = cmd.Wait()
		close(dp.done)
	}()

	// Wait for the "Ofelia is now running" banner so we know scheduler is
	// live before returning. Time out quickly if boot fails.
	if err := dp.waitForLog("Ofelia is now running", 10*time.Second); err != nil {
		// Make sure we don't leak a child before failing.
		_ = dp.signal(syscall.SIGKILL)
		t.Fatalf("daemon did not reach 'running' state within 10s: %v\nstdout=%s\nstderr=%s",
			err, stdout.String(), stderr.String())
	}

	return dp
}

// waitForLog polls the combined stdout for a substring until it appears or
// the timeout elapses. It searches under the buffer's lock rather than
// snapshotting the full bytes on every iteration, so the cost stays O(N)
// per poll instead of O(N²) over the lifetime of the test.
func (p *daemonProcess) waitForLog(needle string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	needleBytes := []byte(needle)
	for time.Now().Before(deadline) {
		if p.stdout.Contains(needleBytes) || p.stderr.Contains(needleBytes) {
			return nil
		}
		if p.exited() {
			return fmt.Errorf("daemon exited before log %q appeared", needle)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for log %q", needle)
}

// countLogOccurrences counts how many times the given substring appears in
// the captured stdout. Uses bytes.Count under the buffer's lock so no
// intermediate copy is needed.
func (p *daemonProcess) countLogOccurrences(needle string) int {
	return p.stdout.Count([]byte(needle))
}

// exited reports whether the daemon process has already terminated.
func (p *daemonProcess) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// signal sends a signal to the daemon process.
func (p *daemonProcess) signal(sig os.Signal) error {
	if p.cmd.Process == nil {
		return errors.New("daemon process is nil")
	}
	return p.cmd.Process.Signal(sig)
}

// killProcessGroup sends `sig` to the entire process group we created for
// the daemon (via Setpgid=true in startDaemon). Falls back to signaling
// the daemon's PID directly if the pgid lookup fails. Using the group
// ensures we reap any short-lived children the daemon may have spawned
// (e.g. local-exec `sh` subprocesses that were caught mid-run by SIGKILL).
func (p *daemonProcess) killProcessGroup(sig syscall.Signal) error {
	if p.cmd.Process == nil {
		return errors.New("daemon process is nil")
	}
	pgid, err := syscall.Getpgid(p.cmd.Process.Pid)
	if err != nil {
		// Fallback: signal the main process directly.
		return p.cmd.Process.Signal(sig)
	}
	// Negative PID ⇒ deliver to process group (see kill(2)).
	return syscall.Kill(-pgid, sig)
}

// shutdown sends SIGTERM and waits for the process to exit. Safe to call
// multiple times. If the daemon doesn't exit within `timeout`, it is killed
// along with any children it spawned.
func (p *daemonProcess) shutdown(t *testing.T, timeout time.Duration) {
	t.Helper()
	p.waitOnce.Do(func() {
		if !p.exited() {
			// Only the daemon itself needs SIGTERM for a graceful exit —
			// the shutdown manager will then stop any in-flight jobs.
			_ = p.signal(syscall.SIGTERM)
		}
		select {
		case <-p.done:
		case <-time.After(timeout):
			t.Logf("daemon did not exit within %s after SIGTERM; SIGKILLing process group", timeout)
			// Reap the entire group so stray job subprocesses don't leak.
			_ = p.killProcessGroup(syscall.SIGKILL)
			<-p.done
		}
	})
}

// waitExit blocks until the daemon exits (or the timeout fires) and returns
// the exit error. Unlike shutdown(), this does not send a signal itself.
func (p *daemonProcess) waitExit(timeout time.Duration) (error, bool) {
	select {
	case <-p.done:
		return p.waitErr, true
	case <-time.After(timeout):
		return nil, false
	}
}

// syncBuffer is a concurrent-safe bytes buffer used to capture child process
// stdout/stderr without racing against the log-scraping goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// Contains performs a needle search against the buffer while holding its
// lock, avoiding the slice copy that `Bytes()` would trigger on each call.
func (s *syncBuffer) Contains(needle []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Contains(s.buf.Bytes(), needle)
}

// Count returns the number of non-overlapping occurrences of needle in the
// buffer. Uses bytes.Count, which is faster than scanning line-by-line and
// avoids bufio.Scanner's per-line allocations.
func (s *syncBuffer) Count(needle []byte) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Count(s.buf.Bytes(), needle)
}

func (s *syncBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, s.buf.Len())
	copy(out, s.buf.Bytes())
	return out
}

func (s *syncBuffer) String() string {
	return string(s.Bytes())
}

// writeConfig writes the given INI body to a file inside the test's temp
// directory and returns the path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ofelia.ini")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// runCommand runs the ofelia binary with the given args, returning stdout,
// stderr and the exit error. Used by validation-error tests.
func runCommand(t *testing.T, args ...string) (stdout, stderr string, exitErr error) {
	t.Helper()
	bin := buildBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	exitErr = cmd.Run()
	return outBuf.String(), errBuf.String(), exitErr
}

// dockerAvailable returns true if the docker CLI is usable and the daemon is
// reachable. Tests that require docker skip cleanly when it is absent.
func dockerAvailable(t *testing.T) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

// dockerLogs returns the combined stdout+stderr log output of a named
// container.
func dockerLogs(t *testing.T, name string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "logs", name)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("docker logs %s: %v\n%s", name, err, out.String())
	}
	return out.String()
}

// dockerRemove best-effort removes a container by name.
func dockerRemove(t *testing.T, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}

// assertExitCode fails the test unless the command exited with want.
//
// The exit status is the only thing a shell, a Makefile or a CI step can act
// on, so it is asserted separately from the human-readable output: a command
// that prints a clear error and still exits 0 is indistinguishable from
// success to everything except a person reading the log.
func assertExitCode(t *testing.T, runErr error, want int, stdout, stderr string) {
	t.Helper()

	got := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("command failed to run at all: %v", runErr)
		}
		got = exitErr.ExitCode()
	}

	if got != want {
		t.Errorf("exit code = %d, want %d\nstdout=%s\nstderr=%s", got, want, stdout, stderr)
	}
}
