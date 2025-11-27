package mock_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// TestMockDockerClientImplementsInterface verifies the mock client implements the interface.
func TestMockDockerClientImplementsInterface(t *testing.T) {
	var _ ports.DockerClient = (*mock.DockerClient)(nil)
}

func TestNewDockerClient(t *testing.T) {
	client := mock.NewDockerClient()
	if client == nil {
		t.Fatal("NewDockerClient() returned nil")
	}

	// Verify all services are initialized
	if client.Containers() == nil {
		t.Error("Containers() returned nil")
	}
	if client.Exec() == nil {
		t.Error("Exec() returned nil")
	}
	if client.Images() == nil {
		t.Error("Images() returned nil")
	}
	if client.Events() == nil {
		t.Error("Events() returned nil")
	}
	if client.Services() == nil {
		t.Error("Services() returned nil")
	}
	if client.Networks() == nil {
		t.Error("Networks() returned nil")
	}
	if client.System() == nil {
		t.Error("System() returned nil")
	}
}

func TestDockerClientClose(t *testing.T) {
	client := mock.NewDockerClient()

	// Initially not closed
	if client.IsClosed() {
		t.Error("IsClosed() should return false initially")
	}

	// Close returns nil by default
	if err := client.Close(); err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}

	// Now closed
	if !client.IsClosed() {
		t.Error("IsClosed() should return true after Close()")
	}
}

func TestDockerClientSetCloseError(t *testing.T) {
	client := mock.NewDockerClient()
	expectedErr := errors.New("close error")

	client.SetCloseError(expectedErr)

	err := client.Close()
	if !errors.Is(err, expectedErr) {
		t.Errorf("Close() = %v, want %v", err, expectedErr)
	}
}

// ContainerService Tests

func TestContainerServiceCreate(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	config := &domain.ContainerConfig{
		Name:  "test-container",
		Image: "alpine:latest",
		Cmd:   []string{"echo", "hello"},
	}

	id, err := containers.Create(ctx, config)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if id != "mock-container-id" {
		t.Errorf("Create() = %v, want mock-container-id", id)
	}

	// Verify call tracking
	if len(containers.CreateCalls) != 1 {
		t.Fatalf("CreateCalls = %d, want 1", len(containers.CreateCalls))
	}
	if containers.CreateCalls[0].Config.Name != "test-container" {
		t.Errorf("CreateCalls[0].Config.Name = %v, want test-container", containers.CreateCalls[0].Config.Name)
	}
}

func TestContainerServiceCreateWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	customID := "custom-container-123"
	callbackCalled := false
	containers.OnCreate = func(ctx context.Context, config *domain.ContainerConfig) (string, error) {
		callbackCalled = true
		return customID, nil
	}

	id, err := containers.Create(ctx, &domain.ContainerConfig{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if id != customID {
		t.Errorf("Create() = %v, want %v", id, customID)
	}

	if !callbackCalled {
		t.Error("OnCreate callback was not called")
	}
}

func TestContainerServiceCreateWithCallbackError(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	expectedErr := errors.New("create failed")
	containers.OnCreate = func(ctx context.Context, config *domain.ContainerConfig) (string, error) {
		return "", expectedErr
	}

	_, err := containers.Create(ctx, &domain.ContainerConfig{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("Create() error = %v, want %v", err, expectedErr)
	}
}

func TestContainerServiceStart(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	err := containers.Start(ctx, "container-id")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if len(containers.StartCalls) != 1 || containers.StartCalls[0] != "container-id" {
		t.Errorf("StartCalls = %v, want [container-id]", containers.StartCalls)
	}
}

func TestContainerServiceStartWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	expectedErr := errors.New("start failed")
	containers.OnStart = func(ctx context.Context, containerID string) error {
		return expectedErr
	}

	err := containers.Start(ctx, "container-id")
	if !errors.Is(err, expectedErr) {
		t.Errorf("Start() error = %v, want %v", err, expectedErr)
	}
}

func TestContainerServiceStop(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	timeout := 10 * time.Second
	err := containers.Stop(ctx, "container-id", &timeout)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(containers.StopCalls) != 1 {
		t.Fatalf("StopCalls = %d, want 1", len(containers.StopCalls))
	}
	if containers.StopCalls[0].ContainerID != "container-id" {
		t.Errorf("StopCalls[0].ContainerID = %v, want container-id", containers.StopCalls[0].ContainerID)
	}
	if *containers.StopCalls[0].Timeout != timeout {
		t.Errorf("StopCalls[0].Timeout = %v, want %v", *containers.StopCalls[0].Timeout, timeout)
	}
}

func TestContainerServiceStopWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	expectedErr := errors.New("stop failed")
	containers.OnStop = func(ctx context.Context, containerID string, timeout *time.Duration) error {
		return expectedErr
	}

	err := containers.Stop(ctx, "container-id", nil)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Stop() error = %v, want %v", err, expectedErr)
	}
}

func TestContainerServiceRemove(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	opts := domain.RemoveOptions{Force: true, RemoveVolumes: true}
	err := containers.Remove(ctx, "container-id", opts)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if len(containers.RemoveCalls) != 1 {
		t.Fatalf("RemoveCalls = %d, want 1", len(containers.RemoveCalls))
	}
	if !containers.RemoveCalls[0].Options.Force {
		t.Error("RemoveCalls[0].Options.Force = false, want true")
	}
}

func TestContainerServiceRemoveWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	expectedErr := errors.New("remove failed")
	containers.OnRemove = func(ctx context.Context, containerID string, opts domain.RemoveOptions) error {
		return expectedErr
	}

	err := containers.Remove(ctx, "container-id", domain.RemoveOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("Remove() error = %v, want %v", err, expectedErr)
	}
}

