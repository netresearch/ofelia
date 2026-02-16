package core_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/adapters/mock"
	"github.com/netresearch/ofelia/core/domain"
)

func TestSDKDockerProviderImplementsInterface(t *testing.T) {
	var _ core.DockerProvider = (*core.SDKDockerProvider)(nil)
}

func newTestProvider() (*core.SDKDockerProvider, *mock.DockerClient) {
	mockClient := mock.NewDockerClient()
	provider := core.NewSDKDockerProviderFromClient(mockClient, nil, nil)
	return provider, mockClient
}

func TestSDKDockerProviderCreateContainer(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)
	containers.OnCreate = func(ctx context.Context, config *domain.ContainerConfig) (string, error) {
		return "created-container-id", nil
	}

	config := &domain.ContainerConfig{
		Name:  "test-container",
		Image: "alpine:latest",
		Cmd:   []string{"echo", "hello"},
	}

	id, err := provider.CreateContainer(ctx, config, "test-container")
	if err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}

	if id != "created-container-id" {
		t.Errorf("CreateContainer() = %v, want created-container-id", id)
	}
}

func TestSDKDockerProviderCreateContainerError(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)
	containers.OnCreate = func(ctx context.Context, config *domain.ContainerConfig) (string, error) {
		return "", errors.New("create failed")
	}

	config := &domain.ContainerConfig{Image: "alpine:latest"}
	_, err := provider.CreateContainer(ctx, config, "test")
	if err == nil {
		t.Error("CreateContainer() expected error, got nil")
	}
}

func TestSDKDockerProviderStartContainer(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)

	err := provider.StartContainer(ctx, "container-id")
	if err != nil {
		t.Fatalf("StartContainer() error = %v", err)
	}

	if len(containers.StartCalls) != 1 || containers.StartCalls[0] != "container-id" {
		t.Errorf("StartCalls = %v, want [container-id]", containers.StartCalls)
	}
}

func TestSDKDockerProviderStartContainerError(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)
	containers.OnStart = func(ctx context.Context, containerID string) error {
		return errors.New("start failed")
	}

	err := provider.StartContainer(ctx, "container-id")
	if err == nil {
		t.Error("StartContainer() expected error, got nil")
	}
}

func TestSDKDockerProviderStopContainer(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)

	timeout := 10 * time.Second
	err := provider.StopContainer(ctx, "container-id", &timeout)
	if err != nil {
		t.Fatalf("StopContainer() error = %v", err)
	}

	if len(containers.StopCalls) != 1 {
		t.Fatalf("StopCalls = %d, want 1", len(containers.StopCalls))
	}
	if containers.StopCalls[0].ContainerID != "container-id" {
		t.Errorf("StopCalls[0].ContainerID = %v, want container-id", containers.StopCalls[0].ContainerID)
	}
}

func TestSDKDockerProviderRemoveContainer(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)

	err := provider.RemoveContainer(ctx, "container-id", true)
	if err != nil {
		t.Fatalf("RemoveContainer() error = %v", err)
	}

	if len(containers.RemoveCalls) != 1 {
		t.Fatalf("RemoveCalls = %d, want 1", len(containers.RemoveCalls))
	}
	if !containers.RemoveCalls[0].Options.Force {
		t.Error("RemoveCalls[0].Options.Force = false, want true")
	}
}

func TestSDKDockerProviderInspectContainer(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)
	containers.OnInspect = func(ctx context.Context, containerID string) (*domain.Container, error) {
		return &domain.Container{
			ID:   containerID,
			Name: "test-container",
			State: domain.ContainerState{
				Running: true,
			},
		}, nil
	}

	info, err := provider.InspectContainer(ctx, "container-id")
	if err != nil {
		t.Fatalf("InspectContainer() error = %v", err)
	}

	if info.ID != "container-id" {
		t.Errorf("InspectContainer().ID = %v, want container-id", info.ID)
	}
	if !info.State.Running {
		t.Error("InspectContainer().State.Running = false, want true")
	}
}

