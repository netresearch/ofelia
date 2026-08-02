// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// Test helper functions and mock implementations for job testing

// TestContainerMonitor provides a test interface to simulate container monitoring
type TestContainerMonitor struct {
	waitForContainerFunc func(string, time.Duration) (*domain.ContainerState, error)
}

// WaitForContainer returns whatever the stubbed waitForContainerFunc yields
// for containerID and maxRuntime. With no stub installed it returns a stopped
// container with exit code 0 and no error, i.e. the success case, without
// waiting.
func (t *TestContainerMonitor) WaitForContainer(containerID string, maxRuntime time.Duration) (*domain.ContainerState, error) {
	if t.waitForContainerFunc != nil {
		return t.waitForContainerFunc(containerID, maxRuntime)
	}
	return &domain.ContainerState{ExitCode: 0, Running: false}, nil
}

// SetUseEventsAPI accepts the events-API toggle and discards it. The stub's
// behavior comes entirely from waitForContainerFunc, so there is no polling
// path to switch away from.
func (t *TestContainerMonitor) SetUseEventsAPI(use bool) {
	// Test implementation - no-op
}