func TestContainerServiceInspect(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	info, err := containers.Inspect(ctx, "container-id")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if info.ID != "container-id" {
		t.Errorf("Inspect().ID = %v, want container-id", info.ID)
	}

	if len(containers.InspectCalls) != 1 || containers.InspectCalls[0] != "container-id" {
		t.Errorf("InspectCalls = %v, want [container-id]", containers.InspectCalls)
	}
}

func TestContainerServiceInspectWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	customContainer := &domain.Container{
		ID:   "custom-id",
		Name: "custom-container",
		State: domain.ContainerState{
			Running:  true,
			ExitCode: 0,
		},
	}

	containers.OnInspect = func(ctx context.Context, containerID string) (*domain.Container, error) {
		return customContainer, nil
	}

	info, err := containers.Inspect(ctx, "container-id")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if info.ID != "custom-id" {
		t.Errorf("Inspect().ID = %v, want custom-id", info.ID)
	}
	if !info.State.Running {
		t.Error("Inspect().State.Running = false, want true")
	}
}

func TestContainerServiceList(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	opts := domain.ListOptions{All: true}
	list, err := containers.List(ctx, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if list == nil {
		t.Error("List() returned nil")
	}

	if len(containers.ListCalls) != 1 {
		t.Errorf("ListCalls = %d, want 1", len(containers.ListCalls))
	}
}

func TestContainerServiceListWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	customList := []domain.Container{
		{ID: "container-1", Name: "test-1"},
		{ID: "container-2", Name: "test-2"},
	}

	containers.OnList = func(ctx context.Context, opts domain.ListOptions) ([]domain.Container, error) {
		return customList, nil
	}

	list, err := containers.List(ctx, domain.ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("List() returned %d containers, want 2", len(list))
	}
}

func TestContainerServiceWait(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	respCh, errCh := containers.Wait(ctx, "container-id")

	select {
	case resp := <-respCh:
		if resp.StatusCode != 0 {
			t.Errorf("Wait().StatusCode = %v, want 0", resp.StatusCode)
		}
	case err := <-errCh:
		// nil error from closed channel is okay
		if err != nil {
			t.Fatalf("Wait() returned unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait() timed out")
	}

	if len(containers.WaitCalls) != 1 || containers.WaitCalls[0] != "container-id" {
		t.Errorf("WaitCalls = %v, want [container-id]", containers.WaitCalls)
	}
}

func TestContainerServiceWaitWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	containers.OnWait = func(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error, 1)
		respCh <- domain.WaitResponse{StatusCode: 42}
		close(respCh)
		close(errCh)
		return respCh, errCh
	}

	respCh, _ := containers.Wait(ctx, "container-id")
	resp := <-respCh
	if resp.StatusCode != 42 {
		t.Errorf("Wait().StatusCode = %v, want 42", resp.StatusCode)
	}
}

func TestContainerServiceLogs(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	opts := domain.LogOptions{ShowStdout: true}
	reader, err := containers.Logs(ctx, "container-id", opts)
	if err != nil {
		t.Fatalf("Logs() error = %v", err)
	}
	defer reader.Close()

	// Read should return EOF since empty reader
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if len(data) != 0 {
		t.Errorf("Logs() returned %d bytes, want 0", len(data))
	}

	if len(containers.LogsCalls) != 1 {
		t.Errorf("LogsCalls = %d, want 1", len(containers.LogsCalls))
	}
}

func TestContainerServiceLogsWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	expectedLogs := "test log output"
	containers.OnLogs = func(ctx context.Context, containerID string, opts domain.LogOptions) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewBufferString(expectedLogs)), nil
	}

	reader, err := containers.Logs(ctx, "container-id", domain.LogOptions{})
	if err != nil {
		t.Fatalf("Logs() error = %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}

	if string(data) != expectedLogs {
		t.Errorf("Logs() = %q, want %q", string(data), expectedLogs)
	}
}

func TestContainerServiceCopyLogs(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	expectedLogs := "test log output"
	containers.OnLogs = func(ctx context.Context, containerID string, opts domain.LogOptions) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewBufferString(expectedLogs)), nil
	}

	var stdout bytes.Buffer
	err := containers.CopyLogs(ctx, "container-id", &stdout, nil, domain.LogOptions{})
	if err != nil {
		t.Fatalf("CopyLogs() error = %v", err)
	}

	if stdout.String() != expectedLogs {
		t.Errorf("stdout = %q, want %q", stdout.String(), expectedLogs)
	}
}

func TestContainerServiceCopyLogsError(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	expectedErr := errors.New("logs failed")
	containers.OnLogs = func(ctx context.Context, containerID string, opts domain.LogOptions) (io.ReadCloser, error) {
		return nil, expectedErr
	}

	var stdout bytes.Buffer
	err := containers.CopyLogs(ctx, "container-id", &stdout, nil, domain.LogOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("CopyLogs() error = %v, want %v", err, expectedErr)
	}
}

func TestContainerServiceKill(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	err := containers.Kill(ctx, "container-id", "SIGTERM")
	if err != nil {
		t.Fatalf("Kill() error = %v", err)
	}

	if len(containers.KillCalls) != 1 {
		t.Fatalf("KillCalls = %d, want 1", len(containers.KillCalls))
	}
	if containers.KillCalls[0].Signal != "SIGTERM" {
		t.Errorf("KillCalls[0].Signal = %v, want SIGTERM", containers.KillCalls[0].Signal)
	}
}

func TestContainerServiceKillWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	expectedErr := errors.New("kill failed")
	containers.OnKill = func(ctx context.Context, containerID string, signal string) error {
		return expectedErr
	}

	err := containers.Kill(ctx, "container-id", "SIGTERM")
	if !errors.Is(err, expectedErr) {
		t.Errorf("Kill() error = %v, want %v", err, expectedErr)
	}
}

func TestContainerServicePause(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	err := containers.Pause(ctx, "container-id")
	if err != nil {
		t.Errorf("Pause() error = %v", err)
	}
}

