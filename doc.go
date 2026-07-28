// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

// Ofelia is a job scheduler for Docker hosts, and a replacement for cron in
// container environments. Jobs are declared in an INI file, through labels on
// the containers themselves, or at runtime through the web API, and run either
// inside an existing container, in a fresh one, on the host, or as a Swarm
// service.
//
// Run the daemon with:
//
//	ofelia daemon --config=/etc/ofelia.conf
//
// Job results can be captured and forwarded by middlewares (log files, e-mail,
// Slack, generic webhooks), and the daemon optionally serves a web UI and REST
// API for inspecting and managing jobs.
//
// See https://github.com/netresearch/ofelia for configuration reference and
// deployment guides.
package main