func TestSDKDockerProviderWaitContainer(t *testing.T) {
	provider, _ := newTestProvider()
	ctx := context.Background()

	exitCode, err := provider.WaitContainer(ctx, "container-id")
	if err != nil {
		t.Fatalf("WaitContainer() error = %v", err)
	}

	if exitCode != 0 {
		t.Errorf("WaitContainer() exitCode = %v, want 0", exitCode)
	}
}

func TestSDKDockerProviderWaitContainerWithError(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)
	containers.OnWait = func(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse, 1)
		errCh := make(chan error, 1)
		// Send response with error BEFORE closing errCh to ensure it's picked up first
		respCh <- domain.WaitResponse{
			StatusCode: 1,
			Error:      &domain.WaitError{Message: "container failed"},
		}
		close(respCh)
		// Don't close errCh yet - let the select pick respCh first
		go func() {
			// Close errCh after a small delay
			errCh <- nil
			close(errCh)
		}()
		return respCh, errCh
	}

	exitCode, err := provider.WaitContainer(ctx, "container-id")
	// The response contains an error, so we expect an error to be returned
	if err == nil {
		t.Error("WaitContainer() expected error for container failure")
	}
	if exitCode != 1 {
		t.Errorf("WaitContainer() exitCode = %v, want 1", exitCode)
	}
}

func TestSDKDockerProviderGetContainerLogs(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)
	containers.OnLogs = func(ctx context.Context, containerID string, opts domain.LogOptions) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("log output")), nil
	}

	opts := core.ContainerLogsOptions{ShowStdout: true}
	reader, err := provider.GetContainerLogs(ctx, "container-id", opts)
	if err != nil {
		t.Fatalf("GetContainerLogs() error = %v", err)
	}
	defer reader.Close()

	data, _ := io.ReadAll(reader)
	if string(data) != "log output" {
		t.Errorf("GetContainerLogs() = %q, want %q", string(data), "log output")
	}
}

func TestSDKDockerProviderCreateExec(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	exec := mockClient.Exec().(*mock.ExecService)
	exec.OnCreate = func(ctx context.Context, containerID string, config *domain.ExecConfig) (string, error) {
		return "exec-instance-id", nil
	}

	config := &domain.ExecConfig{
		Cmd:          []string{"echo", "hello"},
		AttachStdout: true,
	}

	execID, err := provider.CreateExec(ctx, "container-id", config)
	if err != nil {
		t.Fatalf("CreateExec() error = %v", err)
	}

	if execID != "exec-instance-id" {
		t.Errorf("CreateExec() = %v, want exec-instance-id", execID)
	}
}

func TestSDKDockerProviderStartExec(t *testing.T) {
	provider, _ := newTestProvider()
	ctx := context.Background()

	opts := domain.ExecStartOptions{Tty: true}
	resp, err := provider.StartExec(ctx, "exec-id", opts)
	if err != nil {
		t.Fatalf("StartExec() error = %v", err)
	}

	if resp == nil {
		t.Error("StartExec() returned nil response")
	}
}

func TestSDKDockerProviderInspectExec(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	exec := mockClient.Exec().(*mock.ExecService)
	exec.OnInspect = func(ctx context.Context, execID string) (*domain.ExecInspect, error) {
		return &domain.ExecInspect{
			ID:       execID,
			Running:  false,
			ExitCode: 0,
		}, nil
	}

	info, err := provider.InspectExec(ctx, "exec-id")
	if err != nil {
		t.Fatalf("InspectExec() error = %v", err)
	}

	if info.ID != "exec-id" {
		t.Errorf("InspectExec().ID = %v, want exec-id", info.ID)
	}
}