func TestContainerServiceUnpause(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	err := containers.Unpause(ctx, "container-id")
	if err != nil {
		t.Errorf("Unpause() error = %v", err)
	}
}

func TestContainerServiceRename(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	err := containers.Rename(ctx, "container-id", "new-name")
	if err != nil {
		t.Errorf("Rename() error = %v", err)
	}
}

func TestContainerServiceAttach(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	resp, err := containers.Attach(ctx, "container-id", ports.AttachOptions{})
	if err != nil {
		t.Fatalf("Attach() error = %v", err)
	}

	if resp == nil {
		t.Error("Attach() returned nil")
	}
}

// ExecService Tests

func TestExecServiceCreate(t *testing.T) {
	client := mock.NewDockerClient()
	exec := client.Exec().(*mock.ExecService)
	ctx := context.Background()

	config := &domain.ExecConfig{
		Cmd:          []string{"echo", "hello"},
		AttachStdout: true,
	}

	id, err := exec.Create(ctx, "container-id", config)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if id != "mock-exec-id" {
		t.Errorf("Create() = %v, want mock-exec-id", id)
	}

	if len(exec.CreateCalls) != 1 {
		t.Fatalf("CreateCalls = %d, want 1", len(exec.CreateCalls))
	}
	if exec.CreateCalls[0].ContainerID != "container-id" {
		t.Errorf("CreateCalls[0].ContainerID = %v, want container-id", exec.CreateCalls[0].ContainerID)
	}
}

func TestExecServiceCreateWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	exec := client.Exec().(*mock.ExecService)
	ctx := context.Background()

	expectedErr := errors.New("create failed")
	exec.OnCreate = func(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
		return "", expectedErr
	}

	_, err := exec.Create(ctx, "container-id", &domain.ExecConfig{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("Create() error = %v, want %v", err, expectedErr)
	}
}

func TestExecServiceStart(t *testing.T) {
	client := mock.NewDockerClient()
	exec := client.Exec().(*mock.ExecService)
	ctx := context.Background()

	opts := domain.ExecStartOptions{Tty: true}
	resp, err := exec.Start(ctx, "exec-id", opts)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if resp == nil {
		t.Error("Start() returned nil response")
	}

	if len(exec.StartCalls) != 1 {
		t.Errorf("StartCalls = %d, want 1", len(exec.StartCalls))
	}
}

func TestExecServiceStartWithOutput(t *testing.T) {
	client := mock.NewDockerClient()
	exec := client.Exec().(*mock.ExecService)
	ctx := context.Background()

	exec.SetOutput("test output")

	var stdout bytes.Buffer
	opts := domain.ExecStartOptions{OutputStream: &stdout}
	_, err := exec.Start(ctx, "exec-id", opts)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if stdout.String() != "test output" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "test output")
	}
}

func TestExecServiceStartWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	exec := client.Exec().(*mock.ExecService)
	ctx := context.Background()

	expectedErr := errors.New("start failed")
	exec.OnStart = func(ctx context.Context, execID string, opts domain.ExecStartOptions) (*domain.HijackedResponse, error) {
		return nil, expectedErr
	}

	_, err := exec.Start(ctx, "exec-id", domain.ExecStartOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("Start() error = %v, want %v", err, expectedErr)
	}
}

func TestExecServiceInspect(t *testing.T) {
	client := mock.NewDockerClient()
	exec := client.Exec().(*mock.ExecService)
	ctx := context.Background()

	info, err := exec.Inspect(ctx, "exec-id")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if info.ID != "exec-id" {
		t.Errorf("Inspect().ID = %v, want exec-id", info.ID)
	}
	if info.Running {
		t.Error("Inspect().Running = true, want false")
	}
	if info.ExitCode != 0 {
		t.Errorf("Inspect().ExitCode = %v, want 0", info.ExitCode)
	}

	if len(exec.InspectCalls) != 1 || exec.InspectCalls[0] != "exec-id" {
		t.Errorf("InspectCalls = %v, want [exec-id]", exec.InspectCalls)
	}
}

func TestExecServiceInspectWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	exec := client.Exec().(*mock.ExecService)
	ctx := context.Background()

	customInspect := &domain.ExecInspect{
		ID:       "custom-exec-id",
		Running:  true,
		ExitCode: 5,
	}

	exec.OnInspect = func(ctx context.Context, execID string) (*domain.ExecInspect, error) {
		return customInspect, nil
	}

	info, err := exec.Inspect(ctx, "exec-id")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if info.ID != "custom-exec-id" {
		t.Errorf("Inspect().ID = %v, want custom-exec-id", info.ID)
	}
	if !info.Running {
		t.Error("Inspect().Running = false, want true")
	}
	if info.ExitCode != 5 {
		t.Errorf("Inspect().ExitCode = %v, want 5", info.ExitCode)
	}
}

func TestExecServiceRun(t *testing.T) {
	client := mock.NewDockerClient()
	exec := client.Exec().(*mock.ExecService)
	ctx := context.Background()

	// Set simulated output
	exec.SetOutput("hello world")

	var stdout bytes.Buffer
	config := &domain.ExecConfig{Cmd: []string{"echo", "hello"}}

	exitCode, err := exec.Run(ctx, "container-id", config, &stdout, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Run() exitCode = %v, want 0", exitCode)
	}

	if stdout.String() != "hello world" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "hello world")
	}

	if len(exec.RunCalls) != 1 {
		t.Errorf("RunCalls = %d, want 1", len(exec.RunCalls))
	}
}

func TestExecServiceRunWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	exec := client.Exec().(*mock.ExecService)
	ctx := context.Background()

	exec.OnRun = func(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
		if stdout != nil {
			stdout.Write([]byte("custom output"))
		}
		return 42, nil
	}

	var stdout bytes.Buffer
	config := &domain.ExecConfig{Cmd: []string{"test"}}

	exitCode, err := exec.Run(ctx, "container-id", config, &stdout, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if exitCode != 42 {
		t.Errorf("Run() exitCode = %v, want 42", exitCode)
	}

	if stdout.String() != "custom output" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "custom output")
	}
}

