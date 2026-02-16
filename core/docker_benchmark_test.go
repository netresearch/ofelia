//go:build integration

package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

func newBenchClient(b *testing.B) *client.Client {
	b.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	return cli
}

func createBenchContainer(b *testing.B, cli *client.Client, name string, cmd []string) string {
	b.Helper()
	ctx := context.Background()
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "alpine:latest",
		Cmd:   cmd,
	}, nil, nil, nil, name)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	return resp.ID
}

func removeBenchContainer(b *testing.B, cli *client.Client, id string) {
	b.Helper()
	_ = cli.ContainerRemove(context.Background(), id, container.RemoveOptions{Force: true})
}

// BenchmarkContainerCreate measures container creation performance.
func BenchmarkContainerCreate(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	b.ResetTimer()
	for i := range b.N {
		name := fmt.Sprintf("bench-create-%d-%d", time.Now().UnixNano(), i)
		resp, err := cli.ContainerCreate(ctx, &container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"true"},
		}, nil, nil, nil, name)
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}
		removeBenchContainer(b, cli, resp.ID)
	}
}

// BenchmarkContainerStartStop measures container start/stop cycle.
func BenchmarkContainerStartStop(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	name := fmt.Sprintf("bench-startstop-%d", time.Now().UnixNano())
	id := createBenchContainer(b, cli, name, []string{"sleep", "300"})
	defer removeBenchContainer(b, cli, id)

	timeout := 5

	b.ResetTimer()
	for range b.N {
		if err := cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
			b.Fatalf("Start failed: %v", err)
		}
		if err := cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}); err != nil {
			b.Fatalf("Stop failed: %v", err)
		}
	}
}

// BenchmarkContainerInspect measures container inspection performance.
func BenchmarkContainerInspect(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	name := fmt.Sprintf("bench-inspect-%d", time.Now().UnixNano())
	id := createBenchContainer(b, cli, name, []string{"true"})
	defer removeBenchContainer(b, cli, id)

	b.ResetTimer()
	for range b.N {
		if _, err := cli.ContainerInspect(ctx, id); err != nil {
			b.Fatalf("Inspect failed: %v", err)
		}
	}
}

// BenchmarkContainerList measures container listing performance.
func BenchmarkContainerList(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		if _, err := cli.ContainerList(ctx, container.ListOptions{All: true}); err != nil {
			b.Fatalf("List failed: %v", err)
		}
	}
}

// BenchmarkExecRun measures exec run performance.
func BenchmarkExecRun(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	name := fmt.Sprintf("bench-exec-%d", time.Now().UnixNano())
	id := createBenchContainer(b, cli, name, []string{"sleep", "300"})
	defer removeBenchContainer(b, cli, id)

	if err := cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	timeout := 5
	defer func() { _ = cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}) }()

	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	for range b.N {
		execResp, err := cli.ContainerExecCreate(ctx, id, container.ExecOptions{
			Cmd:          []string{"echo", "benchmark"},
			AttachStdout: true,
			AttachStderr: true,
		})
		if err != nil {
			b.Fatalf("CreateExec failed: %v", err)
		}

		attach, err := cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
		if err != nil {
			b.Fatalf("AttachExec failed: %v", err)
		}
		_, _ = io.Copy(&bytes.Buffer{}, attach.Reader)
		attach.Close()
	}
}

// BenchmarkExecRunParallel measures parallel exec performance.
func BenchmarkExecRunParallel(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	name := fmt.Sprintf("bench-exec-parallel-%d", time.Now().UnixNano())
	id := createBenchContainer(b, cli, name, []string{"sleep", "300"})
	defer removeBenchContainer(b, cli, id)

	if err := cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	timeout := 5
	defer func() { _ = cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}) }()

	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			execResp, err := cli.ContainerExecCreate(ctx, id, container.ExecOptions{
				Cmd:          []string{"echo", "benchmark"},
				AttachStdout: true,
				AttachStderr: true,
			})
			if err != nil {
				b.Errorf("CreateExec failed: %v", err)
				return
			}

			attach, err := cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
			if err != nil {
				b.Errorf("AttachExec failed: %v", err)
				return
			}
			_, _ = io.Copy(&bytes.Buffer{}, attach.Reader)
			attach.Close()
		}
	})
}

