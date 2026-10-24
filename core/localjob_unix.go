// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build !windows

package core

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// localSignal sends a signal to a process or, with a negative pid, a process
// group. A var so tests can simulate a group Ofelia may not signal.
var localSignal = syscall.Kill

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
// left, and returns the command's exit error from done. When the group is
// still there localJobStopGrace after SIGKILL — a member changed credentials
// and cannot be signaled — it returns ErrLocalTerminationUnconfirmed instead
// of waiting for the job to end on its own.
func terminateLocalProcess(cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	pgid := cmd.Process.Pid
	_ = localSignal(-pgid, syscall.SIGTERM)

	w := groupWait{pgid: pgid, done: done}
	if w.untilGone(localJobStopGrace) {
		return w.waitErr
	}
	_ = localSignal(-pgid, syscall.SIGKILL)
	if w.untilGone(localJobStopGrace) {
		return w.waitErr
	}
	return fmt.Errorf("%w: process group %d still present after SIGKILL", ErrLocalTerminationUnconfirmed, pgid)
}

// groupWait tracks the command's exit and its process group across the
// SIGTERM and SIGKILL phases of terminateLocalProcess.
type groupWait struct {
	pgid       int
	done       <-chan error
	waitErr    error
	leaderDone bool
}

// untilGone polls for up to d and reports whether the command has exited and
// its process group is gone.
func (w *groupWait) untilGone(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if !w.leaderDone {
			select {
			case w.waitErr = <-w.done:
				w.leaderDone = true
			default:
			}
		}
		if w.leaderDone && !localProcessGroupAlive(w.pgid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(localJobGroupPollInterval)
	}
}

// localProcessGroupAlive reports whether any process of group pgid exists.
// Only ESRCH proves the group is gone; EPERM means a member exists that this
// process may not signal.
func localProcessGroupAlive(pgid int) bool {
	err := localSignal(-pgid, 0)
	return !errors.Is(err, syscall.ESRCH)
}