// ImageService Tests

func TestImageServicePull(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	opts := domain.PullOptions{Repository: "alpine", Tag: "latest"}
	reader, err := images.Pull(ctx, opts)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}

	if len(data) == 0 {
		t.Error("Pull() returned empty data")
	}

	if len(images.PullCalls) != 1 {
		t.Errorf("PullCalls = %d, want 1", len(images.PullCalls))
	}
}

func TestImageServicePullWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	expectedErr := errors.New("pull failed")
	images.OnPull = func(ctx context.Context, opts domain.PullOptions) (io.ReadCloser, error) {
		return nil, expectedErr
	}

	_, err := images.Pull(ctx, domain.PullOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("Pull() error = %v, want %v", err, expectedErr)
	}
}

func TestImageServicePullAndWait(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	opts := domain.PullOptions{Repository: "alpine", Tag: "latest"}
	err := images.PullAndWait(ctx, opts)
	if err != nil {
		t.Fatalf("PullAndWait() error = %v", err)
	}

	if len(images.PullCalls) != 1 {
		t.Errorf("PullCalls = %d, want 1", len(images.PullCalls))
	}
	if len(images.PullAndWaitCalls) != 1 {
		t.Errorf("PullAndWaitCalls = %d, want 1", len(images.PullAndWaitCalls))
	}
}

func TestImageServicePullAndWaitWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	expectedErr := errors.New("pull failed")
	images.OnPullAndWait = func(ctx context.Context, opts domain.PullOptions) error {
		return expectedErr
	}

	err := images.PullAndWait(ctx, domain.PullOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("PullAndWait() error = %v, want %v", err, expectedErr)
	}
}

func TestImageServiceList(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	// Set some test images
	testImages := []domain.ImageSummary{
		{ID: "img-1", RepoTags: []string{"alpine:latest"}},
		{ID: "img-2", RepoTags: []string{"nginx:latest"}},
	}
	images.SetImages(testImages)

	list, err := images.List(ctx, domain.ImageListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("List() returned %d images, want 2", len(list))
	}

	if len(images.ListCalls) != 1 {
		t.Errorf("ListCalls = %d, want 1", len(images.ListCalls))
	}
}

func TestImageServiceListWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	expectedErr := errors.New("list failed")
	images.OnList = func(ctx context.Context, opts domain.ImageListOptions) ([]domain.ImageSummary, error) {
		return nil, expectedErr
	}

	_, err := images.List(ctx, domain.ImageListOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("List() error = %v, want %v", err, expectedErr)
	}
}

func TestImageServiceInspect(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	img, err := images.Inspect(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if img.ID != "alpine:latest" {
		t.Errorf("Inspect().ID = %v, want alpine:latest", img.ID)
	}

	if len(images.InspectCalls) != 1 {
		t.Errorf("InspectCalls = %d, want 1", len(images.InspectCalls))
	}
}

func TestImageServiceInspectWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	expectedErr := errors.New("inspect failed")
	images.OnInspect = func(ctx context.Context, imageID string) (*domain.Image, error) {
		return nil, expectedErr
	}

	_, err := images.Inspect(ctx, "alpine:latest")
	if !errors.Is(err, expectedErr) {
		t.Errorf("Inspect() error = %v, want %v", err, expectedErr)
	}
}

func TestImageServiceRemove(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	err := images.Remove(ctx, "image-id", true, false)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if len(images.RemoveCalls) != 1 {
		t.Fatalf("RemoveCalls = %d, want 1", len(images.RemoveCalls))
	}
	if images.RemoveCalls[0].ImageID != "image-id" {
		t.Errorf("RemoveCalls[0].ImageID = %v, want image-id", images.RemoveCalls[0].ImageID)
	}
	if !images.RemoveCalls[0].Force {
		t.Error("RemoveCalls[0].Force = false, want true")
	}
}

func TestImageServiceRemoveWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	expectedErr := errors.New("remove failed")
	images.OnRemove = func(ctx context.Context, imageID string, force, pruneChildren bool) error {
		return expectedErr
	}

	err := images.Remove(ctx, "image-id", false, false)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Remove() error = %v, want %v", err, expectedErr)
	}
}

func TestImageServiceTag(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	err := images.Tag(ctx, "alpine:latest", "alpine:v1")
	if err != nil {
		t.Errorf("Tag() error = %v", err)
	}
}

func TestImageServiceExists(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	// Default returns true (from NewImageService)
	exists, err := images.Exists(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Error("Exists() = false, want true (default)")
	}

	// Set exists to false
	images.SetExistsResult(false)
	exists, err = images.Exists(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if exists {
		t.Error("Exists() = true, want false")
	}

	if len(images.ExistsCalls) != 2 {
		t.Errorf("ExistsCalls = %d, want 2", len(images.ExistsCalls))
	}
}

func TestImageServiceExistsWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	images := client.Images().(*mock.ImageService)
	ctx := context.Background()

	expectedErr := errors.New("exists check failed")
	images.OnExists = func(ctx context.Context, imageRef string) (bool, error) {
		return false, expectedErr
	}

	_, err := images.Exists(ctx, "alpine:latest")
	if !errors.Is(err, expectedErr) {
		t.Errorf("Exists() error = %v, want %v", err, expectedErr)
	}
}

// SystemService Tests

func TestSystemServiceInfo(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	info, err := system.Info(ctx)
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}

	if info == nil {
		t.Fatal("Info() returned nil")
	}

	if system.InfoCalls != 1 {
		t.Errorf("InfoCalls = %d, want 1", system.InfoCalls)
	}
}

func TestSystemServiceInfoWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	customInfo := &domain.SystemInfo{
		ID:            "custom-id",
		ServerVersion: "99.0.0",
	}

	system.OnInfo = func(ctx context.Context) (*domain.SystemInfo, error) {
		return customInfo, nil
	}

	info, err := system.Info(ctx)
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}

	if info.ID != "custom-id" {
		t.Errorf("Info().ID = %v, want custom-id", info.ID)
	}
}

func TestSystemServiceInfoWithError(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	expectedErr := errors.New("info failed")
	system.SetInfoError(expectedErr)

	_, err := system.Info(ctx)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Info() error = %v, want %v", err, expectedErr)
	}
}

func TestSystemServiceSetInfoResult(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	customInfo := &domain.SystemInfo{
		ID:            "test-id",
		ServerVersion: "99.0.0",
	}
	system.SetInfoResult(customInfo)

	info, err := system.Info(ctx)
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}

	if info.ID != "test-id" {
		t.Errorf("Info().ID = %v, want test-id", info.ID)
	}
}

func TestSystemServicePing(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	resp, err := system.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	if resp == nil {
		t.Fatal("Ping() returned nil")
	}

	if system.PingCalls != 1 {
		t.Errorf("PingCalls = %d, want 1", system.PingCalls)
	}
}

func TestSystemServicePingWithError(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	expectedErr := errors.New("ping failed")
	system.SetPingError(expectedErr)

	_, err := system.Ping(ctx)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Ping() error = %v, want %v", err, expectedErr)
	}
}

func TestSystemServiceSetPingResult(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	customPing := &domain.PingResponse{
		APIVersion: "99.99",
		OSType:     "custom-os",
	}
	system.SetPingResult(customPing)

	resp, err := system.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	if resp.APIVersion != "99.99" {
		t.Errorf("Ping().APIVersion = %v, want 99.99", resp.APIVersion)
	}
}

func TestSystemServiceVersion(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	version, err := system.Version(ctx)
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}

	if version == nil {
		t.Fatal("Version() returned nil")
	}

	if system.VersionCalls != 1 {
		t.Errorf("VersionCalls = %d, want 1", system.VersionCalls)
	}
}

func TestSystemServiceVersionWithError(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	expectedErr := errors.New("version failed")
	system.SetVersionError(expectedErr)

	_, err := system.Version(ctx)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Version() error = %v, want %v", err, expectedErr)
	}
}

func TestSystemServiceSetVersionResult(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	customVersion := &domain.Version{
		Version:    "99.0.0",
		APIVersion: "99.99",
	}
	system.SetVersionResult(customVersion)

	version, err := system.Version(ctx)
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}

	if version.Version != "99.0.0" {
		t.Errorf("Version().Version = %v, want 99.0.0", version.Version)
	}
}

func TestSystemServiceDiskUsage(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	customUsage := &domain.DiskUsage{
		LayersSize: 1024,
	}
	system.SetDiskUsageResult(customUsage)

	usage, err := system.DiskUsage(ctx)
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}

	if usage.LayersSize != 1024 {
		t.Errorf("DiskUsage().LayersSize = %v, want 1024", usage.LayersSize)
	}

	if system.DiskUsageCalls != 1 {
		t.Errorf("DiskUsageCalls = %d, want 1", system.DiskUsageCalls)
	}
}

func TestSystemServiceDiskUsageWithError(t *testing.T) {
	client := mock.NewDockerClient()
	system := client.System().(*mock.SystemService)
	ctx := context.Background()

	expectedErr := errors.New("disk usage failed")
	system.SetDiskUsageError(expectedErr)

	_, err := system.DiskUsage(ctx)
	if !errors.Is(err, expectedErr) {
		t.Errorf("DiskUsage() error = %v, want %v", err, expectedErr)
	}
}

// NetworkService Tests

func TestNetworkServiceConnect(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	config := &domain.EndpointSettings{
		IPAddress: "192.168.1.10",
	}

	err := networks.Connect(ctx, "network-id", "container-id", config)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if len(networks.ConnectCalls) != 1 {
		t.Fatalf("ConnectCalls = %d, want 1", len(networks.ConnectCalls))
	}
	if networks.ConnectCalls[0].NetworkID != "network-id" {
		t.Errorf("ConnectCalls[0].NetworkID = %v, want network-id", networks.ConnectCalls[0].NetworkID)
	}
}

func TestNetworkServiceConnectWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	expectedErr := errors.New("connect failed")
	networks.OnConnect = func(ctx context.Context, networkID, containerID string, config *domain.EndpointSettings) error {
		return expectedErr
	}

	err := networks.Connect(ctx, "network-id", "container-id", nil)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Connect() error = %v, want %v", err, expectedErr)
	}
}

func TestNetworkServiceDisconnect(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	err := networks.Disconnect(ctx, "network-id", "container-id", true)
	if err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	if len(networks.DisconnectCalls) != 1 {
		t.Fatalf("DisconnectCalls = %d, want 1", len(networks.DisconnectCalls))
	}
	if !networks.DisconnectCalls[0].Force {
		t.Error("DisconnectCalls[0].Force = false, want true")
	}
}

func TestNetworkServiceDisconnectWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	expectedErr := errors.New("disconnect failed")
	networks.OnDisconnect = func(ctx context.Context, networkID, containerID string, force bool) error {
		return expectedErr
	}

	err := networks.Disconnect(ctx, "network-id", "container-id", false)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Disconnect() error = %v, want %v", err, expectedErr)
	}
}

func TestNetworkServiceList(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	// Set test networks
	testNetworks := []domain.Network{
		{ID: "net-1", Name: "bridge"},
		{ID: "net-2", Name: "host"},
	}
	networks.SetNetworks(testNetworks)

	opts := domain.NetworkListOptions{}
	list, err := networks.List(ctx, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("List() returned %d networks, want 2", len(list))
	}

	if len(networks.ListCalls) != 1 {
		t.Errorf("ListCalls = %d, want 1", len(networks.ListCalls))
	}
}

func TestNetworkServiceListWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	expectedErr := errors.New("list failed")
	networks.OnList = func(ctx context.Context, opts domain.NetworkListOptions) ([]domain.Network, error) {
		return nil, expectedErr
	}

	_, err := networks.List(ctx, domain.NetworkListOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("List() error = %v, want %v", err, expectedErr)
	}
}

func TestNetworkServiceInspect(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	// Set test networks
	testNetworks := []domain.Network{
		{ID: "net-1", Name: "test-network"},
	}
	networks.SetNetworks(testNetworks)

	net, err := networks.Inspect(ctx, "net-1")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if net.ID != "net-1" {
		t.Errorf("Inspect().ID = %v, want net-1", net.ID)
	}

	if len(networks.InspectCalls) != 1 {
		t.Errorf("InspectCalls = %d, want 1", len(networks.InspectCalls))
	}
}

func TestNetworkServiceInspectByName(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	// Set test networks
	testNetworks := []domain.Network{
		{ID: "net-1", Name: "test-network"},
	}
	networks.SetNetworks(testNetworks)

	net, err := networks.Inspect(ctx, "test-network")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if net.ID != "net-1" {
		t.Errorf("Inspect().ID = %v, want net-1", net.ID)
	}
}

func TestNetworkServiceInspectNotFound(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	net, err := networks.Inspect(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	// Should return default network
	if net.ID != "nonexistent" {
		t.Errorf("Inspect().ID = %v, want nonexistent", net.ID)
	}
}

func TestNetworkServiceInspectWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	expectedErr := errors.New("inspect failed")
	networks.OnInspect = func(ctx context.Context, networkID string) (*domain.Network, error) {
		return nil, expectedErr
	}

	_, err := networks.Inspect(ctx, "network-id")
	if !errors.Is(err, expectedErr) {
		t.Errorf("Inspect() error = %v, want %v", err, expectedErr)
	}
}

func TestNetworkServiceCreate(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	id, err := networks.Create(ctx, "test-network", ports.NetworkCreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if id != "mock-network-id" {
		t.Errorf("Create() = %v, want mock-network-id", id)
	}

	if len(networks.CreateCalls) != 1 {
		t.Errorf("CreateCalls = %d, want 1", len(networks.CreateCalls))
	}
}

func TestNetworkServiceCreateWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	expectedErr := errors.New("create failed")
	networks.OnCreate = func(ctx context.Context, name string, opts ports.NetworkCreateOptions) (string, error) {
		return "", expectedErr
	}

	_, err := networks.Create(ctx, "test-network", ports.NetworkCreateOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("Create() error = %v, want %v", err, expectedErr)
	}
}

func TestNetworkServiceRemove(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	err := networks.Remove(ctx, "network-id")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if len(networks.RemoveCalls) != 1 {
		t.Errorf("RemoveCalls = %d, want 1", len(networks.RemoveCalls))
	}
}

func TestNetworkServiceRemoveWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	networks := client.Networks().(*mock.NetworkService)
	ctx := context.Background()

	expectedErr := errors.New("remove failed")
	networks.OnRemove = func(ctx context.Context, networkID string) error {
		return expectedErr
	}

	err := networks.Remove(ctx, "network-id")
	if !errors.Is(err, expectedErr) {
		t.Errorf("Remove() error = %v, want %v", err, expectedErr)
	}
}

// SwarmService Tests

func TestSwarmServiceCreate(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	spec := domain.ServiceSpec{
		Name: "test-service",
	}
	opts := domain.ServiceCreateOptions{}

	id, err := services.Create(ctx, spec, opts)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if id != "mock-service-id" {
		t.Errorf("Create() = %v, want mock-service-id", id)
	}

	if len(services.CreateCalls) != 1 {
		t.Errorf("CreateCalls = %d, want 1", len(services.CreateCalls))
	}
}

func TestSwarmServiceCreateWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	expectedErr := errors.New("create failed")
	services.OnCreate = func(ctx context.Context, spec domain.ServiceSpec, opts domain.ServiceCreateOptions) (string, error) {
		return "", expectedErr
	}

	_, err := services.Create(ctx, domain.ServiceSpec{}, domain.ServiceCreateOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("Create() error = %v, want %v", err, expectedErr)
	}
}

func TestSwarmServiceInspect(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	// Set test services
	testServices := []domain.Service{
		{ID: "svc-1", Spec: domain.ServiceSpec{Name: "test-service"}},
	}
	services.SetServices(testServices)

	svc, err := services.Inspect(ctx, "svc-1")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if svc.ID != "svc-1" {
		t.Errorf("Inspect().ID = %v, want svc-1", svc.ID)
	}

	if len(services.InspectCalls) != 1 {
		t.Errorf("InspectCalls = %d, want 1", len(services.InspectCalls))
	}
}

func TestSwarmServiceInspectNotFound(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	svc, err := services.Inspect(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	// Should return default service
	if svc.ID != "nonexistent" {
		t.Errorf("Inspect().ID = %v, want nonexistent", svc.ID)
	}
}

func TestSwarmServiceInspectWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	expectedErr := errors.New("inspect failed")
	services.OnInspect = func(ctx context.Context, serviceID string) (*domain.Service, error) {
		return nil, expectedErr
	}

	_, err := services.Inspect(ctx, "service-id")
	if !errors.Is(err, expectedErr) {
		t.Errorf("Inspect() error = %v, want %v", err, expectedErr)
	}
}

func TestSwarmServiceList(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	// Set test services
	testServices := []domain.Service{
		{ID: "svc-1", Spec: domain.ServiceSpec{Name: "service-1"}},
		{ID: "svc-2", Spec: domain.ServiceSpec{Name: "service-2"}},
	}
	services.SetServices(testServices)

	list, err := services.List(ctx, domain.ServiceListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("List() returned %d services, want 2", len(list))
	}

	if len(services.ListCalls) != 1 {
		t.Errorf("ListCalls = %d, want 1", len(services.ListCalls))
	}
}

func TestSwarmServiceListWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	expectedErr := errors.New("list failed")
	services.OnList = func(ctx context.Context, opts domain.ServiceListOptions) ([]domain.Service, error) {
		return nil, expectedErr
	}

	_, err := services.List(ctx, domain.ServiceListOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("List() error = %v, want %v", err, expectedErr)
	}
}

func TestSwarmServiceRemove(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	err := services.Remove(ctx, "service-id")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if len(services.RemoveCalls) != 1 {
		t.Errorf("RemoveCalls = %d, want 1", len(services.RemoveCalls))
	}
}

func TestSwarmServiceRemoveWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	expectedErr := errors.New("remove failed")
	services.OnRemove = func(ctx context.Context, serviceID string) error {
		return expectedErr
	}

	err := services.Remove(ctx, "service-id")
	if !errors.Is(err, expectedErr) {
		t.Errorf("Remove() error = %v, want %v", err, expectedErr)
	}
}

func TestSwarmServiceListTasks(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	// Set test tasks
	testTasks := []domain.Task{
		{ID: "task-1", ServiceID: "svc-1"},
		{ID: "task-2", ServiceID: "svc-1"},
	}
	services.SetTasks(testTasks)

	tasks, err := services.ListTasks(ctx, domain.TaskListOptions{})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("ListTasks() returned %d tasks, want 2", len(tasks))
	}

	if len(services.ListTasksCalls) != 1 {
		t.Errorf("ListTasksCalls = %d, want 1", len(services.ListTasksCalls))
	}
}

func TestSwarmServiceListTasksWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	expectedErr := errors.New("list tasks failed")
	services.OnListTasks = func(ctx context.Context, opts domain.TaskListOptions) ([]domain.Task, error) {
		return nil, expectedErr
	}

	_, err := services.ListTasks(ctx, domain.TaskListOptions{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("ListTasks() error = %v, want %v", err, expectedErr)
	}
}

func TestSwarmServiceWaitForTask(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	task, err := services.WaitForTask(ctx, "task-id", 10*time.Second)
	if err != nil {
		t.Fatalf("WaitForTask() error = %v", err)
	}

	if task.ID != "task-id" {
		t.Errorf("WaitForTask().ID = %v, want task-id", task.ID)
	}

	if task.Status.State != domain.TaskStateComplete {
		t.Errorf("WaitForTask().Status.State = %v, want %v", task.Status.State, domain.TaskStateComplete)
	}
}

func TestSwarmServiceWaitForServiceTasks(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)
	ctx := context.Background()

	// Set test tasks
	testTasks := []domain.Task{
		{ID: "task-1", ServiceID: "svc-1", Status: domain.TaskStatus{State: domain.TaskStateRunning}},
		{ID: "task-2", ServiceID: "svc-1", Status: domain.TaskStatus{State: domain.TaskStateRunning}},
	}
	services.SetTasks(testTasks)

	tasks, err := services.WaitForServiceTasks(ctx, "svc-1", 10*time.Second)
	if err != nil {
		t.Fatalf("WaitForServiceTasks() error = %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("WaitForServiceTasks() returned %d tasks, want 2", len(tasks))
	}

	// All tasks should be marked complete
	for _, task := range tasks {
		if task.Status.State != domain.TaskStateComplete {
			t.Errorf("task %s state = %v, want %v", task.ID, task.Status.State, domain.TaskStateComplete)
		}
	}
}

func TestSwarmServiceAddCompletedTask(t *testing.T) {
	client := mock.NewDockerClient()
	services := client.Services().(*mock.SwarmService)

	services.AddCompletedTask("svc-1", "container-1", 0)

	// Verify task was added
	tasks, _ := services.ListTasks(context.Background(), domain.TaskListOptions{})
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}

	task := tasks[0]
	if task.ServiceID != "svc-1" {
		t.Errorf("task.ServiceID = %v, want svc-1", task.ServiceID)
	}
	if task.Status.State != domain.TaskStateComplete {
		t.Errorf("task.Status.State = %v, want %v", task.Status.State, domain.TaskStateComplete)
	}
	if task.Status.ContainerStatus.ExitCode != 0 {
		t.Errorf("task.Status.ContainerStatus.ExitCode = %v, want 0", task.Status.ContainerStatus.ExitCode)
	}
}

// EventService Tests

func TestEventServiceSubscribe(t *testing.T) {
	client := mock.NewDockerClient()
	events := client.Events().(*mock.EventService)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	filter := domain.EventFilter{
		Filters: map[string][]string{"type": {"container"}},
	}
	eventCh, errCh := events.Subscribe(ctx, filter)

	if eventCh == nil {
		t.Error("Subscribe() returned nil eventCh")
	}
	if errCh == nil {
		t.Error("Subscribe() returned nil errCh")
	}

	if len(events.SubscribeCalls) != 1 {
		t.Errorf("SubscribeCalls = %d, want 1", len(events.SubscribeCalls))
	}
}

