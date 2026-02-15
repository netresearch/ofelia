//go:build integration

package core

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

// BenchmarkContainerCreate measures container creation performance (go-dockerclient).
func BenchmarkContainerCreate(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("bench-create-%d-%d", time.Now().UnixNano(), i)
		container, err := client.CreateContainer(docker.CreateContainerOptions{
			Name: name,
			Config: &docker.Config{
				Image: "alpine:latest",
				Cmd:   []string{"true"},
			},
		})
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}
		// Cleanup
		_ = client.RemoveContainer(docker.RemoveContainerOptions{
			ID:    container.ID,
			Force: true,
		})
	}
}

// BenchmarkContainerStartStop measures container start/stop cycle (go-dockerclient).
func BenchmarkContainerStartStop(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	// Pre-create container
	name := fmt.Sprintf("bench-startstop-%d", time.Now().UnixNano())
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "300"},
		},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer client.RemoveContainer(docker.RemoveContainerOptions{
		ID:    container.ID,
		Force: true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := client.StartContainer(container.ID, nil); err != nil {
			b.Fatalf("Start failed: %v", err)
		}
		if err := client.StopContainer(container.ID, 5); err != nil {
			b.Fatalf("Stop failed: %v", err)
		}
	}
}

// BenchmarkContainerInspect measures container inspection performance (go-dockerclient).
func BenchmarkContainerInspect(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	// Pre-create container
	name := fmt.Sprintf("bench-inspect-%d", time.Now().UnixNano())
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"true"},
		},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer client.RemoveContainer(docker.RemoveContainerOptions{
		ID:    container.ID,
		Force: true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.InspectContainer(container.ID)
		if err != nil {
			b.Fatalf("Inspect failed: %v", err)
		}
	}
}

// BenchmarkContainerList measures container listing performance (go-dockerclient).
func BenchmarkContainerList(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.ListContainers(docker.ListContainersOptions{All: true})
		if err != nil {
			b.Fatalf("List failed: %v", err)
		}
	}
}

// BenchmarkExecRun measures exec run performance (go-dockerclient).
func BenchmarkExecRun(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	// Pre-create and start container
	name := fmt.Sprintf("bench-exec-%d", time.Now().UnixNano())
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "300"},
		},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer client.RemoveContainer(docker.RemoveContainerOptions{
		ID:    container.ID,
		Force: true,
	})

	if err := client.StartContainer(container.ID, nil); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer client.StopContainer(container.ID, 5)

	// Wait for container to be ready
	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec, err := client.CreateExec(docker.CreateExecOptions{
			Container:    container.ID,
			Cmd:          []string{"echo", "benchmark"},
			AttachStdout: true,
			AttachStderr: true,
		})
		if err != nil {
			b.Fatalf("CreateExec failed: %v", err)
		}

		var stdout, stderr bytes.Buffer
		err = client.StartExec(exec.ID, docker.StartExecOptions{
			OutputStream: &stdout,
			ErrorStream:  &stderr,
		})
		if err != nil {
			b.Fatalf("StartExec failed: %v", err)
		}
	}
}

// BenchmarkExecRunParallel measures parallel exec performance (go-dockerclient).
func BenchmarkExecRunParallel(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	// Pre-create and start container
	name := fmt.Sprintf("bench-exec-parallel-%d", time.Now().UnixNano())
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "300"},
		},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer client.RemoveContainer(docker.RemoveContainerOptions{
		ID:    container.ID,
		Force: true,
	})

	if err := client.StartContainer(container.ID, nil); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer client.StopContainer(container.ID, 5)

	// Wait for container to be ready
	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			exec, err := client.CreateExec(docker.CreateExecOptions{
				Container:    container.ID,
				Cmd:          []string{"echo", "benchmark"},
				AttachStdout: true,
				AttachStderr: true,
			})
			if err != nil {
				b.Errorf("CreateExec failed: %v", err)
				return
			}

			var stdout, stderr bytes.Buffer
			err = client.StartExec(exec.ID, docker.StartExecOptions{
				OutputStream: &stdout,
				ErrorStream:  &stderr,
			})
			if err != nil {
				b.Errorf("StartExec failed: %v", err)
				return
			}
		}
	})
}

// BenchmarkImageExists measures image existence check performance (go-dockerclient).
func BenchmarkImageExists(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.InspectImage("alpine:latest")
		if err != nil && err != docker.ErrNoSuchImage {
			b.Fatalf("InspectImage failed: %v", err)
		}
	}
}

