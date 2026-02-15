package core

import (
	"time"

	"github.com/netresearch/ofelia/core/domain"
)

// Test helper functions and mock implementations for job testing

// MockLogger for testing
type MockLogger struct {
	logs []string
}

func (m *MockLogger) Criticalf(format string, args ...any) {
	m.logs = append(m.logs, "CRITICAL: "+format)
}

func (m *MockLogger) Debugf(format string, args ...any) {
	m.logs = append(m.logs, "DEBUG: "+format)
}

func (m *MockLogger) Errorf(format string, args ...any) {
	m.logs = append(m.logs, "ERROR: "+format)
}

func (m *MockLogger) Noticef(format string, args ...any) {
	m.logs = append(m.logs, "NOTICE: "+format)
}

func (m *MockLogger) Warningf(format string, args ...any) {
	m.logs = append(m.logs, "WARNING: "+format)
}

// TestContainerMonitor provides a test interface to simulate container monitoring
type TestContainerMonitor struct {
	waitForContainerFunc func(string, time.Duration) (*domain.ContainerState, error)
}

func (t *TestContainerMonitor) WaitForContainer(containerID string, maxRuntime time.Duration) (*domain.ContainerState, error) {
	if t.waitForContainerFunc != nil {
		return t.waitForContainerFunc(containerID, maxRuntime)
	}
	return &domain.ContainerState{ExitCode: 0, Running: false}, nil
}

func (t *TestContainerMonitor) SetUseEventsAPI(use bool) {
	// Test implementation - no-op
}
