//go:build integration
// +build integration

package docker_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	dockeradapter "github.com/netresearch/ofelia/core/adapters/docker"
	"github.com/netresearch/ofelia/core/domain"
)

// BenchmarkContainerCreate measures container creation performance.
func BenchmarkContainerCreate(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	containers := client.Containers()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("bench-create-%d-%d", time.Now().UnixNano(), i)
		id, err := containers.Create(ctx, &domain.ContainerConfig{
			Name:  name,
			Image: "alpine:latest",
			Cmd:   []string{"true"},
		})
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}
		// Cleanup
		_ = containers.Remove(ctx, id, domain.RemoveOptions{Force: true})
	}
}

// BenchmarkContainerStartStop measures container start/stop cycle.
func BenchmarkContainerStartStop(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	containers := client.Containers()

	// Pre-create container
	name := fmt.Sprintf("bench-startstop-%d", time.Now().UnixNano())
	id, err := containers.Create(ctx, &domain.ContainerConfig{
		Name:  name,
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "300"},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer containers.Remove(ctx, id, domain.RemoveOptions{Force: true})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := containers.Start(ctx, id); err != nil {
			b.Fatalf("Start failed: %v", err)
		}
		timeout := 5 * time.Second
		if err := containers.Stop(ctx, id, &timeout); err != nil {
			b.Fatalf("Stop failed: %v", err)
		}
	}
}

// BenchmarkContainerInspect measures container inspection performance.
func BenchmarkContainerInspect(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	containers := client.Containers()

	// Pre-create container
	name := fmt.Sprintf("bench-inspect-%d", time.Now().UnixNano())
	id, err := containers.Create(ctx, &domain.ContainerConfig{
		Name:  name,
		Image: "alpine:latest",
		Cmd:   []string{"true"},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer containers.Remove(ctx, id, domain.RemoveOptions{Force: true})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := containers.Inspect(ctx, id)
		if err != nil {
			b.Fatalf("Inspect failed: %v", err)
		}
	}
}

// BenchmarkContainerList measures container listing performance.
func BenchmarkContainerList(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	containers := client.Containers()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := containers.List(ctx, domain.ListOptions{All: true})
		if err != nil {
			b.Fatalf("List failed: %v", err)
		}
	}
}

// BenchmarkExecRun measures exec run performance (the main job operation).
func BenchmarkExecRun(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	containers := client.Containers()
	exec := client.Exec()

	// Pre-create and start container
	name := fmt.Sprintf("bench-exec-%d", time.Now().UnixNano())
	id, err := containers.Create(ctx, &domain.ContainerConfig{
		Name:  name,
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "300"},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer containers.Remove(ctx, id, domain.RemoveOptions{Force: true})

	if err := containers.Start(ctx, id); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer func() {
		timeout := 5 * time.Second
		containers.Stop(ctx, id, &timeout)
	}()

	// Wait for container to be ready
	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exitCode, err := exec.Run(ctx, id, &domain.ExecConfig{
			Cmd:          []string{"echo", "benchmark"},
			AttachStdout: true,
			AttachStderr: true,
		}, io.Discard, io.Discard)
		if err != nil {
			b.Fatalf("Exec.Run failed: %v", err)
		}
		if exitCode != 0 {
			b.Fatalf("Exec returned non-zero: %d", exitCode)
		}
	}
}

// BenchmarkExecRunParallel measures parallel exec performance.
func BenchmarkExecRunParallel(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	containers := client.Containers()
	exec := client.Exec()

	// Pre-create and start container
	name := fmt.Sprintf("bench-exec-parallel-%d", time.Now().UnixNano())
	id, err := containers.Create(ctx, &domain.ContainerConfig{
		Name:  name,
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "300"},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer containers.Remove(ctx, id, domain.RemoveOptions{Force: true})

	if err := containers.Start(ctx, id); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer func() {
		timeout := 5 * time.Second
		containers.Stop(ctx, id, &timeout)
	}()

	// Wait for container to be ready
	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			exitCode, err := exec.Run(ctx, id, &domain.ExecConfig{
				Cmd:          []string{"echo", "benchmark"},
				AttachStdout: true,
				AttachStderr: true,
			}, io.Discard, io.Discard)
			if err != nil {
				b.Errorf("Exec.Run failed: %v", err)
				return
			}
			if exitCode != 0 {
				b.Errorf("Exec returned non-zero: %d", exitCode)
				return
			}
		}
	})
}

