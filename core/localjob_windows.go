// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

//go:build windows

package core

import "os/exec"

// setLocalProcessGroup is a no-op on Windows, which has no POSIX process
// groups; terminateLocalProcess ends only the command's own process there.
func setLocalProcessGroup(_ *exec.Cmd) {}

// terminateLocalProcess kills the command's process and returns its exit
// error from done. Windows has no SIGTERM, so there is no grace period, and
// processes the command started are not reached.
func terminateLocalProcess(cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Kill()
	return <-done
}
