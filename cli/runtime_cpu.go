// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"runtime"
	"strconv"
)

// logRuntimeCPU logs the CPU settings the Go runtime runs with.
//
// Since Go 1.25 the runtime derives GOMAXPROCS from the CPU limit of the
// process's cgroup. It rounds the limit up and raises a limit below 2 to 2,
// unless the machine or the CPU affinity mask has fewer than 2 CPUs. A
// container limited to 1 CPU on a larger host therefore runs Go code on 2
// threads. The line shows the value the runtime chose next to the CPU count;
// both come from the runtime itself.
func (c *DaemonCommand) logRuntimeCPU() {
	c.Logger.Info("Go runtime CPU settings",
		"gomaxprocs", runtime.GOMAXPROCS(0),
		"gomaxprocsSource", gomaxprocsSource(os.Getenv("GOMAXPROCS")),
		"cpus", runtime.NumCPU(),
	)
}

// gomaxprocsSource names where GOMAXPROCS came from. The runtime takes the
// environment variable only when it holds a positive whole number.
func gomaxprocsSource(env string) string {
	if n, err := strconv.Atoi(env); err == nil && n > 0 {
		return "GOMAXPROCS environment variable"
	}
	return "default"
}
