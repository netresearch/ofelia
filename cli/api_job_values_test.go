// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import "github.com/netresearch/ofelia/core"

// apiJobUser is the user a run or exec job created through the web API, or
// restored from the state file, runs as when [global] default-user is
// "nobody".
const apiJobUser = "nobody"

// apiJobMiddlewares is the middleware chain of such a job right after it
// was added: the job carries none of its own.
var apiJobMiddlewares = []string{}

// attachRestoreConfig gives the daemon the configuration a restored job is
// assembled against.
func attachRestoreConfig(_ interface{ Helper() }, c *DaemonCommand, provider core.DockerProvider) {
	c.dockerHandler = &DockerHandler{dockerProvider: provider}
	c.config.Global.DefaultUser = "nobody"
	c.config.dockerHandler = c.dockerHandler
}
