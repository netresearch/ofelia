// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

// Package core holds the scheduler and the job types it runs: ExecJob (in a
// running container), RunJob (in a fresh container), RunServiceJob (as a Swarm
// service) and LocalJob (on the host). Docker access goes through the port
// interfaces in core/ports so the scheduler stays independent of the SDK.
package core
