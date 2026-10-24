// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build !windows

package core

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// localJobGroupPollInterval is how often terminateLocalProcess checks whether
// the process group is gone during the grace period.
const localJobGroupPollInterval = 100 * time.Millisecond

// setLocalProcessGroup starts the command in a process group of its own, so
// cancellation can signal every process it starts, not only the first one.
func setLocalProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateLocalProcess sends SIGTERM to the command's process group, waits up
// to localJobStopGrace for the group to disappear, sends SIGKILL to whatever is
// left, and returns the command's exit error from done.
func terminateLocalProcess(cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	var waitErr error
	leaderDone := false
	deadline := time.Now().Add(localJobStopGrace)
	for time.Now().Before(deadline) {
		if !leaderDone {
			select {
			case waitErr = <-done:
				leaderDone = true
			default:
			}
		}
		if leaderDone && !localProcessGroupAlive(pgid) {
			return waitErr
		}
		time.Sleep(localJobGroupPollInterval)
	}

	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	if leaderDone {
		return waitErr
	}
	return <-done
}

// localProcessGroupAlive reports whether any process of group pgid exists.
// Only ESRCH proves the group is gone; EPERM means a member exists that this
// process may not signal.
func localProcessGroupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return !errors.Is(err, syscall.ESRCH)
}