func TestSDKDockerProviderRunExec(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	exec := mockClient.Exec().(*mock.ExecService)
	exec.OnRun = func(ctx context.Context, containerID string, config *domain.ExecConfig, stdout, stderr io.Writer) (int, error) {
		if stdout != nil {
			stdout.Write([]byte("exec output"))
		}
		return 0, nil
	}

	var stdout bytes.Buffer
	config := &domain.ExecConfig{Cmd: []string{"echo", "test"}}

	exitCode, err := provider.RunExec(ctx, "container-id", config, &stdout, nil)
	if err != nil {
		t.Fatalf("RunExec() error = %v", err)
	}

	if exitCode != 0 {
		t.Errorf("RunExec() exitCode = %v, want 0", exitCode)
	}

	if stdout.String() != "exec output" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "exec output")
	}
}

func TestSDKDockerProviderPullImage(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	images := mockClient.Images().(*mock.ImageService)

	err := provider.PullImage(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("PullImage() error = %v", err)
	}

	if len(images.PullCalls) != 1 {
		t.Errorf("PullCalls = %d, want 1", len(images.PullCalls))
	}
}

func TestSDKDockerProviderHasImageLocally(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	images := mockClient.Images().(*mock.ImageService)

	// Default returns true (from NewImageService)
	exists, err := provider.HasImageLocally(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("HasImageLocally() error = %v", err)
	}
	if !exists {
		t.Error("HasImageLocally() = false, want true (default)")
	}

	// Set exists to false
	images.SetExistsResult(false)
	exists, err = provider.HasImageLocally(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("HasImageLocally() error = %v", err)
	}
	if exists {
		t.Error("HasImageLocally() = true, want false")
	}
}

func TestSDKDockerProviderEnsureImage(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	images := mockClient.Images().(*mock.ImageService)
	images.SetExistsResult(false) // Image doesn't exist

	// Image doesn't exist, should pull
	err := provider.EnsureImage(ctx, "alpine:latest", false)
	if err != nil {
		t.Fatalf("EnsureImage() error = %v", err)
	}

	// Verify pull was called
	if len(images.PullCalls) != 1 {
		t.Errorf("PullCalls = %d, want 1", len(images.PullCalls))
	}
}

func TestSDKDockerProviderEnsureImageExistsLocally(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	images := mockClient.Images().(*mock.ImageService)
	// Default is true in mock, so image exists

	// Image exists, should not pull
	err := provider.EnsureImage(ctx, "alpine:latest", false)
	if err != nil {
		t.Fatalf("EnsureImage() error = %v", err)
	}

	// Verify pull was NOT called
	if len(images.PullCalls) != 0 {
		t.Errorf("PullCalls = %d, want 0 (image exists)", len(images.PullCalls))
	}
}

func TestSDKDockerProviderEnsureImageForcePull(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	// images default exists to true

	// Force pull even if exists
	err := provider.EnsureImage(ctx, "alpine:latest", true)
	if err != nil {
		t.Fatalf("EnsureImage() error = %v", err)
	}

	images := mockClient.Images().(*mock.ImageService)
	// Verify pull was called
	if len(images.PullCalls) != 1 {
		t.Errorf("PullCalls = %d, want 1 (force pull)", len(images.PullCalls))
	}
}

func TestSDKDockerProviderConnectNetwork(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	networks := mockClient.Networks().(*mock.NetworkService)

	err := provider.ConnectNetwork(ctx, "network-id", "container-id")
	if err != nil {
		t.Fatalf("ConnectNetwork() error = %v", err)
	}

	if len(networks.ConnectCalls) != 1 {
		t.Fatalf("ConnectCalls = %d, want 1", len(networks.ConnectCalls))
	}
	if networks.ConnectCalls[0].NetworkID != "network-id" {
		t.Errorf("ConnectCalls[0].NetworkID = %v, want network-id", networks.ConnectCalls[0].NetworkID)
	}
	if networks.ConnectCalls[0].ContainerID != "container-id" {
		t.Errorf("ConnectCalls[0].ContainerID = %v, want container-id", networks.ConnectCalls[0].ContainerID)
	}
}

