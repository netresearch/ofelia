// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web_test

import (
	"github.com/netresearch/ofelia/cli"
	webpkg "github.com/netresearch/ofelia/web"
)

// withJobBuilder gives srv the job builder the daemon gives it: job assembly
// against a configuration with every default and no Docker provider, so run
// and exec jobs are refused as on a daemon without Docker.
func withJobBuilder(srv *webpkg.Server) *webpkg.Server {
	srv.SetJobBuilder(cli.NewConfig(stubDiscardLogger()).BuildAPIJob)
	return srv
}
