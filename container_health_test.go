package main

import (
	"context"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestContainerHealth(t *testing.T) {
	ctx := context.Background()

	req := tc.ContainerRequest{
		FromDockerfile: tc.FromDockerfile{
			Context:    ".",
			Dockerfile: "Dockerfile",
		},
		Entrypoint: []string{"sleep", "60"},
		Healthcheck: &dockercontainer.HealthConfig{
			Test:     []string{"CMD-SHELL", "exit 0"},
			Interval: time.Second,
			Timeout:  time.Second,
			Retries:  5,
		},
		WaitingFor: wait.ForHealthcheck(),
	}

	container, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start container: %v", err)
	}
	defer container.Terminate(ctx)

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		state, err := container.State(ctx)
		if err != nil {
			t.Fatalf("error getting container state: %v", err)
		}
		if state.Health != nil && state.Health.Status == "healthy" {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("container did not become healthy")
}