func TestSDKDockerProviderFindNetworkByName(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	networks := mockClient.Networks().(*mock.NetworkService)
	networks.OnList = func(ctx context.Context, opts domain.NetworkListOptions) ([]domain.Network, error) {
		return []domain.Network{
			{ID: "network-1", Name: "test-network"},
		}, nil
	}

	result, err := provider.FindNetworkByName(ctx, "test-network")
	if err != nil {
		t.Fatalf("FindNetworkByName() error = %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("FindNetworkByName() returned %d networks, want 1", len(result))
	}
	if result[0].Name != "test-network" {
		t.Errorf("FindNetworkByName()[0].Name = %v, want test-network", result[0].Name)
	}
}

func TestSDKDockerProviderSubscribeEvents(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := t.Context()

	events := mockClient.Events().(*mock.EventService)

	// Add event before subscribing
	testEvent := domain.Event{Type: "container", Action: "start"}
	events.AddEvent(testEvent)

	filter := domain.EventFilter{
		Filters: map[string][]string{"type": {"container"}},
	}
	eventCh, errCh := provider.SubscribeEvents(ctx, filter)

	if eventCh == nil {
		t.Error("SubscribeEvents() returned nil eventCh")
	}
	if errCh == nil {
		t.Error("SubscribeEvents() returned nil errCh")
	}

	// Verify event is received
	select {
	case received := <-eventCh:
		if received.Type != "container" {
			t.Errorf("received.Type = %v, want container", received.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive event within timeout")
	}
}

func TestSDKDockerProviderInfo(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	system := mockClient.System().(*mock.SystemService)
	system.OnInfo = func(ctx context.Context) (*domain.SystemInfo, error) {
		return &domain.SystemInfo{
			ID:            "test-docker",
			Containers:    10,
			ServerVersion: "24.0.0",
		}, nil
	}

	info, err := provider.Info(ctx)
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}

	if info.ID != "test-docker" {
		t.Errorf("Info().ID = %v, want test-docker", info.ID)
	}
	if info.Containers != 10 {
		t.Errorf("Info().Containers = %v, want 10", info.Containers)
	}
}

func TestSDKDockerProviderPing(t *testing.T) {
	provider, _ := newTestProvider()
	ctx := context.Background()

	err := provider.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestSDKDockerProviderPingError(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx := context.Background()

	system := mockClient.System().(*mock.SystemService)
	system.OnPing = func(ctx context.Context) (*domain.PingResponse, error) {
		return nil, errors.New("connection refused")
	}

	err := provider.Ping(ctx)
	if err == nil {
		t.Error("Ping() expected error, got nil")
	}
}

func TestSDKDockerProviderClose(t *testing.T) {
	provider, mockClient := newTestProvider()

	err := provider.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !mockClient.IsClosed() {
		t.Error("Close() did not close the underlying client")
	}
}

func TestSDKDockerProviderCloseError(t *testing.T) {
	provider, mockClient := newTestProvider()

	expectedErr := errors.New("close error")
	mockClient.SetCloseError(expectedErr)

	err := provider.Close()
	if !errors.Is(err, expectedErr) {
		t.Errorf("Close() = %v, want %v", err, expectedErr)
	}
}

// Context cancellation tests

func TestSDKDockerProviderWaitContainerContextCanceled(t *testing.T) {
	provider, mockClient := newTestProvider()
	ctx, cancel := context.WithCancel(context.Background())

	containers := mockClient.Containers().(*mock.ContainerService)
	containers.OnWait = func(ctx context.Context, containerID string) (<-chan domain.WaitResponse, <-chan error) {
		respCh := make(chan domain.WaitResponse)
		errCh := make(chan error, 1)

		go func() {
			<-ctx.Done()
			errCh <- ctx.Err()
			close(errCh)
			close(respCh)
		}()

		return respCh, errCh
	}

	// Cancel context immediately
	cancel()

	_, err := provider.WaitContainer(ctx, "container-id")
	if err == nil {
		t.Error("WaitContainer() expected context canceled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("WaitContainer() error = %v, want context.Canceled", err)
	}
}

// Test with logger and metrics

// sdkTestHandler is a slog.Handler that captures log records for testing.
type sdkTestHandler struct {
	mu      sync.Mutex
	records []sdkTestRecord
}

type sdkTestRecord struct {
	level   slog.Level
	message string
}

func (h *sdkTestHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *sdkTestHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, sdkTestRecord{level: r.Level, message: r.Message})
	return nil
}

func (h *sdkTestHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *sdkTestHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *sdkTestHandler) infoMessages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var msgs []string
	for _, r := range h.records {
		if r.level == slog.LevelInfo {
			msgs = append(msgs, r.message)
		}
	}
	return msgs
}

func (h *sdkTestHandler) debugMessages() []string { //nolint:unused // kept for future test assertions
	h.mu.Lock()
	defer h.mu.Unlock()
	var msgs []string
	for _, r := range h.records {
		if r.level == slog.LevelDebug {
			msgs = append(msgs, r.message)
		}
	}
	return msgs
}

func newSDKTestLogger() (*slog.Logger, *sdkTestHandler) {
	handler := &sdkTestHandler{}
	return slog.New(handler), handler
}

type testMetrics struct {
	operations []string
	errors     []string
}

func (m *testMetrics) RecordDockerOperation(name string) {
	m.operations = append(m.operations, name)
}

func (m *testMetrics) RecordDockerError(name string) {
	m.errors = append(m.errors, name)
}

func (m *testMetrics) RecordJobRetry(jobName string, attempt int, success bool) {}
func (m *testMetrics) RecordContainerEvent()                                    {}
func (m *testMetrics) RecordContainerMonitorFallback()                          {}
func (m *testMetrics) RecordContainerMonitorMethod(usingEvents bool)            {}
func (m *testMetrics) RecordContainerWaitDuration(seconds float64)              {}
func (m *testMetrics) RecordJobStart(jobName string)                            {}
func (m *testMetrics) RecordJobComplete(jobName string, _ float64, _ bool)      {}
func (m *testMetrics) RecordJobScheduled(jobName string)                        {}

func TestSDKDockerProviderWithLogger(t *testing.T) {
	mockClient := mock.NewDockerClient()
	logger, handler := newSDKTestLogger()
	provider := core.NewSDKDockerProviderFromClient(mockClient, logger, nil)
	ctx := context.Background()

	_, err := provider.CreateContainer(ctx, &domain.ContainerConfig{Image: "alpine"}, "test")
	if err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}

	if len(handler.infoMessages()) != 1 {
		t.Errorf("info messages = %d, want 1", len(handler.infoMessages()))
	}
}

func TestSDKDockerProviderWithMetrics(t *testing.T) {
	mockClient := mock.NewDockerClient()
	metrics := &testMetrics{}
	provider := core.NewSDKDockerProviderFromClient(mockClient, nil, metrics)
	ctx := context.Background()

	_, err := provider.CreateContainer(ctx, &domain.ContainerConfig{Image: "alpine"}, "test")
	if err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}

	if len(metrics.operations) != 1 {
		t.Errorf("metrics.operations = %d, want 1", len(metrics.operations))
	}
	if metrics.operations[0] != "create_container" {
		t.Errorf("metrics.operations[0] = %v, want create_container", metrics.operations[0])
	}
}

func TestSDKDockerProviderMetricsOnError(t *testing.T) {
	mockClient := mock.NewDockerClient()
	metrics := &testMetrics{}
	provider := core.NewSDKDockerProviderFromClient(mockClient, nil, metrics)
	ctx := context.Background()

	containers := mockClient.Containers().(*mock.ContainerService)
	containers.OnStart = func(ctx context.Context, containerID string) error {
		return errors.New("start failed")
	}

	err := provider.StartContainer(ctx, "container-id")
	if err == nil {
		t.Error("StartContainer() expected error")
	}

	if len(metrics.errors) != 1 {
		t.Errorf("metrics.errors = %d, want 1", len(metrics.errors))
	}
	if metrics.errors[0] != "start_container" {
		t.Errorf("metrics.errors[0] = %v, want start_container", metrics.errors[0])
	}
}