// BenchmarkImageExists measures image existence check performance.
func BenchmarkImageExists(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		_, _, err := cli.ImageInspectWithRaw(ctx, "alpine:latest")
		if err != nil {
			b.Fatalf("InspectImage failed: %v", err)
		}
	}
}

// BenchmarkImageList measures image listing performance.
func BenchmarkImageList(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		if _, err := cli.ImageList(ctx, image.ListOptions{All: true}); err != nil {
			b.Fatalf("ListImages failed: %v", err)
		}
	}
}

// BenchmarkSystemPing measures Docker ping performance.
func BenchmarkSystemPing(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		if _, err := cli.Ping(ctx); err != nil {
			b.Fatalf("Ping failed: %v", err)
		}
	}
}

// BenchmarkSystemInfo measures Docker info retrieval performance.
func BenchmarkSystemInfo(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		if _, err := cli.Info(ctx); err != nil {
			b.Fatalf("Info failed: %v", err)
		}
	}
}

// BenchmarkContainerFullLifecycle measures complete container lifecycle.
func BenchmarkContainerFullLifecycle(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	b.ResetTimer()
	for i := range b.N {
		name := fmt.Sprintf("bench-lifecycle-%d-%d", time.Now().UnixNano(), i)

		resp, err := cli.ContainerCreate(ctx, &container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"echo", "done"},
		}, nil, nil, nil, name)
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}

		if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			b.Fatalf("Start failed: %v", err)
		}

		statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
		select {
		case <-statusCh:
		case err := <-errCh:
			if err != nil {
				b.Fatalf("Wait failed: %v", err)
			}
		}

		if err := cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}); err != nil {
			b.Fatalf("Remove failed: %v", err)
		}
	}
}

// BenchmarkExecJobSimulation simulates a typical ExecJob workload.
func BenchmarkExecJobSimulation(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	name := fmt.Sprintf("bench-execjob-%d", time.Now().UnixNano())
	id := createBenchContainer(b, cli, name, []string{"sleep", "300"})
	defer removeBenchContainer(b, cli, id)

	if err := cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	timeout := 5
	defer func() { _ = cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}) }()

	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	for range b.N {
		if _, err := cli.ContainerInspect(ctx, id); err != nil {
			b.Fatalf("Inspect failed: %v", err)
		}

		execResp, err := cli.ContainerExecCreate(ctx, id, container.ExecOptions{
			Cmd:          []string{"sh", "-c", "echo 'job output'; echo 'error' >&2"},
			AttachStdout: true,
			AttachStderr: true,
		})
		if err != nil {
			b.Fatalf("CreateExec failed: %v", err)
		}

		attach, err := cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
		if err != nil {
			b.Fatalf("AttachExec failed: %v", err)
		}
		_, _ = io.Copy(&bytes.Buffer{}, attach.Reader)
		attach.Close()
	}
}

// BenchmarkRunJobSimulation simulates a typical RunJob workload.
func BenchmarkRunJobSimulation(b *testing.B) {
	cli := newBenchClient(b)
	ctx := context.Background()

	b.ResetTimer()
	for i := range b.N {
		name := fmt.Sprintf("bench-runjob-%d-%d", time.Now().UnixNano(), i)

		_, _, err := cli.ImageInspectWithRaw(ctx, "alpine:latest")
		if err != nil {
			b.Fatalf("Image check failed: %v", err)
		}

		resp, err := cli.ContainerCreate(ctx, &container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sh", "-c", "echo 'job output'"},
		}, nil, nil, nil, name)
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}

		if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			b.Fatalf("Start failed: %v", err)
		}

		statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
		select {
		case <-statusCh:
		case err := <-errCh:
			if err != nil {
				b.Fatalf("Wait failed: %v", err)
			}
		}

		if err := cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}); err != nil {
			b.Fatalf("Remove failed: %v", err)
		}
	}
}