func TestEventServiceSubscribeWithPredefinedEvents(t *testing.T) {
	client := mock.NewDockerClient()
	events := client.Events().(*mock.EventService)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Add events before subscribing
	testEvent := domain.Event{
		Type:   "container",
		Action: "start",
		Actor: domain.EventActor{
			ID: "container-123",
		},
	}
	events.AddEvent(testEvent)

	filter := domain.EventFilter{}
	eventCh, _ := events.Subscribe(ctx, filter)

	// Receive the event
	select {
	case received := <-eventCh:
		if received.Type != testEvent.Type {
			t.Errorf("received.Type = %v, want %v", received.Type, testEvent.Type)
		}
		if received.Actor.ID != testEvent.Actor.ID {
			t.Errorf("received.Actor.ID = %v, want %v", received.Actor.ID, testEvent.Actor.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive event within timeout")
	}
}

func TestEventServiceSubscribeWithError(t *testing.T) {
	client := mock.NewDockerClient()
	events := client.Events().(*mock.EventService)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	expectedErr := errors.New("subscribe failed")
	events.SetSubscribeError(expectedErr)

	_, errCh := events.Subscribe(ctx, domain.EventFilter{})

	select {
	case err := <-errCh:
		if !errors.Is(err, expectedErr) {
			t.Errorf("Subscribe() error = %v, want %v", err, expectedErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive error within timeout")
	}
}

func TestEventServiceSubscribeWithCallback(t *testing.T) {
	client := mock.NewDockerClient()
	events := client.Events().(*mock.EventService)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	testEvent := domain.Event{
		Type:   "container",
		Action: "start",
		Actor: domain.EventActor{
			ID: "container-123",
		},
	}
	events.AddEvent(testEvent)

	receivedEvents := []domain.Event{}
	callback := func(event domain.Event) error {
		receivedEvents = append(receivedEvents, event)
		return nil
	}

	err := events.SubscribeWithCallback(ctx, domain.EventFilter{}, callback)
	if err != nil {
		t.Fatalf("SubscribeWithCallback() error = %v", err)
	}

	if len(receivedEvents) != 1 {
		t.Fatalf("received %d events, want 1", len(receivedEvents))
	}

	if receivedEvents[0].Type != testEvent.Type {
		t.Errorf("received event type = %v, want %v", receivedEvents[0].Type, testEvent.Type)
	}
}

func TestEventServiceSubscribeWithCallbackError(t *testing.T) {
	client := mock.NewDockerClient()
	events := client.Events().(*mock.EventService)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events.AddEvent(domain.Event{Type: "container"})

	expectedErr := errors.New("callback error")
	callback := func(event domain.Event) error {
		return expectedErr
	}

	err := events.SubscribeWithCallback(ctx, domain.EventFilter{}, callback)
	if !errors.Is(err, expectedErr) {
		t.Errorf("SubscribeWithCallback() error = %v, want %v", err, expectedErr)
	}
}

func TestEventServiceSetEvents(t *testing.T) {
	client := mock.NewDockerClient()
	events := client.Events().(*mock.EventService)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testEvents := []domain.Event{
		{Type: "container", Action: "start"},
		{Type: "container", Action: "stop"},
	}
	events.SetEvents(testEvents)

	eventCh, _ := events.Subscribe(ctx, domain.EventFilter{})

	receivedCount := 0
	for range testEvents {
		select {
		case <-eventCh:
			receivedCount++
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for events")
		}
	}

	if receivedCount != len(testEvents) {
		t.Errorf("received %d events, want %d", receivedCount, len(testEvents))
	}
}

func TestEventServiceAddContainerStopEvent(t *testing.T) {
	client := mock.NewDockerClient()
	events := client.Events().(*mock.EventService)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events.AddContainerStopEvent("container-123")

	eventCh, _ := events.Subscribe(ctx, domain.EventFilter{})

	select {
	case event := <-eventCh:
		if event.Type != domain.EventTypeContainer {
			t.Errorf("event.Type = %v, want %v", event.Type, domain.EventTypeContainer)
		}
		if event.Action != domain.EventActionDie {
			t.Errorf("event.Action = %v, want %v", event.Action, domain.EventActionDie)
		}
		if event.Actor.ID != "container-123" {
			t.Errorf("event.Actor.ID = %v, want container-123", event.Actor.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive event within timeout")
	}
}

func TestEventServiceClearEvents(t *testing.T) {
	client := mock.NewDockerClient()
	events := client.Events().(*mock.EventService)

	events.AddEvent(domain.Event{Type: "container"})
	events.ClearEvents()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	eventCh, _ := events.Subscribe(ctx, domain.EventFilter{})

	select {
	case <-eventCh:
		t.Error("Received event after ClearEvents()")
	case <-ctx.Done():
		// Expected - no events
	}
}

func TestEventServiceOnSubscribeCallback(t *testing.T) {
	client := mock.NewDockerClient()
	events := client.Events().(*mock.EventService)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	customEventCh := make(chan domain.Event, 1)
	customErrCh := make(chan error, 1)

	events.OnSubscribe = func(ctx context.Context, filter domain.EventFilter) (<-chan domain.Event, <-chan error) {
		return customEventCh, customErrCh
	}

	eventCh, errCh := events.Subscribe(ctx, domain.EventFilter{})

	// Send event through custom channel
	customEvent := domain.Event{Type: "custom", Action: "test"}
	customEventCh <- customEvent

	select {
	case received := <-eventCh:
		if received.Type != "custom" {
			t.Errorf("received.Type = %v, want custom", received.Type)
		}
	case <-errCh:
		t.Error("Received error instead of event")
	case <-time.After(time.Second):
		t.Fatal("Did not receive event within timeout")
	}
}

// Concurrent access test

func TestContainerServiceConcurrentAccess(t *testing.T) {
	client := mock.NewDockerClient()
	containers := client.Containers().(*mock.ContainerService)
	ctx := context.Background()

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			containers.Start(ctx, "container-id")
			containers.Stop(ctx, "container-id", nil)
			containers.Inspect(ctx, "container-id")
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify calls were recorded
	if len(containers.StartCalls) != 10 {
		t.Errorf("StartCalls = %d, want 10", len(containers.StartCalls))
	}
	if len(containers.StopCalls) != 10 {
		t.Errorf("StopCalls = %d, want 10", len(containers.StopCalls))
	}
	if len(containers.InspectCalls) != 10 {
		t.Errorf("InspectCalls = %d, want 10", len(containers.InspectCalls))
	}
}
