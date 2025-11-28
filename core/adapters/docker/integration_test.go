//go:build integration

package docker_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	dockeradapter "github.com/netresearch/ofelia/core/adapters/docker"
	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

const (
	testImage        = "alpine:latest"
	testNetwork      = "ofelia-test-network"
	testTimeout      = 30 * time.Second
	containerTimeout = 5 * time.Second
)

// setupClient creates a Docker client for testing.
func setupClient(t *testing.T) ports.DockerClient {
	t.Helper()
	client, err := dockeradapter.NewClient()
	if err != nil {
		skipOrFailDockerUnavailable(t, err)
	}
	return client
}

// ensureImage ensures the test image is available.
func ensureImage(t *testing.T, client ports.DockerClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	exists, err := client.Images().Exists(ctx, testImage)
	if err != nil {
		t.Fatalf("Failed to check if image exists: %v", err)
	}

	if !exists {
		t.Logf("Pulling test image: %s", testImage)
		err = client.Images().PullAndWait(ctx, domain.PullOptions{
			Repository: "alpine",
			Tag:        "latest",
		})
		if err != nil {
			t.Fatalf("Failed to pull test image: %v", err)
		}
	}
}

// TestSystemOperations tests system-level Docker operations.
func TestSystemOperations(t *testing.T) {
	client := setupClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	t.Run("Ping", func(t *testing.T) {
		resp, err := client.System().Ping(ctx)
		if err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Ping returned nil response")
		}
		if resp.APIVersion == "" {
			t.Error("APIVersion is empty")
		}
		t.Logf("Docker API Version: %s", resp.APIVersion)
	})

	t.Run("Version", func(t *testing.T) {
		version, err := client.System().Version(ctx)
		if err != nil {
			t.Fatalf("Version failed: %v", err)
		}
		if version == nil {
			t.Fatal("Version returned nil")
		}
		if version.Version == "" {
			t.Error("Version string is empty")
		}
		t.Logf("Docker Version: %s", version.Version)
	})

	t.Run("Info", func(t *testing.T) {
		info, err := client.System().Info(ctx)
		if err != nil {
			t.Fatalf("Info failed: %v", err)
		}
		if info == nil {
			t.Fatal("Info returned nil")
		}
		if info.ID == "" {
			t.Error("Docker ID is empty")
		}
		t.Logf("Docker containers: %d running, %d total", info.ContainersRunning, info.Containers)
	})

	t.Run("DiskUsage", func(t *testing.T) {
		du, err := client.System().DiskUsage(ctx)
		if err != nil {
			t.Fatalf("DiskUsage failed: %v", err)
		}
		if du == nil {
			t.Fatal("DiskUsage returned nil")
		}
		t.Logf("Disk usage - Images: %d, Containers: %d, Volumes: %d",
			len(du.Images), len(du.Containers), len(du.Volumes))
	})
}