// BenchmarkImageExists measures image existence check performance.
func BenchmarkImageExists(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	images := client.Images()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := images.Exists(ctx, "alpine:latest")
		if err != nil {
			b.Fatalf("Exists failed: %v", err)
		}
	}
}

// BenchmarkImageList measures image listing performance.
func BenchmarkImageList(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	images := client.Images()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := images.List(ctx, domain.ImageListOptions{All: true})
		if err != nil {
			b.Fatalf("List failed: %v", err)
		}
	}
}

// BenchmarkSystemPing measures Docker ping performance.
func BenchmarkSystemPing(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	system := client.System()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := system.Ping(ctx)
		if err != nil {
			b.Fatalf("Ping failed: %v", err)
		}
	}
}

// BenchmarkSystemInfo measures Docker info retrieval performance.
func BenchmarkSystemInfo(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	system := client.System()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := system.Info(ctx)
		if err != nil {
			b.Fatalf("Info failed: %v", err)
		}
	}
}

// BenchmarkContainerFullLifecycle measures complete container lifecycle.
func BenchmarkContainerFullLifecycle(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	containers := client.Containers()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("bench-lifecycle-%d-%d", time.Now().UnixNano(), i)

		// Create
		id, err := containers.Create(ctx, &domain.ContainerConfig{
			Name:  name,
			Image: "alpine:latest",
			Cmd:   []string{"echo", "done"},
		})
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}

		// Start
		if err := containers.Start(ctx, id); err != nil {
			b.Fatalf("Start failed: %v", err)
		}

		// Wait
		_, _ = containers.Wait(ctx, id)

		// Remove
		if err := containers.Remove(ctx, id, domain.RemoveOptions{Force: true}); err != nil {
			b.Fatalf("Remove failed: %v", err)
		}
	}
}

// BenchmarkExecJobSimulation simulates a typical ExecJob workload.
func BenchmarkExecJobSimulation(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	containers := client.Containers()
	exec := client.Exec()

	// Pre-create and start container (simulating target container)
	name := fmt.Sprintf("bench-execjob-%d", time.Now().UnixNano())
	id, err := containers.Create(ctx, &domain.ContainerConfig{
		Name:  name,
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "300"},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer containers.Remove(ctx, id, domain.RemoveOptions{Force: true})

	if err := containers.Start(ctx, id); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer func() {
		timeout := 5 * time.Second
		containers.Stop(ctx, id, &timeout)
	}()

	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate ExecJob: inspect + exec + capture output
		_, err := containers.Inspect(ctx, id)
		if err != nil {
			b.Fatalf("Inspect failed: %v", err)
		}

		var stdout, stderr strings.Builder
		exitCode, err := exec.Run(ctx, id, &domain.ExecConfig{
			Cmd:          []string{"sh", "-c", "echo 'job output'; echo 'error' >&2"},
			AttachStdout: true,
			AttachStderr: true,
		}, &stdout, &stderr)
		if err != nil {
			b.Fatalf("Exec.Run failed: %v", err)
		}
		if exitCode != 0 {
			b.Fatalf("Exec returned non-zero: %d", exitCode)
		}
	}
}

// BenchmarkRunJobSimulation simulates a typical RunJob workload.
func BenchmarkRunJobSimulation(b *testing.B) {
	client, err := dockeradapter.NewClient()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	containers := client.Containers()
	images := client.Images()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate RunJob: check image + create + start + wait + logs + remove
		name := fmt.Sprintf("bench-runjob-%d-%d", time.Now().UnixNano(), i)

		// Check image exists
		_, err := images.Exists(ctx, "alpine:latest")
		if err != nil {
			b.Fatalf("Image check failed: %v", err)
		}

		// Create container
		id, err := containers.Create(ctx, &domain.ContainerConfig{
			Name:  name,
			Image: "alpine:latest",
			Cmd:   []string{"sh", "-c", "echo 'job output'"},
		})
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}

		// Start
		if err := containers.Start(ctx, id); err != nil {
			containers.Remove(ctx, id, domain.RemoveOptions{Force: true})
			b.Fatalf("Start failed: %v", err)
		}

		// Wait
		_, _ = containers.Wait(ctx, id)

		// Get logs (simulated - just inspect)
		_, _ = containers.Inspect(ctx, id)

		// Remove
		if err := containers.Remove(ctx, id, domain.RemoveOptions{Force: true}); err != nil {
			b.Fatalf("Remove failed: %v", err)
		}
	}
}