// BenchmarkImageList measures image listing performance (go-dockerclient).
func BenchmarkImageList(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.ListImages(docker.ListImagesOptions{All: true})
		if err != nil {
			b.Fatalf("ListImages failed: %v", err)
		}
	}
}

// BenchmarkSystemPing measures Docker ping performance (go-dockerclient).
func BenchmarkSystemPing(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := client.Ping(); err != nil {
			b.Fatalf("Ping failed: %v", err)
		}
	}
}

// BenchmarkSystemInfo measures Docker info retrieval performance (go-dockerclient).
func BenchmarkSystemInfo(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.Info()
		if err != nil {
			b.Fatalf("Info failed: %v", err)
		}
	}
}

// BenchmarkContainerFullLifecycle measures complete container lifecycle (go-dockerclient).
func BenchmarkContainerFullLifecycle(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("bench-lifecycle-%d-%d", time.Now().UnixNano(), i)

		// Create
		container, err := client.CreateContainer(docker.CreateContainerOptions{
			Name: name,
			Config: &docker.Config{
				Image: "alpine:latest",
				Cmd:   []string{"echo", "done"},
			},
		})
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}

		// Start
		if err := client.StartContainer(container.ID, nil); err != nil {
			b.Fatalf("Start failed: %v", err)
		}

		// Wait
		_, _ = client.WaitContainerWithContext(container.ID, ctx)

		// Remove
		if err := client.RemoveContainer(docker.RemoveContainerOptions{
			ID:    container.ID,
			Force: true,
		}); err != nil {
			b.Fatalf("Remove failed: %v", err)
		}
	}
}

// BenchmarkExecJobSimulation simulates a typical ExecJob workload (go-dockerclient).
func BenchmarkExecJobSimulation(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	// Pre-create and start container (simulating target container)
	name := fmt.Sprintf("bench-execjob-%d", time.Now().UnixNano())
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "300"},
		},
	})
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer client.RemoveContainer(docker.RemoveContainerOptions{
		ID:    container.ID,
		Force: true,
	})

	if err := client.StartContainer(container.ID, nil); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer client.StopContainer(container.ID, 5)

	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate ExecJob: inspect + exec + capture output
		_, err := client.InspectContainer(container.ID)
		if err != nil {
			b.Fatalf("Inspect failed: %v", err)
		}

		exec, err := client.CreateExec(docker.CreateExecOptions{
			Container:    container.ID,
			Cmd:          []string{"sh", "-c", "echo 'job output'; echo 'error' >&2"},
			AttachStdout: true,
			AttachStderr: true,
		})
		if err != nil {
			b.Fatalf("CreateExec failed: %v", err)
		}

		var stdout, stderr bytes.Buffer
		err = client.StartExec(exec.ID, docker.StartExecOptions{
			OutputStream: &stdout,
			ErrorStream:  &stderr,
		})
		if err != nil {
			b.Fatalf("StartExec failed: %v", err)
		}
	}
}

// BenchmarkRunJobSimulation simulates a typical RunJob workload (go-dockerclient).
func BenchmarkRunJobSimulation(b *testing.B) {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate RunJob: check image + create + start + wait + remove
		name := fmt.Sprintf("bench-runjob-%d-%d", time.Now().UnixNano(), i)

		// Check image exists
		_, err := client.InspectImage("alpine:latest")
		if err != nil && err != docker.ErrNoSuchImage {
			b.Fatalf("Image check failed: %v", err)
		}

		// Create container
		container, err := client.CreateContainer(docker.CreateContainerOptions{
			Name: name,
			Config: &docker.Config{
				Image: "alpine:latest",
				Cmd:   []string{"sh", "-c", "echo 'job output'"},
			},
		})
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}

		// Start
		if err := client.StartContainer(container.ID, nil); err != nil {
			client.RemoveContainer(docker.RemoveContainerOptions{
				ID:    container.ID,
				Force: true,
			})
			b.Fatalf("Start failed: %v", err)
		}

		// Wait
		_, _ = client.WaitContainerWithContext(container.ID, ctx)

		// Remove
		if err := client.RemoveContainer(docker.RemoveContainerOptions{
			ID:    container.ID,
			Force: true,
		}); err != nil {
			b.Fatalf("Remove failed: %v", err)
		}
	}
}