// TestImageOperations tests image-related operations.
func TestImageOperations(t *testing.T) {
	client := setupClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	t.Run("PullAndWait", func(t *testing.T) {
		err := client.Images().PullAndWait(ctx, domain.PullOptions{
			Repository: "alpine",
			Tag:        "latest",
		})
		if err != nil {
			t.Fatalf("PullAndWait failed: %v", err)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		exists, err := client.Images().Exists(ctx, testImage)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if !exists {
			t.Error("Image should exist after pull")
		}

		// Test non-existent image
		exists, err = client.Images().Exists(ctx, "nonexistent:image")
		if err != nil {
			t.Fatalf("Exists check for nonexistent image failed: %v", err)
		}
		if exists {
			t.Error("Nonexistent image should not exist")
		}
	})

	t.Run("List", func(t *testing.T) {
		images, err := client.Images().List(ctx, domain.ImageListOptions{
			All: false,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(images) == 0 {
			t.Error("Expected at least one image")
		}
		t.Logf("Found %d images", len(images))
	})

	t.Run("ListWithFilters", func(t *testing.T) {
		images, err := client.Images().List(ctx, domain.ImageListOptions{
			All: true,
			Filters: map[string][]string{
				"reference": {"alpine:latest"},
			},
		})
		if err != nil {
			t.Fatalf("List with filters failed: %v", err)
		}
		if len(images) == 0 {
			t.Error("Expected to find alpine:latest image")
		}
	})

	t.Run("Inspect", func(t *testing.T) {
		img, err := client.Images().Inspect(ctx, testImage)
		if err != nil {
			t.Fatalf("Inspect failed: %v", err)
		}
		if img == nil {
			t.Fatal("Inspect returned nil")
		}
		if img.ID == "" {
			t.Error("Image ID is empty")
		}
		t.Logf("Image ID: %s", img.ID)
	})
}

// TestContainerLifecycle tests the complete container lifecycle.
func TestContainerLifecycle(t *testing.T) {
	client := setupClient(t)
	defer client.Close()
	ensureImage(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	var containerID string

	t.Run("Create", func(t *testing.T) {
		config := &domain.ContainerConfig{
			Image: testImage,
			Cmd:   []string{"echo", "hello"},
			Labels: map[string]string{
				"ofelia.test": "integration",
			},
			Name: fmt.Sprintf("ofelia-test-%d", time.Now().Unix()),
		}

		var err error
		containerID, err = client.Containers().Create(ctx, config)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if containerID == "" {
			t.Fatal("Container ID is empty")
		}
		t.Logf("Created container: %s", containerID)
	})

	// Ensure cleanup
	defer func() {
		if containerID != "" {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), testTimeout)
			defer cleanupCancel()
			_ = client.Containers().Remove(cleanupCtx, containerID, domain.RemoveOptions{Force: true})
		}
	}()

	t.Run("Inspect", func(t *testing.T) {
		container, err := client.Containers().Inspect(ctx, containerID)
		if err != nil {
			t.Fatalf("Inspect failed: %v", err)
		}
		if container == nil {
			t.Fatal("Inspect returned nil")
		}
		if container.ID != containerID {
			t.Errorf("Container ID mismatch: got %s, want %s", container.ID, containerID)
		}
	})

	t.Run("Start", func(t *testing.T) {
		err := client.Containers().Start(ctx, containerID)
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
	})

	t.Run("Wait", func(t *testing.T) {
		respCh, errCh := client.Containers().Wait(ctx, containerID)

		select {
		case resp := <-respCh:
			if resp.StatusCode != 0 {
				t.Errorf("Expected exit code 0, got %d", resp.StatusCode)
			}
		case err := <-errCh:
			t.Fatalf("Wait failed: %v", err)
		case <-time.After(containerTimeout):
			t.Fatal("Wait timed out")
		}
	})

	t.Run("Logs", func(t *testing.T) {
		reader, err := client.Containers().Logs(ctx, containerID, domain.LogOptions{
			ShowStdout: true,
			ShowStderr: true,
		})
		if err != nil {
			t.Fatalf("Logs failed: %v", err)
		}
		defer reader.Close()

		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, reader)
		if err != nil {
			t.Fatalf("Failed to read logs: %v", err)
		}

		logs := buf.String()
		if !strings.Contains(logs, "hello") {
			t.Errorf("Expected 'hello' in logs, got: %s", logs)
		}
	})

	t.Run("List", func(t *testing.T) {
		containers, err := client.Containers().List(ctx, domain.ListOptions{
			All: true,
			Filters: map[string][]string{
				"label": {"ofelia.test=integration"},
			},
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(containers) == 0 {
			t.Error("Expected at least one container")
		}
		found := false
		for _, c := range containers {
			if c.ID == containerID || strings.HasPrefix(c.ID, containerID[:12]) {
				found = true
				break
			}
		}
		if !found {
			t.Error("Created container not found in list")
		}
	})

	t.Run("Remove", func(t *testing.T) {
		err := client.Containers().Remove(ctx, containerID, domain.RemoveOptions{
			Force: true,
		})
		if err != nil {
			t.Fatalf("Remove failed: %v", err)
		}
		containerID = "" // Mark as cleaned up
	})
}

// TestContainerStopAndKill tests stopping and killing containers.
func TestContainerStopAndKill(t *testing.T) {
	client := setupClient(t)
	defer client.Close()
	ensureImage(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	t.Run("Stop", func(t *testing.T) {
		// Create long-running container
		containerID, err := client.Containers().Create(ctx, &domain.ContainerConfig{
			Image: testImage,
			Cmd:   []string{"sleep", "30"},
			Name:  fmt.Sprintf("ofelia-stop-test-%d", time.Now().Unix()),
		})
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		defer client.Containers().Remove(ctx, containerID, domain.RemoveOptions{Force: true})

		err = client.Containers().Start(ctx, containerID)
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		timeout := 2 * time.Second
		err = client.Containers().Stop(ctx, containerID, &timeout)
		if err != nil {
			t.Fatalf("Stop failed: %v", err)
		}

		// Verify container is stopped
		container, err := client.Containers().Inspect(ctx, containerID)
		if err != nil {
			t.Fatalf("Inspect failed: %v", err)
		}
		if container.State.Running {
			t.Error("Container should not be running after stop")
		}
	})

	t.Run("Kill", func(t *testing.T) {
		// Create long-running container
		containerID, err := client.Containers().Create(ctx, &domain.ContainerConfig{
			Image: testImage,
			Cmd:   []string{"sleep", "30"},
			Name:  fmt.Sprintf("ofelia-kill-test-%d", time.Now().Unix()),
		})
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		defer client.Containers().Remove(ctx, containerID, domain.RemoveOptions{Force: true})

		err = client.Containers().Start(ctx, containerID)
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		err = client.Containers().Kill(ctx, containerID, "SIGKILL")
		if err != nil {
			t.Fatalf("Kill failed: %v", err)
		}

		// Wait a moment for the kill to take effect
		time.Sleep(500 * time.Millisecond)

		// Verify container is not running
		container, err := client.Containers().Inspect(ctx, containerID)
		if err != nil {
			t.Fatalf("Inspect failed: %v", err)
		}
		if container.State.Running {
			t.Error("Container should not be running after kill")
		}
	})
}

// TestExecOperations tests container exec functionality.
func TestExecOperations(t *testing.T) {
	client := setupClient(t)
	defer client.Close()
	ensureImage(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create a long-running container for exec tests
	containerID, err := client.Containers().Create(ctx, &domain.ContainerConfig{
		Image: testImage,
		Cmd:   []string{"sleep", "30"},
		Name:  fmt.Sprintf("ofelia-exec-test-%d", time.Now().Unix()),
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer client.Containers().Remove(ctx, containerID, domain.RemoveOptions{Force: true})

	err = client.Containers().Start(ctx, containerID)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	t.Run("CreateAndInspect", func(t *testing.T) {
		execID, err := client.Exec().Create(ctx, containerID, &domain.ExecConfig{
			Cmd:          []string{"echo", "test"},
			AttachStdout: true,
			AttachStderr: true,
		})
		if err != nil {
			t.Fatalf("Create exec failed: %v", err)
		}
		if execID == "" {
			t.Fatal("Exec ID is empty")
		}

		inspect, err := client.Exec().Inspect(ctx, execID)
		if err != nil {
			t.Fatalf("Inspect exec failed: %v", err)
		}
		if inspect == nil {
			t.Fatal("Inspect returned nil")
		}
		if inspect.ContainerID != containerID {
			t.Errorf("Container ID mismatch: got %s, want %s", inspect.ContainerID, containerID)
		}
	})

	t.Run("Run", func(t *testing.T) {
		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		exitCode, err := client.Exec().Run(ctx, containerID, &domain.ExecConfig{
			Cmd:          []string{"echo", "hello-exec"},
			AttachStdout: true,
			AttachStderr: true,
		}, stdout, stderr)
		if err != nil {
			t.Fatalf("Run exec failed: %v", err)
		}
		if exitCode != 0 {
			t.Errorf("Expected exit code 0, got %d", exitCode)
		}
		if !strings.Contains(stdout.String(), "hello-exec") {
			t.Errorf("Expected 'hello-exec' in stdout, got: %s", stdout.String())
		}
	})

	t.Run("RunWithError", func(t *testing.T) {
		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		exitCode, err := client.Exec().Run(ctx, containerID, &domain.ExecConfig{
			Cmd:          []string{"sh", "-c", "exit 42"},
			AttachStdout: true,
			AttachStderr: true,
		}, stdout, stderr)
		if err != nil {
			t.Fatalf("Run exec failed: %v", err)
		}
		if exitCode != 42 {
			t.Errorf("Expected exit code 42, got %d", exitCode)
		}
	})
}

// TestNetworkOperations tests network-related operations.
func TestNetworkOperations(t *testing.T) {
	client := setupClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	networkName := fmt.Sprintf("%s-%d", testNetwork, time.Now().Unix())
	var networkID string

	t.Run("Create", func(t *testing.T) {
		var err error
		networkID, err = client.Networks().Create(ctx, networkName, ports.NetworkCreateOptions{
			Driver: "bridge",
			Labels: map[string]string{
				"ofelia.test": "integration",
			},
		})
		if err != nil {
			t.Fatalf("Create network failed: %v", err)
		}
		if networkID == "" {
			t.Fatal("Network ID is empty")
		}
		t.Logf("Created network: %s", networkID)
	})

	defer func() {
		if networkID != "" {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), testTimeout)
			defer cleanupCancel()
			_ = client.Networks().Remove(cleanupCtx, networkID)
		}
	}()

	t.Run("Inspect", func(t *testing.T) {
		network, err := client.Networks().Inspect(ctx, networkID)
		if err != nil {
			t.Fatalf("Inspect network failed: %v", err)
		}
		if network == nil {
			t.Fatal("Inspect returned nil")
		}
		if network.ID != networkID {
			t.Errorf("Network ID mismatch: got %s, want %s", network.ID, networkID)
		}
	})

	t.Run("List", func(t *testing.T) {
		networks, err := client.Networks().List(ctx, domain.NetworkListOptions{
			Filters: map[string][]string{
				"label": {"ofelia.test=integration"},
			},
		})
		if err != nil {
			t.Fatalf("List networks failed: %v", err)
		}
		if len(networks) == 0 {
			t.Error("Expected at least one network")
		}
	})

	t.Run("ConnectAndDisconnect", func(t *testing.T) {
		ensureImage(t, client)

		// Create a container to connect
		containerID, err := client.Containers().Create(ctx, &domain.ContainerConfig{
			Image: testImage,
			Cmd:   []string{"sleep", "30"},
			Name:  fmt.Sprintf("ofelia-network-test-%d", time.Now().Unix()),
		})
		if err != nil {
			t.Fatalf("Create container failed: %v", err)
		}
		defer client.Containers().Remove(ctx, containerID, domain.RemoveOptions{Force: true})

		err = client.Containers().Start(ctx, containerID)
		if err != nil {
			t.Fatalf("Start container failed: %v", err)
		}

		// Connect to network
		err = client.Networks().Connect(ctx, networkID, containerID, nil)
		if err != nil {
			t.Fatalf("Connect to network failed: %v", err)
		}

		// Verify connection
		network, err := client.Networks().Inspect(ctx, networkID)
		if err != nil {
			t.Fatalf("Inspect network failed: %v", err)
		}
		if _, connected := network.Containers[containerID]; !connected {
			t.Error("Container should be connected to network")
		}

		// Disconnect from network
		err = client.Networks().Disconnect(ctx, networkID, containerID, false)
		if err != nil {
			t.Fatalf("Disconnect from network failed: %v", err)
		}
	})

	t.Run("Remove", func(t *testing.T) {
		err := client.Networks().Remove(ctx, networkID)
		if err != nil {
			t.Fatalf("Remove network failed: %v", err)
		}
		networkID = "" // Mark as cleaned up
	})
}

// TestEventSubscription tests Docker event subscription.
func TestEventSubscription(t *testing.T) {
	client := setupClient(t)
	defer client.Close()
	ensureImage(t, client)

	t.Run("SubscribeToEvents", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		// Subscribe to container events
		eventCh, errCh := client.Events().Subscribe(ctx, domain.EventFilter{
			Filters: map[string][]string{
				"type": {"container"},
			},
		})

		// Create a container to generate events
		containerID, err := client.Containers().Create(ctx, &domain.ContainerConfig{
			Image: testImage,
			Cmd:   []string{"echo", "event-test"},
			Name:  fmt.Sprintf("ofelia-event-test-%d", time.Now().Unix()),
		})
		if err != nil {
			t.Fatalf("Create container failed: %v", err)
		}
		defer client.Containers().Remove(ctx, containerID, domain.RemoveOptions{Force: true})

		// Wait for at least one event
		select {
		case event := <-eventCh:
			if event.Type != "container" {
				t.Errorf("Expected container event, got %s", event.Type)
			}
			t.Logf("Received event: %s %s", event.Type, event.Action)
		case err := <-errCh:
			t.Fatalf("Event subscription error: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for events")
		}
	})

	t.Run("SubscribeWithCallback", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		eventReceived := false
		callback := func(event domain.Event) error {
			if event.Type == "container" {
				eventReceived = true
			}
			// Cancel after first event to exit
			cancel()
			return nil
		}

		// Start subscription in goroutine
		errCh := make(chan error, 1)
		go func() {
			err := client.Events().SubscribeWithCallback(ctx, domain.EventFilter{
				Filters: map[string][]string{
					"type": {"container"},
				},
			}, callback)
			errCh <- err
		}()

		// Give subscription time to start
		time.Sleep(100 * time.Millisecond)

		// Create a container to generate event
		createCtx, createCancel := context.WithTimeout(context.Background(), testTimeout)
		defer createCancel()
		containerID, err := client.Containers().Create(createCtx, &domain.ContainerConfig{
			Image: testImage,
			Cmd:   []string{"echo", "callback-test"},
			Name:  fmt.Sprintf("ofelia-callback-test-%d", time.Now().Unix()),
		})
		if err != nil {
			t.Fatalf("Create container failed: %v", err)
		}
		defer client.Containers().Remove(createCtx, containerID, domain.RemoveOptions{Force: true})

		// Wait for callback or timeout
		select {
		case err := <-errCh:
			if err != nil && err != context.Canceled {
				t.Fatalf("Callback subscription error: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for callback")
		}

		if !eventReceived {
			t.Error("Expected to receive container event in callback")
		}
	})
}

// TestSwarmServiceOperations tests Swarm service operations.
// This test will be skipped if Docker is not in Swarm mode.
func TestSwarmServiceOperations(t *testing.T) {
	client := setupClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Check if Docker is in Swarm mode
	info, err := client.System().Info(ctx)
	if err != nil {
		t.Fatalf("Failed to get system info: %v", err)
	}

	if info.Swarm.LocalNodeState != domain.LocalNodeStateActive {
		t.Skip("Skipping Swarm tests - Docker not in Swarm mode")
	}

	ensureImage(t, client)

	var serviceID string

	t.Run("CreateService", func(t *testing.T) {
		replicas := uint64(1)
		serviceID, err = client.Services().Create(ctx, domain.ServiceSpec{
			Name: fmt.Sprintf("ofelia-test-service-%d", time.Now().Unix()),
			Labels: map[string]string{
				"ofelia.test": "integration",
			},
			TaskTemplate: domain.TaskSpec{
				ContainerSpec: domain.ContainerSpec{
					Image:   testImage,
					Command: []string{"sleep", "10"},
				},
			},
			Mode: domain.ServiceMode{
				Replicated: &domain.ReplicatedService{
					Replicas: &replicas,
				},
			},
		}, domain.ServiceCreateOptions{})
		if err != nil {
			t.Fatalf("Create service failed: %v", err)
		}
		if serviceID == "" {
			t.Fatal("Service ID is empty")
		}
		t.Logf("Created service: %s", serviceID)
	})

	defer func() {
		if serviceID != "" {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), testTimeout)
			defer cleanupCancel()
			_ = client.Services().Remove(cleanupCtx, serviceID)
		}
	}()

	t.Run("InspectService", func(t *testing.T) {
		service, err := client.Services().Inspect(ctx, serviceID)
		if err != nil {
			t.Fatalf("Inspect service failed: %v", err)
		}
		if service == nil {
			t.Fatal("Inspect returned nil")
		}
		if service.ID != serviceID {
			t.Errorf("Service ID mismatch: got %s, want %s", service.ID, serviceID)
		}
	})

	t.Run("ListServices", func(t *testing.T) {
		services, err := client.Services().List(ctx, domain.ServiceListOptions{
			Filters: map[string][]string{
				"label": {"ofelia.test=integration"},
			},
		})
		if err != nil {
			t.Fatalf("List services failed: %v", err)
		}
		if len(services) == 0 {
			t.Error("Expected at least one service")
		}
	})

	t.Run("ListTasks", func(t *testing.T) {
		// Wait a moment for tasks to be created
		time.Sleep(2 * time.Second)

		tasks, err := client.Services().ListTasks(ctx, domain.TaskListOptions{
			Filters: map[string][]string{
				"service": {serviceID},
			},
		})
		if err != nil {
			t.Fatalf("List tasks failed: %v", err)
		}
		if len(tasks) == 0 {
			t.Error("Expected at least one task")
		}
		t.Logf("Found %d tasks for service", len(tasks))
	})

	t.Run("RemoveService", func(t *testing.T) {
		err := client.Services().Remove(ctx, serviceID)
		if err != nil {
			t.Fatalf("Remove service failed: %v", err)
		}
		serviceID = "" // Mark as cleaned up
	})
}

// TestContainerWithHostConfig tests container creation with advanced host configuration.
func TestContainerWithHostConfig(t *testing.T) {
	client := setupClient(t)
	defer client.Close()
	ensureImage(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	containerID, err := client.Containers().Create(ctx, &domain.ContainerConfig{
		Image: testImage,
		Cmd:   []string{"echo", "hostconfig"},
		Name:  fmt.Sprintf("ofelia-hostconfig-test-%d", time.Now().Unix()),
		HostConfig: &domain.HostConfig{
			Memory:     64 * 1024 * 1024, // 64MB
			AutoRemove: false,
			RestartPolicy: domain.RestartPolicy{
				Name: "no",
			},
		},
	})
	if err != nil {
		t.Fatalf("Create with host config failed: %v", err)
	}
	defer client.Containers().Remove(ctx, containerID, domain.RemoveOptions{Force: true})

	// Verify the container was created successfully
	container, err := client.Containers().Inspect(ctx, containerID)
	if err != nil {
		t.Fatalf("Inspect failed: %v", err)
	}
	if container == nil {
		t.Fatal("Inspect returned nil")
	}
	if container.ID != containerID {
		t.Errorf("Container ID mismatch: got %s, want %s", container.ID, containerID)
	}
	t.Logf("Created container with host config: %s", containerID)
}

// TestCopyLogs tests the CopyLogs functionality.
func TestCopyLogs(t *testing.T) {
	client := setupClient(t)
	defer client.Close()
	ensureImage(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create and run container
	containerID, err := client.Containers().Create(ctx, &domain.ContainerConfig{
		Image: testImage,
		Cmd:   []string{"sh", "-c", "echo stdout-test && echo stderr-test >&2"},
		Name:  fmt.Sprintf("ofelia-copylogs-test-%d", time.Now().Unix()),
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer client.Containers().Remove(ctx, containerID, domain.RemoveOptions{Force: true})

	err = client.Containers().Start(ctx, containerID)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for container to finish
	respCh, errCh := client.Containers().Wait(ctx, containerID)
	select {
	case <-respCh:
	case err := <-errCh:
		t.Fatalf("Wait failed: %v", err)
	case <-time.After(containerTimeout):
		t.Fatal("Wait timed out")
	}

	// Copy logs
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err = client.Containers().CopyLogs(ctx, containerID, stdout, stderr, domain.LogOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		t.Fatalf("CopyLogs failed: %v", err)
	}

	if !strings.Contains(stdout.String(), "stdout-test") {
		t.Errorf("Expected 'stdout-test' in stdout, got: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "stderr-test") {
		t.Errorf("Expected 'stderr-test' in stderr, got: %s", stderr.String())
	}
}

// TestContainerPauseUnpause tests pausing and unpausing containers.
func TestContainerPauseUnpause(t *testing.T) {
	client := setupClient(t)
	defer client.Close()
	ensureImage(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create and start a long-running container
	containerID, err := client.Containers().Create(ctx, &domain.ContainerConfig{
		Image: testImage,
		Cmd:   []string{"sleep", "30"},
		Name:  fmt.Sprintf("ofelia-pause-test-%d", time.Now().Unix()),
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer client.Containers().Remove(ctx, containerID, domain.RemoveOptions{Force: true})

	err = client.Containers().Start(ctx, containerID)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Pause container
	err = client.Containers().Pause(ctx, containerID)
	if err != nil {
		t.Fatalf("Pause failed: %v", err)
	}

	// Verify paused state
	container, err := client.Containers().Inspect(ctx, containerID)
	if err != nil {
		t.Fatalf("Inspect failed: %v", err)
	}
	if !container.State.Paused {
		t.Error("Container should be paused")
	}

	// Unpause container
	err = client.Containers().Unpause(ctx, containerID)
	if err != nil {
		t.Fatalf("Unpause failed: %v", err)
	}

	// Verify unpaused state
	container, err = client.Containers().Inspect(ctx, containerID)
	if err != nil {
		t.Fatalf("Inspect failed: %v", err)
	}
	if container.State.Paused {
		t.Error("Container should not be paused")
	}
}

// TestContainerRename tests renaming containers.
func TestContainerRename(t *testing.T) {
	client := setupClient(t)
	defer client.Close()
	ensureImage(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	oldName := fmt.Sprintf("ofelia-rename-old-%d", time.Now().Unix())
	newName := fmt.Sprintf("ofelia-rename-new-%d", time.Now().Unix())

	containerID, err := client.Containers().Create(ctx, &domain.ContainerConfig{
		Image: testImage,
		Cmd:   []string{"sleep", "1"},
		Name:  oldName,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer client.Containers().Remove(ctx, containerID, domain.RemoveOptions{Force: true})

	// Rename container
	err = client.Containers().Rename(ctx, containerID, newName)
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	// Verify new name
	container, err := client.Containers().Inspect(ctx, containerID)
	if err != nil {
		t.Fatalf("Inspect failed: %v", err)
	}
	// Docker adds a leading slash to container names
	if container.Name != "/"+newName {
		t.Errorf("Container name mismatch: got %s, want /%s", container.Name, newName)
	}
}
