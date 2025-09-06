package core

import (
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// Test helper functions and mock implementations for job testing

// MockLogger for testing
type MockLogger struct {
	logs []string
}

func (m *MockLogger) Criticalf(format string, args ...interface{}) {
	m.logs = append(m.logs, "CRITICAL: "+format)
}

func (m *MockLogger) Debugf(format string, args ...interface{}) {
	m.logs = append(m.logs, "DEBUG: "+format)
}

func (m *MockLogger) Errorf(format string, args ...interface{}) {
	m.logs = append(m.logs, "ERROR: "+format)
}

func (m *MockLogger) Noticef(format string, args ...interface{}) {
	m.logs = append(m.logs, "NOTICE: "+format)
}

func (m *MockLogger) Warningf(format string, args ...interface{}) {
	m.logs = append(m.logs, "WARNING: "+format)
}

// MockDockerClient provides a test double for Docker client operations
type MockDockerClient struct {
	*docker.Client

	// Exec operations
	createExecResponse  *docker.Exec
	createExecError     error
	startExecError      error
	inspectExecResponse *docker.ExecInspect
	inspectExecError    error

	// Container operations
	inspectContainerResponse *docker.Container
	inspectContainerError    error
	createContainerResponse  *docker.Container
	createContainerError     error
	startContainerError      error
	removeContainerError     error

	// Image operations
	inspectImageResponse *docker.Image
	inspectImageError    error
	pullImageError       error

	// Call tracking
	createExecCalls       int
	startExecCalls        int
	inspectExecCalls      int
	inspectContainerCalls int
	createContainerCalls  int
	startContainerCalls   int
	removeContainerCalls  int
	pullImageCalls        int

	// Last call parameters
	lastCreateExecOpts      *docker.CreateExecOptions
	lastStartExecOpts       *docker.StartExecOptions
	lastCreateContainerOpts *docker.CreateContainerOptions
	lastContainerID         string
}

func NewMockDockerClient() *MockDockerClient {
	return &MockDockerClient{
		Client: &docker.Client{},
	}
}

// Exec operations
func (m *MockDockerClient) CreateExec(opts docker.CreateExecOptions) (*docker.Exec, error) {
	m.createExecCalls++
	m.lastCreateExecOpts = &opts

	if m.createExecError != nil {
		return nil, m.createExecError
	}

	if m.createExecResponse != nil {
		return m.createExecResponse, nil
	}

	return &docker.Exec{ID: "default-exec-id"}, nil
}

func (m *MockDockerClient) StartExec(execID string, opts docker.StartExecOptions) error {
	m.startExecCalls++
	m.lastStartExecOpts = &opts

	// Write test output to streams if provided
	if opts.OutputStream != nil {
		_, _ = opts.OutputStream.Write([]byte("test stdout"))
	}
	if opts.ErrorStream != nil {
		_, _ = opts.ErrorStream.Write([]byte("test stderr"))
	}

	return m.startExecError
}

func (m *MockDockerClient) InspectExec(execID string) (*docker.ExecInspect, error) {
	m.inspectExecCalls++

	if m.inspectExecError != nil {
		return nil, m.inspectExecError
	}

	if m.inspectExecResponse != nil {
		return m.inspectExecResponse, nil
	}

	return &docker.ExecInspect{ExitCode: 0, Running: false}, nil
}

// Container operations
func (m *MockDockerClient) InspectContainerWithOptions(opts docker.InspectContainerOptions) (*docker.Container, error) {
	m.inspectContainerCalls++
	m.lastContainerID = opts.ID

	if m.inspectContainerError != nil {
		return nil, m.inspectContainerError
	}

	if m.inspectContainerResponse != nil {
		return m.inspectContainerResponse, nil
	}

	return &docker.Container{
		ID:    opts.ID,
		Name:  "test-container",
		State: docker.State{Running: false, ExitCode: 0},
	}, nil
}

func (m *MockDockerClient) CreateContainer(opts docker.CreateContainerOptions) (*docker.Container, error) {
	m.createContainerCalls++
	m.lastCreateContainerOpts = &opts

	if m.createContainerError != nil {
		return nil, m.createContainerError
	}

	if m.createContainerResponse != nil {
		return m.createContainerResponse, nil
	}

	return &docker.Container{
		ID:     "default-container-id",
		Name:   opts.Name,
		Config: opts.Config,
	}, nil
}

func (m *MockDockerClient) StartContainer(id string, hostConfig *docker.HostConfig) error {
	m.startContainerCalls++
	m.lastContainerID = id
	return m.startContainerError
}

func (m *MockDockerClient) RemoveContainer(opts docker.RemoveContainerOptions) error {
	m.removeContainerCalls++
	m.lastContainerID = opts.ID
	return m.removeContainerError
}

// Image operations
func (m *MockDockerClient) InspectImage(name string) (*docker.Image, error) {
	if m.inspectImageError != nil {
		return nil, m.inspectImageError
	}

	if m.inspectImageResponse != nil {
		return m.inspectImageResponse, nil
	}

	return &docker.Image{ID: "test-image-id"}, nil
}

func (m *MockDockerClient) PullImage(opts docker.PullImageOptions, auth docker.AuthConfiguration) error {
	m.pullImageCalls++
	return m.pullImageError
}

func (m *MockDockerClient) Logs(opts docker.LogsOptions) error {
	// Simple mock - write test content to provided writers
	if opts.OutputStream != nil {
		_, _ = opts.OutputStream.Write([]byte("container logs stdout"))
	}
	if opts.ErrorStream != nil {
		_, _ = opts.ErrorStream.Write([]byte("container logs stderr"))
	}
	return nil
}

// Helper functions for creating test jobs with mock clients
// (Functions removed to fix linting issues - they can be re-added when needed)

// Simple test interface to simulate container monitoring
type TestContainerMonitor struct {
	waitForContainerFunc func(string, time.Duration) (*docker.State, error)
}

func (t *TestContainerMonitor) WaitForContainer(containerID string, maxRuntime time.Duration) (*docker.State, error) {
	if t.waitForContainerFunc != nil {
		return t.waitForContainerFunc(containerID, maxRuntime)
	}
	return &docker.State{ExitCode: 0, Running: false}, nil
}

func (t *TestContainerMonitor) SetUseEventsAPI(use bool) {
	// Test implementation
}

// Error helpers for common Docker API errors
// (Functions removed to fix linting issues - they can be re-added when needed)
