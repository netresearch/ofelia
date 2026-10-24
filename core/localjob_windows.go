// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build windows

package core

import (
	"fmt"
	"os/exec"
	"time"
)

// setLocalProcessGroup is a no-op on Windows, which has no POSIX process
// groups; terminateLocalProcess ends only the command's own process there.
func setLocalProcessGroup(_ *exec.Cmd) {
	// Nothing to configure: Windows has no process groups to start the
	// command in, so termination targets the command's own process only.
}

// terminateLocalProcess kills the command's process and returns its exit
// error from done. Windows has no SIGTERM, so there is no grace period, and
// processes the command started are not reached. When the process has not
// exited localJobStopGrace after the kill, it returns
// ErrLocalTerminationUnconfirmed instead of waiting for the job to end.
func terminateLocalProcess(cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Kill()
	select {
	case err := <-done:
		return err
	case <-time.After(localJobStopGrace):
		return fmt.Errorf("%w: process %d still running after kill", ErrLocalTerminationUnconfirmed, cmd.Process.Pid)
	}
}
